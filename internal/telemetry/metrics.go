package telemetry

import (
	"context"
	"net"
	"net/http"
	"strconv"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

func initMetrics(ctx context.Context, res *resource.Resource) func(context.Context) error {
	prometheusExporter, err := prometheus.New()
	if err != nil {
		log.Error(ctx, "failed to create prometheus exporter", key.Error.Field(err))
		return func(context.Context) error { return nil }
	}

	metricProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(prometheusExporter),
	)

	otel.SetMeterProvider(metricProvider)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	server := &http.Server{Addr: net.JoinHostPort(FlagTelemetryPrometheusHost.Value(), strconv.Itoa(FlagTelemetryPrometheusPort.Value())), Handler: mux}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error(ctx, "failed to serve prometheus metrics", key.Error.Field(err))
		}
	}()

	return func(ctx context.Context) error {
		err := server.Shutdown(ctx)
		if err != nil {
			log.Error(ctx, "failed to shutdown prometheus server", key.Error.Field(err))
		}
		return metricProvider.Shutdown(ctx)
	}
}
