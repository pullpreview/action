package lightsail

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/lightsail/types"
)

func TestMergeTags(t *testing.T) {
	merged := mergeTags(
		map[string]string{"stack": "pullpreview", "repo": "action"},
		map[string]string{"repo": "fork", "env": "pr"},
	)
	if merged["stack"] != "pullpreview" || merged["repo"] != "fork" || merged["env"] != "pr" {
		t.Fatalf("unexpected merged tags: %#v", merged)
	}
}

func TestMustAtoi(t *testing.T) {
	cases := map[string]int{
		"22":   22,
		" 443": 443,
		"":     0,
		"abc":  0,
		"12x":  0,
	}
	for input, want := range cases {
		if got := mustAtoi(input); got != want {
			t.Fatalf("mustAtoi(%q)=%d, want %d", input, got, want)
		}
	}
}

func TestMatchTags(t *testing.T) {
	actual := []types.Tag{
		{Key: strPtr("stack"), Value: strPtr("pullpreview")},
		{Key: strPtr("repo"), Value: strPtr("action")},
	}
	if !matchTags(actual, map[string]string{"stack": "pullpreview"}) {
		t.Fatalf("expected required subset to match")
	}
	if matchTags(actual, map[string]string{"repo": "other"}) {
		t.Fatalf("expected mismatched tag to fail")
	}
}

func TestTagsConversions(t *testing.T) {
	input := map[string]string{"a": "1", "b": "2"}
	lightTags := toLightsailTags(input)
	if len(lightTags) != 2 {
		t.Fatalf("unexpected lightsail tags: %#v", lightTags)
	}
	back := tagsToMap(lightTags)
	if back["a"] != "1" || back["b"] != "2" {
		t.Fatalf("unexpected converted map: %#v", back)
	}
}

func TestReverseSizeMap(t *testing.T) {
	if got := reverseSizeMap("small"); got != "S" {
		t.Fatalf("reverseSizeMap(small)=%q, want S", got)
	}
	if got := reverseSizeMap("custom"); got != "custom" {
		t.Fatalf("reverseSizeMap(custom)=%q, want custom", got)
	}
}

func TestUsername(t *testing.T) {
	if got := (&Provider{}).Username(); got != "ec2-user" {
		t.Fatalf("Username()=%q, want ec2-user", got)
	}
}

func strPtr(v string) *string { return &v }
