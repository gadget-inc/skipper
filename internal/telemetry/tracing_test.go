package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/shoenig/test/must"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestWithPropagatedAttributes(t *testing.T) {
	ctx := context.Background()
	ctx = WithPropagatedAttributes(ctx, attribute.KeyValue{Key: "test", Value: attribute.StringValue("test")})
	attrs, ok := ctx.Value(attrsCtxKey).([]attribute.KeyValue)
	must.True(t, ok)
	must.Eq(t, []attribute.KeyValue{{Key: "test", Value: attribute.StringValue("test")}}, attrs)

	ctx = WithPropagatedAttributes(ctx, attribute.KeyValue{Key: "test2", Value: attribute.StringValue("test2")})
	attrs, ok = ctx.Value(attrsCtxKey).([]attribute.KeyValue)
	must.True(t, ok)
	must.Eq(t, []attribute.KeyValue{
		{Key: "test", Value: attribute.StringValue("test")},
		{Key: "test2", Value: attribute.StringValue("test2")},
	}, attrs)
}

type testSpan struct {
	noop.Span
	err         error
	statusCode  codes.Code
	description string
}

func (s *testSpan) IsRecording() bool {
	return true
}

func (s *testSpan) RecordError(err error, _ ...trace.EventOption) {
	s.err = err
}

func (s *testSpan) SetStatus(code codes.Code, description string) {
	s.statusCode = code
	s.description = description
}

func TestLogHook(t *testing.T) {
	testSpan := &testSpan{}
	ctx := trace.ContextWithSpan(context.Background(), testSpan)

	err := errors.New("test")
	record := slog.NewRecord(time.Now(), slog.LevelError, "failed to do something", 0)
	record.AddAttrs(key.Error.Field(err))

	logHook(ctx, &record)

	must.Eq(t, err, testSpan.err)
	must.Eq(t, codes.Error, testSpan.statusCode)
	must.Eq(t, err.Error(), testSpan.description)
}
