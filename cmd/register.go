package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/spf13/cobra"

	mwaws "github.com/middleware-labs/mw-ecs-instrumentation/internal/aws"
)

var registerFlags struct {
	files  []string
	region string
}

func init() {
	f := registerCmd.Flags()
	f.StringSliceVar(&registerFlags.files, "file", nil, "Path to task definition JSON file, repeatable or comma-separated")
	f.StringVar(&registerFlags.region, "region", "", "AWS region")

	registerCmd.MarkFlagRequired("file")
	rootCmd.AddCommand(registerCmd)
}

var registerCmd = &cobra.Command{
	Use:   "register",
	Short: "Register task definition JSON files with ECS",
	Long: `Register task definitions from JSON files as new ECS revisions.
Useful when you ran the instrument command without --register and have
local JSON files to register.`,
	Example: `  mw-ecs-instrument register --file my-app-instrumented.json
  mw-ecs-instrument register --file my-app-instrumented.json --file my-api-instrumented.json
  mw-ecs-instrument register --file my-app-instrumented.json,my-api-instrumented.json --region us-west-2`,
	RunE: runRegister,
}

type registerInput struct {
	Family                  string                          `json:"family"`
	ContainerDefinitions    []registerContainer             `json:"containerDefinitions"`
	Volumes                 []registerVolume                `json:"volumes"`
	NetworkMode             string                          `json:"networkMode"`
	RequiresCompatibilities []string                        `json:"requiresCompatibilities"`
	CPU                     string                          `json:"cpu"`
	Memory                  string                          `json:"memory"`
	ExecutionRoleArn        string                          `json:"executionRoleArn"`
	TaskRoleArn             string                          `json:"taskRoleArn"`
	PlacementConstraints    []ecstypes.TaskDefinitionPlacementConstraint  `json:"placementConstraints"`
	RuntimePlatform         *ecstypes.RuntimePlatform       `json:"runtimePlatform"`
	PidMode                 string                          `json:"pidMode"`
	IpcMode                 string                          `json:"ipcMode"`
	EphemeralStorage        *ecstypes.EphemeralStorage      `json:"ephemeralStorage"`
	ProxyConfiguration      *ecstypes.ProxyConfiguration    `json:"proxyConfiguration"`
}

type registerContainer struct {
	Name                  string                          `json:"name"`
	Image                 string                          `json:"image"`
	CPU                   int32                           `json:"cpu"`
	Memory                *int32                          `json:"memory"`
	Essential             bool                            `json:"essential"`
	PortMappings          []registerPortMapping           `json:"portMappings"`
	Environment           []registerKeyValuePair          `json:"environment"`
	MountPoints           []registerMountPoint            `json:"mountPoints"`
	VolumesFrom           []ecstypes.VolumeFrom           `json:"volumesFrom"`
	DependsOn             []registerContainerDependency   `json:"dependsOn"`
	LogConfiguration      *registerLogConfiguration       `json:"logConfiguration"`
	FirelensConfiguration *registerFirelensConfiguration  `json:"firelensConfiguration"`
	User                  string                          `json:"user"`
}

type registerKeyValuePair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type registerPortMapping struct {
	ContainerPort int32  `json:"containerPort"`
	HostPort      int32  `json:"hostPort"`
	Protocol      string `json:"protocol"`
	AppProtocol   string `json:"appProtocol"`
}

type registerMountPoint struct {
	SourceVolume  string `json:"sourceVolume"`
	ContainerPath string `json:"containerPath"`
	ReadOnly      bool   `json:"readOnly"`
}

type registerContainerDependency struct {
	ContainerName string `json:"containerName"`
	Condition     string `json:"condition"`
}

type registerLogConfiguration struct {
	LogDriver string            `json:"logDriver"`
	Options   map[string]string `json:"options"`
}

type registerFirelensConfiguration struct {
	Type    string            `json:"type"`
	Options map[string]string `json:"options"`
}

type registerVolume struct {
	Name string      `json:"name"`
	Host interface{} `json:"host"`
}

func runRegister(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	client, err := mwaws.NewClient(ctx, registerFlags.region)
	if err != nil {
		return err
	}

	for _, file := range registerFlags.files {
		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m reading file %s: %v\n", file, err)
			continue
		}

		var input registerInput
		if err := json.Unmarshal(data, &input); err != nil {
			fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m parsing %s: %v\n", file, err)
			continue
		}

		if input.Family == "" {
			fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m %s: missing 'family' field\n", file)
			continue
		}

		td := toTaskDefinition(input)

		fmt.Fprintf(os.Stderr, "\033[36m[INFO]\033[0m  Registering task definition: %s (%d containers) ...\n",
			aws.ToString(td.Family), len(td.ContainerDefinitions))

		registered, err := client.RegisterTaskDefinition(ctx, &td)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\033[31m[ERROR]\033[0m %s: %v\n", file, err)
			continue
		}

		fmt.Fprintf(os.Stderr, "\033[32m[OK]\033[0m    Registered: %s (revision %d)\n",
			aws.ToString(registered.TaskDefinitionArn), registered.Revision)
	}
	return nil
}

func toTaskDefinition(input registerInput) ecstypes.TaskDefinition {
	td := ecstypes.TaskDefinition{
		Family:               aws.String(input.Family),
		NetworkMode:          ecstypes.NetworkMode(input.NetworkMode),
		Cpu:                  aws.String(input.CPU),
		Memory:               aws.String(input.Memory),
		PlacementConstraints: input.PlacementConstraints,
		RuntimePlatform:      input.RuntimePlatform,
		PidMode:              ecstypes.PidMode(input.PidMode),
		IpcMode:              ecstypes.IpcMode(input.IpcMode),
		EphemeralStorage:     input.EphemeralStorage,
		ProxyConfiguration:   input.ProxyConfiguration,
	}

	if input.ExecutionRoleArn != "" {
		td.ExecutionRoleArn = aws.String(input.ExecutionRoleArn)
	}
	if input.TaskRoleArn != "" {
		td.TaskRoleArn = aws.String(input.TaskRoleArn)
	}

	for _, compat := range input.RequiresCompatibilities {
		td.RequiresCompatibilities = append(td.RequiresCompatibilities, ecstypes.Compatibility(compat))
	}

	for _, v := range input.Volumes {
		vol := ecstypes.Volume{Name: aws.String(v.Name)}
		if v.Host != nil {
			vol.Host = &ecstypes.HostVolumeProperties{}
		}
		td.Volumes = append(td.Volumes, vol)
	}

	for _, c := range input.ContainerDefinitions {
		cd := ecstypes.ContainerDefinition{
			Name:      aws.String(c.Name),
			Image:     aws.String(c.Image),
			Cpu:       c.CPU,
			Memory:    c.Memory,
			Essential: aws.Bool(c.Essential),
		}

		if c.User != "" {
			cd.User = aws.String(c.User)
		}

		for _, pm := range c.PortMappings {
			opm := ecstypes.PortMapping{
				ContainerPort: aws.Int32(pm.ContainerPort),
				HostPort:      aws.Int32(pm.HostPort),
				Protocol:      ecstypes.TransportProtocol(pm.Protocol),
			}
			if pm.AppProtocol != "" {
				opm.AppProtocol = ecstypes.ApplicationProtocol(pm.AppProtocol)
			}
			cd.PortMappings = append(cd.PortMappings, opm)
		}

		for _, env := range c.Environment {
			cd.Environment = append(cd.Environment, ecstypes.KeyValuePair{
				Name:  aws.String(env.Name),
				Value: aws.String(env.Value),
			})
		}

		for _, mp := range c.MountPoints {
			cd.MountPoints = append(cd.MountPoints, ecstypes.MountPoint{
				SourceVolume:  aws.String(mp.SourceVolume),
				ContainerPath: aws.String(mp.ContainerPath),
				ReadOnly:      aws.Bool(mp.ReadOnly),
			})
		}

		cd.VolumesFrom = c.VolumesFrom

		for _, d := range c.DependsOn {
			cd.DependsOn = append(cd.DependsOn, ecstypes.ContainerDependency{
				ContainerName: aws.String(d.ContainerName),
				Condition:     ecstypes.ContainerCondition(d.Condition),
			})
		}

		if c.LogConfiguration != nil {
			cd.LogConfiguration = &ecstypes.LogConfiguration{
				LogDriver: ecstypes.LogDriver(c.LogConfiguration.LogDriver),
				Options:   c.LogConfiguration.Options,
			}
		}

		if c.FirelensConfiguration != nil {
			cd.FirelensConfiguration = &ecstypes.FirelensConfiguration{
				Type:    ecstypes.FirelensConfigurationType(c.FirelensConfiguration.Type),
				Options: c.FirelensConfiguration.Options,
			}
		}

		td.ContainerDefinitions = append(td.ContainerDefinitions, cd)
	}

	return td
}
