package cmd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/gadget-inc/skipper/internal/config"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/router"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// NewRouter creates the router command.
// Optional deps can be provided for testing; if nil, production defaults are used.
func NewRouter(deps *RouterDeps) *cobra.Command {
	if deps == nil {
		deps = DefaultRouterDeps()
	}
	cfg := config.New[router.Config]()
	return Build(Spec{
		Use:     "router",
		Short:   "Start the router",
		Configs: []any{cfg},
		Base:    true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRouter(cmd.Context(), cfg, deps)
		},
	})
}

func runRouter(ctx context.Context, cfg *router.Config, deps *RouterDeps) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	client, err := deps.NewControllerClient(cfg)
	if err != nil {
		return fmt.Errorf("failed to create controller client: %w", err)
	}
	defer client.Close()

	r := router.New(cfg, client)
	r.Start(ctx)

	httpServer := &http.Server{
		Addr: net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)),
		Handler: otelhttp.NewHandler(r, "",
			otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/healthz" }),
			otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string { return "HTTP " + r.Method }),
		),
	}

	httpServerError := make(chan error, 1)

	go func() {
		log.Info(ctx, "serving router", key.Addr.Slog(httpServer.Addr))
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			httpServerError <- err
		}
	}()

	select {
	case err := <-httpServerError:
		return fmt.Errorf("failed to serve router: %w", err)
	case <-ctx.Done():
		log.Info(ctx, "shutting down router")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("failed to shutdown router: %w", err)
	}

	log.Info(ctx, "router shutdown")
	return nil
}
