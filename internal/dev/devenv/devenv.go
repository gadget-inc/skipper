// Package devenv holds environment-shaped helpers shared by the dev CLI:
// CI-detection, repo-root resolution, the --only / "skipper" alias
// expander, the kind-load image step, and the on-disk PASETO keypair
// management used by both `dev up` and `dev deploy`.
package devenv

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gadget-inc/skipper/internal/dev/exec"
)

// IsCI reports whether the process is running in a CI environment, based
// on the standard CI env var.
func IsCI() bool {
	v := os.Getenv("CI")
	return v == "1" || v == "true"
}

// RepoRoot returns the project repository root. The dev shell sets
// WORKSPACE_DIR via direnv; outside that we fall back to the current
// working directory.
func RepoRoot() string {
	if d := os.Getenv("WORKSPACE_DIR"); d != "" {
		return d
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// ExpandSkipperAlias rewrites a comma-separated --only string into a set
// of components, promoting "skipper" into "controller,router".
func ExpandSkipperAlias(only string) map[string]bool {
	out := map[string]bool{}
	for token := range strings.SplitSeq(only, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if token == "skipper" {
			out["controller"] = true
			out["router"] = true
			continue
		}
		out[token] = true
	}
	return out
}

// KindLoadImage loads an image into the local kind cluster. The build
// guard short-circuits to a no-op when the caller did not build the
// image this run, so callers can pass the same flag they used to gate
// the docker build.
func KindLoadImage(ctx context.Context, name, registry, tag string, build bool) error {
	if !build {
		return nil
	}
	full := name + ":" + tag
	if registry != "" {
		full = registry + "/" + full
	}
	return exec.Run(ctx, "kind", []string{"load", "docker-image", full})
}

// PasetoKeypairExists reports whether tmp/paseto/{private,public}.pem
// both exist on disk under the repo root.
func PasetoKeypairExists() bool {
	root := RepoRoot()
	_, e1 := os.Stat(filepath.Join(root, "tmp", "paseto", "private.pem"))
	_, e2 := os.Stat(filepath.Join(root, "tmp", "paseto", "public.pem"))
	return e1 == nil && e2 == nil
}

// GeneratePasetoKeypairFiles regenerates an ed25519 keypair under
// tmp/paseto/, replacing any existing files. Prints the new file
// listing on success.
func GeneratePasetoKeypairFiles() error {
	root := RepoRoot()
	dir := filepath.Join(root, "tmp", "paseto")
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate ed25519 keypair: %w", err)
	}

	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return fmt.Errorf("encode private key as PKCS8: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	if err := os.WriteFile(filepath.Join(dir, "private.pem"), privPEM, 0o600); err != nil {
		return err
	}

	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return fmt.Errorf("encode public key as SPKI: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	if err := os.WriteFile(filepath.Join(dir, "public.pem"), pubPEM, 0o644); err != nil {
		return err
	}

	return listDir(dir)
}

func listDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			return err
		}
		fmt.Printf("%s\t%d\t%s\n", info.Mode(), info.Size(), filepath.Join(dir, e.Name()))
	}
	return nil
}
