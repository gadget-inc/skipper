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

// NewController creates the controller subcommand.
// Optional deps can be provided for testing; if nil, production defaults are used.
func NewController(deps *ControllerDeps) *cobra.Command {
	if deps == nil {
		deps = DefaultControllerDeps()
	}

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

			newClientFunc := deps.NewClientFunc(cfg.Protocol, cfg.GRPCPort)

			ctrl := controller.New(cfg, newClientFunc, kubernetesClient, kubernetesMetrics)
			if err := ctrl.Start(ctx); err != nil {
				return fmt.Errorf("failed to start controller: %w", err)
			}

			httpServer := &http.Server{
				Addr: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.HTTPPort)),
				Handler: otelhttp.NewHandler(ctrl.Handler(), "",
					otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/healthz" }),
					otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string { return "HTTP " + r.Method + " " + r.URL.Path }),
				),
			}

			httpServerError := make(chan error, 1)

			go func() {
				log.Info(ctx, "serving controller HTTP", key.Addr.Slog(httpServer.Addr))
				if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					httpServerError <- err
				}
			}()

			grpcServer := grpc.NewServer(
				grpc.StatsHandler(otelgrpc.NewServerHandler()),
			)
			skipper.RegisterControllerServiceServer(grpcServer, controller.NewGRPCServer(ctrl))
			healthServer := health.NewServer()
			healthServer.SetServingStatus("", healthgrpc.HealthCheckResponse_SERVING)
			healthgrpc.RegisterHealthServer(grpcServer, healthServer)

			grpcListener, err := net.Listen("tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.GRPCPort)))
			if err != nil {
				return fmt.Errorf("failed to create gRPC listener: %w", err)
			}

			grpcServerError := make(chan error, 1)

			go func() {
				log.Info(ctx, "serving controller gRPC", key.Addr.Slog(grpcListener.Addr().String()))
				if err := grpcServer.Serve(grpcListener); err != nil {
					grpcServerError <- err
				}
			}()

			select {
			case err := <-httpServerError:
				return fmt.Errorf("failed to serve controller HTTP: %w", err)
			case err := <-grpcServerError:
				return fmt.Errorf("failed to serve controller gRPC: %w", err)
			case <-ctx.Done():
				log.Info(ctx, "shutting down controller")
			}

			shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
			defer cancel()

			grpcStopped := make(chan struct{})
			go func() {
				grpcServer.GracefulStop()
				close(grpcStopped)
			}()

			select {
			case <-grpcStopped:
			case <-shutdownCtx.Done():
				log.Warn(ctx, "gRPC graceful stop timed out, forcing stop")
				grpcServer.Stop()
			}

			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("failed to shutdown controller HTTP: %w", err)
			}

			ctrl.Close()

			log.Info(ctx, "controller shutdown")
			return nil
		},
	}

	config.Bind(cmd, cfg)

	return cmd
}
