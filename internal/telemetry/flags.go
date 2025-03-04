package telemetry

import (
	"time"

	"github.com/gadget-inc/fusion/internal/flag"
)

var (
	FlagTelemetry = flag.Flag[bool]{
		Name:        "telemetry",
		Default:     false,
		Description: "Whether to enable OpenTelemetry",
	}

	FlagTelemetryShutdownTimeout = flag.Flag[time.Duration]{
		Name:        "telemetry-shutdown-timeout",
		Default:     5 * time.Second,
		Description: "The timeout for shutting down the telemetry",
	}
)
