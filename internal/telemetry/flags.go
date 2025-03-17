package telemetry

import (
	"time"

	"github.com/gadget-inc/skipper/internal/flag"
)

var (
	FlagTelemetry = flag.Flag[bool]{
		Name:        "telemetry",
		Default:     false,
		Description: "Whether to enable OpenTelemetry",
	}

	FlagTelemetryTrace = flag.Flag[bool]{
		Name:        "telemetry-trace",
		Default:     true,
		Description: "Whether to enable tracing if telemetry is enabled",
	}

	FlagTelemetryMetric = flag.Flag[bool]{
		Name:        "telemetry-metric",
		Default:     true,
		Description: "Whether to enable metrics if telemetry is enabled",
	}

	FlagTelemetryShutdownTimeout = flag.Flag[time.Duration]{
		Name:        "telemetry-shutdown-timeout",
		Default:     5 * time.Second,
		Description: "The timeout for shutting down the telemetry",
	}

	FlagTelemetryPrometheusHost = flag.Flag[string]{
		Name:        "telemetry-prometheus-host",
		Default:     "0.0.0.0",
		Description: "The host for the Prometheus metrics endpoint",
	}

	FlagTelemetryPrometheusPort = flag.Flag[int]{
		Name:        "telemetry-prometheus-port",
		Default:     9090,
		Description: "The port for the Prometheus metrics endpoint",
	}

	FlagTelemetryMetricOTLP = flag.Flag[bool]{
		Name:        "telemetry-metric-otlp",
		Default:     false,
		Description: "Whether to send metrics to the OTLP endpoint",
	}
)
