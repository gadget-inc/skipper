package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/gadget-inc/fusion/cmd"
	"github.com/gadget-inc/fusion/internal/key"
)

func main() {
	ctx := context.Background()
	err := cmd.Execute(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to execute command", key.Error.Field(err))
		os.Exit(1)
	}
}
