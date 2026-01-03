package pprof

import (
	"time"
)

// Config holds the pprof configuration.
type Config struct {
	Enabled         bool          `flag:"pprof" description:"Whether to enable pprof." default:"true"`
	Host            string        `flag:"pprof-host" description:"The host to serve the pprof on." default:"0.0.0.0"`
	Port            int           `flag:"pprof-port" description:"The port to serve the pprof on." default:"6060"`
	ShutdownTimeout time.Duration `flag:"pprof-shutdown-timeout" description:"The timeout for shutting down the pprof." default:"5s"`
}
