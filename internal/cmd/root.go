package cmd

import (
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/pprof"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/spf13/cobra"
)

func NewRoot() *cobra.Command {
	cmd := &cobra.Command{
		Use: "skipper",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			log.Init()
			shutdownTelemetry := telemetry.Init(cmd.Context(), cmd.Name())
			shutdownPprof := pprof.Init(cmd.Context())

			// we can't use PersistentPostRunE because it doesn't run if RunE returns an error
			// https://github.com/spf13/cobra/issues/1893
			cobra.OnFinalize(shutdownTelemetry, shutdownPprof)
			return nil
		},
	}

	cmd.AddCommand(NewController())
	cmd.AddCommand(NewRouter())

	log.FlagLogFormat.BindPersistent(cmd)
	log.FlagLogLevel.BindPersistent(cmd)
	pprof.FlagPprof.BindPersistent(cmd)
	pprof.FlagPprofHost.BindPersistent(cmd)
	pprof.FlagPprofPort.BindPersistent(cmd)
	pprof.FlagPprofShutdownTimeout.BindPersistent(cmd)
	telemetry.FlagTelemetry.BindPersistent(cmd)
	telemetry.FlagTelemetryMetric.BindPersistent(cmd)
	telemetry.FlagTelemetryMetricOTLP.BindPersistent(cmd)
	telemetry.FlagTelemetryPrometheusHost.BindPersistent(cmd)
	telemetry.FlagTelemetryPrometheusPort.BindPersistent(cmd)
	telemetry.FlagTelemetryShutdownTimeout.BindPersistent(cmd)
	telemetry.FlagTelemetryTrace.BindPersistent(cmd)

	return cmd
}
