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
	taskDefs    []string
	mwApiKey    string
	mwTarget    string
	region      string
	serviceName string
	language    string
	enableAPM   bool
	enableLogs  bool
	fargate     bool
	output      string
	register    bool
	run         bool
	cluster     string
	subnets     string
	secGroups   string
	dryRun      bool
	all         bool
}

func init() {
	f := instrumentCmd.Flags()
	f.StringSliceVar(&instrumentFlags.taskDefs, "task-definition", nil, "ECS task definition (family:revision or ARN), repeatable or comma-separated")
	f.StringVar(&instrumentFlags.mwApiKey, "mw-api-key", "", "Middleware API key (required)")
	f.StringVar(&instrumentFlags.mwTarget, "mw-target", "", "Middleware target URL (required)")
	f.StringVar(&instrumentFlags.region, "region", "", "AWS region")
	f.StringVar(&instrumentFlags.serviceName, "service-name", "", "MW_SERVICE_NAME for the app container")
	f.StringVar(&instrumentFlags.language, "language", "", "APM language: all, java, node, python")
	f.BoolVar(&instrumentFlags.enableAPM, "enable-apm", false, "Add APM init container")
	f.BoolVar(&instrumentFlags.enableLogs, "enable-logs", false, "Add FireLens log_router sidecar + awsfirelens log config")
	f.BoolVar(&instrumentFlags.fargate, "fargate", false, "Configure for Fargate (awsvpc network mode)")
	f.StringVar(&instrumentFlags.output, "output", "", "Output file path (default: <family>-instrumented.json)")
	f.BoolVar(&instrumentFlags.register, "register", false, "Register the new task definition with ECS")
	f.BoolVar(&instrumentFlags.run, "run", false, "Run a task after registering (requires --register, skips prompt)")
	f.StringVar(&instrumentFlags.cluster, "cluster", "", "ECS cluster for --run (interactive if omitted)")
	f.StringVar(&instrumentFlags.subnets, "subnets", "", "Subnet IDs for Fargate --run, comma-separated")
	f.StringVar(&instrumentFlags.secGroups, "security-groups", "", "Security group IDs for Fargate --run, comma-separated")
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

Use --task-definition for one or more task definitions (repeatable or comma-separated),
or --all to discover and instrument every active task definition family in the account.`,
	Example: `  # Single task definition
  mw-ecs-instrument instrument --task-definition my-app:3 --mw-api-key abc --mw-target https://uid.middleware.io

  # Multiple task definitions (repeatable flag)
  mw-ecs-instrument instrument --task-definition my-app:3 --task-definition my-api:2 \
    --mw-api-key abc --mw-target https://uid.middleware.io --language java --enable-apm --enable-logs

  # Multiple task definitions (comma-separated)
  mw-ecs-instrument instrument --task-definition my-app:3,my-api:2,my-worker:1 \
    --mw-api-key abc --mw-target https://uid.middleware.io --language java --enable-apm --enable-logs

  # All families
  mw-ecs-instrument instrument --all --mw-api-key abc --mw-target https://uid.middleware.io \
    --language node --enable-apm --enable-logs --dry-run`,
	RunE: runInstrument,
}

func runInstrument(cmd *cobra.Command, args []string) error {
	if len(instrumentFlags.taskDefs) == 0 && !instrumentFlags.all {
		return fmt.Errorf("either --task-definition or --all is required")
	}
	if len(instrumentFlags.taskDefs) > 0 && instrumentFlags.all {
		return fmt.Errorf("--task-definition and --all are mutually exclusive")
	}

	if instrumentFlags.all || len(instrumentFlags.taskDefs) > 1 {
		instrumentFlags.language = "all"
	} else if instrumentFlags.language == "" {
		instrumentFlags.language = "all"
	}

	ctx := cmd.Context()
	client, err := mwaws.NewClient(ctx, instrumentFlags.region)
	if err != nil {
		return err
	}

	newPrompter := func() (*prompt.Prompter, error) {
		p, err := prompt.New()
		if err != nil {
			return nil, err
		}
		return p, nil
	}

	if instrumentFlags.all {
		return runBatchInstrument(ctx, client, newPrompter)
	}
	if len(instrumentFlags.taskDefs) == 1 {
		return runSingleInstrument(ctx, client, newPrompter, instrumentFlags.taskDefs[0])
	}
	return runMultiInstrument(ctx, client, newPrompter, instrumentFlags.taskDefs)
}

func runBatchInstrument(ctx context.Context, client *mwaws.Client, newPrompter func() (*prompt.Prompter, error)) error {
	fmt.Fprintln(os.Stderr, "\033[36m[INFO]\033[0m  Discovering task definition families ...")
	families, err := client.ListFamilies(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\033[32m[OK]\033[0m    Found %d families\n", len(families))

	p, err := newPrompter()
	if err != nil {
		return err
	}
	defer p.Close()

	interactive := !instrumentFlags.enableAPM && !instrumentFlags.enableLogs
	if interactive {
		resolveInteractiveFlags(p, "batch")
	}

	var registered []registeredTask
	var instrumented, notInstrumented int
	for _, family := range families {
		fmt.Fprintf(os.Stderr, "\n\033[36m[INFO]\033[0m  Processing: %s\n", family)

		td, err := client.LatestTaskDefinition(ctx, family)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m %s: %v\n", family, err)
			notInstrumented++
			continue
		}

		opts, decisions := buildOptionsNonInteractive(td)
		result := instrument.Patch(td, opts, decisions)
		reg, err := handleOutput(td, aws.ToString(td.Family), result, opts, ctx, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m %s: %v\n", family, err)
			notInstrumented++
			continue
		}
		if reg != nil {
			registered = append(registered, *reg)
		}
		instrumented++
	}

	fmt.Fprintf(os.Stderr, "\n  Instrumented: \033[32m%d\033[0m  |  Not instrumented: \033[33m%d\033[0m\n", instrumented, notInstrumented)

	if len(registered) > 0 {
		return runRegisteredTasks(ctx, client, p, registered)
	}
	return nil
}

func runMultiInstrument(ctx context.Context, client *mwaws.Client, newPrompter func() (*prompt.Prompter, error), taskDefs []string) error {
	p, err := newPrompter()
	if err != nil {
		return err
	}
	defer p.Close()

	interactive := !instrumentFlags.enableAPM && !instrumentFlags.enableLogs
	if interactive {
		resolveInteractiveFlags(p, "batch")
	}

	var registered []registeredTask
	var instrumented, notInstrumented int
	for _, taskDef := range taskDefs {
		fmt.Fprintf(os.Stderr, "\n\033[36m[INFO]\033[0m  Processing: %s\n", taskDef)

		td, err := client.DescribeTaskDefinition(ctx, taskDef)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m %s: %v\n", taskDef, err)
			notInstrumented++
			continue
		}

		opts, decisions := buildOptionsNonInteractive(td)
		result := instrument.Patch(td, opts, decisions)
		reg, err := handleOutput(td, aws.ToString(td.Family), result, opts, ctx, client)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m %s: %v\n", taskDef, err)
			notInstrumented++
			continue
		}
		if reg != nil {
			registered = append(registered, *reg)
		}
		instrumented++
	}

	fmt.Fprintf(os.Stderr, "\n  Instrumented: \033[32m%d\033[0m  |  Not instrumented: \033[33m%d\033[0m\n", instrumented, notInstrumented)

	if len(registered) > 0 {
		return runRegisteredTasks(ctx, client, p, registered)
	}
	return nil
}

func runSingleInstrument(ctx context.Context, client *mwaws.Client, newPrompter func() (*prompt.Prompter, error), taskDef string) error {
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

	needsInteractive := !instrumentFlags.enableAPM && !instrumentFlags.enableLogs && instrumentFlags.language == ""
	needsRunPrompt := instrumentFlags.register && !instrumentFlags.run

	var p *prompt.Prompter
	if needsInteractive || needsRunPrompt {
		p, err = newPrompter()
		if err != nil {
			return err
		}
		defer p.Close()
	}

	var opts instrument.Options
	var decisions instrument.ReplaceDecision

	if needsInteractive {
		opts, decisions = resolveInteractive(p, td, family)
	} else {
		opts, decisions = buildOptionsNonInteractive(td)
	}

	result := instrument.Patch(td, opts, decisions)
	reg, err := handleOutput(td, family, result, opts, ctx, client)
	if err != nil {
		return err
	}
	if reg != nil {
		return runRegisteredTasks(ctx, client, p, []registeredTask{*reg})
	}
	return nil
}

func resolveInteractiveFlags(p *prompt.Prompter, _ string) {
	if !instrumentFlags.enableAPM {
		instrumentFlags.enableAPM = p.AskYesNo("Enable APM auto-instrumentation (init container)?", true)
	}
	if !instrumentFlags.enableLogs {
		instrumentFlags.enableLogs = p.AskYesNo("Add FireLens log routing (Fluent Bit sidecar + awsfirelens on app)?", true)
	}
}

func resolveInteractive(p *prompt.Prompter, td *ecstypes.TaskDefinition, family string) (instrument.Options, instrument.ReplaceDecision) {
	var decisions instrument.ReplaceDecision
	enableAPM := instrumentFlags.enableAPM
	enableLogs := instrumentFlags.enableLogs
	fargate := instrumentFlags.fargate

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

	if !enableLogs {
		enableLogs = p.AskYesNo("Add FireLens log routing (Fluent Bit sidecar + awsfirelens on app)?", true)
	}

	if enableLogs && instrument.HasExistingLogConfig(td.ContainerDefinitions) {
		existing := instrument.DetectLogConfig(td.ContainerDefinitions)
		fmt.Fprintf(os.Stderr, "\033[33m[WARN]\033[0m  App containers already have log configuration: %s\n", existing)
		decisions.OverrideLogConfig = p.AskYesNo("Override existing log configuration with awsfirelens?", false)
	} else if enableLogs {
		decisions.OverrideLogConfig = true
	}

	if !fargate {
		detectedLaunch := instrument.DetectLaunchType(td.Compatibilities)
		if detectedLaunch == "FARGATE" {
			fargate = true
			fmt.Fprintln(os.Stderr, "\033[36m[INFO]\033[0m  Detected Fargate compatibility, using awsvpc network mode")
		} else {
			_, chosen := p.AskChoice("Select the launch type:", []string{"EC2", "FARGATE"})
			fargate = chosen == "FARGATE"
		}
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
		Fargate:     fargate,
	}, decisions
}

func buildOptionsNonInteractive(td *ecstypes.TaskDefinition) (instrument.Options, instrument.ReplaceDecision) {
	family := aws.ToString(td.Family)
	serviceName := instrumentFlags.serviceName
	if serviceName == "" {
		serviceName = family
	}

	fargate := instrumentFlags.fargate
	if !fargate && instrument.DetectLaunchType(td.Compatibilities) == "FARGATE" {
		fargate = true
	}

	return instrument.Options{
		MWApiKey:    instrumentFlags.mwApiKey,
		MWTarget:    instrumentFlags.mwTarget,
		ServiceName: serviceName,
		Language:    instrument.Language(instrumentFlags.language),
		EnableAPM:   instrumentFlags.enableAPM,
		EnableLogs:  instrumentFlags.enableLogs,
		Fargate:     fargate,
	}, instrument.ReplaceDecision{
		ReplaceMWAgent:    true,
		ReplaceInit:       true,
		ReplaceFirelens:   true,
		OverrideLogConfig: true,
	}
}

type registeredTask struct {
	arn     string
	fargate bool
}

func handleOutput(td *ecstypes.TaskDefinition, family string, result instrument.PatchResult, opts instrument.Options, ctx context.Context, client *mwaws.Client) (*registeredTask, error) {
	printSummary(family, result, opts)

	data, err := instrument.SerializeTaskDefinition(td)
	if err != nil {
		return nil, fmt.Errorf("marshaling task definition: %w", err)
	}

	if instrumentFlags.dryRun {
		fmt.Fprintln(os.Stderr, "\033[36m[INFO]\033[0m  Dry run — printing modified task definition to stdout:")
		_, err := os.Stdout.Write(append(data, '\n'))
		return nil, err
	}

	output := instrumentFlags.output
	if output == "" {
		output = family + "-instrumented.json"
	}

	if err := os.WriteFile(output, append(data, '\n'), 0644); err != nil {
		return nil, fmt.Errorf("writing output file: %w", err)
	}
	fmt.Fprintf(os.Stderr, "\033[32m[OK]\033[0m    Written to: %s\n", output)

	if instrumentFlags.run && !instrumentFlags.register {
		return nil, fmt.Errorf("--run requires --register; use both flags together")
	}

	if instrumentFlags.register {
		fmt.Fprintln(os.Stderr, "\033[36m[INFO]\033[0m  Registering new task definition revision ...")
		registered, err := client.RegisterTaskDefinition(ctx, td)
		if err != nil {
			return nil, err
		}
		newArn := aws.ToString(registered.TaskDefinitionArn)
		newRev := registered.Revision
		fmt.Fprintf(os.Stderr, "\033[32m[OK]\033[0m    Registered: %s (revision %d)\n", newArn, newRev)
		return &registeredTask{arn: newArn, fargate: opts.Fargate}, nil
	}

	return nil, nil
}

func runRegisteredTasks(ctx context.Context, client *mwaws.Client, p *prompt.Prompter, tasks []registeredTask) error {
	shouldRun := instrumentFlags.run
	if !shouldRun && p != nil {
		shouldRun = p.AskYesNo("Run tasks with the new definitions?", false)
	}
	if !shouldRun {
		return nil
	}

	cluster := instrumentFlags.cluster
	subnets := instrumentFlags.subnets
	secGroups := instrumentFlags.secGroups

	hasFargate := false
	for _, t := range tasks {
		if t.fargate {
			hasFargate = true
			break
		}
	}

	if p != nil {
		if cluster == "" {
			cluster = p.AskString("ECS cluster name", "default")
		}
		if hasFargate {
			if subnets == "" {
				subnets = p.AskString("Subnet IDs (comma-separated)", "")
			}
			if secGroups == "" {
				secGroups = p.AskString("Security group IDs (comma-separated)", "")
			}
		}
	}

	for _, t := range tasks {
		launchType := "EC2"
		if t.fargate {
			launchType = "FARGATE"
		}

		fmt.Fprintf(os.Stderr, "\033[36m[INFO]\033[0m  Running task %s on cluster %q (launch type: %s) ...\n", t.arn, cluster, launchType)
		taskArn, err := client.RunTask(ctx, mwaws.RunTaskInput{
			Cluster:        cluster,
			TaskDefinition: t.arn,
			LaunchType:     launchType,
			Subnets:        subnets,
			SecurityGroups: secGroups,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m %s: %v\n", t.arn, err)
			continue
		}
		fmt.Fprintf(os.Stderr, "\033[32m[OK]\033[0m    Task started: %s\n", taskArn)
	}

	return nil
}

func printSummary(family string, result instrument.PatchResult, opts instrument.Options) {
	fmt.Fprintln(os.Stderr, "\n\033[1m── Summary ──────────────────────────────────────────\033[0m")
	fmt.Fprintf(os.Stderr, "  Family:           \033[36m%s\033[0m\n", family)

	launchType := "EC2"
	if opts.Fargate {
		launchType = "FARGATE"
	}
	fmt.Fprintf(os.Stderr, "  Launch type:      \033[36m%s\033[0m\n", launchType)

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
	if result.LogOverridden {
		fmt.Fprintln(os.Stderr, "  Log config:       \033[32moverridden → awsfirelens\033[0m")
	}

	fmt.Fprintf(os.Stderr, "  Task CPU:         \033[36m%d\033[0m\n", result.TotalCPU)
	fmt.Fprintf(os.Stderr, "  Task Memory:      \033[36m%d\033[0m\n", result.TotalMemory)
}
