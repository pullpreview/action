package pullpreview

import (
	"crypto/sha1"
	"fmt"
	"strings"
)

const defaultPullPreviewLabel = "pullpreview"

func canonicalLabelValue(label string) string {
	return strings.ToLower(NormalizeName(strings.TrimSpace(label)))
}

func labelScopeKey(label string) string {
	canonical := canonicalLabelValue(label)
	if canonical == "" || canonical == defaultPullPreviewLabel {
		return ""
	}
	sum := fmt.Sprintf("%x", sha1.Sum([]byte(canonical)))
	return "l" + sum[:6]
}

func deploymentRuntime(target DeploymentTarget) string {
	switch NormalizeDeploymentTarget(string(target)) {
	case DeploymentTargetHelm:
		return "k3s"
	default:
		return "docker"
	}
}

func DeploymentIdentityMismatch(existing, desired map[string]string) (string, bool) {
	labelDesired := canonicalLabelValue(desired["pullpreview_label"])
	labelExisting := canonicalLabelValue(existing["pullpreview_label"])
	if labelDesired != "" {
		if labelExisting == "" {
			if labelDesired != defaultPullPreviewLabel {
				return fmt.Sprintf("pullpreview_label missing (wanted %q)", labelDesired), true
			}
		} else if labelExisting != labelDesired {
			return fmt.Sprintf("pullpreview_label mismatch (existing=%q wanted=%q)", labelExisting, labelDesired), true
		}
	}

	for key, defaultValue := range map[string]string{
		"pullpreview_target":  string(DeploymentTargetCompose),
		"pullpreview_runtime": deploymentRuntime(DeploymentTargetCompose),
	} {
		want := strings.TrimSpace(strings.ToLower(desired[key]))
		if want == "" {
			continue
		}
		got := strings.TrimSpace(strings.ToLower(existing[key]))
		if got == "" {
			if want != defaultValue {
				return fmt.Sprintf("%s missing (wanted %q)", key, want), true
			}
			continue
		}
		if got != want {
			return fmt.Sprintf("%s mismatch (existing=%q wanted=%q)", key, got, want), true
		}
	}

	return "", false
}

