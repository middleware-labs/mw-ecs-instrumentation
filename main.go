package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/middleware-labs/mw-ecs-instrumentation/cmd"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := cmd.Execute(ctx); err != nil {
		os.Exit(1)
	}
}
