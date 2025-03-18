package telemetry

import (
	"context"
	"log/slog"
	"sync"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"github.com/go-logr/logr"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

func Init(ctx context.Context, component string) func() {
	if !FlagTelemetry.Value() {
		log.Info(ctx, "telemetry disabled", slog.String("component", component))
		return func() {}
	}

	otel.SetLogger(logr.FromSlogHandler(log.Handler()))

	res, err := resource.New(ctx,
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
			semconv.ServiceNameKey.String("skipper."+component),
			// semconv.ServiceNamespaceKey.String("skipper"),
			// semconv.ServiceVersionKey.String(version.Version),
		),
	)
	if err != nil {
		log.Error(ctx, "failed to create otel resource", key.Error.Field(err))
		return func() {}
	}

	shutdownTracing := initTracing(ctx, res)
	shutdownMetrics := initMetrics(ctx)
	log.Info(ctx, "telemetry enabled", slog.String("component", component))

	return func() {
		ctx, cancel := context.WithTimeout(context.Background(), FlagTelemetryShutdownTimeout.Value())
		defer cancel()

		wg := new(sync.WaitGroup)
		wg.Add(2)

		go func() {
			defer wg.Done()
			err := shutdownTracing(ctx)
			if err != nil {
				log.Error(ctx, "failed to shutdown tracing", key.Error.Field(err))
			}
		}()

		go func() {
			defer wg.Done()
			err := shutdownMetrics(ctx)
			if err != nil {
				log.Error(ctx, "failed to shutdown metrics", key.Error.Field(err))
			}
		}()

		wg.Wait()
	}
}
