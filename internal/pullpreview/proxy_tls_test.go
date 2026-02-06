package pullpreview

import (
	"encoding/json"
	"testing"
)

func TestParseProxyTLSTarget(t *testing.T) {
	target, err := parseProxyTLSTarget("web:80")
	if err != nil {
		t.Fatalf("parseProxyTLSTarget() error: %v", err)
	}
	if target.Service != "web" || target.Port != 80 {
		t.Fatalf("unexpected target: %#v", target)
	}
}

func TestParseProxyTLSTargetRejectsInvalidValue(t *testing.T) {
	_, err := parseProxyTLSTarget("web")
	if err == nil {
		t.Fatalf("expected error for invalid proxy_tls value")
	}
}

func TestApplyProxyTLSInjectsCaddyProxyService(t *testing.T) {
	input := map[string]any{
		"services": map[string]any{
			"web": map[string]any{
				"image": "nginx:alpine",
				"ports": []any{
					"80:80",
				},
			},
		},
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	output, err := applyProxyTLS(raw, "web:80", "abc123.my.preview.run", nil)
	if err != nil {
		t.Fatalf("applyProxyTLS() error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}

	services := result["services"].(map[string]any)
	proxy := services["pullpreview-proxy"].(map[string]any)
	if proxy["image"] != "caddy:2-alpine" {
		t.Fatalf("unexpected proxy image: %#v", proxy["image"])
	}
	ports := proxy["ports"].([]any)
	if len(ports) != 1 || ports[0] != "443:443" {
		t.Fatalf("unexpected proxy ports: %#v", ports)
	}
	command := proxy["command"].([]any)
	if len(command) != 6 || command[2] != "--from" || command[3] != "abc123.my.preview.run" || command[4] != "--to" || command[5] != "web:80" {
		t.Fatalf("unexpected proxy command: %#v", command)
	}
	volumes := result["volumes"].(map[string]any)
	if _, ok := volumes["pullpreview_caddy_data"]; !ok {
		t.Fatalf("expected pullpreview_caddy_data volume to be present")
	}
	if _, ok := volumes["pullpreview_caddy_config"]; !ok {
		t.Fatalf("expected pullpreview_caddy_config volume to be present")
	}
}

func TestApplyProxyTLSSkipsWhenHostPort443AlreadyPublished(t *testing.T) {
	input := map[string]any{
		"services": map[string]any{
			"web": map[string]any{
				"ports": []any{
					map[string]any{"target": 443, "published": "443", "protocol": "tcp"},
				},
			},
		},
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	output, err := applyProxyTLS(raw, "web:80", "abc123.my.preview.run", nil)
	if err != nil {
		t.Fatalf("applyProxyTLS() error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	services := result["services"].(map[string]any)
	if len(services) != 1 {
		t.Fatalf("expected services to remain unchanged, got %#v", services)
	}
	if _, ok := services["pullpreview-proxy"]; ok {
		t.Fatalf("did not expect proxy service when host port 443 is already published")
	}
}
