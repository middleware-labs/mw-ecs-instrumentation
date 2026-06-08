package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"

	mwaws "github.com/middleware-labs/mw-ecs-instrumentation/internal/aws"
	"github.com/middleware-labs/mw-ecs-instrumentation/internal/prompt"
)

var rollbackFlags struct {
	taskDefs []string
	region   string
}

func init() {
	rollbackCmd.Flags().StringSliceVar(&rollbackFlags.taskDefs, "task-definition", nil, "ECS task definition (family:revision), repeatable or comma-separated")
	rollbackCmd.Flags().StringVar(&rollbackFlags.region, "region", "", "AWS region")
	rollbackCmd.MarkFlagRequired("task-definition")
	rootCmd.AddCommand(rollbackCmd)
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Register the previous revision of task definitions (before instrumentation)",
	Long: `Roll back to the previous revision of one or more task definitions. This
fetches the revision before the specified one and re-registers it as a new
revision.

Useful for undoing an instrumentation if something went wrong.`,
	Example: `  # Roll back a single task definition
  mw-ecs-instrument rollback --task-definition my-app:5

  # Roll back multiple task definitions
  mw-ecs-instrument rollback --task-definition my-app:5 --task-definition my-api:3

  # Comma-separated
  mw-ecs-instrument rollback --task-definition my-app:5,my-api:3`,
	RunE: runRollback,
}

func runRollback(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	client, err := mwaws.NewClient(ctx, rollbackFlags.region)
	if err != nil {
		return err
	}

	p, err := prompt.New()
	if err != nil {
		return err
	}
	defer p.Close()

	for _, taskDef := range rollbackFlags.taskDefs {
		parts := strings.SplitN(taskDef, ":", 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m %s: must be in family:revision format\n", taskDef)
			continue
		}
		family := parts[0]
		rev, err := strconv.Atoi(parts[1])
		if err != nil || rev < 2 {
			fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m %s: revision must be a number >= 2 to roll back\n", taskDef)
			continue
		}

		prevRef := fmt.Sprintf("%s:%d", family, rev-1)

		fmt.Fprintf(os.Stderr, "\033[36m[INFO]\033[0m  Fetching previous revision: %s ...\n", prevRef)
		td, err := client.DescribeTaskDefinition(ctx, prevRef)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m could not fetch %s: %v\n", prevRef, err)
			continue
		}

		fmt.Fprintf(os.Stderr, "\033[32m[OK]\033[0m    Found revision %d with %d containers\n", td.Revision, len(td.ContainerDefinitions))
		fmt.Fprintln(os.Stderr, "\n\033[36m[INFO]\033[0m  Containers in previous revision:")
		for _, c := range td.ContainerDefinitions {
			fmt.Fprintf(os.Stderr, "  - \033[36m%s\033[0m\n", aws.ToString(c.Name))
		}

		if !p.AskYesNo(fmt.Sprintf("Re-register %s as a new revision?", prevRef), false) {
			fmt.Fprintln(os.Stderr, "\033[33m[SKIP]\033[0m  Skipped.")
			continue
		}

		fmt.Fprintln(os.Stderr, "\033[36m[INFO]\033[0m  Registering ...")
		registered, err := client.RegisterTaskDefinition(ctx, td)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m %s: %v\n", taskDef, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "\033[32m[OK]\033[0m    Registered: %s (revision %d)\n",
			aws.ToString(registered.TaskDefinitionArn), registered.Revision)
	}
	return nil
}
