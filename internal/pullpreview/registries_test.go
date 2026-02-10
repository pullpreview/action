package pullpreview

import "testing"

func TestParseRegistryCredentials(t *testing.T) {
	got := ParseRegistryCredentials([]string{
		"docker://user:pass@ghcr.io",
		"docker://token@registry.example.com",
		"https://bad.example.com",
	}, nil)

	if len(got) != 2 {
		t.Fatalf("expected 2 valid registries, got %d", len(got))
	}

	if got[0].Host != "ghcr.io" || got[0].Username != "user" || got[0].Password != "pass" {
		t.Fatalf("unexpected first credential: %#v", got[0])
	}

	if got[1].Host != "registry.example.com" || got[1].Username != "doesnotmatter" || got[1].Password != "token" {
		t.Fatalf("unexpected second credential: %#v", got[1])
	}
}

func TestParseRegistryCredentialRejectsInvalid(t *testing.T) {
	if _, err := parseRegistryCredential("docker://@ghcr.io"); err == nil {
		t.Fatalf("expected missing token to fail")
	}
	if _, err := parseRegistryCredential("https://ghcr.io"); err == nil {
		t.Fatalf("expected non-docker scheme to fail")
	}
}
