package instrument

import (
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

func NewMWAgentSidecar(apiKey, target string) ecstypes.ContainerDefinition {
	return ecstypes.ContainerDefinition{
		Name:      aws.String(ContainerMWAgent),
		Image:     aws.String(MWAgentImage),
		Cpu:       SidecarCPU,
		Memory:    aws.Int32(SidecarMemory),
		Essential: aws.Bool(false),
		Environment: []ecstypes.KeyValuePair{
			{Name: aws.String("MW_API_KEY"), Value: aws.String(apiKey)},
			{Name: aws.String("MW_TARGET"), Value: aws.String(target)},
		},
	}
}

func NewInitContainer(lang Language) ecstypes.ContainerDefinition {
	mountPath := fmt.Sprintf("%s/%s", MountBasePath, lang.MountSubpath())
	return ecstypes.ContainerDefinition{
		Name:      aws.String(ContainerInit),
		Image:     aws.String(lang.InitImage()),
		Cpu:       InitCPU,
		Memory:    aws.Int32(InitMemory),
		Essential: aws.Bool(false),
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

func APMEnvVars(lang Language, apiKey, target, serviceName string) []ecstypes.KeyValuePair {
	vars := []ecstypes.KeyValuePair{
		{Name: aws.String("MW_API_KEY"), Value: aws.String(apiKey)},
		{Name: aws.String("MW_TARGET"), Value: aws.String(target)},
		{Name: aws.String("MW_SERVICE_NAME"), Value: aws.String(serviceName)},
	}

	switch lang {
	case LangJava:
		vars = append(vars, ecstypes.KeyValuePair{
			Name:  aws.String("JAVA_TOOL_OPTIONS"),
			Value: aws.String(fmt.Sprintf("-javaagent:%s/java/middleware-javaagent.jar", MountBasePath)),
		})
	case LangNode:
		vars = append(vars, ecstypes.KeyValuePair{
			Name:  aws.String("NODE_OPTIONS"),
			Value: aws.String(fmt.Sprintf("--require %s/node/instrument.js", MountBasePath)),
		})
		vars = append(vars, ecstypes.KeyValuePair{
			Name:  aws.String("NODE_PATH"),
			Value: aws.String(fmt.Sprintf("%s/node/node_modules", MountBasePath)),
		})
	case LangPython:
		vars = append(vars, ecstypes.KeyValuePair{
			Name:  aws.String("PYTHONPATH"),
			Value: aws.String(fmt.Sprintf("%s/python/packages/opentelemetry/instrumentation/auto_instrumentation:%s/python/packages", MountBasePath, MountBasePath)),
		})
	}

	return vars
}
