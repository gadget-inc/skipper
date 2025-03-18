package telemetry

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func initMetrics(ctx context.Context) func(context.Context) error {
	if !FlagTelemetryMetric.Value() {
		return func(context.Context) error { return nil }
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(prometheus.DefaultGatherer, promhttp.HandlerOpts{
		ProcessStartTime: time.Now(),
		ErrorLog:         log.StdLogger(slog.LevelError),
	}))

	promServer := &http.Server{
		Addr:    net.JoinHostPort(FlagTelemetryPrometheusHost.Value(), strconv.Itoa(FlagTelemetryPrometheusPort.Value())),
		Handler: mux,
	}

	go func() {
		if err := promServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error(ctx, "failed to serve prometheus metrics", key.Error.Field(err))
		}
	}()

	log.Info(ctx, "metrics enabled")
	return promServer.Shutdown
}
