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

var (
	goCollectorOnce sync.Once
	goMetricsReg    *prometheus.Registry
)

func initMetrics(ctx context.Context) func(context.Context) error {
	if !FlagTelemetryMetric.Value() {
		return func(context.Context) error { return nil }
	}

	// Register Go runtime metrics collector (only once)
	goCollectorOnce.Do(func() {
		goMetricsReg = prometheus.NewRegistry()

		enhancedCollector := collectors.NewGoCollector(
			collectors.WithGoCollectorMemStatsMetricsDisabled(),
			collectors.WithGoCollectorRuntimeMetrics(
				collectors.MetricsAll,
			),
		)
		err := goMetricsReg.Register(enhancedCollector)
		if err != nil {
			log.Error(ctx, "failed to register Go collector with runtime metrics", key.Error.Field(err))
		}
	})

	combinedGatherer := prometheus.Gatherers{
		prometheus.DefaultGatherer,
		goMetricsReg,
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(combinedGatherer, promhttp.HandlerOpts{
		ProcessStartTime: time.Now(),
		ErrorLog:         log.StdLogger(slog.LevelError),
		ErrorHandling:    promhttp.ContinueOnError, // Continue even if some metrics conflict
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
