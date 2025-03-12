package telemetry

import (
	"context"
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

func Init(ctx context.Context, component string) func() {
	if !FlagTelemetry.Value() {
		log.Info(ctx, "telemetry disabled", slog.String("component", component))
		return func() {}
	}

	traceExporter, err := otlptrace.New(ctx, otlptracehttp.NewClient())
	if err != nil {
		log.Error(ctx, "failed to create otlptrace exporter", key.Error.Field(err))
		return func() {}
	}

	resourceOptions := []resource.Option{
		resource.WithContainer(),
		resource.WithFromEnv(),
		resource.WithHost(),
		resource.WithOS(),
		resource.WithProcessExecutableName(),
		resource.WithProcessExecutablePath(),
		resource.WithProcessOwner(),
		resource.WithProcessRuntimeName(),
		resource.WithProcessRuntimeVersion(),
		resource.WithProcessRuntimeDescription(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String("skipper." + component),
			// semconv.ServiceNamespaceKey.String("skipper"),
			// semconv.ServiceVersionKey.String(version.Version),
		),
	}

	res, err := resource.New(ctx, resourceOptions...)
	if err != nil {
		log.Error(ctx, "failed to create otel resource", key.Error.Field(err))
		return func() {}
	}

	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	)

	otel.SetTracerProvider(traceProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	otel.SetLogger(logr.FromSlogHandler(log.Handler()))

	log.AddHook(func(ctx context.Context, record *slog.Record) {
		if span := trace.SpanContextFromContext(ctx); span.IsValid() {
			record.AddAttrs(
				slog.String("trace_id", span.TraceID().String()),
				slog.String("span_id", span.SpanID().String()),
			)
		}
	})

	log.Info(ctx, "telemetry enabled", slog.String("component", component))

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), FlagTelemetryShutdownTimeout.Value())
		defer cancel()
		err = traceProvider.Shutdown(ctx)
		if err != nil {
			log.Error(ctx, "failed to shutdown telemetry", key.Error.Field(err))
		}
	}
}
