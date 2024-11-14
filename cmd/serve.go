package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gadget-inc/fusion/internal/pod"
	"github.com/gadget-inc/fusion/internal/router"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func NewCmdServe() *cobra.Command {
	var (
		fusionNamespace string
		fusionIP        string
		namespaces      []string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve HTTP requests",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := rest.InClusterConfig()
			if errors.Is(err, rest.ErrNotInCluster) {
				config, err = clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
			}
			if err != nil {
				return fmt.Errorf("failed to load kubernetes config: %w", err)
			}

			config.QPS = 100
			config.Burst = 200

			clientset, err := kubernetes.NewForConfig(config)
			if err != nil {
				return fmt.Errorf("failed to create kubernetes client: %w", err)
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			podManager := pod.NewManager(clientset)
			podManager.Start(ctx, namespaces)

			rtr := router.New(fusionIP, clientset, podManager)
			rtr.Start(ctx, fusionNamespace)

			srv := &http.Server{
				Addr:    ":8080",
				Handler: rtr,
			}

			serverErrors := make(chan error, 1)

			go func() {
				err := srv.ListenAndServe()
				if err != nil && err != http.ErrServerClosed {
					slog.ErrorContext(ctx, "failed to serve listen and serve", slog.Any("error", err))
					serverErrors <- err
				}
			}()

			slog.InfoContext(ctx, "server started", slog.String("address", srv.Addr))
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

			select {
			case err := <-serverErrors:
				return fmt.Errorf("server error: %w", err)
			case sig := <-quit:
				slog.InfoContext(ctx, "received signal, shutting down", slog.String("signal", sig.String()))
			}

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err = srv.Shutdown(shutdownCtx)
			if err != nil {
				return fmt.Errorf("failed to shutdown server: %w", err)
			}

			slog.InfoContext(ctx, "server shutdown")

			return nil
		},
	}

	cmd.Flags().StringVarP(&fusionNamespace, "fusion-namespace", "f", os.Getenv("FUSION_NAMESPACE"), "namespace where this fusion router is deployed")
	cmd.Flags().StringVarP(&fusionIP, "fusion-ip", "i", os.Getenv("FUSION_IP"), "ip address of this fusion router")
	cmd.Flags().StringArrayVarP(&namespaces, "namespace", "n", nil, "namespaces to watch for deployments")
	// cmd.MarkFlagRequired("fusion-namespace")
	// cmd.MarkFlagRequired("fusion-ip")
	cmd.MarkFlagRequired("namespace")

	return cmd
}
