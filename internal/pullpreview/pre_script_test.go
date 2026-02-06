package pullpreview

import (
	"strings"
	"testing"
)

func TestBuildPreScriptIncludesRegistriesAndPreScript(t *testing.T) {
	script := BuildPreScript(
		[]string{
			"docker://user:pass@ghcr.io",
			"docker://token@registry.example.com",
		},
		"./scripts/pre.sh",
		nil,
	)

	if !strings.Contains(script, `docker login "ghcr.io" -u "user" --password-stdin`) {
		t.Fatalf("expected ghcr login command in script:\n%s", script)
	}
	if !strings.Contains(script, `docker login "registry.example.com" -u "doesnotmatter" --password-stdin`) {
		t.Fatalf("expected token login command in script:\n%s", script)
	}
	if !strings.Contains(script, "bash -e ./scripts/pre.sh") {
		t.Fatalf("expected pre-script invocation in script:\n%s", script)
	}
}

func TestBuildPreScriptSkipsInvalidRegistries(t *testing.T) {
	script := BuildPreScript(
		[]string{
			"invalid",
			"https://registry.example.com",
		},
		"",
		nil,
	)

	if strings.Contains(script, "docker login") {
		t.Fatalf("expected no docker login commands for invalid registries:\n%s", script)
	}
}
