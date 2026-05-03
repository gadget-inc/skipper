package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gadget-inc/skipper/internal/cmd"
	"github.com/gadget-inc/skipper/internal/dev/exec"
	"github.com/spf13/cobra"
)

// newCleanCmd deletes every Kubernetes namespace skipper deploys plus
// the local krane / paseto / log / test scratch directories.
func newCleanCmd() *cobra.Command {
	return cmd.Build(cmd.Spec{
		Use:   "clean",
		Short: "Tear down skipper's K8s namespaces and local scratch dirs",
		RunE: func(c *cobra.Command, args []string) error {
			ctx := c.Context()
			kubectx := os.Getenv("SKIPPER_KUBECTL_CONTEXT")
			if kubectx == "" {
				kubectx = "orbstack"
			}

			namespaces := []string{
				"skipper-development",
				"skipper-development-fixtures",
				"skipper-test",
				"skipper-test-fixtures",
				"otel-lgtm",
			}
			for _, ns := range namespaces {
				_ = exec.Run(ctx, "kubectl",
					[]string{"--context=" + kubectx, "delete", "namespace", ns, "--ignore-not-found"},
				)
			}

			manifests := []string{
				"deploy/cluster/metrics-server.yaml",
				"deploy/kube-system/metrics-server.yaml",
			}
			for _, f := range manifests {
				_ = exec.Run(ctx, "kubectl",
					[]string{"--context=" + kubectx, "delete", "-f", f, "--ignore-not-found"},
				)
			}

			workspace := os.Getenv("WORKSPACE_DIR")
			scratch := []string{"tmp/krane", "tmp/logs", "tmp/paseto", "tmp/test"}
			for _, sub := range scratch {
				path := filepath.Join(workspace, sub)
				if err := os.RemoveAll(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return err
				}
			}
			return nil
		},
	})
}
