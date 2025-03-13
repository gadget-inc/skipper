package telemetry

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("github.com/gadget-inc/skipper")

type contextKey struct{}

var ctxKey = contextKey{}

func Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if attributes, ok := ctx.Value(ctxKey).([]attribute.KeyValue); ok {
		opts = append(opts, trace.WithAttributes(attributes...))
	}
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

func SetAttributes(ctx context.Context, attributes ...attribute.KeyValue) {
	trace.SpanFromContext(ctx).SetAttributes(attributes...)
}

func WithPropagatedAttributes(ctx context.Context, attributes ...attribute.KeyValue) context.Context {
	SetAttributes(ctx, attributes...)
	return context.WithValue(ctx, ctxKey, attributes)
}
