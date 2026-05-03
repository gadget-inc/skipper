// Package exec wraps os/exec.CommandContext with project conventions:
// commands inherit the parent's environment, default to running from
// the repo root (resolved via WORKSPACE_DIR), and stream stdout /
// stderr to the caller. RunOut captures stdout for use as a string;
// RunQuiet suppresses stdout when only the exit status matters.
package exec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"strings"
)

// Option configures a single command invocation.
type Option func(*config)

type config struct {
	dir    string
	env    []string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

// Dir overrides the working directory. Defaults to the repository root
// (the value of WORKSPACE_DIR if set, otherwise the current directory).
func Dir(d string) Option { return func(c *config) { c.dir = d } }

// Env appends entries (KEY=VALUE) on top of the inherited environment.
func Env(entries ...string) Option { return func(c *config) { c.env = append(c.env, entries...) } }

// Stdin attaches a stdin reader.
func Stdin(r io.Reader) Option { return func(c *config) { c.stdin = r } }

// Stderr redirects stderr.
func Stderr(w io.Writer) Option { return func(c *config) { c.stderr = w } }

// Stdout redirects stdout.
func Stdout(w io.Writer) Option { return func(c *config) { c.stdout = w } }

// Run executes the command, streaming stdout / stderr to the caller's
// process, and returns its exit error.
func Run(ctx context.Context, name string, args []string, opts ...Option) error {
	cfg := apply(opts, os.Stdout, os.Stderr)
	cmd := command(ctx, name, args, cfg)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// RunOut captures stdout and returns it as a trimmed string. Stderr
// streams to the caller.
func RunOut(ctx context.Context, name string, args []string, opts ...Option) (string, error) {
	var buf bytes.Buffer
	cfg := apply(opts, &buf, os.Stderr)
	cmd := command(ctx, name, args, cfg)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(buf.String()), nil
}

// RunQuiet runs the command with stdout discarded; stderr still streams
// so failures remain diagnosable.
func RunQuiet(ctx context.Context, name string, args []string, opts ...Option) error {
	cfg := apply(opts, io.Discard, os.Stderr)
	cmd := command(ctx, name, args, cfg)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// RunAll runs each step in order. A non-zero exit from one step does not
// skip the rest -- developers want to see findings from every formatter
// and linter, not just the first. The first non-nil error is returned to
// set the process exit code.
func RunAll(ctx context.Context, steps [][]string) error {
	var firstErr error
	for _, step := range steps {
		if err := Run(ctx, step[0], step[1:]); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func apply(opts []Option, defStdout, defStderr io.Writer) *config {
	cfg := &config{stdout: defStdout, stderr: defStderr}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.dir == "" {
		cfg.dir = workspaceDir()
	}
	return cfg
}

func command(ctx context.Context, name string, args []string, cfg *config) *osexec.Cmd {
	cmd := osexec.CommandContext(ctx, name, args...)
	cmd.Dir = cfg.dir
	if len(cfg.env) > 0 {
		cmd.Env = append(os.Environ(), cfg.env...)
	}
	cmd.Stdin = cfg.stdin
	cmd.Stdout = cfg.stdout
	cmd.Stderr = cfg.stderr
	return cmd
}

// workspaceDir resolves the project's repo root. The dev shell sets
// WORKSPACE_DIR via direnv; outside of that we fall back to the current
// directory, which matches Go's default and is usually right when the
// caller invokes `dev` from anywhere inside the worktree.
func workspaceDir() string {
	if d := os.Getenv("WORKSPACE_DIR"); d != "" {
		return d
	}
	return ""
}
