package pullpreview

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type captureRunner struct {
	args [][]string
}

func (r *captureRunner) Run(cmd *exec.Cmd) error {
	r.args = append(r.args, append([]string{}, cmd.Args...))
	return nil
}

type sshCredentialCapture struct {
	args       []string
	input      string
	privateKey string
	certKey    string
}

type sshCredentialCaptureRunner struct {
	calls []sshCredentialCapture
	err   error
}

func (r *sshCredentialCaptureRunner) Run(cmd *exec.Cmd) error {
	call := sshCredentialCapture{args: append([]string{}, cmd.Args...)}
	if cmd.Stdin != nil {
		input, err := io.ReadAll(cmd.Stdin)
		if err != nil {
			return err
		}
		call.input = string(input)
	}
	for idx, arg := range cmd.Args {
		if arg == "-i" && idx+1 < len(cmd.Args) {
			content, err := os.ReadFile(cmd.Args[idx+1])
			if err != nil {
				return err
			}
			call.privateKey = strings.TrimSpace(string(content))
		}
		if strings.HasPrefix(arg, "CertificateFile=") {
			content, err := os.ReadFile(strings.TrimPrefix(arg, "CertificateFile="))
			if err != nil {
				return err
			}
			call.certKey = strings.TrimSpace(string(content))
		}
	}
	r.calls = append(r.calls, call)
	return r.err
}

type launchSpyProvider struct {
	launchOpts     []LaunchOptions
	terminateCalls int
}

func (p *launchSpyProvider) Launch(name string, opts LaunchOptions) (AccessDetails, error) {
	p.launchOpts = append(p.launchOpts, opts)
	return AccessDetails{IPAddress: "1.2.3.4", Username: "ec2-user", PrivateKey: "PRIVATE"}, nil
}

func (p *launchSpyProvider) Terminate(name string) error {
	p.terminateCalls++
	return nil
}

func (p *launchSpyProvider) Running(name string) (bool, error) {
	return false, nil
}

func (p *launchSpyProvider) ListInstances(tags map[string]string) ([]InstanceSummary, error) {
	return nil, nil
}

func (p *launchSpyProvider) Username() string {
	return "ec2-user"
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

func TestHandoffExpiringSSHAccessUsesRunScopedKey(t *testing.T) {
	expiresAt := time.Now().Add(10 * time.Minute)
	inst := NewInstance("my-app", CommonOptions{}, fakeProvider{}, nil)
	inst.Access = AccessDetails{
		IPAddress:  "1.2.3.4",
		Username:   "ec2-user",
		PrivateKey: "TEMP PRIVATE",
		CertKey:    "TEMP CERT",
		ExpiresAt:  expiresAt,
	}
	runner := &sshCredentialCaptureRunner{}
	inst.Runner = runner

	if err := inst.handoffExpiringSSHAccess(); err != nil {
		t.Fatalf("handoffExpiringSSHAccess() error: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected one bootstrap SSH call, got %d", len(runner.calls))
	}
	if runner.calls[0].privateKey != "TEMP PRIVATE" || runner.calls[0].certKey != "TEMP CERT" {
		t.Fatalf("bootstrap did not use temporary access details: %#v", runner.calls[0])
	}
	if !strings.Contains(runner.calls[0].input, "pullpreview-run") {
		t.Fatalf("bootstrap did not append the run-scoped public key: %q", runner.calls[0].input)
	}
	if inst.Access.CertKey != "" || !inst.Access.ExpiresAt.IsZero() {
		t.Fatalf("temporary certificate remained active: %#v", inst.Access)
	}
	if _, err := ssh.ParsePrivateKey([]byte(inst.Access.PrivateKey)); err != nil {
		t.Fatalf("run-scoped private key is invalid: %v", err)
	}
	if !strings.Contains(inst.runSSHPublicKey, "pullpreview-run") {
		t.Fatalf("run-scoped public key was not retained for cleanup: %q", inst.runSSHPublicKey)
	}

	runPrivateKey := inst.Access.PrivateKey
	if err := inst.cleanupRunSSHAccess(); err != nil {
		t.Fatalf("cleanupRunSSHAccess() error: %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("expected cleanup SSH call, got %d calls", len(runner.calls))
	}
	if runner.calls[1].privateKey != runPrivateKey || runner.calls[1].certKey != "" {
		t.Fatalf("cleanup did not use the run-scoped key: %#v", runner.calls[1])
	}
	if !strings.Contains(strings.Join(runner.calls[1].args, " "), "grep -Fvx") {
		t.Fatalf("cleanup did not remove the run-scoped public key: %v", runner.calls[1].args)
	}
	if inst.runSSHPublicKey != "" {
		t.Fatalf("run-scoped public key remained after cleanup: %q", inst.runSSHPublicKey)
	}
}

func TestHandoffExpiringSSHAccessLeavesCredentialsOnFailure(t *testing.T) {
	expiresAt := time.Now().Add(10 * time.Minute)
	inst := NewInstance("my-app", CommonOptions{}, fakeProvider{}, nil)
	inst.Access = AccessDetails{
		PrivateKey: "TEMP PRIVATE",
		CertKey:    "TEMP CERT",
		ExpiresAt:  expiresAt,
	}
	inst.Runner = &sshCredentialCaptureRunner{err: errors.New("append failed")}

	err := inst.handoffExpiringSSHAccess()
	if err == nil || !strings.Contains(err.Error(), "append failed") {
		t.Fatalf("expected append failure, got %v", err)
	}
	if inst.Access.PrivateKey != "TEMP PRIVATE" || inst.Access.CertKey != "TEMP CERT" || !inst.Access.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("temporary credentials changed after failed handoff: %#v", inst.Access)
	}
	if inst.runSSHPublicKey != "" {
		t.Fatalf("failed handoff retained a cleanup key: %q", inst.runSSHPublicKey)
	}
}

func TestHandoffExpiringSSHAccessSkipsNonExpiringCredentials(t *testing.T) {
	inst := NewInstance("my-app", CommonOptions{}, fakeProvider{}, nil)
	inst.Access = AccessDetails{PrivateKey: "PRIVATE"}
	runner := &sshCredentialCaptureRunner{}
	inst.Runner = runner

	if err := inst.handoffExpiringSSHAccess(); err != nil {
		t.Fatalf("handoffExpiringSSHAccess() error: %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("non-expiring credentials triggered SSH handoff: %#v", runner.calls)
	}
	if inst.Access.PrivateKey != "PRIVATE" {
		t.Fatalf("non-expiring credentials changed: %#v", inst.Access)
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

func TestLaunchAndWaitRetriesOnceAfterSSHTimeout(t *testing.T) {
	provider := &launchSpyProvider{}
	inst := NewInstance("my-app", CommonOptions{}, provider, nil)

	originalWait := waitUntilInstanceSSHReady
	defer func() { waitUntilInstanceSSHReady = originalWait }()

	waitCalls := 0
	waitUntilInstanceSSHReady = func(ctx context.Context, probe func() bool) bool {
		waitCalls++
		return false
	}

	originalSSH := runSSHCombinedOutput
	defer func() { runSSHCombinedOutput = originalSSH }()

	runSSHCombinedOutput = func(cmd *exec.Cmd) ([]byte, error) {
		return []byte("ready-marker-missing"), errors.New("exit status 1")
	}

	err := inst.LaunchAndWait()
	if !errors.Is(err, errInstanceSSHUnavailable) {
		t.Fatalf("LaunchAndWait() error = %v, want %v", err, errInstanceSSHUnavailable)
	}
	if len(provider.launchOpts) != 2 {
		t.Fatalf("expected two launch attempts, got %d", len(provider.launchOpts))
	}
	if waitCalls != 2 {
		t.Fatalf("expected two SSH wait cycles, got %d", waitCalls)
	}
	if provider.terminateCalls != 1 {
		t.Fatalf("expected one terminate on SSH timeout, got %d", provider.terminateCalls)
	}
}
