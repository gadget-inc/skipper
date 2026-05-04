// Package krane wraps the `krane render` and `krane deploy` shellouts
// the dev tooling needs to render and apply ERB-templated Kubernetes
// manifests. Render and Deploy operate on a deploy/<namespace>
// directory; RenderManifest exposes a lower-level single-file render
// for callers (kube-lint, the krane unit tests) that just want the
// rendered YAML without filesystem side effects.
package krane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"sort"

	"github.com/gadget-inc/skipper/internal/dev/exec"
)

// RenderManifest invokes `krane render --filenames=<templatePath>
// --bindings=<JSON>` and returns the rendered YAML written to stdout.
// Krane's progress chatter on stderr is buffered and only printed on
// failure so callers (and tests) get a clean stdout pipe.
func RenderManifest(ctx context.Context, templatePath string, bindings map[string]any) ([]byte, error) {
	bindingsJSON, err := marshalBindings(bindings)
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	err = exec.Run(ctx, "krane", []string{
		"render",
		"--filenames=" + templatePath,
		"--bindings=" + bindingsJSON,
	}, exec.Stdout(&stdout), exec.Stderr(&stderr))
	if err != nil {
		if stderr.Len() > 0 {
			os.Stderr.Write(stderr.Bytes())
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

// Render renders deploy/<namespace> through krane, copies any
// secrets.ejson from the deploy dir into the render dir, writes the
// resulting YAML to tmp/krane/<namespace>/rendered.yaml, and returns
// the absolute path of the render directory. The deploy_dir,
// render_dir, and workspace_dir bindings are scaffolded; caller-
// supplied keys win on conflict.
func Render(ctx context.Context, namespace string, bindings map[string]any) (string, error) {
	root, err := workspaceDir()
	if err != nil {
		return "", err
	}
	deployDir := filepath.Join(root, "deploy", namespace)
	renderDir := filepath.Join(root, "tmp", "krane", namespace)

	if err := EmptyDir(renderDir); err != nil {
		return "", err
	}

	secrets := filepath.Join(deployDir, "secrets.ejson")
	if _, err := os.Stat(secrets); err == nil {
		if err := copyFile(secrets, filepath.Join(renderDir, "secrets.ejson")); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	scaffolded := map[string]any{
		"deploy_dir":    deployDir,
		"render_dir":    renderDir,
		"workspace_dir": root,
	}
	maps.Copy(scaffolded, bindings)

	manifest, err := RenderManifest(ctx, deployDir, scaffolded)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(renderDir, "rendered.yaml"), manifest, 0o644); err != nil {
		return "", err
	}
	return renderDir, nil
}

// Deploy renders the namespace, ensures it exists in the cluster, and
// applies the rendered manifests via `krane deploy`. The kubectl
// context defaults to "orbstack" when SKIPPER_KUBECTL_CONTEXT is unset.
func Deploy(ctx context.Context, namespace string, bindings map[string]any) error {
	if os.Getenv("SKIPPER_KUBECTL_CONTEXT") == "" {
		os.Setenv("SKIPPER_KUBECTL_CONTEXT", "orbstack")
	}
	kubectx := os.Getenv("SKIPPER_KUBECTL_CONTEXT")

	renderDir, err := Render(ctx, namespace, bindings)
	if err != nil {
		return err
	}

	// Best-effort namespace creation; ignore "already exists" failures.
	_ = exec.RunQuiet(ctx, "kubectl",
		[]string{"--context=" + kubectx, "create", "namespace", namespace},
		exec.Stderr(io.Discard),
	)

	files, err := ManifestFiles(renderDir)
	if err != nil {
		return err
	}
	args := []string{"deploy", namespace, kubectx}
	for _, f := range files {
		args = append(args, "-f", f)
	}
	return exec.Run(ctx, "krane", args)
}

// marshalBindings JSON-encodes bindings with deterministic key ordering
// (encoding/json sorts map keys alphabetically) so test goldens stay
// stable across runs.
func marshalBindings(bindings map[string]any) (string, error) {
	if bindings == nil {
		bindings = map[string]any{}
	}
	buf, err := json.Marshal(bindings)
	if err != nil {
		return "", fmt.Errorf("marshal krane bindings: %w", err)
	}
	return string(buf), nil
}

func workspaceDir() (string, error) {
	if d := os.Getenv("WORKSPACE_DIR"); d != "" {
		return d, nil
	}
	return os.Getwd()
}

// EmptyDir removes dir if it exists and recreates it empty. Used to
// clear a render directory before re-rendering bindings into it.
func EmptyDir(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.MkdirAll(dir, 0o755)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

// ManifestFiles returns the absolute paths of every regular file
// directly in renderDir, sorted lexicographically.
func ManifestFiles(renderDir string) ([]string, error) {
	entries, err := os.ReadDir(renderDir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		files = append(files, filepath.Join(renderDir, e.Name()))
	}
	sort.Strings(files)
	return files, nil
}
