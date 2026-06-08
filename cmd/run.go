package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	mwaws "github.com/middleware-labs/mw-ecs-instrumentation/internal/aws"
	"github.com/middleware-labs/mw-ecs-instrumentation/internal/prompt"
)

var runFlags struct {
	taskDefs   []string
	cluster    string
	launchType string
	subnets    string
	secGroups  string
	region     string
}

func init() {
	f := runCmd.Flags()
	f.StringSliceVar(&runFlags.taskDefs, "task-definition", nil, "ECS task definition (family:revision or ARN), repeatable or comma-separated")
	f.StringVar(&runFlags.cluster, "cluster", "", "ECS cluster name (interactive if omitted)")
	f.StringVar(&runFlags.launchType, "launch-type", "", "Launch type: EC2 or FARGATE (interactive if omitted)")
	f.StringVar(&runFlags.subnets, "subnets", "", "Subnet IDs, comma-separated (required for FARGATE)")
	f.StringVar(&runFlags.secGroups, "security-groups", "", "Security group IDs, comma-separated (required for FARGATE)")
	f.StringVar(&runFlags.region, "region", "", "AWS region")

	runCmd.MarkFlagRequired("task-definition")
	rootCmd.AddCommand(runCmd)
}

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run ECS tasks with given task definitions",
	Long: `Run ECS tasks using the specified task definitions. Supports one or more
task definitions (repeatable or comma-separated). Useful for testing
instrumented task definitions after registration.`,
	Example: `  # Single task definition
  mw-ecs-instrument run --task-definition my-app:6

  # Multiple task definitions
  mw-ecs-instrument run --task-definition my-app:6 --task-definition my-api:3

  # Comma-separated
  mw-ecs-instrument run --task-definition my-app:6,my-api:3 --cluster my-cluster --launch-type EC2`,
	RunE: runRunCmd,
}

func runRunCmd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	client, err := mwaws.NewClient(ctx, runFlags.region)
	if err != nil {
		return err
	}

	cluster := runFlags.cluster
	launchType := runFlags.launchType
	subnets := runFlags.subnets
	secGroups := runFlags.secGroups

	interactive := cluster == "" || launchType == ""
	if interactive {
		p, err := prompt.New()
		if err != nil {
			return err
		}
		defer p.Close()

		if cluster == "" {
			cluster = p.AskString("ECS cluster name", "default")
		}
		if launchType == "" {
			_, launchType = p.AskChoice("Select the launch type:", []string{"EC2", "FARGATE"})
		}
		if launchType == "FARGATE" {
			if subnets == "" {
				subnets = p.AskString("Subnet IDs (comma-separated)", "")
			}
			if secGroups == "" {
				secGroups = p.AskString("Security group IDs (comma-separated)", "")
			}
		}
	}

	for _, taskDef := range runFlags.taskDefs {
		fmt.Fprintf(os.Stderr, "\033[36m[INFO]\033[0m  Running task %s on cluster %q (launch type: %s) ...\n",
			taskDef, cluster, launchType)

		taskArn, err := client.RunTask(ctx, mwaws.RunTaskInput{
			Cluster:        cluster,
			TaskDefinition: taskDef,
			LaunchType:     launchType,
			Subnets:        subnets,
			SecurityGroups: secGroups,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m %s: %v\n", taskDef, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "\033[32m[OK]\033[0m    Task started: %s\n", taskArn)
	}
	return nil
}
