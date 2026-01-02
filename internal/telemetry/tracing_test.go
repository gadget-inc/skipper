package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/gadget-inc/skipper/internal/key"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"gotest.tools/v3/assert"
)

func TestWithPropagatedAttributes(t *testing.T) {
	ctx := context.Background()
	ctx = WithPropagatedAttributes(ctx, attribute.KeyValue{Key: "test", Value: attribute.StringValue("test")})
	attrs, ok := ctx.Value(attrsCtxKey).([]attribute.KeyValue)
	assert.Assert(t, ok)
	assert.DeepEqual(t, attrs, []attribute.KeyValue{{Key: "test", Value: attribute.StringValue("test")}}, cmpopts.EquateComparable(attribute.KeyValue{}))

	ctx = WithPropagatedAttributes(ctx, attribute.KeyValue{Key: "test2", Value: attribute.StringValue("test2")})
	attrs, ok = ctx.Value(attrsCtxKey).([]attribute.KeyValue)
	assert.Assert(t, ok)
	assert.DeepEqual(t, attrs, []attribute.KeyValue{
		{Key: "test", Value: attribute.StringValue("test")},
		{Key: "test2", Value: attribute.StringValue("test2")},
	}, cmpopts.EquateComparable(attribute.KeyValue{}))
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
	record.AddAttrs(key.Error.Slog(err))

	logHook(ctx, &record)

	assert.Assert(t, testSpan.err == err)
	assert.Assert(t, testSpan.statusCode == codes.Error)
	assert.Assert(t, testSpan.description == err.Error())
}
