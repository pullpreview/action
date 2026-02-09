package pullpreview

import (
	"encoding/json"
	"errors"
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

func TestMergeEnvironmentOverridesAndAdds(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"PULLPREVIEW_PUBLIC_DNS=old.preview.run",
	}
	merged := mergeEnvironment(base, map[string]string{
		"PULLPREVIEW_PUBLIC_DNS": "new.preview.run",
		"PULLPREVIEW_FIRST_RUN":  "true",
	})

	dnsCount := 0
	lookup := map[string]string{}
	for _, entry := range merged {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		lookup[parts[0]] = parts[1]
		if parts[0] == "PULLPREVIEW_PUBLIC_DNS" {
			dnsCount++
		}
	}

	if dnsCount != 1 {
		t.Fatalf("expected exactly one PULLPREVIEW_PUBLIC_DNS entry, got %d", dnsCount)
	}
	if lookup["PULLPREVIEW_PUBLIC_DNS"] != "new.preview.run" {
		t.Fatalf("expected DNS override to be applied, got %q", lookup["PULLPREVIEW_PUBLIC_DNS"])
	}
	if lookup["PULLPREVIEW_FIRST_RUN"] != "true" {
		t.Fatalf("expected PULLPREVIEW_FIRST_RUN to be added")
	}
	if lookup["PATH"] != "/usr/bin" {
		t.Fatalf("expected unrelated env vars to remain untouched")
	}
}

func TestParseComposePSOutputJSON(t *testing.T) {
	raw := `[
		{"Service":"web","Name":"app-web-1","State":"exited","Health":"","ExitCode":1},
		{"Service":"db","Name":"app-db-1","State":"running","Health":"unhealthy","ExitCode":0},
		{"Service":"cache","Name":"app-cache-1","State":"running","Health":"","ExitCode":0}
	]`

	containers, err := parseComposePSOutput(raw)
	if err != nil {
		t.Fatalf("parseComposePSOutput() error: %v", err)
	}
	if len(containers) != 3 {
		t.Fatalf("expected 3 containers, got %d", len(containers))
	}
	if containers[0].Service != "web" || containers[0].Name != "app-web-1" || containers[0].ExitCode != 1 {
		t.Fatalf("unexpected first container: %#v", containers[0])
	}
}

func TestSelectFailedContainers(t *testing.T) {
	containers := []composePSContainer{
		{Service: "web", Name: "app-web-1", State: "exited", ExitCode: 1},
		{Service: "db", Name: "app-db-1", State: "running", Health: "unhealthy"},
		{Service: "cache", Name: "app-cache-1", State: "running", ExitCode: 0},
	}
	failed := selectFailedContainers(containers)
	if len(failed) != 2 {
		t.Fatalf("expected 2 failed containers, got %d", len(failed))
	}
	if failed[0].Service != "web" {
		t.Fatalf("expected web to be first failed container, got %q", failed[0].Service)
	}
	if failed[1].Service != "db" {
		t.Fatalf("expected db to be second failed container, got %q", failed[1].Service)
	}
}

func TestRenderComposeFailureReportIncludesTroubleshooting(t *testing.T) {
	inst := NewInstance("demo", CommonOptions{DNS: "preview.run", Admins: []string{"alice", "bob"}}, outputTestProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "ec2-user"}

	failed := []composePSContainer{
		{Service: "web", Name: "app-web-1", State: "exited", ExitCode: 1},
	}
	serviceLogs := map[string]string{
		"web": "web failed to boot",
	}
	report := renderComposeFailureReport(
		inst,
		[]string{"up", "--wait", "--remove-orphans", "-d"},
		errors.New("docker compose up failed"),
		failed,
		"NAME STATUS",
		serviceLogs,
		[]string{"sample diagnostic note"},
	)

	required := []string{
		"## PullPreview Troubleshooting Report",
		"ssh ec2-user@1.2.3.4",
		"docker compose ps -a",
		"docker compose logs --tail 200 web",
		"`web` (`app-web-1`) state=`exited`",
		"sample diagnostic note",
		"web failed to boot",
	}
	for _, needle := range required {
		if !strings.Contains(report, needle) {
			t.Fatalf("expected report to contain %q, got:\n%s", needle, report)
		}
	}
}

func TestOrderedServiceLogKeysIncludesFallbackServices(t *testing.T) {
	keys := orderedServiceLogKeys(
		[]composePSContainer{{Service: "web"}},
		map[string]string{
			"web":   "one",
			"db":    "two",
			"cache": "three",
		},
	)
	if len(keys) != 3 {
		t.Fatalf("expected 3 services, got %d", len(keys))
	}
	if keys[0] != "web" {
		t.Fatalf("expected failed service first, got %q", keys[0])
	}
	if keys[1] != "cache" || keys[2] != "db" {
		t.Fatalf("expected remaining services sorted, got %#v", keys)
	}
}
