package cmd

import (
	"github.com/gadget-inc/skipper/internal/config"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/pprof"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/spf13/cobra"
)

func NewRoot() *cobra.Command {
	logCfg := config.New[log.Config]()
	pprofCfg := config.New[pprof.Config]()
	telemetryCfg := config.New[telemetry.Config]()

	cmd := &cobra.Command{
		Use: "skipper",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if err := logCfg.Validate(); err != nil {
				return err
			}

			log.Init(logCfg)
			shutdownTelemetry := telemetry.Init(cmd.Context(), telemetryCfg, cmd.Name())
			shutdownPprof := pprof.Init(cmd.Context(), pprofCfg)

			// we can't use PersistentPostRunE because it doesn't run if RunE returns an error
			// https://github.com/spf13/cobra/issues/1893
			cobra.OnFinalize(shutdownTelemetry, shutdownPprof)
			return nil
		},
	}

	cmd.AddCommand(NewController())
	cmd.AddCommand(NewRouter())

	config.BindPersistent(cmd, logCfg)
	config.BindPersistent(cmd, pprofCfg)
	config.BindPersistent(cmd, telemetryCfg)

	return cmd
}
