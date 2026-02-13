package lightsail

import (
	"context"
	"fmt"
	"strings"

	"github.com/pullpreview/action/internal/providers"
	"github.com/pullpreview/action/internal/pullpreview"
)

const (
	DefaultRegion = "us-east-1"
)

type Config struct {
	Region string
}

func (c Config) ProviderName() string {
	return "lightsail"
}

func (c Config) ProviderDisplayName() string {
	return "AWS Lightsail"
}

func (c Config) Validate() error {
	if c.Region == "" {
		return fmt.Errorf("AWS region is required")
	}
	return nil
}

func ParseConfigFromEnv(env map[string]string) (pullpreview.ProviderConfig, error) {
	region := strings.TrimSpace(env["REGION"])
	if region == "" {
		region = env["AWS_REGION"]
	}
	if region == "" {
		region = DefaultRegion
	}
	cfg := Config{
		Region: region,
	}
	return cfg, cfg.Validate()
}

func newFromConfig(ctx context.Context, cfg pullpreview.ProviderConfig, logger *pullpreview.Logger) (pullpreview.Provider, error) {
	typed, ok := cfg.(Config)
	if !ok {
		if pointer, ok := cfg.(*Config); ok {
			typed = *pointer
		} else {
			return nil, fmt.Errorf("invalid lightsail configuration type")
		}
	}
	return New(ctx, typed.Region, logger)
}

func (p *Provider) Name() string {
	return "lightsail"
}

func (p *Provider) DisplayName() string {
	return "AWS Lightsail"
}

func (p *Provider) SupportsSnapshots() bool {
	return true
}

func (p *Provider) SupportsRestore() bool {
	return true
}

func (p *Provider) SupportsFirewall() bool {
	return true
}

func (p *Provider) BuildUserData(options pullpreview.UserDataOptions) (string, error) {
	script := pullpreview.UserData{
		AppPath:       options.AppPath,
		SSHPublicKeys: options.SSHPublicKeys,
		Username:      options.Username,
	}
	return script.Script(), nil
}

func init() {
	_ = providers.RegisterProvider("lightsail", "AWS Lightsail", ParseConfigFromEnv, newFromConfig)
}
