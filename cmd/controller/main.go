package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/gadget-inc/skipper/internal/cmd"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := cmd.NewController(nil).ExecuteContext(ctx); err != nil {
		log.Fatal(err)
	}
}
