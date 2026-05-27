package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/spf13/cobra"

	mwaws "github.com/middleware-labs/mw-ecs-instrumentation/internal/aws"
	"github.com/middleware-labs/mw-ecs-instrumentation/internal/instrument"
	"github.com/middleware-labs/mw-ecs-instrumentation/internal/prompt"
)

var instrumentFlags struct {
	taskDef     string
	mwApiKey    string
	mwTarget    string
	region      string
	serviceName string
	language    string
	enableAPM   bool
	enableLogs  bool
	output      string
	register    bool
	dryRun      bool
	all         bool
}

func init() {
	f := instrumentCmd.Flags()
	f.StringVar(&instrumentFlags.taskDef, "task-definition", "", "ECS task definition (family:revision or ARN)")
	f.StringVar(&instrumentFlags.mwApiKey, "mw-api-key", "", "Middleware API key (required)")
	f.StringVar(&instrumentFlags.mwTarget, "mw-target", "", "Middleware target URL (required)")
	f.StringVar(&instrumentFlags.region, "region", "", "AWS region")
	f.StringVar(&instrumentFlags.serviceName, "service-name", "", "MW_SERVICE_NAME for the app container")
	f.StringVar(&instrumentFlags.language, "language", "", "APM language: java, node, python")
	f.BoolVar(&instrumentFlags.enableAPM, "enable-apm", false, "Add APM init container")
	f.BoolVar(&instrumentFlags.enableLogs, "enable-logs", false, "Add FireLens log_router sidecar + awsfirelens log config")
	f.StringVar(&instrumentFlags.output, "output", "", "Output file path (default: <family>-instrumented.json)")
	f.BoolVar(&instrumentFlags.register, "register", false, "Register the new task definition with ECS")
	f.BoolVar(&instrumentFlags.dryRun, "dry-run", false, "Print modified task definition without writing or registering")
	f.BoolVar(&instrumentFlags.all, "all", false, "Discover and instrument all active task definition families")

	instrumentCmd.MarkFlagRequired("mw-api-key")
	instrumentCmd.MarkFlagRequired("mw-target")

	rootCmd.AddCommand(instrumentCmd)
}

var instrumentCmd = &cobra.Command{
	Use:   "instrument",
	Short: "Inject MW agent sidecar, APM init container, and FireLens log routing",
	Long: `Instrument an ECS task definition by injecting:
  - mw-agent sidecar container
  - APM init container (java/node/python) with shared volume
  - FireLens log_router sidecar with awsfirelens logConfiguration

Use --task-definition for a single task definition, or --all to discover and
instrument every active task definition family in the account.`,
	Example: `  # Interactive mode
  mw-ecs-instrument instrument --task-definition my-app:3 --mw-api-key abc --mw-target https://uid.middleware.io

  # Non-interactive, single task definition
  mw-ecs-instrument instrument --task-definition my-app:3 --mw-api-key abc --mw-target https://uid.middleware.io \
    --language java --enable-apm --enable-logs --register

  # Batch mode, all families
  mw-ecs-instrument instrument --all --mw-api-key abc --mw-target https://uid.middleware.io \
    --language node --enable-apm --enable-logs --dry-run`,
	RunE: runInstrument,
}

func runInstrument(cmd *cobra.Command, args []string) error {
	if instrumentFlags.taskDef == "" && !instrumentFlags.all {
		return fmt.Errorf("either --task-definition or --all is required")
	}
	if instrumentFlags.taskDef != "" && instrumentFlags.all {
		return fmt.Errorf("--task-definition and --all are mutually exclusive")
	}

	ctx := context.Background()
	client, err := mwaws.NewClient(ctx, instrumentFlags.region)
	if err != nil {
		return err
	}

	p, err := prompt.New()
	if err != nil {
		return err
	}
	defer p.Close()

	if instrumentFlags.all {
		return runBatchInstrument(ctx, client, p)
	}
	return runSingleInstrument(ctx, client, p, instrumentFlags.taskDef)
}

func runBatchInstrument(ctx context.Context, client *mwaws.Client, p *prompt.Prompter) error {
	fmt.Fprintln(os.Stderr, "\033[36m[INFO]\033[0m  Discovering task definition families ...")
	families, err := client.ListFamilies(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\033[32m[OK]\033[0m    Found %d families\n", len(families))

	interactive := !instrumentFlags.enableAPM && !instrumentFlags.enableLogs
	if interactive {
		resolveInteractiveFlags(p, "batch")
	}

	var succeeded, skipped, failed int
	for _, family := range families {
		fmt.Fprintf(os.Stderr, "\n\033[36m[INFO]\033[0m  Processing: %s\n", family)

		td, err := client.LatestTaskDefinition(ctx, family)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m %s: %v\n", family, err)
			failed++
			continue
		}

		alreadyInstrumented := instrument.HasContainer(td.ContainerDefinitions, instrument.ContainerMWAgent)
		if alreadyInstrumented {
			fmt.Fprintf(os.Stderr, "\033[33m[SKIP]\033[0m  %s already has mw-agent, skipping\n", family)
			skipped++
			continue
		}

		opts, decisions := buildOptionsNonInteractive(td)
		result := instrument.Patch(td, opts, decisions)
		if err := handleOutput(td, aws.ToString(td.Family), result, ctx, client); err != nil {
			fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m %s: %v\n", family, err)
			failed++
			continue
		}
		succeeded++
	}

	fmt.Fprintf(os.Stderr, "\n\033[1m── Batch Summary ──────────────────────────────────────\033[0m\n")
	fmt.Fprintf(os.Stderr, "  Succeeded: \033[32m%d\033[0m\n", succeeded)
	fmt.Fprintf(os.Stderr, "  Skipped:   \033[33m%d\033[0m\n", skipped)
	fmt.Fprintf(os.Stderr, "  Failed:    \033[31m%d\033[0m\n", failed)
	return nil
}

func runSingleInstrument(ctx context.Context, client *mwaws.Client, p *prompt.Prompter, taskDef string) error {
	fmt.Fprintf(os.Stderr, "\033[36m[INFO]\033[0m  Fetching task definition: %s ...\n", taskDef)
	td, err := client.DescribeTaskDefinition(ctx, taskDef)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "\033[32m[OK]\033[0m    Fetched task definition successfully")

	family := aws.ToString(td.Family)
	networkMode := string(td.NetworkMode)
	fmt.Fprintf(os.Stderr, "\033[36m[INFO]\033[0m  Family: %s  |  Network mode: %s\n", family, networkMode)

	fmt.Fprintln(os.Stderr, "\n\033[36m[INFO]\033[0m  Existing containers:")
	for _, c := range td.ContainerDefinitions {
		fmt.Fprintf(os.Stderr, "  - \033[36m%s\033[0m\n", aws.ToString(c.Name))
	}
	fmt.Fprintln(os.Stderr)

	interactive := !instrumentFlags.enableAPM && !instrumentFlags.enableLogs && instrumentFlags.language == ""
	var opts instrument.Options
	var decisions instrument.ReplaceDecision

	if interactive {
		opts, decisions = resolveInteractive(p, td, family)
	} else {
		opts, decisions = buildOptionsNonInteractive(td)
	}

	result := instrument.Patch(td, opts, decisions)
	return handleOutput(td, family, result, ctx, client)
}

func resolveInteractiveFlags(p *prompt.Prompter, _ string) {
	if !instrumentFlags.enableAPM {
		instrumentFlags.enableAPM = p.AskYesNo("Enable APM auto-instrumentation (init container)?", true)
	}
	if instrumentFlags.enableAPM && instrumentFlags.language == "" {
		langs := []string{"java", "node", "python"}
		_, instrumentFlags.language = p.AskChoice("Select the application language for APM:", langs)
	}
	if !instrumentFlags.enableLogs {
		instrumentFlags.enableLogs = p.AskYesNo("Add FireLens log routing (Fluent Bit sidecar + awsfirelens on app)?", true)
	}
}

func resolveInteractive(p *prompt.Prompter, td *ecstypes.TaskDefinition, family string) (instrument.Options, instrument.ReplaceDecision) {
	var decisions instrument.ReplaceDecision
	enableAPM := instrumentFlags.enableAPM
	enableLogs := instrumentFlags.enableLogs

	if instrument.HasContainer(td.ContainerDefinitions, instrument.ContainerMWAgent) {
		fmt.Fprintln(os.Stderr, "\033[33m[WARN]\033[0m  Task definition already has an 'mw-agent' sidecar container.")
		decisions.ReplaceMWAgent = p.AskYesNo("Replace it?", false)
	} else {
		decisions.ReplaceMWAgent = true
	}

	if instrument.HasContainer(td.ContainerDefinitions, instrument.ContainerInit) {
		fmt.Fprintln(os.Stderr, "\033[33m[WARN]\033[0m  Task definition already has an 'instrumentation-init' container.")
		decisions.ReplaceInit = p.AskYesNo("Replace it?", false)
		if !decisions.ReplaceInit {
			enableAPM = false
		}
	}

	if instrument.HasContainer(td.ContainerDefinitions, instrument.ContainerFirelens) {
		fmt.Fprintln(os.Stderr, "\033[33m[WARN]\033[0m  Task definition already has a 'log_router' container.")
		decisions.ReplaceFirelens = p.AskYesNo("Replace it?", false)
		if !decisions.ReplaceFirelens {
			enableLogs = false
		}
	}

	if !enableAPM {
		enableAPM = p.AskYesNo("Enable APM auto-instrumentation (init container)?", true)
	}

	lang := instrument.Language(instrumentFlags.language)
	if enableAPM && !lang.Valid() {
		langs := []string{"java", "node", "python"}
		_, chosen := p.AskChoice("Select the application language for APM:", langs)
		lang = instrument.Language(chosen)
	}

	if !enableLogs {
		enableLogs = p.AskYesNo("Add FireLens log routing (Fluent Bit sidecar + awsfirelens on app)?", true)
	}

	serviceName := instrumentFlags.serviceName
	if serviceName == "" && enableAPM {
		serviceName = p.AskString("MW_SERVICE_NAME for the app container", family)
	}

	return instrument.Options{
		MWApiKey:    instrumentFlags.mwApiKey,
		MWTarget:    instrumentFlags.mwTarget,
		ServiceName: serviceName,
		Language:    lang,
		EnableAPM:   enableAPM,
		EnableLogs:  enableLogs,
	}, decisions
}

func buildOptionsNonInteractive(td *ecstypes.TaskDefinition) (instrument.Options, instrument.ReplaceDecision) {
	family := aws.ToString(td.Family)
	serviceName := instrumentFlags.serviceName
	if serviceName == "" {
		serviceName = family
	}

	return instrument.Options{
		MWApiKey:    instrumentFlags.mwApiKey,
		MWTarget:    instrumentFlags.mwTarget,
		ServiceName: serviceName,
		Language:    instrument.Language(instrumentFlags.language),
		EnableAPM:   instrumentFlags.enableAPM,
		EnableLogs:  instrumentFlags.enableLogs,
	}, instrument.ReplaceDecision{
		ReplaceMWAgent:  true,
		ReplaceInit:     true,
		ReplaceFirelens: true,
	}
}

func handleOutput(td *ecstypes.TaskDefinition, family string, result instrument.PatchResult, ctx context.Context, client *mwaws.Client) error {
	printSummary(family, result)

	data, err := instrument.SerializeTaskDefinition(td)
	if err != nil {
		return fmt.Errorf("marshaling task definition: %w", err)
	}

	if instrumentFlags.dryRun {
		fmt.Fprintln(os.Stderr, "\033[36m[INFO]\033[0m  Dry run — printing modified task definition to stdout:")
		_, err := os.Stdout.Write(append(data, '\n'))
		return err
	}

	output := instrumentFlags.output
	if output == "" {
		output = family + "-instrumented.json"
	}

	if err := os.WriteFile(output, append(data, '\n'), 0644); err != nil {
		return fmt.Errorf("writing output file: %w", err)
	}
	fmt.Fprintf(os.Stderr, "\033[32m[OK]\033[0m    Written to: %s\n", output)

	if instrumentFlags.register {
		fmt.Fprintln(os.Stderr, "\033[36m[INFO]\033[0m  Registering new task definition revision ...")
		registered, err := client.RegisterTaskDefinition(ctx, td)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "\033[32m[OK]\033[0m    Registered: %s (revision %d)\n",
			aws.ToString(registered.TaskDefinitionArn), registered.Revision)
	}

	return nil
}

func printSummary(family string, result instrument.PatchResult) {
	fmt.Fprintln(os.Stderr, "\n\033[1m── Summary ──────────────────────────────────────────\033[0m")
	fmt.Fprintf(os.Stderr, "  Family:           \033[36m%s\033[0m\n", family)

	if result.AddedMWAgent {
		fmt.Fprintln(os.Stderr, "  MW Agent sidecar: \033[32madded\033[0m")
	} else if result.SkippedMWAgent {
		fmt.Fprintln(os.Stderr, "  MW Agent sidecar: \033[33mkept existing\033[0m")
	}

	if result.AddedInit {
		fmt.Fprintln(os.Stderr, "  APM init:         \033[32madded\033[0m")
	} else if result.SkippedInit {
		fmt.Fprintln(os.Stderr, "  APM init:         \033[33mkept existing\033[0m")
	}

	if result.AddedFirelens {
		fmt.Fprintln(os.Stderr, "  FireLens logs:    \033[32madded\033[0m")
	} else if result.SkippedFirelens {
		fmt.Fprintln(os.Stderr, "  FireLens logs:    \033[33mkept existing\033[0m")
	}

	fmt.Fprintf(os.Stderr, "  Task CPU:         \033[36m%d\033[0m\n", result.TotalCPU)
	fmt.Fprintf(os.Stderr, "  Task Memory:      \033[36m%d\033[0m\n", result.TotalMemory)
}
