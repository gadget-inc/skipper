package key

import (
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
)

type boolKey struct{ key }

var _ Key[bool] = boolKey{}

func (k boolKey) Field(value bool) slog.Attr {
	return slog.Bool(k.underscored, value)
}

func (k boolKey) Attribute(value bool) attribute.KeyValue {
	return attribute.Bool(k.name, value)
}

type stringKey struct{ key }

func (k stringKey) Field(value string) slog.Attr {
	return slog.String(string(k.underscored), value)
}

func (k stringKey) Attribute(value string) attribute.KeyValue {
	return attribute.String(string(k.name), value)
}

type stringSliceKey struct{ key }

var _ Key[[]string] = stringSliceKey{}

func (k stringSliceKey) Field(value []string) slog.Attr {
	return slog.Any(string(k.underscored), value)
}

func (k stringSliceKey) Attribute(value []string) attribute.KeyValue {
	return attribute.StringSlice(string(k.name), value)
}

type intKey struct{ key }

var _ Key[int] = intKey{}

func (k intKey) Field(value int) slog.Attr {
	return slog.Int(string(k.underscored), value)
}

func (k intKey) Attribute(value int) attribute.KeyValue {
	return attribute.Int(string(k.name), value)
}

type int64Key struct{ key }

var _ Key[int64] = int64Key{}

func (k int64Key) Field(value int64) slog.Attr {
	return slog.Int64(string(k.underscored), value)
}

func (k int64Key) Attribute(value int64) attribute.KeyValue {
	return attribute.Int64(string(k.name), value)
}

type durationKey struct{ key }

var _ Key[time.Duration] = durationKey{}

func (k durationKey) Field(value time.Duration) slog.Attr {
	return slog.Duration(string(k.underscored), value)
}

func (k durationKey) Attribute(value time.Duration) attribute.KeyValue {
	return attribute.Float64(string(k.name), float64(value.Milliseconds()))
}

type float64Key struct{ key }

var _ Key[float64] = float64Key{}

func (k float64Key) Field(value float64) slog.Attr {
	return slog.Float64(string(k.underscored), value)
}

func (k float64Key) Attribute(value float64) attribute.KeyValue {
	return attribute.Float64(string(k.name), value)
}

type timeKey struct{ key }

var _ Key[time.Time] = timeKey{}

func (k timeKey) Field(value time.Time) slog.Attr {
	return slog.Time(string(k.underscored), value)
}

func (k timeKey) Attribute(value time.Time) attribute.KeyValue {
	return attribute.String(string(k.name), value.Format(time.RFC3339))
}

type stringerKey struct{ key }

var _ Key[fmt.Stringer] = stringerKey{}

func (k stringerKey) Field(value fmt.Stringer) slog.Attr {
	return slog.String(string(k.underscored), value.String())
}

func (k stringerKey) Attribute(value fmt.Stringer) attribute.KeyValue {
	return attribute.String(string(k.name), value.String())
}

type errorKey struct{ key }

var _ Key[error] = errorKey{}

func (k errorKey) Field(value error) slog.Attr {
	return slog.String(string(k.underscored), value.Error())
}

func (k errorKey) Attribute(value error) attribute.KeyValue {
	return attribute.String(string(k.name), value.Error())
}

type Value interface {
	LogValue() slog.Value
	AttributeValue() attribute.Value
}

type valueKey struct{ key }

var _ Key[Value] = valueKey{}

func (k valueKey) Field(value Value) slog.Attr {
	return slog.Any(string(k.underscored), value.LogValue())
}

func (k valueKey) Attribute(value Value) attribute.KeyValue {
	return attribute.KeyValue{Key: attribute.Key(k.name), Value: value.AttributeValue()}
}
