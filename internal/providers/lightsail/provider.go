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

func (p *Provider) SupportsFirewall() bool {
	return true
}

func (p *Provider) SupportsDeploymentTarget(target pullpreview.DeploymentTarget) bool {
	switch pullpreview.NormalizeDeploymentTarget(string(target)) {
	case pullpreview.DeploymentTargetCompose, pullpreview.DeploymentTargetHelm:
		return true
	default:
		return false
	}
}

func (p *Provider) BuildUserData(options pullpreview.UserDataOptions) (string, error) {
	return pullpreview.BuildBootstrapScript(pullpreview.BootstrapOptions{
		AppPath:          options.AppPath,
		Username:         options.Username,
		SSHPublicKeys:    options.SSHPublicKeys,
		DeploymentTarget: options.DeploymentTarget,
		ImageName:        "amazon-linux-2023",
		HostTuning: []string{
			"test -s /swapfile || ( fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile && echo '/swapfile none swap sw 0 0' | tee -a /etc/fstab )",
			"systemctl disable --now tmp.mount",
			"systemctl mask tmp.mount",
			"sysctl vm.swappiness=10 && sysctl vm.vfs_cache_pressure=50",
			"echo 'vm.swappiness=10' | tee -a /etc/sysctl.conf",
			"echo 'vm.vfs_cache_pressure=50' | tee -a /etc/sysctl.conf",
		},
		PropagateRootSSH: true,
	})
}

func init() {
	_ = providers.RegisterProvider("lightsail", "AWS Lightsail", ParseConfigFromEnv, newFromConfig)
}
