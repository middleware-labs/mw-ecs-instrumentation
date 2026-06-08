package cmd

import (
	"context"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "mw-ecs-instrument",
	Short: "Middleware ECS auto-instrumentation CLI",
	Long:  "Inject Middleware APM and log routing into AWS ECS task definitions.",
}

func Execute(ctx context.Context) error {
	return rootCmd.ExecuteContext(ctx)
}
