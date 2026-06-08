package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

type Client struct {
	ecs *ecs.Client
}

func NewClient(ctx context.Context, region string) (*Client, error) {
	var opts []func(*awsconfig.LoadOptions) error
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}
	return &Client{ecs: ecs.NewFromConfig(cfg)}, nil
}

func (c *Client) DescribeTaskDefinition(ctx context.Context, taskDef string) (*ecstypes.TaskDefinition, error) {
	out, err := c.ecs.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String(taskDef),
	})
	if err != nil {
		return nil, fmt.Errorf("describing task definition %q: %w", taskDef, err)
	}
	return out.TaskDefinition, nil
}

func (c *Client) RegisterTaskDefinition(ctx context.Context, td *ecstypes.TaskDefinition) (*ecstypes.TaskDefinition, error) {
	input := &ecs.RegisterTaskDefinitionInput{
		Family:                  td.Family,
		ContainerDefinitions:    td.ContainerDefinitions,
		Volumes:                 td.Volumes,
		NetworkMode:             td.NetworkMode,
		RequiresCompatibilities: td.RequiresCompatibilities,
		Cpu:                     td.Cpu,
		Memory:                  td.Memory,
		ExecutionRoleArn:        td.ExecutionRoleArn,
		TaskRoleArn:             td.TaskRoleArn,
		PlacementConstraints:    td.PlacementConstraints,
		ProxyConfiguration:      td.ProxyConfiguration,
		RuntimePlatform:         td.RuntimePlatform,
		PidMode:                 td.PidMode,
		IpcMode:                 td.IpcMode,
		EphemeralStorage:        td.EphemeralStorage,
	}
	out, err := c.ecs.RegisterTaskDefinition(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("registering task definition: %w", err)
	}
	return out.TaskDefinition, nil
}

func (c *Client) ListFamilies(ctx context.Context) ([]string, error) {
	var families []string
	paginator := ecs.NewListTaskDefinitionFamiliesPaginator(c.ecs, &ecs.ListTaskDefinitionFamiliesInput{
		Status: ecstypes.TaskDefinitionFamilyStatusActive,
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing task definition families: %w", err)
		}
		families = append(families, page.Families...)
	}
	return families, nil
}

func (c *Client) LatestTaskDefinition(ctx context.Context, family string) (*ecstypes.TaskDefinition, error) {
	return c.DescribeTaskDefinition(ctx, family)
}

type RunTaskInput struct {
	Cluster        string
	TaskDefinition string
	LaunchType     string
	Subnets        string
	SecurityGroups string
}

func (c *Client) RunTask(ctx context.Context, input RunTaskInput) (string, error) {
	runInput := &ecs.RunTaskInput{
		Cluster:        aws.String(input.Cluster),
		TaskDefinition: aws.String(input.TaskDefinition),
		LaunchType:     ecstypes.LaunchType(input.LaunchType),
		Count:          aws.Int32(1),
	}

	if input.LaunchType == "FARGATE" && input.Subnets != "" {
		subnets := splitCSV(input.Subnets)
		sgs := splitCSV(input.SecurityGroups)
		runInput.NetworkConfiguration = &ecstypes.NetworkConfiguration{
			AwsvpcConfiguration: &ecstypes.AwsVpcConfiguration{
				Subnets:        subnets,
				SecurityGroups: sgs,
				AssignPublicIp: ecstypes.AssignPublicIpEnabled,
			},
		}
	}

	out, err := c.ecs.RunTask(ctx, runInput)
	if err != nil {
		return "", fmt.Errorf("running task: %w", err)
	}
	if len(out.Failures) > 0 {
		return "", fmt.Errorf("task failed to start: %s — %s",
			aws.ToString(out.Failures[0].Arn), aws.ToString(out.Failures[0].Reason))
	}
	if len(out.Tasks) == 0 {
		return "", fmt.Errorf("no tasks started")
	}
	return aws.ToString(out.Tasks[0].TaskArn), nil
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := make([]string, 0)
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}
