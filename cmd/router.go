package cmd

import (
	"log/slog"
	"time"

	"github.com/gadget-inc/fusion/internal/kubernetes"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/labels"
)

func NewCmdRouter() *cobra.Command {
	var namespace string

	cmd := &cobra.Command{
		Use:   "router",
		Short: "Start the router",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, err := kubernetes.NewClient(ctx)
			if err != nil {
				return err
			}
			defer client.Close()

			ticker := time.NewTicker(time.Second)
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-ticker.C:
					pods, err := client.ListPods(ctx, namespace, labels.Everything())
					if err != nil {
						return err
					}

					podsField := make(map[string]any)
					for _, pod := range pods {
						podsField[pod.Name] = map[string]any{
							"name":      pod.Name,
							"namespace": pod.Namespace,
							"status":    pod.Status.Phase,
							"ip":        pod.Status.PodIP,
						}
					}

					slog.InfoContext(ctx, "pods", slog.Any("pods", podsField))
				}
			}
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "namespace to watch")
	cmd.MarkFlagRequired("namespace")

	return cmd
}
