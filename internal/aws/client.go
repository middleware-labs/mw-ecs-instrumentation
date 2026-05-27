package aws

import (
	"context"
	"fmt"

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
