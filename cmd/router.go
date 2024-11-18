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

	"github.com/gadget-inc/fusion/internal/controller"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/pod"
	"github.com/gadget-inc/fusion/internal/router"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func NewCmdRouter() *cobra.Command {
	var (
		namespaces     []string
		controllerHost string
	)

	cmd := &cobra.Command{
		Use:   "router",
		Short: "Start the fusion router",
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := rest.InClusterConfig()
			if errors.Is(err, rest.ErrNotInCluster) {
				config, err = clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
			}
			if err != nil {
				return fmt.Errorf("failed to load kubernetes config: %w", err)
			}

			clientset, err := kubernetes.NewForConfig(config)
			if err != nil {
				return fmt.Errorf("failed to create kubernetes client: %w", err)
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			podManager := pod.NewManager(clientset)
			err = podManager.Start(ctx, namespaces)
			if err != nil {
				return fmt.Errorf("failed to start pod manager: %w", err)
			}

			controllerClient := controller.NewClient(controllerHost)

			rtr := router.New(controllerClient, clientset, podManager)
			srv := &http.Server{
				Addr:    ":8080",
				Handler: rtr,
			}

			serverErrors := make(chan error, 1)

			go func() {
				err := srv.ListenAndServe()
				if err != nil && err != http.ErrServerClosed {
					slog.ErrorContext(ctx, "failed to listen and serve", key.Error.Field(err))
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

	cmd.Flags().StringArrayVarP(&namespaces, "namespace", "n", nil, "namespaces to watch for deployments")
	cmd.Flags().StringVarP(&controllerHost, "controller-host", "", "", "host of the fusion controller")
	cmd.MarkFlagRequired("namespace")
	cmd.MarkFlagRequired("controller-host")

	return cmd
}
