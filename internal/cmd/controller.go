package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/gadget-inc/skipper/internal/config"
	"github.com/gadget-inc/skipper/internal/controller"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/klog/v2"
	kubernetesmetrics "k8s.io/metrics/pkg/client/clientset/versioned"
)

func NewController() *cobra.Command {
	cfg := config.New[controller.Config]()

	cmd := &cobra.Command{
		Use:   "controller",
		Short: "Start the controller",
		RunE: func(cmd *cobra.Command, args []string) error {
			// flags have been parsed and validated by now, so don't print usage or errors anymore
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true

			// validate config
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("invalid configuration: %w", err)
			}

			// make klog use slog which will have already been configured by the root command
			klog.SetSlogLogger(slog.Default())

			kubeConfig, err := rest.InClusterConfig()
			if errors.Is(err, rest.ErrNotInCluster) {
				kubeConfig, err = clientcmd.BuildConfigFromFlags("", filepath.Join(homedir.HomeDir(), ".kube", "config"))
			}
			if err != nil {
				return fmt.Errorf("failed to load kubernetes config: %w", err)
			}

			kubeConfig.QPS = cfg.KubeConfigQPS
			kubeConfig.Burst = cfg.KubeConfigBurst
			kubeConfig.WrapTransport = func(rt http.RoundTripper) http.RoundTripper { return otelhttp.NewTransport(rt) }

			kubernetesClient, err := kubernetes.NewForConfig(kubeConfig)
			if err != nil {
				return fmt.Errorf("failed to create kubernetes client: %w", err)
			}

			kubernetesMetrics, err := kubernetesmetrics.NewForConfig(kubeConfig)
			if err != nil {
				return fmt.Errorf("failed to create metrics client: %w", err)
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			ctrl := controller.New(cfg, controller.NewHTTPClient, kubernetesClient, kubernetesMetrics)
			if err := ctrl.Start(ctx); err != nil {
				return fmt.Errorf("failed to start controller: %w", err)
			}

			httpServer := &http.Server{
				Addr: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
				Handler: otelhttp.NewHandler(ctrl.Handler(), "",
					otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/healthz" }),
					otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string { return "HTTP " + r.Method + " " + r.URL.Path }),
				),
			}

			httpServerError := make(chan error, 1)

			go func() {
				log.Info(ctx, "serving controller", key.Addr.Slog(httpServer.Addr))
				if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					httpServerError <- err
				}
			}()

			select {
			case err := <-httpServerError:
				return fmt.Errorf("failed to serve controller: %w", err)
			case <-ctx.Done():
				log.Info(ctx, "shutting down controller")
			}

			shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
			defer cancel()

			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("failed to shutdown controller: %w", err)
			}

			log.Info(ctx, "controller shutdown")
			return nil
		},
	}

	config.Bind(cmd, cfg)

	return cmd
}
