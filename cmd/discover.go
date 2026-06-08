package cmd

import (
	"fmt"
	"os"
	"strings"

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
container, log configuration) and the launch type.`,
	Example: `  mw-ecs-instrument discover
  mw-ecs-instrument discover --region us-west-2`,
	RunE: runDiscover,
}

type discoveryRow struct {
	name       string
	hasMW      bool
	hasInit    bool
	logConfig  instrument.LogConfigType
	launchType string
}

func runDiscover(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
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
	var fetchErrors []string

	for _, family := range families {
		td, err := client.LatestTaskDefinition(ctx, family)
		if err != nil {
			fetchErrors = append(fetchErrors, fmt.Sprintf("%s: %v", family, err))
			continue
		}

		rows = append(rows, discoveryRow{
			name:       fmt.Sprintf("%s:%d", aws.ToString(td.Family), td.Revision),
			hasMW:      instrument.HasContainer(td.ContainerDefinitions, instrument.ContainerMWAgent),
			hasInit:    instrument.HasContainer(td.ContainerDefinitions, instrument.ContainerInit),
			logConfig:  instrument.DetectLogConfig(td.ContainerDefinitions),
			launchType: instrument.DetectLaunchType(td.Compatibilities),
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
	colLog := "LOG CONFIG"
	colLaunch := "LAUNCH"

	fmt.Fprintf(os.Stderr, "  \033[1m%-*s   %-*s   %-*s   %-*s   %-*s\033[0m\n",
		maxName, "FAMILY", len(colMW), colMW, len(colInit), colInit, len(colLog), colLog, len(colLaunch), colLaunch)
	fmt.Fprintf(os.Stderr, "  %s   %s   %s   %s   %s\n",
		strings.Repeat("─", maxName), strings.Repeat("─", len(colMW)), strings.Repeat("─", len(colInit)),
		strings.Repeat("─", len(colLog)), strings.Repeat("─", len(colLaunch)))

	var instrumented, notInstrumented int
	for _, r := range rows {
		fmt.Fprintf(os.Stderr, "  %-*s   %s   %s   %s   %s\n",
			maxName, r.name,
			boolCell(r.hasMW, len(colMW)),
			boolCell(r.hasInit, len(colInit)),
			logConfigCell(r.logConfig, len(colLog)),
			textCell(r.launchType, len(colLaunch)),
		)
		if r.hasMW {
			instrumented++
		} else {
			notInstrumented++
		}
	}

	for _, e := range fetchErrors {
		fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m %s\n", e)
	}

	fmt.Fprintf(os.Stderr, "\n  Instrumented: \033[32m%d\033[0m  |  Not instrumented: \033[33m%d\033[0m\n", instrumented, notInstrumented)
	return nil
}

func boolCell(present bool, colWidth int) string {
	if present {
		return padColored("\033[32m", "✔ yes", colWidth)
	}
	return padColored("\033[33m", "✘ no", colWidth)
}

func logConfigCell(lc instrument.LogConfigType, colWidth int) string {
	text := string(lc)
	switch lc {
	case instrument.LogConfigFirelens:
		return padColored("\033[32m", text, colWidth)
	case instrument.LogConfigCloudWatch:
		return padColored("\033[36m", text, colWidth)
	case instrument.LogConfigNone:
		return padColored("\033[33m", text, colWidth)
	default:
		return padColored("\033[33m", text, colWidth)
	}
}

func textCell(text string, colWidth int) string {
	return padColored("\033[36m", text, colWidth)
}

func padColored(color, text string, colWidth int) string {
	visLen := len([]rune(text))
	pad := colWidth - visLen
	if pad < 0 {
		pad = 0
	}
	return fmt.Sprintf("%s%s\033[0m%s", color, text, strings.Repeat(" ", pad))
}
