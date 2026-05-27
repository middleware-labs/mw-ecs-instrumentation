package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/spf13/cobra"

	mwaws "github.com/middleware-labs/mw-ecs-instrumentation/internal/aws"
	"github.com/middleware-labs/mw-ecs-instrumentation/internal/instrument"
)

var discoverFlags struct {
	region string
}

func init() {
	discoverCmd.Flags().StringVar(&discoverFlags.region, "region", "", "AWS region")
	rootCmd.AddCommand(discoverCmd)
}

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "List all active task definition families and their instrumentation status",
	Long: `Discover all active ECS task definition families in the account and show
whether each one already has Middleware instrumentation (mw-agent, init
container, log_router).`,
	Example: `  mw-ecs-instrument discover
  mw-ecs-instrument discover --region us-west-2`,
	RunE: runDiscover,
}

type discoveryRow struct {
	name    string
	hasMW   bool
	hasInit bool
	hasFL   bool
}

func runDiscover(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, err := mwaws.NewClient(ctx, discoverFlags.region)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "\033[36m[INFO]\033[0m  Discovering task definition families ...")
	families, err := client.ListFamilies(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\033[32m[OK]\033[0m    Found %d families\n\n", len(families))

	var rows []discoveryRow
	var errors []string

	for _, family := range families {
		td, err := client.LatestTaskDefinition(ctx, family)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", family, err))
			continue
		}

		rows = append(rows, discoveryRow{
			name:    fmt.Sprintf("%s:%d", aws.ToString(td.Family), td.Revision),
			hasMW:   instrument.HasContainer(td.ContainerDefinitions, instrument.ContainerMWAgent),
			hasInit: instrument.HasContainer(td.ContainerDefinitions, instrument.ContainerInit),
			hasFL:   instrument.HasContainer(td.ContainerDefinitions, instrument.ContainerFirelens),
		})
	}

	maxName := len("FAMILY")
	for _, r := range rows {
		if len(r.name) > maxName {
			maxName = len(r.name)
		}
	}

	colMW := "MW-AGENT"
	colInit := "APM-INIT"
	colFL := "FIRELENS"

	w := tabwriter.NewWriter(os.Stderr, 0, 0, 3, ' ', 0)
	fmt.Fprintf(w, "  \033[1m%-*s   %-8s   %-8s   %-8s\033[0m\n", maxName, "FAMILY", colMW, colInit, colFL)
	fmt.Fprintf(w, "  %s   %s   %s   %s\n", strings.Repeat("─", maxName), strings.Repeat("─", len(colMW)), strings.Repeat("─", len(colInit)), strings.Repeat("─", len(colFL)))

	var instrumented, notInstrumented int
	for _, r := range rows {
		fmt.Fprintf(w, "  %-*s   %s   %s   %s\n",
			maxName, r.name,
			statusCell(r.hasMW, len(colMW)),
			statusCell(r.hasInit, len(colInit)),
			statusCell(r.hasFL, len(colFL)),
		)
		if r.hasMW {
			instrumented++
		} else {
			notInstrumented++
		}
	}
	w.Flush()

	for _, e := range errors {
		fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m %s\n", e)
	}

	fmt.Fprintf(os.Stderr, "\n  Instrumented: \033[32m%d\033[0m  |  Not instrumented: \033[33m%d\033[0m\n", instrumented, notInstrumented)
	return nil
}

func statusCell(present bool, colWidth int) string {
	// "✔ yes" = 5 visible chars, "✘ no " = 5 visible chars (with trailing space)
	// Pad both to colWidth visible characters for alignment.
	if present {
		text := "✔ yes"
		return fmt.Sprintf("\033[32m%s\033[0m%s", text, strings.Repeat(" ", colWidth-5))
	}
	text := "✘ no"
	return fmt.Sprintf("\033[33m%s\033[0m%s", text, strings.Repeat(" ", colWidth-4))
}
