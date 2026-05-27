package instrument

import (
	"fmt"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

type Options struct {
	MWApiKey    string
	MWTarget    string
	ServiceName string
	Language    Language
	EnableAPM   bool
	EnableLogs  bool
}

type PatchResult struct {
	AddedMWAgent  bool
	AddedInit     bool
	AddedFirelens bool
	SkippedMWAgent  bool
	SkippedInit     bool
	SkippedFirelens bool
	TotalCPU      int32
	TotalMemory   int32
}

func HasContainer(containers []ecstypes.ContainerDefinition, name string) bool {
	for _, c := range containers {
		if aws.ToString(c.Name) == name {
			return true
		}
	}
	return false
}

func RemoveContainer(containers []ecstypes.ContainerDefinition, name string) []ecstypes.ContainerDefinition {
	result := make([]ecstypes.ContainerDefinition, 0, len(containers))
	for _, c := range containers {
		if aws.ToString(c.Name) != name {
			result = append(result, c)
		}
	}
	return result
}

type ReplaceDecision struct {
	ReplaceMWAgent  bool
	ReplaceInit     bool
	ReplaceFirelens bool
}

func Patch(td *ecstypes.TaskDefinition, opts Options, decisions ReplaceDecision) PatchResult {
	result := PatchResult{}

	hasMWAgent := HasContainer(td.ContainerDefinitions, ContainerMWAgent)
	hasInit := HasContainer(td.ContainerDefinitions, ContainerInit)
	hasFirelens := HasContainer(td.ContainerDefinitions, ContainerFirelens)

	if hasMWAgent {
		if decisions.ReplaceMWAgent {
			td.ContainerDefinitions = RemoveContainer(td.ContainerDefinitions, ContainerMWAgent)
		} else {
			result.SkippedMWAgent = true
		}
	}
	if hasInit {
		if decisions.ReplaceInit {
			td.ContainerDefinitions = RemoveContainer(td.ContainerDefinitions, ContainerInit)
		} else {
			result.SkippedInit = true
		}
	}
	if hasFirelens {
		if decisions.ReplaceFirelens {
			td.ContainerDefinitions = RemoveContainer(td.ContainerDefinitions, ContainerFirelens)
		} else {
			result.SkippedFirelens = true
		}
	}

	if !result.SkippedMWAgent {
		td.ContainerDefinitions = append(td.ContainerDefinitions, NewMWAgentSidecar(opts.MWApiKey, opts.MWTarget))
		result.AddedMWAgent = true
	}

	if opts.EnableLogs && !result.SkippedFirelens {
		td.ContainerDefinitions = append(td.ContainerDefinitions, NewFirelensContainer())
		result.AddedFirelens = true
	}

	if opts.EnableAPM && !result.SkippedInit {
		td.ContainerDefinitions = append(td.ContainerDefinitions, NewInitContainer(opts.Language))
		result.AddedInit = true

		ensureVolume(td)

		mountPath := fmt.Sprintf("%s/%s", MountBasePath, opts.Language.MountSubpath())
		envVars := APMEnvVars(opts.Language, opts.MWApiKey, opts.MWTarget, opts.ServiceName)

		for i := range td.ContainerDefinitions {
			c := &td.ContainerDefinitions[i]
			if !aws.ToBool(c.Essential) || aws.ToString(c.Name) == ContainerFirelens {
				continue
			}
			mergeEnvVars(c, envVars)
			ensureMountPoint(c, mountPath)
			ensureDependsOn(c)
		}
	}

	if opts.EnableLogs {
		logConfig := NewFirelensLogConfig()
		for i := range td.ContainerDefinitions {
			c := &td.ContainerDefinitions[i]
			if !aws.ToBool(c.Essential) || aws.ToString(c.Name) == ContainerFirelens {
				continue
			}
			if c.LogConfiguration != nil && c.LogConfiguration.LogDriver == ecstypes.LogDriverAwslogs {
				c.LogConfiguration = logConfig
			} else if c.LogConfiguration == nil {
				c.LogConfiguration = logConfig
			}
		}
	}

	result.TotalCPU, result.TotalMemory = recalcResources(td)

	return result
}

func ensureVolume(td *ecstypes.TaskDefinition) {
	for _, v := range td.Volumes {
		if aws.ToString(v.Name) == VolumeName {
			return
		}
	}
	td.Volumes = append(td.Volumes, ecstypes.Volume{
		Name: aws.String(VolumeName),
		Host: &ecstypes.HostVolumeProperties{},
	})
}

func mergeEnvVars(c *ecstypes.ContainerDefinition, newVars []ecstypes.KeyValuePair) {
	newKeys := make(map[string]bool)
	for _, v := range newVars {
		newKeys[aws.ToString(v.Name)] = true
	}

	merged := make([]ecstypes.KeyValuePair, 0, len(c.Environment)+len(newVars))
	for _, v := range c.Environment {
		if !newKeys[aws.ToString(v.Name)] {
			merged = append(merged, v)
		}
	}
	merged = append(merged, newVars...)
	c.Environment = merged
}

func ensureMountPoint(c *ecstypes.ContainerDefinition, mountPath string) {
	for _, mp := range c.MountPoints {
		if aws.ToString(mp.SourceVolume) == VolumeName {
			return
		}
	}
	c.MountPoints = append(c.MountPoints, ecstypes.MountPoint{
		SourceVolume:  aws.String(VolumeName),
		ContainerPath: aws.String(mountPath),
		ReadOnly:      aws.Bool(true),
	})
}

func ensureDependsOn(c *ecstypes.ContainerDefinition) {
	for _, d := range c.DependsOn {
		if aws.ToString(d.ContainerName) == ContainerInit {
			return
		}
	}
	c.DependsOn = append(c.DependsOn, ecstypes.ContainerDependency{
		ContainerName: aws.String(ContainerInit),
		Condition:     ecstypes.ContainerConditionSuccess,
	})
}

func recalcResources(td *ecstypes.TaskDefinition) (int32, int32) {
	var totalCPU, totalMem int32
	for _, c := range td.ContainerDefinitions {
		totalCPU += c.Cpu
		if c.Memory != nil {
			totalMem += *c.Memory
		}
	}
	td.Cpu = aws.String(strconv.Itoa(int(totalCPU)))
	td.Memory = aws.String(strconv.Itoa(int(totalMem)))
	return totalCPU, totalMem
}
