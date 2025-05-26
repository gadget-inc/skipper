package cmd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/gadget-inc/skipper/internal/controller"
	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/router"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func NewRouter() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "router",
		Short: "Start the router",
		RunE: func(cmd *cobra.Command, args []string) error {
			// flags have been parsed and validated by now, so don't print usage or errors anymore
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			r := router.New(controller.NewHTTPClient(router.FlagControllerServiceHost.Value(), router.FlagControllerServicePort.Value()))
			r.Start(ctx)

			httpServer := &http.Server{
				Addr: net.JoinHostPort(router.FlagHost.Value(), strconv.Itoa(router.FlagPort.Value())),
				Handler: otelhttp.NewHandler(r, "",
					otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/healthz" }),
					otelhttp.WithSpanNameFormatter(func(operation string, r *http.Request) string { return "HTTP " + r.Method }),
				),
			}

			httpServerError := make(chan error, 1)

			go func() {
				log.Info(ctx, "serving router", key.Addr.Field(httpServer.Addr))
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

			shutdownCtx, cancel := context.WithTimeout(context.Background(), router.FlagShutdownTimeout.Value())
			defer cancel()

			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("failed to shutdown router: %w", err)
			}

			log.Info(ctx, "router shutdown")
			return nil
		},
	}

	router.FlagControllerServiceGRPCPort.Bind(cmd)
	router.FlagControllerServiceHost.Bind(cmd)
	router.FlagControllerServicePort.Bind(cmd)
	router.FlagHeartbeatInterval.Bind(cmd)
	router.FlagHost.Bind(cmd)
	router.FlagMaxRoundTripAttempts.Bind(cmd)
	router.FlagPodIP.Bind(cmd)
	router.FlagPort.Bind(cmd)
	router.FlagRoundTripRetryMaxTimeout.Bind(cmd)
	router.FlagRoundTripRetryMinTimeout.Bind(cmd)
	router.FlagShutdownTimeout.Bind(cmd)

	return cmd
}
