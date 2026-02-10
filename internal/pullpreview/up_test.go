package pullpreview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type outputTestProvider struct{}

func (outputTestProvider) Launch(name string, opts LaunchOptions) (AccessDetails, error) {
	return AccessDetails{}, nil
}

func (outputTestProvider) Terminate(name string) error { return nil }

func (outputTestProvider) Running(name string) (bool, error) { return false, nil }

func (outputTestProvider) ListInstances(tags map[string]string) ([]InstanceSummary, error) {
	return nil, nil
}

func (outputTestProvider) Username() string { return "ec2-user" }

func TestWriteGithubOutputsUsesHTTPSURLWhenProxyTLSEnabled(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "github_output.txt")
	if err := os.WriteFile(outputPath, nil, 0644); err != nil {
		t.Fatalf("failed to create github output file: %v", err)
	}
	t.Setenv("GITHUB_OUTPUT", outputPath)

	inst := NewInstance("my-app", CommonOptions{ProxyTLS: "web:80", DNS: "my.preview.run"}, outputTestProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "ec2-user"}

	writeGithubOutputs(inst)

	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read github output file: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "url=https://") {
		t.Fatalf("expected https URL in github outputs, got %q", content)
	}
	if strings.Contains(content, "url=http://") {
		t.Fatalf("did not expect http URL in github outputs, got %q", content)
	}
	if !strings.Contains(content, "host=1.2.3.4") {
		t.Fatalf("expected host output, got %q", content)
	}
	if !strings.Contains(content, "username=ec2-user") {
		t.Fatalf("expected username output, got %q", content)
	}
}
