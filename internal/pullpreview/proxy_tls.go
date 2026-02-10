package pullpreview

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type proxyTLSTarget struct {
	Service string
	Port    int
}

func parseProxyTLSTarget(raw string) (proxyTLSTarget, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return proxyTLSTarget{}, fmt.Errorf("proxy_tls value is empty")
	}

	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return proxyTLSTarget{}, fmt.Errorf("proxy_tls must have format service:port")
	}

	service := strings.TrimSpace(parts[0])
	if !validComposeServiceName(service) {
		return proxyTLSTarget{}, fmt.Errorf("proxy_tls service %q is invalid", service)
	}

	port, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || port < 1 || port > 65535 {
		return proxyTLSTarget{}, fmt.Errorf("proxy_tls port %q is invalid", parts[1])
	}

	return proxyTLSTarget{Service: service, Port: port}, nil
}

func validComposeServiceName(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		isLower := ch >= 'a' && ch <= 'z'
		isUpper := ch >= 'A' && ch <= 'Z'
		isDigit := ch >= '0' && ch <= '9'
		if isLower || isUpper || isDigit || ch == '_' || ch == '-' || ch == '.' {
			continue
		}
		return false
	}
	return true
}

func applyProxyTLS(composeConfigJSON []byte, proxyTLS, publicDNS string, logger *Logger) ([]byte, error) {
	proxyTLS = strings.TrimSpace(proxyTLS)
	if proxyTLS == "" {
		return composeConfigJSON, nil
	}

	target, err := parseProxyTLSTarget(proxyTLS)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(publicDNS) == "" {
		return nil, fmt.Errorf("proxy_tls requires a non-empty public DNS")
	}

	var config map[string]any
	if err := json.Unmarshal(composeConfigJSON, &config); err != nil {
		return nil, fmt.Errorf("unable to parse compose config: %w", err)
	}

	rawServices, ok := config["services"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("compose config has no services")
	}

	if _, ok := rawServices[target.Service]; !ok {
		return nil, fmt.Errorf("proxy_tls target service %q not found in compose config", target.Service)
	}

	if publishesHostPort443(rawServices) {
		if logger != nil {
			logger.Warnf("proxy_tls=%q ignored because compose already publishes host port 443", proxyTLS)
		}
		return composeConfigJSON, nil
	}

	const proxyServiceName = "pullpreview-proxy"
	if _, exists := rawServices[proxyServiceName]; exists {
		return nil, fmt.Errorf("compose service %q already exists", proxyServiceName)
	}

	rawServices[proxyServiceName] = map[string]any{
		"image":   "caddy:2-alpine",
		"restart": "unless-stopped",
		"ports": []any{
			"443:443",
		},
		"depends_on": []any{target.Service},
		"volumes": []any{
			"pullpreview_caddy_data:/data",
			"pullpreview_caddy_config:/config",
		},
		"command": []any{
			"caddy",
			"reverse-proxy",
			"--from", publicDNS,
			"--to", fmt.Sprintf("%s:%d", target.Service, target.Port),
		},
	}

	volumes, _ := config["volumes"].(map[string]any)
	if volumes == nil {
		volumes = map[string]any{}
		config["volumes"] = volumes
	}
	if _, exists := volumes["pullpreview_caddy_data"]; !exists {
		volumes["pullpreview_caddy_data"] = map[string]any{}
	}
	if _, exists := volumes["pullpreview_caddy_config"]; !exists {
		volumes["pullpreview_caddy_config"] = map[string]any{}
	}

	return json.Marshal(config)
}

func publishesHostPort443(services map[string]any) bool {
	for _, rawService := range services {
		service, ok := rawService.(map[string]any)
		if !ok {
			continue
		}
		ports, ok := service["ports"].([]any)
		if !ok {
			continue
		}
		for _, rawPort := range ports {
			if portPublishes443(rawPort) {
				return true
			}
		}
	}
	return false
}

func portPublishes443(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		published, ok := v["published"]
		if !ok {
			return false
		}
		return tokenContainsPort443(published)
	case string:
		raw := strings.TrimSpace(v)
		if raw == "" {
			return false
		}
		if idx := strings.Index(raw, "/"); idx >= 0 {
			raw = raw[:idx]
		}
		parts := strings.Split(raw, ":")
		hostPort := ""
		if len(parts) == 1 {
			hostPort = parts[0]
		} else {
			hostPort = parts[len(parts)-2]
		}
		return tokenContainsPort443(hostPort)
	default:
		return false
	}
}

func tokenContainsPort443(value any) bool {
	switch v := value.(type) {
	case int:
		return v == 443
	case int32:
		return v == 443
	case int64:
		return v == 443
	case float64:
		return int(v) == 443
	case string:
		raw := strings.Trim(strings.TrimSpace(v), "[]")
		if raw == "" {
			return false
		}
		if strings.Contains(raw, "-") {
			bounds := strings.SplitN(raw, "-", 2)
			if len(bounds) != 2 {
				return false
			}
			start, errStart := strconv.Atoi(strings.TrimSpace(bounds[0]))
			end, errEnd := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if errStart != nil || errEnd != nil {
				return false
			}
			if start > end {
				start, end = end, start
			}
			return start <= 443 && 443 <= end
		}
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return false
		}
		return parsed == 443
	default:
		return false
	}
}
