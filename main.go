package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/gadget-inc/fusion/internal/cmd"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cmd := cmd.NewRoot()
	if err := cmd.ExecuteContext(ctx); err != nil {
		log.Fatal(err)
	}
}
