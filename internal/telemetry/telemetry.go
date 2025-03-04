package telemetry

import (
	"context"
	"log/slog"

	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

var tracer = otel.Tracer("github.com/gadget-inc/fusion")

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
			semconv.ServiceNameKey.String("fusion." + component),
			// semconv.ServiceNamespaceKey.String("fusion"),
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
	log.Info(ctx, "telemetry enabled", slog.String("component", component))

	return func() {
		ctx, cancel := context.WithTimeout(ctx, FlagTelemetryShutdownTimeout.Value())
		defer cancel()
		err = traceProvider.Shutdown(ctx)
		if err != nil {
			log.Error(ctx, "failed to shutdown telemetry", key.Error.Field(err))
		}
	}
}
