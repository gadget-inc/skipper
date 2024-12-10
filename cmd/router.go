package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gadget-inc/fusion/internal/controller"
	"github.com/gadget-inc/fusion/internal/function"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/router"
	"github.com/spf13/cobra"
)

func NewCmdRouter() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "router",
		Short: "Start the fusion router",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			controllerClient := controller.NewHTTPClient(router.FlagControllerServiceHost.Value, router.FlagControllerServicePort.Value)
			r := router.New(controllerClient)
			r.Start(ctx)

			httpServer := &http.Server{
				Addr:    ":" + strconv.Itoa(router.FlagPort.Value),
				Handler: r,
			}

			httpServerErrors := make(chan error, 1)

			go func() {
				err := httpServer.ListenAndServe()
				if err != http.ErrServerClosed {
					log.Error(ctx, "failed to listen and serve", key.Error.Field(err))
					httpServerErrors <- err
				}
			}()

			log.Info(ctx, "server started", slog.String("address", httpServer.Addr))
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

			select {
			case err := <-httpServerErrors:
				return fmt.Errorf("server error: %w", err)
			case sig := <-quit:
				log.Info(ctx, "received signal, shutting down", slog.String("signal", sig.String()))
			}

			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err := httpServer.Shutdown(shutdownCtx)
			if err != nil {
				return fmt.Errorf("failed to shutdown server: %w", err)
			}

			log.Info(ctx, "server shutdown")

			return nil
		},
	}

	function.FlagNamespaces.Bind(cmd)
	function.FlagSkipForbiddenNamespaces.Bind(cmd)
	router.FlagControllerServiceHost.Bind(cmd)
	router.FlagControllerServicePort.Bind(cmd)
	router.FlagMaxRoundTripAttempts.Bind(cmd)
	router.FlagHeartbeatInterval.Bind(cmd)
	router.FlagPort.Bind(cmd)

	return cmd
}
