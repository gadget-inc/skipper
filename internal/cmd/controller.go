package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"

	"github.com/gadget-inc/skipper/internal/config"
	"github.com/gadget-inc/skipper/internal/controller"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/skipper"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
	"k8s.io/klog/v2"
)

var klogOnce sync.Once

// NewController creates the controller command.
// Optional deps can be provided for testing; if nil, production defaults are used.
func NewController(deps *ControllerDeps) *cobra.Command {
	if deps == nil {
		deps = DefaultControllerDeps()
	}

	baseCfg := NewBaseConfig()
	cfg := config.New[controller.Config]()

	cmd := &cobra.Command{
		Use:   "controller",
		Short: "Start the controller",
		RunE: func(cmd *cobra.Command, args []string) error {
			// flags have been parsed and validated by now, so don't print usage or errors anymore
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true

			cleanup, err := baseCfg.Init(cmd)
			if err != nil {
				return err
			}
			defer cleanup()

			// validate config
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("invalid configuration: %w", err)
			}

			// make klog use slog which will have already been configured by the root command
			klogOnce.Do(func() { klog.SetSlogLogger(slog.Default()) })

			kubeConfig, err := deps.LoadKubeConfig()
			if err != nil {
				return fmt.Errorf("failed to load kubernetes config: %w", err)
			}

			kubeConfig.QPS = cfg.KubeConfigQPS
			kubeConfig.Burst = cfg.KubeConfigBurst
			kubeConfig.WrapTransport = func(rt http.RoundTripper) http.RoundTripper { return otelhttp.NewTransport(rt) }

			kubernetesClient, err := deps.NewK8sClient(kubeConfig)
			if err != nil {
				return fmt.Errorf("failed to create kubernetes client: %w", err)
			}

			kubernetesMetrics, err := deps.NewMetricsClient(kubeConfig)
			if err != nil {
				return fmt.Errorf("failed to create metrics client: %w", err)
			}

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			newClientFunc := deps.NewClientFunc(cfg.Port)

			ctrl := controller.New(cfg, newClientFunc, kubernetesClient, kubernetesMetrics)

			server := grpc.NewServer(
				grpc.StatsHandler(otelgrpc.NewServerHandler()),
			)
			skipper.RegisterControllerServiceServer(server, controller.NewServer(ctrl))
			healthServer := health.NewServer()
			healthServer.SetServingStatus("", healthgrpc.HealthCheckResponse_NOT_SERVING)
			healthgrpc.RegisterHealthServer(server, healthServer)

			listener, err := net.Listen("tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
			if err != nil {
				return fmt.Errorf("failed to create listener: %w", err)
			}

			serverError := make(chan error, 1)

			go func() {
				log.Info(ctx, "serving controller", key.Addr.Slog(listener.Addr().String()))
				if err := server.Serve(listener); err != nil {
					serverError <- err
				}
			}()

			if err := ctrl.Start(ctx); err != nil {
				server.Stop()
				return fmt.Errorf("failed to start controller: %w", err)
			}

			healthServer.SetServingStatus("", healthgrpc.HealthCheckResponse_SERVING)

			select {
			case err := <-serverError:
				return fmt.Errorf("failed to serve controller: %w", err)
			case <-ctx.Done():
				log.Info(ctx, "shutting down controller")
			}

			shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
			defer cancel()

			stopped := make(chan struct{})
			go func() {
				server.GracefulStop()
				close(stopped)
			}()

			select {
			case <-stopped:
			case <-shutdownCtx.Done():
				log.Warn(ctx, "graceful stop timed out, forcing stop")
				server.Stop()
			}

			ctrl.Close()

			log.Info(ctx, "controller shutdown")
			return nil
		},
	}

	baseCfg.Bind(cmd)
	config.Bind(cmd, cfg)

	return cmd
}
