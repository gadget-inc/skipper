package cmd

import (
	"github.com/gadget-inc/fusion/internal/log"
	"github.com/gadget-inc/fusion/internal/telemetry"
	"github.com/spf13/cobra"
)

func NewRoot() *cobra.Command {
	cmd := &cobra.Command{
		Use: "fusion",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			log.Init()
			shutdownTelemetry := telemetry.Init(cmd.Context(), cmd.Name())

			// we can't use PersistentPostRunE because it doesn't run if RunE returns an error
			// https://github.com/spf13/cobra/issues/1893
			cobra.OnFinalize(shutdownTelemetry)
			return nil
		},
	}

	cmd.AddCommand(NewController())
	cmd.AddCommand(NewRouter())

	log.FlagLogFormat.BindPersistent(cmd)
	log.FlagLogLevel.BindPersistent(cmd)
	telemetry.FlagTelemetry.BindPersistent(cmd)
	telemetry.FlagTelemetryShutdownTimeout.BindPersistent(cmd)

	return cmd
}
