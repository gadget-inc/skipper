package main

import (
	"context"
	"os"

	"github.com/gadget-inc/fusion/cmd"
	"github.com/gadget-inc/fusion/internal/key"
	"github.com/gadget-inc/fusion/internal/log"
)

func main() {
	ctx := context.Background()
	err := cmd.Execute(ctx)
	if err != nil {
		log.Error(ctx, "failed to execute command", key.Error.Field(err))
		os.Exit(1)
	}
}
