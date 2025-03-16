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
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

var Meter = otel.Meter("github.com/gadget-inc/skipper")

func initMetrics(ctx context.Context, res *resource.Resource) func(context.Context) error {
	prometheusExporter, err := prometheus.New()
	if err != nil {
		log.Error(ctx, "failed to create prometheus exporter", key.Error.Field(err))
		return func(context.Context) error { return nil }
	}

	opts := []sdkmetric.Option{
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(prometheusExporter),
	}

	if FlagTelemetryMetricOTLP.Value() {
		metricExporter, err := otlpmetrichttp.New(ctx)
		if err != nil {
			log.Error(ctx, "failed to create otlp metric exporter", key.Error.Field(err))
			// keep going and just use the prometheus exporter
		} else {
			opts = append(opts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter)))
		}
	}

	metricProvider := sdkmetric.NewMeterProvider(opts...)
	otel.SetMeterProvider(metricProvider)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	promServer := &http.Server{Addr: net.JoinHostPort(FlagTelemetryPrometheusHost.Value(), strconv.Itoa(FlagTelemetryPrometheusPort.Value())), Handler: mux}
	go func() {
		if err := promServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error(ctx, "failed to serve prometheus metrics", key.Error.Field(err))
		}
	}()

	return func(ctx context.Context) error {
		err := promServer.Shutdown(ctx)
		if err != nil {
			log.Error(ctx, "failed to shutdown prometheus server", key.Error.Field(err))
		}
		return metricProvider.Shutdown(ctx)
	}
}
