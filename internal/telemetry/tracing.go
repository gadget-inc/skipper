package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("github.com/gadget-inc/skipper")

func Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return tracer.Start(ctx, spanName, opts...)
}

func StartRoot(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	opts = append(opts, trace.WithNewRoot(), trace.WithLinks(trace.LinkFromContext(ctx)))
	return Start(ctx, spanName, opts...)
}

func Trace[T any](ctx context.Context, spanName string, fn func(context.Context, trace.Span) (T, error)) (T, error) {
	ctx, span := Start(ctx, spanName)
	defer span.End()
	return fn(ctx, span)
}
