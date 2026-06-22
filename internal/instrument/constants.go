package instrument

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

const (
	MWAgentImage        = "ghcr.io/middleware-labs/mw-host-agent:1.20.1"
	InitImageJava       = "ghcr.io/open-telemetry/opentelemetry-operator/autoinstrumentation-java:2.19.0"
	InitImageNode       = "ghcr.io/open-telemetry/opentelemetry-operator/autoinstrumentation-nodejs:0.53.0"
	InitImagePython     = "ghcr.io/open-telemetry/opentelemetry-operator/autoinstrumentation-python:0.59b0"
	FluentBitImage      = "public.ecr.aws/aws-observability/aws-for-fluent-bit:stable"
	VolumeName          = "mw-agent-instrumentation"
	SidecarCPUFargate   = 256
	SidecarCPUEC2       = 256
	SidecarMemoryEC2    = 256
	InitCPU             = 128
	InitMemory          = 128
	MountPathJava       = "/otel-auto-instrumentation-java"
	MountPathNode       = "/otel-auto-instrumentation-nodejs"
	MountPathPython     = "/otel-auto-instrumentation-python"
	SidecarOTLPEndpoint = "http://localhost:9320"
	MuslSuffix          = "-musl"
	ContainerMWAgent    = "mw-agent"
	ContainerInit       = "instrumentation-init"
	ContainerFirelens   = "log_router"
)

type Language string

const (
	LangJava   Language = "java"
	LangNode   Language = "node"
	LangPython Language = "python"
)

func (l Language) Valid() bool {
	switch l {
	case LangJava, LangNode, LangPython:
		return true
	}
	return false
}

type LibC string

const (
	LibCGlibc LibC = "glibc"
	LibCMusl  LibC = "musl"
)

func (l LibC) Valid() bool {
	switch l {
	case LibCGlibc, LibCMusl:
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
	default:
		return ""
	}
}

func (l Language) InitCommand(mountPath string) []string {
	switch l {
	case LangJava:
		return []string{"cp", "/javaagent.jar", mountPath + "/javaagent.jar"}
	case LangNode:
		return []string{"cp", "-r", "/autoinstrumentation/.", mountPath}
	case LangPython:
		return []string{"cp", "-r", "/autoinstrumentation/.", mountPath}
	default:
		return nil
	}
}

func (l Language) MountPath(libc LibC) string {
	var base string
	switch l {
	case LangJava:
		base = MountPathJava
	case LangNode:
		base = MountPathNode
	case LangPython:
		base = MountPathPython
		if libc == LibCMusl {
			base = base + MuslSuffix
		}
	default:
		return ""
	}
	return base
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

func IsLocalhostReachable(networkMode ecstypes.NetworkMode) bool {
	return networkMode == ecstypes.NetworkModeAwsvpc || networkMode == ecstypes.NetworkModeHost
}

func HasExistingLogConfig(containers []ecstypes.ContainerDefinition) bool {
	for _, c := range containers {
		if aws.ToBool(c.Essential) && aws.ToString(c.Name) != ContainerFirelens && c.LogConfiguration != nil {
			return true
		}
	}
	return false
}
