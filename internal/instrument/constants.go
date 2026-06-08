package instrument

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

const (
	MWAgentImage      = "ghcr.io/middleware-labs/mw-host-agent:latest"
	InitImageJava     = "ghcr.io/middleware-labs/mw-ecs-autoinstrumentation-java:latest"
	InitImageNode     = "ghcr.io/middleware-labs/mw-ecs-autoinstrumentation-node:latest"
	InitImagePython   = "ghcr.io/middleware-labs/mw-ecs-autoinstrumentation-python:latest"
	InitImageAll      = "ghcr.io/middleware-labs/mw-ecs-autoinstrumentation-all:latest"
	FluentBitImage    = "public.ecr.aws/aws-observability/aws-for-fluent-bit:stable"
	VolumeName        = "mw-agent-instrumentation"
	SidecarCPU        = 128
	SidecarMemory     = 200
	InitCPU           = 128
	InitMemory        = 128
	MountBasePath     = "/mnt/mw-agent/instrumentation"
	ContainerMWAgent  = "mw-agent"
	ContainerInit     = "instrumentation-init"
	ContainerFirelens = "log_router"
)

type Language string

const (
	LangJava   Language = "java"
	LangNode   Language = "node"
	LangPython Language = "python"
	LangAll    Language = "all"
)

func (l Language) Valid() bool {
	switch l {
	case LangJava, LangNode, LangPython, LangAll:
		return true
	}
	return false
}

func (l Language) InitImage() string {
	switch l {
	case LangJava:
		return InitImageJava
	case LangNode:
		return InitImageNode
	case LangPython:
		return InitImagePython
	case LangAll:
		return InitImageAll
	}
	return ""
}

func (l Language) MountSubpath() string {
	if l == LangAll {
		return ""
	}
	return string(l)
}

type LogConfigType string

const (
	LogConfigNone       LogConfigType = "—"
	LogConfigCloudWatch LogConfigType = "cloudwatch"
	LogConfigFirelens   LogConfigType = "firelens"
	LogConfigOther      LogConfigType = "other"
)

func DetectLogConfig(containers []ecstypes.ContainerDefinition) LogConfigType {
	for _, c := range containers {
		if aws.ToBool(c.Essential) && aws.ToString(c.Name) != ContainerFirelens && c.LogConfiguration != nil {
			switch c.LogConfiguration.LogDriver {
			case ecstypes.LogDriverAwslogs:
				return LogConfigCloudWatch
			case ecstypes.LogDriverAwsfirelens:
				return LogConfigFirelens
			default:
				return LogConfigOther
			}
		}
	}
	return LogConfigNone
}

func DetectLaunchType(compatibilities []ecstypes.Compatibility) string {
	for _, c := range compatibilities {
		if c == ecstypes.CompatibilityFargate {
			return "FARGATE"
		}
	}
	return "EC2"
}

func HasExistingLogConfig(containers []ecstypes.ContainerDefinition) bool {
	for _, c := range containers {
		if aws.ToBool(c.Essential) && aws.ToString(c.Name) != ContainerFirelens && c.LogConfiguration != nil {
			return true
		}
	}
	return false
}
