package telemetry

import (
	"context"
	"testing"

	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel/attribute"
)

func TestWithPropagatedAttributes(t *testing.T) {
	ctx := context.Background()
	ctx = WithPropagatedAttributes(ctx, attribute.KeyValue{Key: "test", Value: attribute.StringValue("test")})
	attrs, ok := ctx.Value(ctxKey).([]attribute.KeyValue)
	must.True(t, ok)
	must.Eq(t, []attribute.KeyValue{{Key: "test", Value: attribute.StringValue("test")}}, attrs)

	ctx = WithPropagatedAttributes(ctx, attribute.KeyValue{Key: "test2", Value: attribute.StringValue("test2")})
	attrs, ok = ctx.Value(ctxKey).([]attribute.KeyValue)
	must.True(t, ok)
	must.Eq(t, []attribute.KeyValue{
		{Key: "test", Value: attribute.StringValue("test")},
		{Key: "test2", Value: attribute.StringValue("test2")},
	}, attrs)
}
