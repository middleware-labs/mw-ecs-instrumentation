package main

import (
	"os"

	"github.com/middleware-labs/mw-ecs-instrumentation/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
