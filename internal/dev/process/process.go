// Package process orchestrates long-running child processes for
// `dev up`. A Group runs N child processes with shared cancellation,
// prefixed log streams, optional restart-on-signal, and a SIGTERM →
// SIGKILL teardown sequence.
package process

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
)

// SpawnFunc returns a fully configured *exec.Cmd ready to Start.
// Implementations should set Cmd.Stdout / Stderr to the writers the
// orchestrator passes in via the configure helper.
type SpawnFunc func(ctx context.Context, stdout, stderr io.Writer) *osexec.Cmd

// Group orchestrates a set of long-running child processes that share
// cancellation. Use New, then Spawn or SpawnWithRestart, then Wait.
type Group struct {
	ctx       context.Context
	cancel    context.CancelFunc
	eg        *errgroup.Group
	out       io.Writer
	err       io.Writer
	gracefulT time.Duration
}

// New returns a Group that uses os.Stdout / os.Stderr for prefixed
// child output and grants children 5s to exit after SIGTERM before
// SIGKILL. The group's lifetime ends when ctx is cancelled or any
// non-restart process exits with an error.
func New(ctx context.Context) *Group {
	gctx, cancel := context.WithCancel(ctx)
	eg, egctx := errgroup.WithContext(gctx)
	return &Group{
		ctx:       egctx,
		cancel:    cancel,
		eg:        eg,
		out:       os.Stdout,
		err:       os.Stderr,
		gracefulT: 5 * time.Second,
	}
}

// Spawn starts a process under the given name. Its stdout / stderr are
// line-prefixed with `[name]` and forwarded to the group's writers. If
// the process exits with a non-nil error and ctx is not yet cancelled,
// the entire group is cancelled.
func (g *Group) Spawn(name string, spawn SpawnFunc) {
	g.eg.Go(func() error {
		stdout, stderr := g.prefixWriters(name)
		cmd := spawn(g.ctx, stdout, stderr)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("%s: start: %w", name, err)
		}

		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		select {
		case <-g.ctx.Done():
			cancelKill := killProcess(cmd, g.gracefulT)
			<-done
			cancelKill()
			return nil
		case err := <-done:
			if g.ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			return nil
		}
	})
}

// RunFunc is an in-process task body. Implementations should respect
// ctx for cancellation and return when it is cancelled.
type RunFunc func(ctx context.Context, stdout, stderr io.Writer) error

// Run schedules an in-process task with the same prefixed-output and
// shared-cancellation semantics as Spawn. Use it for tasks that don't
// fork a child process (e.g. an HTTP server hosted inside the dev
// binary). A non-nil return value cancels the group.
func (g *Group) Run(name string, run RunFunc) {
	g.eg.Go(func() error {
		stdout, stderr := g.prefixWriters(name)
		err := run(g.ctx, stdout, stderr)
		if g.ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	})
}

// SpawnWithRestart behaves like Spawn but restarts the process each
// time restart receives a value, until ctx is cancelled. A clean exit
// of the underlying process (exit 0 with no error) does NOT cancel the
// group; the process simply waits for the next restart signal.
func (g *Group) SpawnWithRestart(name string, spawn SpawnFunc, restart <-chan struct{}) {
	g.eg.Go(func() error {
		stdout, stderr := g.prefixWriters(name)

		startOne := func() (*osexec.Cmd, chan error, error) {
			cmd := spawn(g.ctx, stdout, stderr)
			if err := cmd.Start(); err != nil {
				return nil, nil, fmt.Errorf("%s: start: %w", name, err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			return cmd, done, nil
		}

		cmd, done, err := startOne()
		if err != nil {
			return err
		}

		for {
			select {
			case <-g.ctx.Done():
				cancelKill := killProcess(cmd, g.gracefulT)
				<-done
				cancelKill()
				return nil
			case <-restart:
				fmt.Fprintf(stdout, "Restarting %s...\n", name)
				cancelKill := killProcess(cmd, g.gracefulT)
				<-done
				cancelKill()
				cmd, done, err = startOne()
				if err != nil {
					return err
				}
			case waitErr := <-done:
				if g.ctx.Err() != nil {
					return nil
				}
				if waitErr != nil {
					fmt.Fprintf(stderr, "%s exited: %v (waiting for restart)\n", name, waitErr)
				}
				// Wait for either restart or cancellation.
				select {
				case <-g.ctx.Done():
					return nil
				case <-restart:
					fmt.Fprintf(stdout, "Restarting %s...\n", name)
					cmd, done, err = startOne()
					if err != nil {
						return err
					}
				}
			}
		}
	})
}

// Wait blocks until all spawned goroutines return. Cancelling the
// parent context (or returning an error from any Spawn) propagates to
// every child.
func (g *Group) Wait() error {
	defer g.cancel()
	return g.eg.Wait()
}

// Cancel triggers shutdown of every process in the group.
func (g *Group) Cancel() { g.cancel() }

func (g *Group) prefixWriters(name string) (io.Writer, io.Writer) {
	prefix := "[" + name + "] "
	return newPrefixWriter(g.out, prefix), newPrefixWriter(g.err, prefix)
}

// killProcess sends SIGTERM and schedules a SIGKILL after grace if the
// caller's wait hasn't completed. When the child was started in its
// own process group (SysProcAttr.Setpgid), the signal is delivered to
// the entire group so children of the group leader (e.g. the binary
// spawned by `go run`) also exit.
//
// killProcess does not Wait on the process — the caller already has a
// goroutine doing that and a second Waiter would race for the exit
// status. The returned cancel func should be called once the caller's
// own wait returns, to disarm the SIGKILL timer.
func killProcess(cmd *osexec.Cmd, grace time.Duration) func() {
	if cmd == nil || cmd.Process == nil {
		return func() {}
	}
	pid := cmd.Process.Pid
	pgid, err := syscall.Getpgid(pid)
	useGroup := err == nil && pgid == pid
	if useGroup {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
	timer := time.AfterFunc(grace, func() {
		if useGroup {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			_ = cmd.Process.Kill()
		}
	})
	return func() { timer.Stop() }
}

// prefixWriter prepends a fixed string to every line written through
// it. Partial-line writes accumulate in a small buffer until a newline
// is seen, so prefixes never appear mid-line. Output is serialized so
// concurrent writers don't interleave inside a single line.
type prefixWriter struct {
	mu     sync.Mutex
	out    io.Writer
	prefix string
	buf    []byte
}

func newPrefixWriter(out io.Writer, prefix string) *prefixWriter {
	return &prefixWriter{out: out, prefix: prefix}
}

func (w *prefixWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		if _, err := fmt.Fprintf(w.out, "%s%s\n", w.prefix, w.buf[:i]); err != nil {
			return len(p), err
		}
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}
