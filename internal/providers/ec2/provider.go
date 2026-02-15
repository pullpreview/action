package ec2

import (
	"context"
	"fmt"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	ec2svc "github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/pullpreview/action/internal/providers"
	"github.com/pullpreview/action/internal/providers/sshca"
	"github.com/pullpreview/action/internal/pullpreview"
)

const (
	defaultEC2Region       = "us-east-1"
	defaultEC2ImagePrefix  = "al2023-ami-2023"
	defaultEC2InstanceType = "t3.small"
	defaultEC2SSHUser      = "ec2-user"
)

type Config struct {
	Region      string
	Image       string
	CAKey       string
	CAKeyEnv    string
	SSHUsername string
}

func (c Config) ProviderName() string {
	return "ec2"
}

func (c Config) ProviderDisplayName() string {
	return "AWS EC2"
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.Region) == "" {
		return fmt.Errorf("AWS region is required")
	}
	if strings.TrimSpace(c.CAKey) == "" {
		return fmt.Errorf("PULLPREVIEW_CA_KEY is required for provider=ec2")
	}
	if strings.TrimSpace(c.SSHUsername) == "" {
		return fmt.Errorf("ssh username is required")
	}
	return nil
}

func ParseConfigFromEnv(env map[string]string) (pullpreview.ProviderConfig, error) {
	region := strings.TrimSpace(env["REGION"])
	if region == "" {
		region = strings.TrimSpace(env["AWS_REGION"])
	}
	if region == "" {
		region = defaultEC2Region
	}
	caResolution := sshca.ResolveFromEnv(env, "PULLPREVIEW_CA_KEY")
	cfg := Config{
		Region:      region,
		Image:       strings.TrimSpace(env["IMAGE"]),
		CAKey:       strings.TrimSpace(caResolution.Value),
		CAKeyEnv:    caResolution.EnvKey,
		SSHUsername: defaultEC2SSHUser,
	}
	if _, err := sshca.Parse(cfg.CAKey, cfg.CAKeyEnv); err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

func New(ctx context.Context, cfg Config, logger *pullpreview.Logger) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(pullpreview.EnsureContext(ctx), awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, err
	}
	return newProviderWithClient(ctx, cfg, logger, ec2ClientAdapter{client: ec2svc.NewFromConfig(awsCfg)})
}

func NewWithContext(ctx context.Context, cfg pullpreview.ProviderConfig, logger *pullpreview.Logger) (pullpreview.Provider, error) {
	typed, ok := cfg.(Config)
	if !ok {
		pointer, ok := cfg.(*Config)
		if !ok {
			return nil, fmt.Errorf("invalid ec2 configuration type")
		}
		typed = *pointer
	}
	return New(ctx, typed, logger)
}

func init() {
	_ = providers.RegisterProvider("ec2", "AWS EC2", ParseConfigFromEnv, NewWithContext)
}
