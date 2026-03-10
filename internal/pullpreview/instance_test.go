package pullpreview

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type captureRunner struct {
	args [][]string
}

func (r *captureRunner) Run(cmd *exec.Cmd) error {
	r.args = append(r.args, append([]string{}, cmd.Args...))
	return nil
}

func TestPortsWithDefaultsDeduplicatesValues(t *testing.T) {
	inst := NewInstance("example", CommonOptions{
		Ports:       []string{"443/tcp", "22", "443/tcp"},
		DefaultPort: "443",
	}, fakeProvider{}, nil)

	got := inst.PortsWithDefaults()
	expected := map[string]bool{
		"443/tcp": true,
		"22":      true,
		"443":     true,
	}
	if len(got) != len(expected) {
		t.Fatalf("unexpected ports list: %#v", got)
	}
	for _, p := range got {
		if !expected[p] {
			t.Fatalf("unexpected port %q in %#v", p, got)
		}
	}
}

func TestURLUsesHTTPSForPort443(t *testing.T) {
	inst := NewInstance("my-app", CommonOptions{DNS: "my.preview.run", DefaultPort: "443"}, fakeProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "ec2-user"}
	url := inst.URL()
	if !strings.HasPrefix(url, "https://") {
		t.Fatalf("expected https URL, got %q", url)
	}
	if !strings.Contains(url, ":443") {
		t.Fatalf("expected :443 in URL, got %q", url)
	}
}

func TestProxyTLSForcesHTTPSDefaults(t *testing.T) {
	inst := NewInstance("my-app", CommonOptions{
		DNS:         "my.preview.run",
		DefaultPort: "8080",
		Ports:       []string{"1234/tcp", "80", "80/tcp"},
		ProxyTLS:    "web:80",
	}, fakeProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "ec2-user"}

	if inst.DefaultPort != "443" {
		t.Fatalf("expected default port to be forced to 443, got %q", inst.DefaultPort)
	}
	ports := inst.PortsWithDefaults()
	expected := map[string]bool{
		"1234/tcp": true,
		"443":      true,
		"22":       true,
	}
	if len(ports) != len(expected) {
		t.Fatalf("unexpected ports list: %#v", ports)
	}
	for _, port := range ports {
		if !expected[port] {
			t.Fatalf("unexpected port %q in %#v", port, ports)
		}
	}
	if !strings.HasPrefix(inst.URL(), "https://") {
		t.Fatalf("expected https URL with proxy_tls enabled, got %q", inst.URL())
	}
}

func TestProxyTLSKeepsPort80ForHelm(t *testing.T) {
	inst := NewInstance("my-app", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		DNS:              "my.preview.run",
		DefaultPort:      "8080",
		Ports:            []string{"80/tcp", "443/tcp"},
		ProxyTLS:         "app-wordpress:80",
	}, fakeProvider{}, nil)

	ports := inst.PortsWithDefaults()
	expected := map[string]bool{
		"80/tcp":  true,
		"443/tcp": true,
		"443":     true,
		"22":      true,
	}
	if len(ports) != len(expected) {
		t.Fatalf("unexpected ports list: %#v", ports)
	}
	for _, port := range ports {
		if !expected[port] {
			t.Fatalf("unexpected port %q in %#v", port, ports)
		}
	}
}

func TestFirewallRuleTargetsPort(t *testing.T) {
	cases := []struct {
		rule   string
		port   int
		expect bool
	}{
		{rule: "80", port: 80, expect: true},
		{rule: "80/tcp", port: 80, expect: true},
		{rule: "0.0.0.0:80", port: 80, expect: true},
		{rule: "443", port: 80, expect: false},
		{rule: "8080/tcp", port: 80, expect: false},
	}

	for _, tc := range cases {
		got := firewallRuleTargetsPort(tc.rule, tc.port)
		if got != tc.expect {
			t.Fatalf("firewallRuleTargetsPort(%q, %d)=%v, want %v", tc.rule, tc.port, got, tc.expect)
		}
	}
}

func TestWriteTempKeysWritesPrivateAndCertFiles(t *testing.T) {
	inst := NewInstance("my-app", CommonOptions{}, fakeProvider{}, nil)
	inst.Access = AccessDetails{PrivateKey: "PRIVATE", CertKey: "CERT"}

	keyPath, certPath, err := inst.writeTempKeys()
	if err != nil {
		t.Fatalf("writeTempKeys() error: %v", err)
	}
	defer os.Remove(keyPath)
	defer os.Remove(certPath)

	keyContent, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("failed reading private key file: %v", err)
	}
	if !strings.Contains(string(keyContent), "PRIVATE") {
		t.Fatalf("private key not written correctly: %q", string(keyContent))
	}
	if certPath != keyPath+"-cert.pub" {
		t.Fatalf("unexpected cert file path: %q", certPath)
	}
	certContent, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("failed reading cert key file: %v", err)
	}
	if !strings.Contains(string(certContent), "CERT") {
		t.Fatalf("cert key not written correctly: %q", string(certContent))
	}
}

func TestCloneIfURLNoOpForLocalPath(t *testing.T) {
	inst := NewInstance("my-app", CommonOptions{}, fakeProvider{}, nil)
	path := filepath.Join(t.TempDir(), "app")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("failed creating local app path: %v", err)
	}
	gotPath, cleanup, err := inst.CloneIfURL(path)
	if err != nil {
		t.Fatalf("CloneIfURL() error: %v", err)
	}
	cleanup()
	if gotPath != path {
		t.Fatalf("expected local path passthrough, got %q", gotPath)
	}
}

func TestSSHBuildsCommandWithExpectedArguments(t *testing.T) {
	inst := NewInstance("my-app", CommonOptions{}, fakeProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "ec2-user", PrivateKey: "PRIVATE"}
	runner := &captureRunner{}
	inst.Runner = runner

	if err := inst.SSH("echo ok", nil); err != nil {
		t.Fatalf("SSH() error: %v", err)
	}
	if len(runner.args) != 1 {
		t.Fatalf("expected one ssh command execution, got %d", len(runner.args))
	}
	args := strings.Join(runner.args[0], " ")
	if !strings.Contains(args, "ec2-user@1.2.3.4") || !strings.Contains(args, "echo ok") {
		t.Fatalf("unexpected ssh command args: %s", args)
	}
	if !strings.Contains(args, "BatchMode=yes") || !strings.Contains(args, "IdentityAgent=none") {
		t.Fatalf("expected non-interactive ssh options, got: %s", args)
	}
}

func TestSetupSSHAccessAppendsAuthorizedKeys(t *testing.T) {
	inst := NewInstance("my-app", CommonOptions{
		AdminPublicKeys: []string{"ssh-rsa AAA", "ssh-ed25519 BBB"},
	}, fakeProvider{}, nil)
	runner := &captureRunner{}
	inst.Runner = runner

	if err := inst.SetupSSHAccess(); err != nil {
		t.Fatalf("SetupSSHAccess() error: %v", err)
	}
	if len(runner.args) != 1 {
		t.Fatalf("expected one ssh command, got %d", len(runner.args))
	}
	command := strings.Join(runner.args[0], " ")
	if !strings.Contains(command, "cat - >>") {
		t.Fatalf("expected SetupSSHAccess to append authorized_keys, command: %s", command)
	}
}

func TestSSHReadyDiagnosticIncludesRemoteDetails(t *testing.T) {
	inst := NewInstance("my-app", CommonOptions{}, fakeProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "ec2-user", PrivateKey: "PRIVATE"}

	original := runSSHCombinedOutput
	defer func() { runSSHCombinedOutput = original }()

	runSSHCombinedOutput = func(cmd *exec.Cmd) ([]byte, error) {
		args := strings.Join(cmd.Args, " ")
		if !strings.Contains(args, "ready-marker-missing") {
			t.Fatalf("expected SSH readiness diagnostic command, got %s", args)
		}
		return []byte("ready-marker-missing\n-- cloud-init status --\nstatus: error"), errors.New("exit status 1")
	}

	err := inst.SSHReadyDiagnostic()
	if err == nil {
		t.Fatalf("expected SSHReadyDiagnostic() error")
	}
	if !strings.Contains(err.Error(), "ready-marker-missing") {
		t.Fatalf("expected ready marker context in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "status: error") {
		t.Fatalf("expected cloud-init details in error, got %v", err)
	}
}
