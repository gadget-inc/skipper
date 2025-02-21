package cmd

import (
	"context"

	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/telemetry"
	"github.com/spf13/cobra"
)

func NewCmdRoot() *cobra.Command {
	cmd := &cobra.Command{
		Use: "fusion",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			log.Init()
			return nil
		},
	}

	cmd.AddCommand(NewCmdController())
	cmd.AddCommand(NewCmdRouter())

	log.FlagLogLevel.BindPersistent(cmd)
	log.FlagLogFormat.BindPersistent(cmd)
	telemetry.FlagTelemetry.BindPersistent(cmd)
	return cmd
}

func Execute(ctx context.Context) error {
	return NewCmdRoot().ExecuteContext(ctx)
}
