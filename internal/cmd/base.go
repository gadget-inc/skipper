package cmd

import (
	"github.com/gadget-inc/skipper/internal/config"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/gadget-inc/skipper/internal/pprof"
	"github.com/gadget-inc/skipper/internal/telemetry"
	"github.com/spf13/cobra"
)

// BaseConfig holds configuration for log, pprof, and telemetry that is shared
// between the controller and router commands.
type BaseConfig struct {
	Log       *log.Config
	Pprof     *pprof.Config
	Telemetry *telemetry.Config
}

// NewBaseConfig creates a new BaseConfig with default values.
func NewBaseConfig() *BaseConfig {
	return &BaseConfig{
		Log:       config.New[log.Config](),
		Pprof:     config.New[pprof.Config](),
		Telemetry: config.New[telemetry.Config](),
	}
}

// Bind binds the common configuration flags to the command as persistent flags.
func (c *BaseConfig) Bind(cmd *cobra.Command) {
	config.BindPersistent(cmd, c.Log)
	config.BindPersistent(cmd, c.Pprof)
	config.BindPersistent(cmd, c.Telemetry)
}

// Init initializes logging, telemetry, and pprof. It should be called at the
// start of RunE after flags have been parsed. Returns a cleanup function that
// should be deferred by the caller.
func (c *BaseConfig) Init(cmd *cobra.Command) (cleanup func(), err error) {
	if err := c.Log.Validate(); err != nil {
		return nil, err
	}

	log.Init(c.Log)
	shutdownTelemetry := telemetry.Init(cmd.Context(), c.Telemetry, cmd.Name())
	shutdownPprof := pprof.Init(cmd.Context(), c.Pprof)

	return func() {
		shutdownTelemetry()
		shutdownPprof()
	}, nil
}
