package main

import (
	"context"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"syscall"

	"github.com/gadget-inc/skipper/internal/dev/process"
	"github.com/gadget-inc/skipper/internal/dev/watcher"
	"github.com/spf13/cobra"
)

// newUpCmd starts the local development orchestrator: controller,
// router, and the docs site as long-lived child processes sharing
// cancellation. Controller and router restart on .go file changes;
// the docs server runs unsupervised. SIGINT / SIGTERM trigger a
// SIGTERM → SIGKILL teardown of every child.
func newUpCmd() *cobra.Command {
	var only string

	cmd := &cobra.Command{
		Use:   "up [flags]",
		Short: "Run controller, router, and the docs site locally",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			components := expandSkipperAlias(only)

			runController := components["controller"]
			runRouter := components["router"]
			runDocs := components["docs"]

			if !runController && !runRouter && !runDocs {
				return fmt.Errorf("up: --only=%q produced no components", only)
			}

			root := repoRoot()

			if runController {
				if err := ensurePasetoKeypair(); err != nil {
					return err
				}
				warnIfFixturesNamespaceMissing(ctx)
			}

			if runController || runRouter {
				if err := os.MkdirAll(filepath.Join(root, "tmp", "logs"), 0o755); err != nil {
					return err
				}
			}

			grp := process.New(ctx)

			var restartChans []chan struct{}
			if runController || runRouter {
				w, err := watcher.NewGoWatcher(ctx, root)
				if err != nil {
					return fmt.Errorf("watch %s: %w", root, err)
				}
				n := 0
				if runController {
					n++
				}
				if runRouter {
					n++
				}
				restartChans = fanOut(ctx, w.Events, n)
			}

			pop := func() <-chan struct{} {
				if len(restartChans) == 0 {
					return nil
				}
				ch := restartChans[0]
				restartChans = restartChans[1:]
				return ch
			}

			if runController {
				grp.SpawnWithRestart("controller", controllerSpawn(root), pop())
			}
			if runRouter {
				grp.SpawnWithRestart("router", routerSpawn(root), pop())
			}
			if runDocs {
				grp.Spawn("docs", docsSpawn(root))
			}

			printEndpoints(runController, runRouter, runDocs)

			err := grp.Wait()
			if ctx.Err() != nil {
				return nil
			}
			return err
		},
	}

	cmd.Flags().StringVar(&only, "only", "controller,router,docs", "Components to run (controller,router,docs)")
	return cmd
}

func controllerSpawn(root string) process.SpawnFunc {
	return func(ctx context.Context, stdout, stderr io.Writer) *osexec.Cmd {
		pasetoKey, _ := os.ReadFile(filepath.Join(root, "tmp", "paseto", "private.pem"))
		cmd := osexec.CommandContext(ctx, "go", "run", "./cmd/controller")
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"SKIPPER_NAMESPACE=skipper-development",
			"SKIPPER_POD_IP=127.0.0.1",
			"SKIPPER_PASETO_PRIVATE_KEY="+string(pasetoKey),
			"SKIPPER_FUNCTION_NAMESPACES=skipper-development-fixtures,skipper-test-fixtures",
			"SKIPPER_WEB_TEMPLATE_DIR="+filepath.Join(root, "internal", "web"),
			"SKIPPER_SINGLE_CONTROLLER_MODE=true",
			"SKIPPER_HOST=127.0.0.1",
			"SKIPPER_LOG_FORMAT=text",
			"SKIPPER_LOG_FILE="+filepath.Join(root, "tmp", "logs", "controller.jsonl"),
			"SKIPPER_LOG_FILE_LEVEL=debug",
			"SKIPPER_LOG_FILE_FORMAT=json",
			"SKIPPER_SKIP_FORBIDDEN_NAMESPACES=true",
		)
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		// New process group so SIGTERM hits the `go run` parent and any
		// children it spawned; otherwise the compiled binary outlives
		// the wrapper.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		return cmd
	}
}

func routerSpawn(root string) process.SpawnFunc {
	return func(ctx context.Context, stdout, stderr io.Writer) *osexec.Cmd {
		cmd := osexec.CommandContext(ctx, "go", "run", "./cmd/router")
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"SKIPPER_POD_IP=127.0.0.1",
			"SKIPPER_CONTROLLER_SERVICE_HOST=127.0.0.1",
			"SKIPPER_HOST=127.0.0.1",
			"SKIPPER_PORT=8081",
			"SKIPPER_LOG_FORMAT=text",
			"SKIPPER_LOG_FILE="+filepath.Join(root, "tmp", "logs", "router.jsonl"),
			"SKIPPER_LOG_FILE_LEVEL=debug",
			"SKIPPER_LOG_FILE_FORMAT=json",
		)
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		return cmd
	}
}

func docsSpawn(root string) process.SpawnFunc {
	return func(ctx context.Context, stdout, stderr io.Writer) *osexec.Cmd {
		// Phase 11 swaps this for an in-process call to
		// internal/dev/docssite.Serve.
		cmd := osexec.CommandContext(ctx, "pnpm", "--filter", "docs", "dev")
		cmd.Dir = root
		cmd.Env = os.Environ()
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		return cmd
	}
}

// fanOut multiplies a single source channel into n buffered output
// channels so multiple SpawnWithRestart consumers each see every
// signal. Slow consumers drop signals rather than blocking the source.
// Returns nil when n == 0.
func fanOut(ctx context.Context, in <-chan struct{}, n int) []chan struct{} {
	if n == 0 {
		return nil
	}
	outs := make([]chan struct{}, n)
	for i := range outs {
		outs[i] = make(chan struct{}, 1)
	}
	go func() {
		defer func() {
			for _, c := range outs {
				close(c)
			}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-in:
				if !ok {
					return
				}
				for _, c := range outs {
					select {
					case c <- struct{}{}:
					default:
					}
				}
			}
		}
	}()
	return outs
}

func ensurePasetoKeypair() error {
	root := repoRoot()
	dir := filepath.Join(root, "tmp", "paseto")
	priv := filepath.Join(dir, "private.pem")
	pub := filepath.Join(dir, "public.pem")

	_, e1 := os.Stat(priv)
	_, e2 := os.Stat(pub)
	if e1 == nil && e2 == nil {
		return nil
	}

	if err := generatePasetoKeypairFiles(); err != nil {
		return err
	}
	fmt.Println("Generated PASETO keypair in tmp/paseto/")
	return nil
}

func warnIfFixturesNamespaceMissing(ctx context.Context) {
	cmd := osexec.CommandContext(ctx, "kubectl", "get", "namespace", "skipper-development-fixtures")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "⚠ Namespace skipper-development-fixtures not found. Run: dev deploy --only=fixtures,metrics-server")
	}
}

func printEndpoints(controller, router, docs bool) {
	if controller {
		fmt.Println("Controller gRPC: 127.0.0.1:50051")
		fmt.Println("Web UI:          http://127.0.0.1:8080")
	}
	if router {
		fmt.Println("Router:          http://127.0.0.1:8081")
	}
	if docs {
		fmt.Println("Docs:            http://localhost:4321")
	}
}
