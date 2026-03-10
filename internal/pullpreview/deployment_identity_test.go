package pullpreview

import "testing"

func TestLabelScopeKey(t *testing.T) {
	if got := labelScopeKey("pullpreview"); got != "" {
		t.Fatalf("expected default label to have empty scope, got %q", got)
	}
	if got := labelScopeKey("PullPreview Helm"); got == "" {
		t.Fatalf("expected non-default label to have scope")
	}
	if gotA, gotB := labelScopeKey("pullpreview-helm"), labelScopeKey("PullPreview Helm"); gotA != gotB {
		t.Fatalf("expected canonical label scope key, got %q vs %q", gotA, gotB)
	}
}

func TestDeploymentIdentityMismatch(t *testing.T) {
	if reason, mismatch := DeploymentIdentityMismatch(
		map[string]string{
			"pullpreview_label":   "pullpreview",
			"pullpreview_target":  "compose",
			"pullpreview_runtime": "docker",
		},
		map[string]string{
			"pullpreview_label":   "pullpreview",
			"pullpreview_target":  "compose",
			"pullpreview_runtime": "docker",
		},
	); mismatch {
		t.Fatalf("expected matching identity, got mismatch %q", reason)
	}

	if reason, mismatch := DeploymentIdentityMismatch(
		map[string]string{
			"pullpreview_label":   "pullpreview-helm",
			"pullpreview_target":  "helm",
			"pullpreview_runtime": "k3s",
		},
		map[string]string{
			"pullpreview_label":   "pullpreview",
			"pullpreview_target":  "compose",
			"pullpreview_runtime": "docker",
		},
	); !mismatch || reason == "" {
		t.Fatalf("expected mismatch for different label/target/runtime")
	}

	if reason, mismatch := DeploymentIdentityMismatch(
		map[string]string{},
		map[string]string{
			"pullpreview_label":   "pullpreview",
			"pullpreview_target":  "compose",
			"pullpreview_runtime": "docker",
		},
	); mismatch {
		t.Fatalf("expected legacy default compose identity to remain compatible, got %q", reason)
	}

	if reason, mismatch := DeploymentIdentityMismatch(
		map[string]string{},
		map[string]string{
			"pullpreview_label":   "pullpreview",
			"pullpreview_target":  "helm",
			"pullpreview_runtime": "k3s",
		},
	); !mismatch || reason == "" {
		t.Fatalf("expected legacy instance mismatch for helm identity")
	}
}

