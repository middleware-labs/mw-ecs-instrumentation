package instrument

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

func NewMWAgentSidecar(apiKey, target string, fargate bool) ecstypes.ContainerDefinition {
	if fargate {
		return newMWAgentFargate(apiKey, target)
	}
	return newMWAgentEC2(apiKey, target)
}

func newMWAgentFargate(apiKey, target string) ecstypes.ContainerDefinition {
	return ecstypes.ContainerDefinition{
		Name:      aws.String(ContainerMWAgent),
		Image:     aws.String(MWAgentImage),
		Cpu:       SidecarCPUFargate,
		Essential: aws.Bool(false),
		PortMappings: []ecstypes.PortMapping{
			{
				Name:          aws.String("8006-tcp"),
				ContainerPort: aws.Int32(8006),
				HostPort:      aws.Int32(8006),
				Protocol:      ecstypes.TransportProtocolTcp,
				AppProtocol:   ecstypes.ApplicationProtocolHttp,
			},
			{
				Name:          aws.String("mw-agent-9320-tcp"),
				ContainerPort: aws.Int32(9320),
				HostPort:      aws.Int32(9320),
				Protocol:      ecstypes.TransportProtocolTcp,
			},
		},
		Environment: mwAgentEnvVars(apiKey, target),
	}
}

func newMWAgentEC2(apiKey, target string) ecstypes.ContainerDefinition {
	return ecstypes.ContainerDefinition{
		Name:       aws.String(ContainerMWAgent),
		Image:      aws.String(MWAgentImage),
		Cpu:        SidecarCPUEC2,
		Memory:     aws.Int32(SidecarMemoryEC2),
		Essential:  aws.Bool(false),
		Privileged: aws.Bool(true),
		PortMappings: []ecstypes.PortMapping{
			{
				Name:          aws.String("mw-agent-9319-tcp"),
				ContainerPort: aws.Int32(9319),
				HostPort:      aws.Int32(0),
				Protocol:      ecstypes.TransportProtocolTcp,
				AppProtocol:   ecstypes.ApplicationProtocolGrpc,
			},
			{
				Name:          aws.String("mw-agent-8006-tcp"),
				ContainerPort: aws.Int32(8006),
				HostPort:      aws.Int32(0),
				Protocol:      ecstypes.TransportProtocolTcp,
			},
			{
				Name:          aws.String("mw-agent-9320-tcp"),
				ContainerPort: aws.Int32(9320),
				HostPort:      aws.Int32(0),
				Protocol:      ecstypes.TransportProtocolTcp,
			},
		},
		Environment: mwAgentEnvVars(apiKey, target),
		MountPoints: []ecstypes.MountPoint{
			{SourceVolume: aws.String("docker-sock"), ContainerPath: aws.String("/var/run/docker.sock"), ReadOnly: aws.Bool(false)},
			{SourceVolume: aws.String("proc"), ContainerPath: aws.String("/rootfs/proc"), ReadOnly: aws.Bool(true)},
			{SourceVolume: aws.String("dev"), ContainerPath: aws.String("/rootfs/dev"), ReadOnly: aws.Bool(true)},
			{SourceVolume: aws.String("al2_cgroup"), ContainerPath: aws.String("/sys/fs/cgroup"), ReadOnly: aws.Bool(true)},
			{SourceVolume: aws.String("al1_cgroup"), ContainerPath: aws.String("/cgroup"), ReadOnly: aws.Bool(true)},
			{SourceVolume: aws.String("al2_cgroup"), ContainerPath: aws.String("/rootfs/sys/fs/cgroup"), ReadOnly: aws.Bool(true)},
			{SourceVolume: aws.String("al1_cgroup"), ContainerPath: aws.String("/rootfs/cgroup"), ReadOnly: aws.Bool(true)},
			{SourceVolume: aws.String("docker-containers-root"), ContainerPath: aws.String("/var/lib/docker/containers"), ReadOnly: aws.Bool(true)},
			{SourceVolume: aws.String("var-log-host"), ContainerPath: aws.String("/var/log"), ReadOnly: aws.Bool(true)},
		},
	}
}

func mwAgentEnvVars(apiKey, target string) []ecstypes.KeyValuePair {
	return []ecstypes.KeyValuePair{
		{Name: aws.String("MW_API_KEY"), Value: aws.String(apiKey)},
		{Name: aws.String("MW_TARGET"), Value: aws.String(target)},
		{Name: aws.String("OTEL_EXPORTER_OTLP_PROTOCOL"), Value: aws.String("grpc")},
	}
}

// MWAgentEC2Volumes returns the host volumes required by the EC2 mw-agent sidecar.
func MWAgentEC2Volumes() []ecstypes.Volume {
	return []ecstypes.Volume{
		{Name: aws.String("docker-sock"), Host: &ecstypes.HostVolumeProperties{SourcePath: aws.String("/var/run/docker.sock")}},
		{Name: aws.String("proc"), Host: &ecstypes.HostVolumeProperties{SourcePath: aws.String("/proc")}},
		{Name: aws.String("dev"), Host: &ecstypes.HostVolumeProperties{SourcePath: aws.String("/dev")}},
		{Name: aws.String("al1_cgroup"), Host: &ecstypes.HostVolumeProperties{SourcePath: aws.String("/cgroup")}},
		{Name: aws.String("al2_cgroup"), Host: &ecstypes.HostVolumeProperties{SourcePath: aws.String("/sys/fs/cgroup")}},
		{Name: aws.String("docker-containers-root"), Host: &ecstypes.HostVolumeProperties{SourcePath: aws.String("/var/lib/docker/containers")}},
		{Name: aws.String("var-log-host"), Host: &ecstypes.HostVolumeProperties{SourcePath: aws.String("/var/log")}},
	}
}

func NewInitContainer(lang Language, libc LibC) ecstypes.ContainerDefinition {
	mountPath := lang.MountPath(libc)
	return ecstypes.ContainerDefinition{
		Name:      aws.String(ContainerInit),
		Image:     aws.String(lang.InitImage()),
		Cpu:       InitCPU,
		Memory:    aws.Int32(InitMemory),
		Essential: aws.Bool(false),
		Command:   lang.InitCommand(mountPath),
		MountPoints: []ecstypes.MountPoint{
			{
				SourceVolume:  aws.String(VolumeName),
				ContainerPath: aws.String(mountPath),
				ReadOnly:      aws.Bool(false),
			},
		},
	}
}

func NewFirelensContainer() ecstypes.ContainerDefinition {
	return ecstypes.ContainerDefinition{
		Name:      aws.String(ContainerFirelens),
		Image:     aws.String(FluentBitImage),
		Cpu:       0,
		Essential: aws.Bool(true),
		User:      aws.String("0"),
		FirelensConfiguration: &ecstypes.FirelensConfiguration{
			Type: ecstypes.FirelensConfigurationTypeFluentbit,
			Options: map[string]string{
				"enable-ecs-log-metadata": "true",
			},
		},
	}
}

func NewFirelensLogConfig() *ecstypes.LogConfiguration {
	return &ecstypes.LogConfiguration{
		LogDriver: ecstypes.LogDriverAwsfirelens,
		Options: map[string]string{
			"Name": "forward",
			"Host": "127.0.0.1",
			"Port": "8006",
		},
	}
}

var langSpecificEnvKeys = map[string]bool{
	"JAVA_TOOL_OPTIONS":           true,
	"NODE_OPTIONS":                true,
	"NODE_PATH":                   true,
	"PYTHONPATH":                  true,
	"MW_API_KEY":                  true,
	"MW_TARGET":                   true,
	"MW_SERVICE_NAME":             true,
	"OTEL_EXPORTER_OTLP_ENDPOINT": true,
	"OTEL_EXPORTER_OTLP_PROTOCOL": true,
	"OTEL_SERVICE_NAME":           true,
	"OTEL_RESOURCE_ATTRIBUTES":    true,
}

func APMEnvVars(lang Language, libc LibC, apiKey, target, serviceName string, localhostReachable bool) []ecstypes.KeyValuePair {
	otlpEndpoint := target
	otlpProtocol := "http/protobuf"
	if localhostReachable {
		otlpEndpoint = SidecarOTLPEndpoint
	}

	vars := []ecstypes.KeyValuePair{
		{Name: aws.String("OTEL_EXPORTER_OTLP_ENDPOINT"), Value: aws.String(otlpEndpoint)},
		{Name: aws.String("OTEL_EXPORTER_OTLP_PROTOCOL"), Value: aws.String(otlpProtocol)},
		{Name: aws.String("OTEL_SERVICE_NAME"), Value: aws.String(serviceName)},
		{Name: aws.String("OTEL_RESOURCE_ATTRIBUTES"), Value: aws.String(fmt.Sprintf("mw.account_key=%s", apiKey))},
	}

	mountPath := lang.MountPath(libc)
	switch lang {
	case LangJava:
		vars = append(vars, ecstypes.KeyValuePair{
			Name:  aws.String("JAVA_TOOL_OPTIONS"),
			Value: aws.String(fmt.Sprintf("-javaagent:%s/javaagent.jar", mountPath)),
		})
	case LangNode:
		vars = append(vars, ecstypes.KeyValuePair{
			Name:  aws.String("NODE_OPTIONS"),
			Value: aws.String(fmt.Sprintf("--require %s/autoinstrumentation.js", mountPath)),
		})
	case LangPython:
		vars = append(vars, ecstypes.KeyValuePair{
			Name:  aws.String("PYTHONPATH"),
			Value: aws.String(fmt.Sprintf("%s/opentelemetry/instrumentation/auto_instrumentation:%s", mountPath, mountPath)),
		})
	}

	return vars
}
