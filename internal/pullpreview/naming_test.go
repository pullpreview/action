package pullpreview

import (
	"strings"
	"testing"
)

func TestNormalizeName(t *testing.T) {
	cases := map[string]string{
		"gh-123-pr-4":          "gh-123-pr-4",
		"My Repo!!":            "My-Repo",
		"foo---bar--baz":       "foo-bar-baz",
		"--leading--trailing-": "leading-trailing",
	}
	for input, expected := range cases {
		if got := NormalizeName(input); got != expected {
			t.Fatalf("NormalizeName(%q)=%q, want %q", input, got, expected)
		}
	}
}

func TestPublicDNS(t *testing.T) {
	// Default max domain length should allow full subdomain.
	t.Setenv("PULLPREVIEW_MAX_DOMAIN_LENGTH", "62")
	got := PublicDNS("feature-branch", "my.preview.run", "1.2.3.4")
	want := "feature-branch-ip-1-2-3-4.my.preview.run"
	if got != want {
		t.Fatalf("PublicDNS=%q, want %q", got, want)
	}

	// With a smaller limit, subdomain is truncated.
	t.Setenv("PULLPREVIEW_MAX_DOMAIN_LENGTH", "40")
	short := PublicDNS("verylongfeaturebranchname", "my.preview.run", "1.2.3.4")
	if short == "" || !strings.HasSuffix(short, ".my.preview.run") {
		t.Fatalf("PublicDNS unexpected value: %s", short)
	}
}
