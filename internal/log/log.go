package log

import (
	"context"
	"log/slog"
	"os"
)

const (
	LevelTrace = slog.Level(-8)
)

func Init() {
	logOptions := &slog.HandlerOptions{
		Level: FlagLogLevel.Value(),
		ReplaceAttr: func(groups []string, field slog.Attr) slog.Attr {
			if field.Key == slog.LevelKey && field.Value.Any().(slog.Level) == LevelTrace {
				field.Value = slog.StringValue("TRACE")
			}
			return field
		},
	}

	var handler slog.Handler
	if FlagLogFormat.Value() == "json" {
		handler = slog.NewJSONHandler(os.Stderr, logOptions)
	} else {
		handler = slog.NewTextHandler(os.Stderr, logOptions)
	}

	slog.SetDefault(slog.New(slogHandler{Handler: handler}))
}

func Handler() slog.Handler {
	return slog.Default().Handler()
}

func With(ctx context.Context, fields ...slog.Attr) context.Context {
	if contextFields, ok := ctx.Value(ctxKey).([]slog.Attr); ok {
		fields = append(contextFields, fields...)
	}
	return context.WithValue(ctx, ctxKey, fields)
}

func Trace(ctx context.Context, msg string, fields ...slog.Attr) {
	slog.LogAttrs(ctx, LevelTrace, msg, fields...)
}

func Debug(ctx context.Context, msg string, fields ...slog.Attr) {
	slog.LogAttrs(ctx, slog.LevelDebug, msg, fields...)
}

func Info(ctx context.Context, msg string, fields ...slog.Attr) {
	slog.LogAttrs(ctx, slog.LevelInfo, msg, fields...)
}

func Warn(ctx context.Context, msg string, fields ...slog.Attr) {
	slog.LogAttrs(ctx, slog.LevelWarn, msg, fields...)
}

func Error(ctx context.Context, msg string, fields ...slog.Attr) {
	slog.LogAttrs(ctx, slog.LevelError, msg, fields...)
}
