package pullpreview

import (
	"encoding/json"
	"errors"
	"os"
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

func TestCollectBindMountSyncsIncludesFilesAndDirectories(t *testing.T) {
	appPath := t.TempDir()
	dumpsPath := filepath.Join(appPath, "dumps")
	if err := os.MkdirAll(dumpsPath, 0755); err != nil {
		t.Fatalf("mkdir dumps: %v", err)
	}
	caddyPath := filepath.Join(appPath, "Caddyfile")
	if err := os.WriteFile(caddyPath, []byte("localhost"), 0644); err != nil {
		t.Fatalf("write caddyfile: %v", err)
	}

	input := map[string]any{
		"services": map[string]any{
			"proxy": map[string]any{
				"volumes": []any{
					map[string]any{
						"type":   "bind",
						"source": dumpsPath,
						"target": "/dumps",
					},
					map[string]any{
						"type":   "bind",
						"source": caddyPath,
						"target": "/etc/caddy/Caddyfile",
					},
				},
			},
		},
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	syncs, err := collectBindMountSyncs(raw, appPath, "/app")
	if err != nil {
		t.Fatalf("collectBindMountSyncs() error: %v", err)
	}
	if len(syncs) != 2 {
		t.Fatalf("expected 2 sync entries, got %d", len(syncs))
	}

	if syncs[0].RemoteSource != "/app/Caddyfile" || syncs[0].IsDir {
		t.Fatalf("unexpected first sync entry: %#v", syncs[0])
	}
	if syncs[1].RemoteSource != "/app/dumps" || !syncs[1].IsDir {
		t.Fatalf("unexpected second sync entry: %#v", syncs[1])
	}
}

func TestCollectBindMountSyncsFailsOnMissingSource(t *testing.T) {
	appPath := t.TempDir()
	input := map[string]any{
		"services": map[string]any{
			"proxy": map[string]any{
				"volumes": []any{
					map[string]any{
						"type":   "bind",
						"source": filepath.Join(appPath, "does-not-exist"),
						"target": "/missing",
					},
				},
			},
		},
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	_, err = collectBindMountSyncs(raw, appPath, "/app")
	if err == nil {
		t.Fatalf("expected missing-source error")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInlinePreScriptLoadsLocalScriptContent(t *testing.T) {
	appPath := t.TempDir()
	if err := os.MkdirAll(filepath.Join(appPath, "scripts"), 0755); err != nil {
		t.Fatalf("mkdir scripts: %v", err)
	}
	scriptPath := filepath.Join(appPath, "scripts", "pre.sh")
	if err := os.WriteFile(scriptPath, []byte("echo hello from pre-script\n"), 0755); err != nil {
		t.Fatalf("write pre script: %v", err)
	}

	inst := NewInstance("demo", CommonOptions{PreScript: "scripts/pre.sh"}, outputTestProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "ec2-user"}

	inline, err := inst.inlinePreScript(appPath)
	if err != nil {
		t.Fatalf("inlinePreScript() error: %v", err)
	}
	if !strings.Contains(inline, "source /etc/pullpreview/env") {
		t.Fatalf("expected inline script to source pullpreview env, got %q", inline)
	}
	if !strings.Contains(inline, "echo hello from pre-script") {
		t.Fatalf("expected inline script to contain pre-script body, got %q", inline)
	}
}

func TestParseDockerPSOutputJSONLines(t *testing.T) {
	raw := strings.Join([]string{
		`{"Names":"app-web-1","Status":"Exited (1) 5 seconds ago","Labels":"com.docker.compose.project=app,com.docker.compose.service=web"}`,
		`{"Names":"app-db-1","Status":"Up 2 minutes (unhealthy)","Labels":"com.docker.compose.project=app,com.docker.compose.service=db"}`,
		`{"Names":"app-cache-1","Status":"Up 2 minutes","Labels":"com.docker.compose.project=app,com.docker.compose.service=cache"}`,
	}, "\n")

	containers, err := parseDockerPSOutput(raw)
	if err != nil {
		t.Fatalf("parseDockerPSOutput() error: %v", err)
	}
	if len(containers) != 3 {
		t.Fatalf("expected 3 containers, got %d", len(containers))
	}
	if containers[0].Service != "web" || containers[0].Name != "app-web-1" || containers[0].State != "exited" || containers[0].ExitCode != 1 {
		t.Fatalf("unexpected first container: %#v", containers[0])
	}
	if containers[1].Service != "db" || containers[1].Health != "unhealthy" || containers[1].State != "running" {
		t.Fatalf("unexpected second container: %#v", containers[1])
	}
}

func TestSelectFailedContainers(t *testing.T) {
	containers := []composePSContainer{
		{Service: "web", Name: "app-web-1", State: "exited", ExitCode: 1},
		{Service: "web", Name: "app-web-2", State: "exited", ExitCode: 2},
		{Service: "db", Name: "app-db-1", State: "running", Health: "unhealthy"},
		{Service: "cache", Name: "app-cache-1", State: "running", ExitCode: 0},
	}
	failed := selectFailedContainers(containers)
	if len(failed) != 3 {
		t.Fatalf("expected 3 failed containers, got %d", len(failed))
	}
	if failed[0].Service != "web" {
		t.Fatalf("expected web to be first failed container, got %q", failed[0].Service)
	}
	if failed[1].Name != "app-web-2" {
		t.Fatalf("expected second failed container to be app-web-2, got %q", failed[1].Name)
	}
	if failed[2].Service != "db" {
		t.Fatalf("expected db to be third failed container, got %q", failed[2].Service)
	}
}

func TestRenderComposeFailureReportIncludesTroubleshooting(t *testing.T) {
	inst := NewInstance("demo", CommonOptions{DNS: "preview.run", Admins: []string{"alice", "bob"}}, outputTestProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "ec2-user"}

	containers := []composePSContainer{
		{Service: "web", Name: "app-web-1", State: "exited", ExitCode: 1},
		{Service: "db", Name: "app-db-1", State: "running", Health: "healthy", ExitCode: 0},
	}
	failed := []composePSContainer{
		{Service: "web", Name: "app-web-1", State: "exited", ExitCode: 1},
	}
	report := renderComposeFailureReport(
		inst,
		[]string{"up", "--wait", "--remove-orphans", "-d"},
		errors.New("docker compose up failed"),
		containers,
		failed,
		"NAME STATUS",
		[]string{"sample diagnostic note"},
	)

	required := []string{
		"## PullPreview Troubleshooting Report",
		"ssh ec2-user@1.2.3.4",
		"docker ps -a",
		"Container Health Overview",
		"Total containers: `2`",
		"Failed: `1`",
		"`web` (`app-web-1`) state=`exited`",
		"sample diagnostic note",
		"status=`failed`",
	}
	for _, needle := range required {
		if !strings.Contains(report, needle) {
			t.Fatalf("expected report to contain %q, got:\n%s", needle, report)
		}
	}
	if strings.Contains(report, "Last 20 log lines") {
		t.Fatalf("did not expect report to include container logs, got:\n%s", report)
	}
	if strings.Contains(report, "docker compose ps -a") {
		t.Fatalf("did not expect report to include docker compose ps command, got:\n%s", report)
	}
}

func TestFailedContainerLogKeyPrefersContainerName(t *testing.T) {
	if got := failedContainerLogKey(composePSContainer{Name: "app-web-1", Service: "web"}); got != "app-web-1" {
		t.Fatalf("expected container name key, got %q", got)
	}
	if got := failedContainerLogKey(composePSContainer{Service: "web"}); got != "web" {
		t.Fatalf("expected service fallback key, got %q", got)
	}
}
