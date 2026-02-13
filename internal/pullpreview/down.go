package pullpreview

import (
	"strconv"
	"strings"
)

func RunDown(opts DownOptions, provider Provider, logger *Logger) error {
	instanceName := strings.TrimSpace(opts.Name)
	if strings.HasPrefix(instanceName, "pullpreview-") {
		instanceName = normalizeDownInstanceName(instanceName)
	}
	instance := NewInstance(instanceName, CommonOptions{}, provider, logger)
	if logger != nil {
		logger.Infof("Destroying instance name=%s", instance.Name)
	}
	return instance.Terminate()
}

func normalizeDownInstanceName(value string) string {
	name := strings.TrimSpace(value)
	if name == "" {
		return name
	}
	normalized := NormalizeName(name)
	trimmed := strings.TrimPrefix(normalized, "pullpreview-")
	lastDash := strings.LastIndex(trimmed, "-")
	if lastDash <= 0 || lastDash >= len(trimmed)-1 {
		return normalized
	}
	suffix := trimmed[lastDash+1:]
	if len(suffix) >= 10 {
		if _, err := strconv.ParseInt(suffix, 10, 64); err == nil {
			return trimmed[:lastDash]
		}
	}
	return normalized
}
