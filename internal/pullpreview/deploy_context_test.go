package pullpreview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	dockerconfig "github.com/docker/cli/cli/config/configfile"
	composeapi "github.com/docker/compose/v2/pkg/api"
)

func TestRewriteProjectBindSourcesUnderAppPath(t *testing.T) {
	appPath := filepath.Clean("/tmp/app")
	project := &composetypes.Project{
		Services: composetypes.Services{
			"web": composetypes.ServiceConfig{
				Volumes: []composetypes.ServiceVolumeConfig{
					{
						Type:   composetypes.VolumeTypeBind,
						Source: filepath.Join(appPath, "dumps"),
						Target: "/dump",
					},
					{
						Type:   composetypes.VolumeTypeVolume,
						Source: "db_data",
						Target: "/var/lib/mysql",
					},
				},
			},
		},
	}

	if err := rewriteProjectBindSources(project, appPath, "/app"); err != nil {
		t.Fatalf("rewriteProjectBindSources() error: %v", err)
	}

	web := project.Services["web"]
	if web.Volumes[0].Source != "/app/dumps" {
		t.Fatalf("expected bind source rewritten to /app/dumps, got %#v", web.Volumes[0].Source)
	}
	if web.Volumes[1].Source != "db_data" {
		t.Fatalf("expected named volume unchanged, got %#v", web.Volumes[1].Source)
	}
}

func TestRewriteProjectBindSourcesRejectsAbsoluteOutsideAppPath(t *testing.T) {
	appPath := filepath.Clean("/tmp/app")
	project := &composetypes.Project{
		Services: composetypes.Services{
			"web": composetypes.ServiceConfig{
				Volumes: []composetypes.ServiceVolumeConfig{
					{
						Type:   composetypes.VolumeTypeBind,
						Source: "/tmp/other/dumps",
						Target: "/dump",
					},
				},
			},
		},
	}

	err := rewriteProjectBindSources(project, appPath, "/app")
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

func TestParseComposeRuntimeOptions(t *testing.T) {
	options := parseComposeRuntimeOptions([]string{
		"--build",
		"--force-recreate",
		"--always-recreate-deps",
		"--renew-anon-volumes",
		"--wait-timeout",
		"42",
	}, nil)

	if options.Build == nil {
		t.Fatalf("expected --build to set build options")
	}
	if options.Recreate != composeapi.RecreateForce {
		t.Fatalf("unexpected recreate strategy: %s", options.Recreate)
	}
	if options.RecreateDependencies != composeapi.RecreateForce {
		t.Fatalf("unexpected dependency recreate strategy: %s", options.RecreateDependencies)
	}
	if options.InheritVolumes {
		t.Fatalf("expected --renew-anon-volumes to disable volume inheritance")
	}
	if options.WaitTimeout != 42*time.Second {
		t.Fatalf("unexpected wait timeout: %s", options.WaitTimeout)
	}
}

func TestWriteDockerConfigDir(t *testing.T) {
	dir, cleanup, err := writeDockerConfigDir([]RegistryCredential{
		{
			Host:     "ghcr.io",
			Username: "user",
			Password: "pass",
		},
	})
	if err != nil {
		t.Fatalf("writeDockerConfigDir() error: %v", err)
	}
	defer cleanup()

	config := dockerconfig.New(filepath.Join(dir, "config.json"))
	if err := config.LoadFromReader(strings.NewReader(readFile(t, config.GetFilename()))); err != nil {
		t.Fatalf("loading config failed: %v", err)
	}
	auth, ok := config.AuthConfigs["ghcr.io"]
	if !ok {
		t.Fatalf("expected ghcr.io auth entry")
	}
	if auth.Username != "user" || auth.Password != "pass" {
		t.Fatalf("unexpected auth config: %#v", auth)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}
