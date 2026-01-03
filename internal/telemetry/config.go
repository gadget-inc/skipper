package telemetry

import (
	"time"
)

// Config holds the telemetry configuration.
type Config struct {
	Enabled         bool          `flag:"telemetry" description:"Whether to enable OpenTelemetry." default:"false"`
	Trace           bool          `flag:"telemetry-trace" description:"Whether to enable tracing if telemetry is enabled." default:"true"`
	Metric          bool          `flag:"telemetry-metric" description:"Whether to enable metrics if telemetry is enabled." default:"true"`
	ShutdownTimeout time.Duration `flag:"telemetry-shutdown-timeout" description:"The timeout for shutting down the telemetry." default:"5s"`
	PrometheusHost  string        `flag:"telemetry-prometheus-host" description:"The host for the Prometheus metrics endpoint." default:"0.0.0.0"`
	PrometheusPort  int           `flag:"telemetry-prometheus-port" description:"The port for the Prometheus metrics endpoint." default:"9090"`
	MetricOTLP      bool          `flag:"telemetry-metric-otlp" description:"Whether to send metrics to the OTLP endpoint." default:"false"`
}
