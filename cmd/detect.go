package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/middleware-labs/mw-ecs-instrumentation/internal/instrument"
)

func init() {
	rootCmd.AddCommand(detectCmd)
}

var detectCmd = &cobra.Command{
	Use:   "detect <image>",
	Short: "Auto-detect language and libc from a container image",
	Long: `Inspect a container image's metadata (Entrypoint, Cmd, Env) to detect
the runtime language and C library variant. Useful for verifying auto-detection
before running instrument.

Supports ECR, Docker Hub, GHCR, and any OCI-compliant registry.
Uses credentials from ~/.docker/config.json automatically if available.`,
	Example: `  mw-ecs-instrument detect docker.io/advait11/demo-node-app
  mw-ecs-instrument detect nginx:alpine
  mw-ecs-instrument detect ghcr.io/org/repo:v1
  mw-ecs-instrument detect 123456789.dkr.ecr.us-east-1.amazonaws.com/my-app:latest`,
	Args: cobra.ExactArgs(1),
	RunE: runDetect,
}

func runDetect(cmd *cobra.Command, args []string) error {
	imageURI := args[0]
	ctx := cmd.Context()

	fmt.Fprintf(os.Stderr, "\033[36m➜\033[0m  Inspecting image: %s\n", imageURI)

	lang, libc, err := instrument.DetectLanguage(ctx, imageURI)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\033[31m✘\033[0m  Could not detect language from image: %v\n", err)
		return nil
	}

	if !lang.Valid() {
		fmt.Fprintf(os.Stderr, "\033[33m!\033[0m  Could not determine language from image metadata\n")
		return nil
	}

	fmt.Fprintf(os.Stderr, "\033[32m✔\033[0m  Language: %s\n", string(lang))
	fmt.Fprintf(os.Stderr, "\033[32m✔\033[0m  LibC:     %s\n", string(libc))
	return nil
}
