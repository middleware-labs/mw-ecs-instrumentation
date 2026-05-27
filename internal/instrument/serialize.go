package instrument

import (
	"encoding/json"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

type outputKeyValuePair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type outputPortMapping struct {
	ContainerPort int32  `json:"containerPort"`
	HostPort      int32  `json:"hostPort,omitempty"`
	Protocol      string `json:"protocol,omitempty"`
	AppProtocol   string `json:"appProtocol,omitempty"`
}

type outputMountPoint struct {
	SourceVolume  string `json:"sourceVolume"`
	ContainerPath string `json:"containerPath"`
	ReadOnly      bool   `json:"readOnly"`
}

type outputContainerDependency struct {
	ContainerName string `json:"containerName"`
	Condition     string `json:"condition"`
}

type outputLogConfiguration struct {
	LogDriver string            `json:"logDriver"`
	Options   map[string]string `json:"options,omitempty"`
}

type outputFirelensConfiguration struct {
	Type    string            `json:"type"`
	Options map[string]string `json:"options,omitempty"`
}

type outputContainerDefinition struct {
	Name                  string                       `json:"name"`
	Image                 string                       `json:"image"`
	CPU                   int32                        `json:"cpu"`
	Memory                *int32                       `json:"memory,omitempty"`
	PortMappings          []outputPortMapping          `json:"portMappings,omitempty"`
	Essential             bool                         `json:"essential"`
	Environment           []outputKeyValuePair         `json:"environment,omitempty"`
	MountPoints           []outputMountPoint           `json:"mountPoints,omitempty"`
	VolumesFrom           []interface{}                `json:"volumesFrom,omitempty"`
	DependsOn             []outputContainerDependency  `json:"dependsOn,omitempty"`
	LogConfiguration      *outputLogConfiguration      `json:"logConfiguration,omitempty"`
	FirelensConfiguration *outputFirelensConfiguration `json:"firelensConfiguration,omitempty"`
	SystemControls        []interface{}                `json:"systemControls,omitempty"`
	User                  string                       `json:"user,omitempty"`
}

type outputVolume struct {
	Name string      `json:"name"`
	Host interface{} `json:"host,omitempty"`
}

type outputTaskDefinition struct {
	ContainerDefinitions    []outputContainerDefinition `json:"containerDefinitions"`
	Family                  string                      `json:"family"`
	ExecutionRoleArn        string                      `json:"executionRoleArn,omitempty"`
	TaskRoleArn             string                      `json:"taskRoleArn,omitempty"`
	NetworkMode             string                      `json:"networkMode,omitempty"`
	Volumes                 []outputVolume              `json:"volumes,omitempty"`
	PlacementConstraints    []interface{}               `json:"placementConstraints,omitempty"`
	RequiresCompatibilities []string                    `json:"requiresCompatibilities,omitempty"`
	CPU                     string                      `json:"cpu,omitempty"`
	Memory                  string                      `json:"memory,omitempty"`
}

func SerializeTaskDefinition(td *ecstypes.TaskDefinition) ([]byte, error) {
	out := outputTaskDefinition{
		Family:      aws.ToString(td.Family),
		NetworkMode: string(td.NetworkMode),
		CPU:         aws.ToString(td.Cpu),
		Memory:      aws.ToString(td.Memory),
	}

	if td.ExecutionRoleArn != nil {
		out.ExecutionRoleArn = aws.ToString(td.ExecutionRoleArn)
	}
	if td.TaskRoleArn != nil {
		out.TaskRoleArn = aws.ToString(td.TaskRoleArn)
	}

	for _, compat := range td.RequiresCompatibilities {
		out.RequiresCompatibilities = append(out.RequiresCompatibilities, string(compat))
	}

	for _, v := range td.Volumes {
		ov := outputVolume{Name: aws.ToString(v.Name)}
		if v.Host != nil {
			ov.Host = struct{}{}
		}
		out.Volumes = append(out.Volumes, ov)
	}

	if len(td.PlacementConstraints) == 0 {
		out.PlacementConstraints = []interface{}{}
	}

	for _, c := range td.ContainerDefinitions {
		oc := outputContainerDefinition{
			Name:      aws.ToString(c.Name),
			Image:     aws.ToString(c.Image),
			CPU:       c.Cpu,
			Memory:    c.Memory,
			Essential: aws.ToBool(c.Essential),
		}

		if c.User != nil {
			oc.User = aws.ToString(c.User)
		}

		for _, pm := range c.PortMappings {
			opm := outputPortMapping{
				ContainerPort: aws.ToInt32(pm.ContainerPort),
				HostPort:      aws.ToInt32(pm.HostPort),
				Protocol:      string(pm.Protocol),
			}
			if pm.AppProtocol != "" {
				opm.AppProtocol = string(pm.AppProtocol)
			}
			oc.PortMappings = append(oc.PortMappings, opm)
		}

		for _, env := range c.Environment {
			oc.Environment = append(oc.Environment, outputKeyValuePair{
				Name:  aws.ToString(env.Name),
				Value: aws.ToString(env.Value),
			})
		}

		for _, mp := range c.MountPoints {
			oc.MountPoints = append(oc.MountPoints, outputMountPoint{
				SourceVolume:  aws.ToString(mp.SourceVolume),
				ContainerPath: aws.ToString(mp.ContainerPath),
				ReadOnly:      aws.ToBool(mp.ReadOnly),
			})
		}

		for _, d := range c.DependsOn {
			oc.DependsOn = append(oc.DependsOn, outputContainerDependency{
				ContainerName: aws.ToString(d.ContainerName),
				Condition:     string(d.Condition),
			})
		}

		if c.LogConfiguration != nil {
			oc.LogConfiguration = &outputLogConfiguration{
				LogDriver: string(c.LogConfiguration.LogDriver),
				Options:   c.LogConfiguration.Options,
			}
		}

		if c.FirelensConfiguration != nil {
			oc.FirelensConfiguration = &outputFirelensConfiguration{
				Type:    string(c.FirelensConfiguration.Type),
				Options: c.FirelensConfiguration.Options,
			}
		}

		if len(c.VolumesFrom) == 0 && len(c.MountPoints) > 0 {
			oc.VolumesFrom = []interface{}{}
		}
		if len(c.SystemControls) == 0 && c.LogConfiguration != nil {
			oc.SystemControls = []interface{}{}
		}

		out.ContainerDefinitions = append(out.ContainerDefinitions, oc)
	}

	return json.MarshalIndent(out, "", "    ")
}
