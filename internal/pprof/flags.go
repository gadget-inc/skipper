package pprof

import (
	"time"

	"github.com/gadget-inc/skipper/internal/flag"
)

var (
	FlagPprof = flag.Flag[bool]{
		Name:        "pprof",
		Default:     true,
		Description: "Whether to enable pprof",
	}

	FlagPprofPort = flag.Flag[int]{
		Name:        "pprof-port",
		Default:     6060,
		Description: "The port to serve the pprof on",
	}

	FlagPprofHost = flag.Flag[string]{
		Name:        "pprof-host",
		Default:     "0.0.0.0",
		Description: "The host to serve the pprof on",
	}

	FlagPprofShutdownTimeout = flag.Flag[time.Duration]{
		Name:        "pprof-shutdown-timeout",
		Default:     5 * time.Second,
		Description: "The timeout for shutting down the pprof",
	}
)
