package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/gadget-inc/fusion/cmd"
)

func main() {
	ctx := context.Background()
	err := cmd.Execute(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to execute command", slog.Any("error", err))
		os.Exit(1)
	}
}
