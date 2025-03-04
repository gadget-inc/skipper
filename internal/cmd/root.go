package cmd

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/telemetry"
	"github.com/spf13/cobra"
)

func NewRoot() *cobra.Command {
	cmd := &cobra.Command{
		Use: "fusion",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			log.Init()
			return nil
		},
	}

	cmd.AddCommand(NewController())
	cmd.AddCommand(NewRouter())

	log.FlagLogLevel.BindPersistent(cmd)
	log.FlagLogFormat.BindPersistent(cmd)
	telemetry.FlagTelemetry.BindPersistent(cmd)
	return cmd
}

func Run(ctx context.Context) error {
	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	return NewRoot().ExecuteContext(ctx)
}
