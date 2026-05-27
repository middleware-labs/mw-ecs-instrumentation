package cmd

import (
	"context"
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
	taskDef string
	region  string
}

func init() {
	rollbackCmd.Flags().StringVar(&rollbackFlags.taskDef, "task-definition", "", "ECS task definition (family:revision)")
	rollbackCmd.Flags().StringVar(&rollbackFlags.region, "region", "", "AWS region")
	rollbackCmd.MarkFlagRequired("task-definition")
	rootCmd.AddCommand(rollbackCmd)
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Register the previous revision of a task definition (before instrumentation)",
	Long: `Roll back to the previous revision of a task definition. This fetches the
revision before the specified one and re-registers it as a new revision.

Useful for undoing an instrumentation if something went wrong.`,
	Example: `  # Roll back from revision 5 to revision 4
  mw-ecs-instrument rollback --task-definition my-app:5`,
	RunE: runRollback,
}

func runRollback(cmd *cobra.Command, args []string) error {
	parts := strings.SplitN(rollbackFlags.taskDef, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("--task-definition must be in family:revision format")
	}
	family := parts[0]
	rev, err := strconv.Atoi(parts[1])
	if err != nil || rev < 2 {
		return fmt.Errorf("revision must be a number >= 2 to roll back")
	}

	prevRef := fmt.Sprintf("%s:%d", family, rev-1)

	ctx := context.Background()
	client, err := mwaws.NewClient(ctx, rollbackFlags.region)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\033[36m[INFO]\033[0m  Fetching previous revision: %s ...\n", prevRef)
	td, err := client.DescribeTaskDefinition(ctx, prevRef)
	if err != nil {
		return fmt.Errorf("could not fetch %s: %w", prevRef, err)
	}

	fmt.Fprintf(os.Stderr, "\033[32m[OK]\033[0m    Found revision %d with %d containers\n", td.Revision, len(td.ContainerDefinitions))
	fmt.Fprintln(os.Stderr, "\n\033[36m[INFO]\033[0m  Containers in previous revision:")
	for _, c := range td.ContainerDefinitions {
		fmt.Fprintf(os.Stderr, "  - \033[36m%s\033[0m\n", aws.ToString(c.Name))
	}

	p, err := prompt.New()
	if err != nil {
		return err
	}
	defer p.Close()
	if !p.AskYesNo(fmt.Sprintf("Re-register %s as a new revision?", prevRef), false) {
		fmt.Fprintln(os.Stderr, "Aborted.")
		return nil
	}

	fmt.Fprintln(os.Stderr, "\033[36m[INFO]\033[0m  Registering ...")
	registered, err := client.RegisterTaskDefinition(ctx, td)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\033[32m[OK]\033[0m    Registered: %s (revision %d)\n",
		aws.ToString(registered.TaskDefinitionArn), registered.Revision)
	return nil
}
