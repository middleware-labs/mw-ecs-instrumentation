package instrument

const (
	MWAgentImage      = "ghcr.io/middleware-labs/mw-host-agent:1.19.1"
	InitImageJava     = "docker.io/advait11/aws-ecs-java-autoinstrumentation:latest"
	InitImageNode     = "docker.io/advait11/aws-ecs-node-autoinstrumentation:latest"
	InitImagePython   = "docker.io/advait11/aws-ecs-python-autoinstrumentation:latest"
	FluentBitImage    = "amazon/aws-for-fluent-bit:stable"
	VolumeName        = "mw-agent-instrumentation"
	SidecarCPU        = 256
	SidecarMemory     = 256
	InitCPU           = 256
	InitMemory        = 256
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
)

func (l Language) Valid() bool {
	switch l {
	case LangJava, LangNode, LangPython:
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
	}
	return ""
}

func (l Language) MountSubpath() string {
	return string(l)
}
