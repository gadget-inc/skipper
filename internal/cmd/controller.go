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
	"github.com/gadget-inc/skipper/internal/web"
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
	cfg := config.New[controller.Config]()
	return Build(Spec{
		Use:     "controller",
		Short:   "Start the controller",
		Configs: []any{cfg},
		Base:    true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runController(cmd.Context(), cfg, deps)
		},
	})
}

func runController(ctx context.Context, cfg *controller.Config, deps *ControllerDeps) error {
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

	ctx, cancel := context.WithCancel(ctx)
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

	serverError := make(chan error, 2)

	go func() {
		log.Info(ctx, "serving controller", key.Addr.Slog(listener.Addr().String()))
		if err := server.Serve(listener); err != nil {
			serverError <- err
		}
	}()

	// Start web UI server
	var webOpts []web.Option
	if cfg.WebTemplateDir != "" {
		webOpts = append(webOpts, web.WithTemplateDir(cfg.WebTemplateDir))
		webOpts = append(webOpts, web.WithStaticDir(cfg.WebTemplateDir))
	}
	webServer := web.New(ctrl.ClusterState, webOpts...)
	webAddr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.WebPort))
	webHTTPServer := &http.Server{Addr: webAddr, Handler: webServer.Handler()}
	go func() {
		log.Info(ctx, "serving web ui", key.Addr.Slog(webAddr))
		if err := webHTTPServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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
		if err := webHTTPServer.Shutdown(shutdownCtx); err != nil {
			log.Warn(ctx, "web server shutdown error", key.Error.Slog(err))
		}
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-shutdownCtx.Done():
		log.Warn(ctx, "graceful stop timed out, forcing stop")
		server.Stop()
		webHTTPServer.Close()
	}

	ctrl.Close()

	log.Info(ctx, "controller shutdown")
	return nil
}
