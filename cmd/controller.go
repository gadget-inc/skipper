package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/gadget-inc/fusion/internal/controller"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/telemetry"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

func NewCmdController() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "controller",
		Short: "Start the fusion controller",
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

			metricsClientset, err := metricsclientset.NewForConfig(config)
			if err != nil {
				return fmt.Errorf("failed to create metrics client: %w", err)
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			shutdownTelemetry := telemetry.Init(ctx, telemetry.ComponentController)
			defer shutdownTelemetry()

			ctrl := controller.New(controller.NewHTTPClient, clientset, metricsClientset)
			err = ctrl.Start(ctx)
			if err != nil {
				return fmt.Errorf("failed to start controller: %w", err)
			}

			srv := &http.Server{
				Addr: ":" + strconv.Itoa(controller.FlagPort.Value()),
				Handler: otelhttp.NewHandler(ctrl, "",
					otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/healthz" }),
					otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string { return "HTTP " + r.Method + " " + r.URL.Path }),
				),
			}

			serverErrors := make(chan error, 1)

			go func() {
				err := srv.ListenAndServe()
				if err != nil && err != http.ErrServerClosed {
					log.Error(ctx, "failed to serve listen and serve", key.Error.Field(err))
					serverErrors <- err
				}
			}()

			log.Info(ctx, "server started", key.Addr.Field(srv.Addr))
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

			select {
			case err := <-serverErrors:
				return fmt.Errorf("server error: %w", err)
			case sig := <-quit:
				log.Info(ctx, "received signal, shutting down", key.Signal.Field(sig.String()))
			}

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err = srv.Shutdown(shutdownCtx)
			if err != nil {
				return fmt.Errorf("failed to shutdown server: %w", err)
			}

			log.Info(ctx, "server shutdown")

			return nil
		},
	}

	controller.FlagIP.Bind(cmd)
	controller.FlagNamespace.Bind(cmd)
	controller.FlagPasetoPrivateKey.Bind(cmd)
	controller.FlagPort.Bind(cmd)
	function.FlagAssignPath.Bind(cmd)
	function.FlagAssignTimeout.Bind(cmd)
	function.FlagNamespaces.Bind(cmd)
	function.FlagPort.Bind(cmd)
	function.FlagSkipForbiddenNamespaces.Bind(cmd)

	return cmd
}
