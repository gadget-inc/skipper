package telemetry

import (
	"context"
	"log/slog"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/gadget-inc/skipper/internal/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("github.com/gadget-inc/skipper")

type contextKey struct{}

var ctxKey = contextKey{}

type contextSpanProcessor struct {
	sdktrace.SpanProcessor
}

func (c contextSpanProcessor) OnStart(ctx context.Context, s sdktrace.ReadWriteSpan) {
	if attributes, ok := ctx.Value(ctxKey).([]attribute.KeyValue); ok {
		s.SetAttributes(attributes...)
	}
	c.SpanProcessor.OnStart(ctx, s)
}

func initTracing(ctx context.Context, res *resource.Resource) func(context.Context) error {
	traceExporter, err := otlptrace.New(ctx, otlptracehttp.NewClient())
	if err != nil {
		log.Error(ctx, "failed to create otlptrace exporter", key.Error.Field(err))
		return func(context.Context) error { return nil }
	}

	traceProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(contextSpanProcessor{sdktrace.NewBatchSpanProcessor(traceExporter)}),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	)

	otel.SetTracerProvider(traceProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	log.AddHook(func(ctx context.Context, record *slog.Record) {
		if span := trace.SpanContextFromContext(ctx); span.IsValid() {
			record.AddAttrs(
				slog.String("trace_id", span.TraceID().String()),
				slog.String("span_id", span.SpanID().String()),
			)
		}
	})

	return traceProvider.Shutdown
}

func Trace(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return tracer.Start(ctx, spanName, opts...)
}

func TraceRoot(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	opts = append(opts, trace.WithNewRoot(), trace.WithLinks(trace.LinkFromContext(ctx)))
	return Trace(ctx, spanName, opts...)
}

func SetAttributes(ctx context.Context, attributes ...attribute.KeyValue) {
	trace.SpanFromContext(ctx).SetAttributes(attributes...)
}

func WithPropagatedAttributes(ctx context.Context, attributes ...attribute.KeyValue) context.Context {
	SetAttributes(ctx, attributes...)
	return context.WithValue(ctx, ctxKey, attributes)
}
