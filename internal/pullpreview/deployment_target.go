package pullpreview

import (
	"fmt"
	"strings"
)

func NormalizeDeploymentTarget(value string) DeploymentTarget {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(DeploymentTargetCompose):
		return DeploymentTargetCompose
	case string(DeploymentTargetHelm):
		return DeploymentTargetHelm
	default:
		return DeploymentTarget(strings.ToLower(strings.TrimSpace(value)))
	}
}

func (t DeploymentTarget) Validate() error {
	switch NormalizeDeploymentTarget(string(t)) {
	case DeploymentTargetCompose, DeploymentTargetHelm:
		return nil
	default:
		return fmt.Errorf("unsupported deployment target %q", t)
	}
}
