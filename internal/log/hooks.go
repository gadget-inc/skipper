package log

import (
	"context"
	"log/slog"
)

type Hook func(ctx context.Context, record *slog.Record)

var hooks []Hook

func AddHook(hook Hook) {
	hooks = append(hooks, hook)
}
