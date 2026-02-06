package pullpreview

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteRelativeBindSourcesUnderAppPath(t *testing.T) {
	appPath := filepath.Clean("/tmp/app")
	input := map[string]any{
		"services": map[string]any{
			"web": map[string]any{
				"volumes": []any{
					map[string]any{
						"type":   "bind",
						"source": filepath.Join(appPath, "dumps"),
						"target": "/dump",
					},
					map[string]any{
						"type":   "volume",
						"source": "db_data",
						"target": "/var/lib/mysql",
					},
				},
			},
		},
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	output, err := rewriteRelativeBindSources(raw, appPath, "/app")
	if err != nil {
		t.Fatalf("rewriteRelativeBindSources() error: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	services := result["services"].(map[string]any)
	web := services["web"].(map[string]any)
	volumes := web["volumes"].([]any)
	first := volumes[0].(map[string]any)
	second := volumes[1].(map[string]any)

	if first["source"] != "/app/dumps" {
		t.Fatalf("expected bind source rewritten to /app/dumps, got %#v", first["source"])
	}
	if second["source"] != "db_data" {
		t.Fatalf("expected named volume unchanged, got %#v", second["source"])
	}
}

func TestRewriteRelativeBindSourcesRejectsAbsoluteOutsideAppPath(t *testing.T) {
	appPath := filepath.Clean("/tmp/app")
	input := map[string]any{
		"services": map[string]any{
			"web": map[string]any{
				"volumes": []any{
					map[string]any{
						"type":   "bind",
						"source": "/tmp/other/dumps",
						"target": "/dump",
					},
				},
			},
		},
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	_, err = rewriteRelativeBindSources(raw, appPath, "/app")
	if err == nil {
		t.Fatalf("expected error for bind mount source outside app path")
	}
	if !strings.Contains(err.Error(), "outside app_path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRemoteBindSourceRootPath(t *testing.T) {
	appPath := filepath.Clean("/tmp/app")
	got, err := remoteBindSource(appPath, appPath, "/app")
	if err != nil {
		t.Fatalf("remoteBindSource() error: %v", err)
	}
	if got != "/app" {
		t.Fatalf("remoteBindSource()=%q, want /app", got)
	}
}

func TestLoginRegistriesOnRunnerNoop(t *testing.T) {
	if err := loginRegistriesOnRunner(nil, nil, nil); err != nil {
		t.Fatalf("expected empty registry list to be a no-op: %v", err)
	}
}
