package main

import (
	"context"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// initTelemetry wires OTLP trace and metric exporters configured from
// standard OTEL_* environment variables and returns a shutdown function the
// caller MUST invoke before exiting. If exporter setup fails the fixture
// keeps running with no-op providers — the container's primary job is to
// serve requests, not emit telemetry.
func initTelemetry(ctx context.Context) func(context.Context) error {
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcessExecutableName(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(semconv.ServiceNameKey.String("skipper.fixture")),
	)
	if err != nil {
		log.Warn(ctx, "telemetry resource init failed", key.Error.Slog(err))
		return func(context.Context) error { return nil }
	}

	shutdownTrace := initTrace(ctx, res)
	shutdownMetric := initMetric(ctx, res)

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(ctx context.Context) error {
		if err := shutdownTrace(ctx); err != nil {
			return err
		}
		return shutdownMetric(ctx)
	}
}

func initTrace(ctx context.Context, res *resource.Resource) func(context.Context) error {
	exporter, err := otlptrace.New(ctx, otlptracehttp.NewClient())
	if err != nil {
		log.Warn(ctx, "trace exporter init failed", key.Error.Slog(err))
		return func(context.Context) error { return nil }
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown
}

func initMetric(ctx context.Context, res *resource.Resource) func(context.Context) error {
	exporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		log.Warn(ctx, "metric exporter init failed", key.Error.Slog(err))
		return func(context.Context) error { return nil }
	}
	mp := metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(exporter)),
	)
	otel.SetMeterProvider(mp)
	return mp.Shutdown
}
