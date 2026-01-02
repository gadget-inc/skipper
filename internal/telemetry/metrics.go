package telemetry

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var goCollectorOnce sync.Once

func initMetrics(ctx context.Context) func(context.Context) error {
	if !FlagTelemetryMetric.Value() {
		return func(context.Context) error { return nil }
	}

	// Register Go runtime metrics collector (only once)
	// Unregister the default basic Go collector and replace it with our enhanced version
	goCollectorOnce.Do(func() {
		// Try to unregister the default Go collector from the default registry
		// Create a basic Go collector instance - if one matches what's registered, it will be unregistered
		if reg, ok := prometheus.DefaultRegisterer.(*prometheus.Registry); ok {
			basicCollector := collectors.NewGoCollector()
			unregistered := reg.Unregister(basicCollector)
			if unregistered {
				log.Debug(ctx, "unregistered default Go collector")
			}
		}

		// Register our enhanced Go collector with runtime metrics to the default registry
		enhancedCollector := collectors.NewGoCollector(
			collectors.WithGoCollectorMemStatsMetricsDisabled(),
			collectors.WithGoCollectorRuntimeMetrics(
				collectors.MetricsAll,
			),
		)
		err := prometheus.DefaultRegisterer.Register(enhancedCollector)
		if err != nil {
			log.Error(ctx, "failed to register Go collector with runtime metrics", key.Error.Slog(err))
		} else {
			log.Debug(ctx, "registered enhanced Go collector with runtime metrics")
		}
	})

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
			log.Error(ctx, "failed to serve prometheus metrics", key.Error.Slog(err))
		}
	}()

	log.Info(ctx, "metrics enabled")
	return promServer.Shutdown
}
