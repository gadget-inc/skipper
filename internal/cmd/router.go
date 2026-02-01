package cmd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/gadget-inc/skipper/internal/config"
	"github.com/gadget-inc/skipper/internal/controller"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/router"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func NewRouter() *cobra.Command {
	cfg := config.New[router.Config]()

	cmd := &cobra.Command{
		Use:   "router",
		Short: "Start the router",
		RunE: func(cmd *cobra.Command, args []string) error {
			// flags have been parsed and validated by now, so don't print usage or errors anymore
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			var client controller.Client
			switch cfg.ControllerProtocol {
			case "grpc":
				// Use the gRPC-specific headless service host if configured,
				// otherwise fall back to the standard service host
				grpcHost := cfg.ControllerGRPCServiceHost
				if grpcHost == "" {
					grpcHost = cfg.ControllerServiceHost
				}

				var err error
				client, err = controller.NewGRPCClient(grpcHost, cfg.ControllerGRPCPort)
				if err != nil {
					return fmt.Errorf("failed to create gRPC client: %w", err)
				}
			default:
				client = controller.NewHTTPClient(cfg.ControllerServiceHost, cfg.ControllerHTTPPort)
			}

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

			if err := client.Close(); err != nil {
				return fmt.Errorf("failed to close controller client: %w", err)
			}

			log.Info(ctx, "router shutdown")
			return nil
		},
	}

	config.Bind(cmd, cfg)

	return cmd
}
