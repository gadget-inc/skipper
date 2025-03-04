package main

import (
	"context"
	"log"

	"github.com/gadget-inc/fusion/internal/cmd"
)

func main() {
	ctx := context.Background()
	err := cmd.Run(ctx)
	if err != nil {
		log.Fatal(err)
	}
}
