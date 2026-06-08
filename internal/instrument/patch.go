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
	Fargate     bool
}

type PatchResult struct {
	AddedMWAgent    bool
	AddedInit       bool
	AddedFirelens   bool
	SkippedMWAgent  bool
	SkippedInit     bool
	SkippedFirelens bool
	LogOverridden   bool
	TotalCPU        int32
	TotalMemory     int32
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
	ReplaceMWAgent   bool
	ReplaceInit      bool
	ReplaceFirelens  bool
	OverrideLogConfig bool
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

		mountPath := MountBasePath
		if sub := opts.Language.MountSubpath(); sub != "" {
			mountPath = fmt.Sprintf("%s/%s", MountBasePath, sub)
		}
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
			if c.LogConfiguration == nil {
				c.LogConfiguration = logConfig
			} else if decisions.OverrideLogConfig {
				c.LogConfiguration = logConfig
				result.LogOverridden = true
			}
		}
	}

	if opts.Fargate {
		td.NetworkMode = ecstypes.NetworkModeAwsvpc
		hasFargate := false
		for _, c := range td.RequiresCompatibilities {
			if c == ecstypes.CompatibilityFargate {
				hasFargate = true
				break
			}
		}
		if !hasFargate {
			td.RequiresCompatibilities = append(td.RequiresCompatibilities, ecstypes.CompatibilityFargate)
		}
	}

	if !result.AddedFirelens && !HasContainer(td.ContainerDefinitions, ContainerFirelens) && hasFirelensLogConfig(td.ContainerDefinitions) {
		td.ContainerDefinitions = append(td.ContainerDefinitions, NewFirelensContainer())
		result.AddedFirelens = true
	}

	result.TotalCPU, result.TotalMemory = recalcResources(td, opts.Fargate)

	return result
}

func hasFirelensLogConfig(containers []ecstypes.ContainerDefinition) bool {
	for _, c := range containers {
		if c.LogConfiguration != nil && c.LogConfiguration.LogDriver == ecstypes.LogDriverAwsfirelens {
			return true
		}
	}
	return false
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
		key := aws.ToString(v.Name)
		if !newKeys[key] && !langSpecificEnvKeys[key] {
			merged = append(merged, v)
		}
	}
	merged = append(merged, newVars...)
	c.Environment = merged
}

func ensureMountPoint(c *ecstypes.ContainerDefinition, mountPath string) {
	for i, mp := range c.MountPoints {
		if aws.ToString(mp.SourceVolume) == VolumeName {
			c.MountPoints[i].ContainerPath = aws.String(mountPath)
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

func recalcResources(td *ecstypes.TaskDefinition, fargate bool) (int32, int32) {
	var totalCPU, totalMem int32
	for _, c := range td.ContainerDefinitions {
		totalCPU += c.Cpu
		if c.Memory != nil {
			totalMem += *c.Memory
		}
	}

	if fargate {
		totalCPU, totalMem = snapToFargate(totalCPU, totalMem)
	}

	td.Cpu = aws.String(strconv.Itoa(int(totalCPU)))
	td.Memory = aws.String(strconv.Itoa(int(totalMem)))
	return totalCPU, totalMem
}

type fargateTier struct {
	cpu    int32
	minMem int32
	maxMem int32
	step   int32
}

var fargateTiers = []fargateTier{
	{256, 512, 2048, 512},
	{512, 1024, 4096, 1024},
	{1024, 2048, 8192, 1024},
	{2048, 4096, 16384, 1024},
	{4096, 8192, 30720, 1024},
	{8192, 16384, 61440, 4096},
	{16384, 32768, 122880, 8192},
}

func snapToFargate(cpu, mem int32) (int32, int32) {
	for _, t := range fargateTiers {
		if cpu <= t.cpu {
			snappedMem := t.minMem
			if mem > t.minMem {
				steps := (mem - t.minMem + t.step - 1) / t.step
				snappedMem = t.minMem + steps*t.step
				if snappedMem > t.maxMem {
					snappedMem = t.maxMem
				}
			}
			return t.cpu, snappedMem
		}
	}
	last := fargateTiers[len(fargateTiers)-1]
	return last.cpu, last.maxMem
}
