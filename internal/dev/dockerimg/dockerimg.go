// Package dockerimg wraps the docker shellouts that scripts/_utils.ts
// exposed for the dev tooling: querying the local Docker daemon for the
// OS / architecture it builds for, and computing the project's
// per-commit image tag.
package dockerimg

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Os returns the Docker server's OS string ("linux", "darwin", ...).
func Os(ctx context.Context) (string, error) {
	return dockerVersion(ctx, "{{.Server.Os}}")
}

// Arch returns the Docker server's architecture string ("arm64", "amd64", ...).
func Arch(ctx context.Context) (string, error) {
	return dockerVersion(ctx, "{{.Server.Arch}}")
}

// Platform returns "<os>/<arch>", suitable for docker buildx --platform.
func Platform(ctx context.Context) (string, error) {
	osVal, err := Os(ctx)
	if err != nil {
		return "", err
	}
	arch, err := Arch(ctx)
	if err != nil {
		return "", err
	}
	return osVal + "/" + arch, nil
}

// Tag returns the per-commit image tag, "sha-<short-git-sha>-<arch>".
func Tag(ctx context.Context) (string, error) {
	sha, err := gitSha(ctx)
	if err != nil {
		return "", err
	}
	arch, err := Arch(ctx)
	if err != nil {
		return "", err
	}
	return "sha-" + sha + "-" + arch, nil
}

// ImageID returns the digest of the local image for the given name at
// the current tag. Empty string with an error when no such image exists.
func ImageID(ctx context.Context, name string) (string, error) {
	tag, err := Tag(ctx)
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, "docker", "images", "--no-trunc", "--quiet", name+":"+tag).Output()
	if err != nil {
		return "", fmt.Errorf("docker images %s:%s: %w", name, tag, err)
	}
	digest := strings.TrimSpace(string(out))
	if digest == "" {
		return "", fmt.Errorf("image digest for %s:%s not found", name, tag)
	}
	return digest, nil
}

func dockerVersion(ctx context.Context, format string) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", "version", "--format", format).Output()
	if err != nil {
		return "", fmt.Errorf("docker version --format %q: %w", format, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func gitSha(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --short HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
