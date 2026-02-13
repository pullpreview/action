package hetzner

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/pullpreview/action/internal/pullpreview"
)

func TestParseConfigFromEnv(t *testing.T) {
	_, caKey, err := mustGenerateSSHKeyPair()
	if err != nil {
		t.Fatalf("failed to generate ca key: %v", err)
	}
	cfgRaw, err := ParseConfigFromEnv(map[string]string{
		"HCLOUD_TOKEN":   "abc",
		"REGION":         "fra1",
		"IMAGE":          "debian-12",
		"HETZNER_CA_KEY": caKey,
	})
	if err != nil {
		t.Fatalf("ParseConfigFromEnv() error: %v", err)
	}
	cfg := cfgRaw.(Config)
	if cfg.APIToken != "abc" {
		t.Fatalf("token = %q, want %q", cfg.APIToken, "abc")
	}
	if cfg.Location != "fra1" || cfg.Image != "debian-12" || cfg.CAKey != caKey || cfg.SSHUsername != defaultHetznerSSHUser {
		t.Fatalf("unexpected parsed config: %#v", cfg)
	}

	caKeyFile := filepath.Join(t.TempDir(), "hetzner-ca.key")
	if err := os.WriteFile(caKeyFile, []byte(caKey), 0600); err != nil {
		t.Fatalf("failed to write CA key file: %v", err)
	}
	cfgRaw, err = ParseConfigFromEnv(map[string]string{
		"HCLOUD_TOKEN":   "fallback",
		"HETZNER_CA_KEY": caKeyFile,
	})
	if err != nil {
		t.Fatalf("ParseConfigFromEnv() with file path error: %v", err)
	}
	cfg = cfgRaw.(Config)
	if cfg.APIToken != "fallback" {
		t.Fatalf("unexpected token value: %q", cfg.APIToken)
	}
	if cfg.Location != defaultHetznerLocation || cfg.Image != defaultHetznerImage || cfg.CAKey != caKeyFile || cfg.SSHUsername != defaultHetznerSSHUser {
		t.Fatalf("expected defaults and file-backed CA key path, got %#v", cfg)
	}

	if _, err := ParseConfigFromEnv(map[string]string{"HCLOUD_TOKEN": "fallback", "HETZNER_CA_KEY": ""}); err == nil {
		t.Fatalf("expected missing CA key error")
	}
	if _, err := ParseConfigFromEnv(map[string]string{"HCLOUD_TOKEN": "fallback"}); err == nil {
		t.Fatalf("expected missing CA key error")
	}
	if _, err := ParseConfigFromEnv(map[string]string{"HETZNER_CA_KEY": caKey}); err == nil {
		t.Fatalf("expected missing token error")
	}
	if _, err := ParseConfigFromEnv(map[string]string{"HCLOUD_TOKEN": "fallback", "HETZNER_CA_KEY": "not-a-key"}); err == nil {
		t.Fatalf("expected invalid CA key error")
	}
}

func TestBuildUserDataBranchesAndPaths(t *testing.T) {
	p := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           "ubuntu-24.04",
		SSHUsername:     "root",
		SSHKeysCacheDir: t.TempDir(),
	})
	opts := pullpreview.UserDataOptions{
		AppPath:       "/app",
		Username:      "ec2-user",
		SSHPublicKeys: []string{"ssh-ed25519 AAA", "ssh-rsa BBB"},
	}
	script, err := p.BuildUserData(opts)
	if err != nil {
		t.Fatalf("BuildUserData() error: %v", err)
	}
	if !strings.Contains(script, fmt.Sprintf("IMAGE_NAME=%q", "ubuntu-24.04")) {
		t.Fatalf("missing image declaration: %s", script)
	}
	if !strings.Contains(script, "if command -v apt-get >/dev/null 2>&1; then") {
		t.Fatalf("missing apt branch: %s", script)
	}
	if !strings.Contains(script, "DISTRO=ubuntu") {
		t.Fatalf("missing ubuntu distro branch: %s", script)
	}
	if !strings.Contains(script, "mkdir -p /home/ec2-user/.ssh") {
		t.Fatalf("missing SSH key directory setup: %s", script)
	}
	if !strings.Contains(script, "cp /root/.ssh/authorized_keys /home/ec2-user/.ssh/authorized_keys") {
		t.Fatalf("missing authorized key propagation from root: %s", script)
	}
	if !strings.Contains(script, "chown -R ec2-user:ec2-user /home/ec2-user/.ssh") {
		t.Fatalf("missing user path + chown: %s", script)
	}
	if !strings.Contains(script, "chmod 0700 /home/ec2-user/.ssh && chmod 0600 /home/ec2-user/.ssh/authorized_keys") {
		t.Fatalf("missing ssh key file protection setup: %s", script)
	}
	if !strings.Contains(script, "echo 'ssh-ed25519 AAA\nssh-rsa BBB' >> /home/ec2-user/.ssh/authorized_keys") {
		t.Fatalf("missing authorized_keys injection: %s", script)
	}
	if !strings.Contains(script, "cat <<'EOF' > /etc/ssh/pullpreview-user-ca.pub") {
		t.Fatalf("missing pullpreview user CA key install step: %s", script)
	}
	if !strings.Contains(script, "TrustedUserCAKeys /etc/ssh/pullpreview-user-ca.pub") {
		t.Fatalf("missing TrustedUserCAKeys directive: %s", script)
	}
	if !strings.Contains(script, "systemctl restart ssh || systemctl restart sshd || true") {
		t.Fatalf("missing SSH restart for CA trust config: %s", script)
	}

	noKeyScript, err := p.BuildUserData(pullpreview.UserDataOptions{
		AppPath:  "/app",
		Username: "ec2-user",
	})
	if err != nil {
		t.Fatalf("BuildUserData() error: %v", err)
	}
	if !strings.Contains(noKeyScript, "cp /root/.ssh/authorized_keys /home/ec2-user/.ssh/authorized_keys") {
		t.Fatalf("expected root authorized_keys propagation for non-root users: %s", noKeyScript)
	}

	debian := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           "debian-12",
		SSHUsername:     "root",
		SSHKeysCacheDir: t.TempDir(),
	})
	debianScript, err := debian.BuildUserData(pullpreview.UserDataOptions{AppPath: "/app", Username: "root"})
	if err != nil {
		t.Fatalf("BuildUserData() error: %v", err)
	}
	if !strings.Contains(debianScript, "DISTRO=debian") {
		t.Fatalf("missing debian distro branch: %s", debianScript)
	}
	if strings.Contains(debianScript, "authorized_keys") {
		t.Fatalf("did not expect authorized_keys setup without keys: %s", debianScript)
	}
}

func TestValidateSSHPrivateKeyFormat(t *testing.T) {
	_, key, err := generateSSHKeyPair("test")
	if err != nil {
		t.Fatalf("generateSSHKeyPair() error: %v", err)
	}
	if err := validateSSHPrivateKeyFormat(key); err != nil {
		t.Fatalf("expected valid PEM key to pass: %v", err)
	}
	if err := validateSSHPrivateKeyFormat(""); err == nil {
		t.Fatalf("expected empty key to fail")
	}
	if err := validateSSHPrivateKeyFormat("bad"); err == nil {
		t.Fatalf("expected non-pem key to fail")
	}
}

func TestParseFirewallPortAndRules(t *testing.T) {
	start, end, proto, err := parseFirewallPort("443/TCP")
	if err != nil || start != 443 || end != 443 || strings.ToLower(proto) != "tcp" {
		t.Fatalf("unexpected parseFirewallPort result: %d-%d/%s err=%v", start, end, proto, err)
	}
	start, end, proto, err = parseFirewallPort("1024-1028/udp")
	if err != nil || start != 1024 || end != 1028 || proto != "udp" {
		t.Fatalf("unexpected parseFirewallPort udp range: %d-%d/%s err=%v", start, end, proto, err)
	}
	start, end, proto, err = parseFirewallPort("5/icmp")
	if err != nil || start != 5 || end != 5 || proto != "icmp" {
		t.Fatalf("unexpected parseFirewallPort icmp: %d-%d/%s err=%v", start, end, proto, err)
	}
	if _, _, _, err := parseFirewallPort("junk"); err == nil {
		t.Fatalf("expected invalid firewall port to fail")
	}
	if _, _, _, err := parseFirewallPort("70000"); err == nil {
		t.Fatalf("expected out-of-range port to fail")
	}

	rules, err := parseFirewallRules([]string{"443/TCP", "80/udp", "1/ICMP", "443/tcp"}, []string{"10.0.0.1"})
	if err != nil {
		t.Fatalf("parseFirewallRules() error: %v", err)
	}
	if len(rules) < 3 {
		t.Fatalf("expected at least 3 rules with implicit ssh, got %d", len(rules))
	}
	contains := map[string]bool{}
	for _, rule := range rules {
		contains[firewallRuleSignature(rule)] = true
	}
	key22 := firewallRuleSignature(hcloud.FirewallRule{
		Direction: hcloud.FirewallRuleDirectionIn,
		Protocol:  hcloud.FirewallRuleProtocolTCP,
		Port:      strPtr("22"),
		SourceIPs: []net.IPNet{*mustParseCIDRForTest("0.0.0.0/0")},
	})
	if _, ok := contains[key22]; !ok {
		t.Fatalf("expected implicit SSH rule in parsed result")
	}
}

func TestFirewallRulesMatchAndSignatureNormalization(t *testing.T) {
	base := hcloud.FirewallRule{
		Direction: hcloud.FirewallRuleDirectionIn,
		SourceIPs: []net.IPNet{*mustParseCIDRForTest("10.0.0.0/8"), *mustParseCIDRForTest("192.168.0.0/24")},
		Protocol:  hcloud.FirewallRuleProtocolTCP,
		Port:      strPtr("443"),
	}
	upper := hcloud.FirewallRule{
		Direction:   hcloud.FirewallRuleDirectionIn,
		SourceIPs:   []net.IPNet{*mustParseCIDRForTest("192.168.0.0/24"), *mustParseCIDRForTest("10.0.0.0/8")},
		Protocol:    hcloud.FirewallRuleProtocolTCP,
		Port:        strPtr("443"),
		Description: strPtr("A"),
	}
	diff := hcloud.FirewallRule{
		Direction: hcloud.FirewallRuleDirectionIn,
		Protocol:  hcloud.FirewallRuleProtocolUDP,
		Port:      strPtr("443"),
	}
	if !firewallRulesMatch([]hcloud.FirewallRule{base}, []hcloud.FirewallRule{upper}) {
		t.Fatalf("expected rule signatures to match case/order normalized")
	}
	if firewallRulesMatch([]hcloud.FirewallRule{base}, []hcloud.FirewallRule{diff}) {
		t.Fatalf("expected rules with different protocol to mismatch")
	}
}

func TestMakeFirewallUsesPullpreviewPrefix(t *testing.T) {
	provider := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           defaultHetznerImage,
		SSHUsername:     defaultHetznerSSHUser,
		SSHKeysCacheDir: t.TempDir(),
	})
	client := &fakeHcloudClient{
		firewallCreateResult: hcloud.FirewallCreateResult{
			Firewall: &hcloud.Firewall{
				ID:    123,
				Name:  "ignored",
				Rules: []hcloud.FirewallRule{},
			},
		},
	}
	provider.client = client

	name := "Feature/Thing"
	_, err := provider.makeFirewall(name, []string{"80/tcp"}, []string{"0.0.0.0/0"})
	if err != nil {
		t.Fatalf("makeFirewall() error: %v", err)
	}
	if client.firewallCreateLastOpts == nil {
		t.Fatalf("expected firewall create opts to be captured")
	}
	want := fmt.Sprintf("pullpreview-%s", sanitizeNameForHetzner(name))
	if client.firewallCreateLastOpts.Name != want {
		t.Fatalf("unexpected firewall name: got=%q want=%q", client.firewallCreateLastOpts.Name, want)
	}
}

func TestTerminateDeletesCanonicalFirewallWhenServerMissing(t *testing.T) {
	cacheDir := t.TempDir()
	provider := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           defaultHetznerImage,
		SSHUsername:     defaultHetznerSSHUser,
		SSHKeysCacheDir: cacheDir,
	})
	name := "gh-1-pr-1"
	cachePath := provider.cachePath(name)
	if err := provider.saveCachedSSHCredentials(name, cachedHetznerSSHCredentials{
		InstanceName: name,
		PrivateKey:   "private",
	}); err != nil {
		t.Fatalf("failed to setup cache: %v", err)
	}
	canonicalName := fmt.Sprintf("pullpreview-%s", sanitizeNameForHetzner(name))
	client := &fakeHcloudClient{
		serverListResponses: [][]*hcloud.Server{{}},
		firewallByNameMap: map[string]*hcloud.Firewall{
			canonicalName: {ID: 10, Name: canonicalName},
		},
	}
	provider.client = client

	if err := provider.Terminate(name); err != nil {
		t.Fatalf("Terminate() error: %v", err)
	}
	if client.serverDeleteCalls != 0 {
		t.Fatalf("expected no server delete when server is missing, got %d", client.serverDeleteCalls)
	}
	if client.firewallDeleteCalls != 1 {
		t.Fatalf("expected one firewall delete, got %d (%v)", client.firewallDeleteCalls, client.firewallDeleteNames)
	}
	if len(client.firewallDeleteNames) != 1 || client.firewallDeleteNames[0] != canonicalName {
		t.Fatalf("unexpected deleted firewalls: %v", client.firewallDeleteNames)
	}
	if _, statErr := os.Stat(cachePath); !os.IsNotExist(statErr) {
		t.Fatalf("expected cache file to be removed, stat err=%v", statErr)
	}
}

func TestCachePathAndRoundTrip(t *testing.T) {
	cacheDir := t.TempDir()
	provider := &Provider{sshKeysCacheDir: cacheDir}

	name := "Test Instance/Name!!"
	expectedFile := hetznerSSHKeyCacheFilenameSanitizer.ReplaceAllString(strings.ToLower(name), "-")
	expectedFile = strings.Trim(expectedFile, "-")
	expected := filepath.Join(cacheDir, expectedFile+"."+hetznerSSHKeyCacheExt)
	if got := provider.cachePath(name); got != expected {
		t.Fatalf("cachePath()=%q, want %q", got, expected)
	}
	if got := (&Provider{sshKeysCacheDir: "  "}).cachePath("x"); got != "" {
		t.Fatalf("expected empty cache path when cache dir missing")
	}

	payload := cachedHetznerSSHCredentials{
		InstanceName: "gh-1-pr-1",
		PrivateKey:   "private",
		PublicKey:    "public",
		SSHKeyID:     99,
		SSHKeyName:   "name",
	}
	if err := provider.saveCachedSSHCredentials(name, payload); err != nil {
		t.Fatalf("saveCachedSSHCredentials() error: %v", err)
	}
	loaded, ok := provider.loadCachedSSHCredentials(name)
	if !ok {
		t.Fatalf("expected cache load success")
	}
	if loaded.PrivateKey != payload.PrivateKey || loaded.SSHKeyID != 99 {
		t.Fatalf("unexpected loaded payload: %#v", loaded)
	}

	path := provider.cachePath(name)
	if err := os.WriteFile(path, []byte("not-json"), 0600); err != nil {
		t.Fatalf("failed to write invalid cache payload")
	}
	if _, ok := provider.loadCachedSSHCredentials(name); ok {
		t.Fatalf("expected invalid cache payload to fail")
	}
	if _, ok := provider.loadCachedSSHCredentials("missing-unknown-instance"); ok {
		t.Fatalf("expected missing cache payload to fail")
	}
}

func TestHetznerLabelSanitizationAndListMatching(t *testing.T) {
	provider := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           defaultHetznerImage,
		SSHUsername:     defaultHetznerSSHUser,
		SSHKeysCacheDir: t.TempDir(),
	})
	instance := "gh-255978101-pr-122"
	key := mustTestSSHKey(33)
	created := makeTestServer(instance, "203.0.113.10", hcloud.ServerStatusRunning, nil)
	client := &fakeHcloudClient{
		sshKeyCreateResult: key,
		serverCreateResult: hcloud.ServerCreateResult{Server: created},
	}
	provider.client = client

	_, err := provider.Launch(instance, pullpreview.LaunchOptions{
		Tags: map[string]string{
			"pullpreview_repo":   "pullpreview/action",
			"pullpreview_branch": "feature/add-ssl/path",
			"repo_name":          "pull-preview/action",
			"org_name":           "pullpreview/ORG",
			"version":            "1.2.3+meta",
			"stack":              "pullpreview",
			"_invalid":           "_leading",
			"valid":              "-abc_123-",
			"longvalue":          strings.Repeat("a", 100),
		},
	})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}
	if client.serverCreateLastOpts == nil {
		t.Fatalf("expected captured server create opts")
	}
	if got := client.serverCreateLastOpts.Labels["pullpreview_repo"]; got != "pullpreview-action" {
		t.Fatalf("pullpreview_repo label not sanitized: %q", got)
	}
	if got := client.serverCreateLastOpts.Labels["pullpreview_branch"]; got != "feature-add-ssl-path" {
		t.Fatalf("pullpreview_branch label not sanitized: %q", got)
	}
	if got := client.serverCreateLastOpts.Labels["org_name"]; got != "pullpreview-org" {
		t.Fatalf("org_name label not sanitized: %q", got)
	}
	if got := client.serverCreateLastOpts.Labels["version"]; got != "1.2.3-meta" {
		t.Fatalf("version label not sanitized: %q", got)
	}
	if got := client.serverCreateLastOpts.Labels["invalid"]; got != "leading" {
		t.Fatalf("leading edge chars not trimmed in key: %q", got)
	}
	if got := client.serverCreateLastOpts.Labels["valid"]; got != "abc_123" {
		t.Fatalf("leading/trailing edge chars not trimmed: %q", got)
	}
	if got := client.serverCreateLastOpts.Labels["longvalue"]; got != strings.Repeat("a", 63) {
		t.Fatalf("long value should be truncated to 63 chars, got %q", got)
	}

	// Verify list matching still works when caller passes unsanitized tags.
	server := makeTestServer(instance, "203.0.113.10", hcloud.ServerStatusRunning, nil)
	server.Labels = client.serverCreateLastOpts.Labels
	client.serverListResponses = [][]*hcloud.Server{{server}}
	instances, err := provider.ListInstances(map[string]string{
		"pullpreview_repo":   "pullpreview/action",
		"pullpreview_branch": "feature/add-ssl/path",
		"org_name":           "pullpreview/ORG",
		"version":            "1.2.3+meta",
	})
	if err != nil {
		t.Fatalf("ListInstances() error: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("expected one matching instance, got %d", len(instances))
	}
}

func TestHetznerLaunchLifecycleReusesExistingServerWithCert(t *testing.T) {
	provider := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           defaultHetznerImage,
		SSHUsername:     defaultHetznerSSHUser,
		SSHKeysCacheDir: t.TempDir(),
	})
	existing := makeTestServer("gh-1-pr-1", "198.51.100.1", hcloud.ServerStatusRunning, nil)
	client := &fakeHcloudClient{
		serverListResponses: [][]*hcloud.Server{{existing}},
	}
	provider.client = client
	originalRunSSHCommand := runSSHCommand
	defer func() { runSSHCommand = originalRunSSHCommand }()
	runSSHCommand = func(context.Context, string, string, string, string) ([]byte, error) {
		return []byte("ok"), nil
	}

	access, err := provider.Launch("gh-1-pr-1", pullpreview.LaunchOptions{})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}
	if access.IPAddress != existing.PublicNet.IPv4.IP.String() {
		t.Fatalf("unexpected access ip: %#v", access.IPAddress)
	}
	if access.CertKey == "" {
		t.Fatalf("expected cert key in reused access details")
	}
	if client.serverListCalls != 1 {
		t.Fatalf("expected one server list call, got %d", client.serverListCalls)
	}
	if client.serverDeleteCalls != 0 {
		t.Fatalf("expected no delete calls, got %d", client.serverDeleteCalls)
	}
	if client.serverCreateCalls != 0 {
		t.Fatalf("expected no create calls, got %d", client.serverCreateCalls)
	}
}

func TestHetznerLaunchLifecycleRecreateWhenCacheMissing(t *testing.T) {
	provider := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           defaultHetznerImage,
		SSHUsername:     defaultHetznerSSHUser,
		SSHKeysCacheDir: t.TempDir(),
	})
	instance := "gh-1-pr-1"
	existing := makeTestServer(instance, "198.51.100.1", hcloud.ServerStatusRunning, nil)
	created := makeTestServer(instance, "203.0.113.10", hcloud.ServerStatusRunning, nil)
	client := &fakeHcloudClient{
		serverListResponses: [][]*hcloud.Server{{existing}, nil},
		sshKeyCreateResult:  mustTestSSHKey(1),
		serverCreateResult:  hcloud.ServerCreateResult{Server: created},
	}
	provider.client = client
	originalRunSSHCommand := runSSHCommand
	defer func() { runSSHCommand = originalRunSSHCommand }()
	runSSHCommand = func(_ context.Context, _ string, certFile string, _ string, _ string) ([]byte, error) {
		if strings.TrimSpace(certFile) != "" {
			return nil, fmt.Errorf("ssh unavailable")
		}
		return []byte("ok"), nil
	}

	_, err := provider.Launch(instance, pullpreview.LaunchOptions{})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}
	if client.serverListCalls != 2 {
		t.Fatalf("expected two server list calls, got %d", client.serverListCalls)
	}
	if client.serverDeleteCalls != 1 {
		t.Fatalf("expected one delete call for SSH precheck failure, got %d", client.serverDeleteCalls)
	}
	if client.serverCreateCalls != 1 {
		t.Fatalf("expected one create call for SSH precheck failure, got %d", client.serverCreateCalls)
	}
}

func TestHetznerLaunchLifecycleRecreateWhenPublicIPMissing(t *testing.T) {
	provider := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           defaultHetznerImage,
		SSHUsername:     defaultHetznerSSHUser,
		SSHKeysCacheDir: t.TempDir(),
	})
	instance := "gh-1-pr-1"
	existing := makeTestServer(instance, "", hcloud.ServerStatusRunning, nil)
	created := makeTestServer(instance, "203.0.113.10", hcloud.ServerStatusRunning, nil)
	client := &fakeHcloudClient{
		serverListResponses: [][]*hcloud.Server{{existing}, nil},
		sshKeyCreateResult:  mustTestSSHKey(12),
		serverCreateResult:  hcloud.ServerCreateResult{Server: created},
	}
	provider.client = client
	originalRunSSHCommand := runSSHCommand
	defer func() { runSSHCommand = originalRunSSHCommand }()
	runSSHCommand = func(context.Context, string, string, string, string) ([]byte, error) {
		return []byte("ok"), nil
	}

	_, err := provider.Launch(instance, pullpreview.LaunchOptions{UserData: "userdata"})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}
	if client.serverListCalls != 2 {
		t.Fatalf("expected two server list calls, got %d", client.serverListCalls)
	}
	if client.serverDeleteCalls != 1 {
		t.Fatalf("expected one delete call when public IP missing, got %d", client.serverDeleteCalls)
	}
	if client.serverCreateCalls != 1 {
		t.Fatalf("expected one create call when public IP missing, got %d", client.serverCreateCalls)
	}
}

func TestHetznerCreateLifecycleRecreateWhenSSHPrecheckFails(t *testing.T) {
	cacheDir := t.TempDir()
	provider := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           defaultHetznerImage,
		SSHUsername:     defaultHetznerSSHUser,
		SSHKeysCacheDir: cacheDir,
	})
	instance := "gh-1-pr-1"
	created := makeTestServer(instance, "203.0.113.10", hcloud.ServerStatusRunning, nil)
	client := &fakeHcloudClient{
		serverListResponses: [][]*hcloud.Server{{nil}},
		serverCreateResult:  hcloud.ServerCreateResult{Server: created},
		sshKeyCreateResult:  mustTestSSHKey(12),
	}
	provider.client = client

	originalRunSSHCommand := runSSHCommand
	defer func() { runSSHCommand = originalRunSSHCommand }()
	runSSHCommand = func(context.Context, string, string, string, string) ([]byte, error) {
		return nil, fmt.Errorf("ssh unavailable")
	}

	_, err := provider.Launch(instance, pullpreview.LaunchOptions{})
	if err == nil {
		t.Fatalf("expected Launch() error")
	}
	if !strings.Contains(err.Error(), "ssh unavailable") {
		t.Fatalf("expected ssh validation failure, got %v", err)
	}
	if client.serverDeleteCalls != 1 {
		t.Fatalf("expected one delete call on create SSH precheck failure, got %d", client.serverDeleteCalls)
	}
	if client.sshKeyDeleteCalls != 1 {
		t.Fatalf("expected one ssh key delete call on create SSH precheck failure, got %d", client.sshKeyDeleteCalls)
	}
}

func TestHetznerCreateFailureCleansUpServerAndKey(t *testing.T) {
	cacheDir := t.TempDir()
	provider := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           defaultHetznerImage,
		SSHUsername:     defaultHetznerSSHUser,
		SSHKeysCacheDir: cacheDir,
	})
	instance := "gh-1-pr-1"
	createdServer := makeTestServer(instance, "203.0.113.10", hcloud.ServerStatusRunning, nil)
	key := mustTestSSHKey(99)
	client := &fakeHcloudClient{
		sshKeyCreateResult: key,
		serverCreateResult: hcloud.ServerCreateResult{Server: createdServer, Action: &hcloud.Action{}},
		waitForErr:         fmt.Errorf("action failed"),
		sshKeyDeleteCalls:  0,
		serverDeleteResult: &hcloud.ServerDeleteResult{},
	}
	provider.client = client

	_, err := provider.Launch(instance, pullpreview.LaunchOptions{})
	if err == nil || !strings.Contains(err.Error(), "action failed") {
		t.Fatalf("expected launch failure from action, got %v", err)
	}
	if client.serverDeleteCalls != 1 {
		t.Fatalf("expected cleanup server delete, got %d", client.serverDeleteCalls)
	}
	if client.sshKeyDeleteCalls == 0 {
		t.Fatalf("expected ssh key cleanup on create failure")
	}
}

func TestDestroyInstanceAndCacheSkipsCacheOnDeleteFailure(t *testing.T) {
	cacheDir := t.TempDir()
	provider := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           defaultHetznerImage,
		SSHUsername:     defaultHetznerSSHUser,
		SSHKeysCacheDir: cacheDir,
	})
	instance := "gh-1-pr-1"
	server := makeTestServer(instance, "198.51.100.1", hcloud.ServerStatusRunning, nil)
	cachePath := provider.cachePath(instance)
	if err := provider.saveCachedSSHCredentials(instance, cachedHetznerSSHCredentials{
		InstanceName: instance,
		PrivateKey:   "private",
	}); err != nil {
		t.Fatalf("failed to setup cache: %v", err)
	}
	client := &fakeHcloudClient{
		serverDeleteError: fmt.Errorf("delete failed"),
	}
	provider.client = client

	err := provider.destroyInstanceAndCache(server, instance)
	if err == nil || !strings.Contains(err.Error(), "failed to delete instance") {
		t.Fatalf("expected wrapped delete error, got %v", err)
	}
	if _, statErr := os.Stat(cachePath); statErr != nil {
		t.Fatalf("expected cache file to persist after delete failure, got %v", statErr)
	}
}

type fakeHcloudClient struct {
	serverListCalls     int
	serverCreateCalls   int
	serverDeleteCalls   int
	sshKeyCreateCalls   int
	sshKeyDeleteCalls   int
	serverListResponses [][]*hcloud.Server
	serverListError     error

	sshKeyCreateResult *hcloud.SSHKey
	sshKeyCreateError  error

	sshKeyByID         map[int64]*hcloud.SSHKey
	sshKeyGetByIDError error

	serverCreateResult   hcloud.ServerCreateResult
	serverCreateError    error
	serverCreateLastOpts *hcloud.ServerCreateOpts

	serverDeleteResult *hcloud.ServerDeleteResult
	serverDeleteError  error

	serverPoweronResult *hcloud.Action
	serverPoweronError  error

	firewallByNameCalls int
	firewallGetByNameFn func(context.Context, string) (*hcloud.Firewall, *hcloud.Response, error)
	firewallByNameMap   map[string]*hcloud.Firewall

	firewallCreateResult   hcloud.FirewallCreateResult
	firewallCreateError    error
	firewallCreateCalls    int
	firewallCreateLastOpts *hcloud.FirewallCreateOpts

	firewallSetRulesCalls int
	firewallSetRulesError error
	firewallApplyCalls    int
	firewallApplyError    error
	firewallDeleteCalls   int
	firewallDeleteNames   []string

	waitForCalls int
	waitForErr   error
}

func (f *fakeHcloudClient) SSHKeyCreate(ctx context.Context, opts hcloud.SSHKeyCreateOpts) (*hcloud.SSHKey, *hcloud.Response, error) {
	f.sshKeyCreateCalls++
	if f.sshKeyCreateError != nil {
		return nil, nil, f.sshKeyCreateError
	}
	if f.sshKeyCreateResult == nil {
		return mustTestSSHKey(0), nil, nil
	}
	return f.sshKeyCreateResult, nil, nil
}

func (f *fakeHcloudClient) SSHKeyDelete(ctx context.Context, key *hcloud.SSHKey) (*hcloud.Response, error) {
	f.sshKeyDeleteCalls++
	if f.sshKeyCreateError != nil {
		return nil, f.sshKeyCreateError
	}
	if f.sshKeyGetByIDError != nil {
		return nil, f.sshKeyGetByIDError
	}
	return nil, nil
}

func (f *fakeHcloudClient) SSHKeyGetByID(ctx context.Context, id int64) (*hcloud.SSHKey, *hcloud.Response, error) {
	if f.sshKeyGetByIDError != nil {
		return nil, nil, f.sshKeyGetByIDError
	}
	if f.sshKeyByID != nil {
		if key, ok := f.sshKeyByID[id]; ok {
			return key, nil, nil
		}
		return nil, nil, fmt.Errorf("key missing")
	}
	return nil, nil, nil
}

func (f *fakeHcloudClient) ServerList(ctx context.Context, opts hcloud.ServerListOpts) ([]*hcloud.Server, *hcloud.Response, error) {
	f.serverListCalls++
	if f.serverListError != nil {
		return nil, nil, f.serverListError
	}
	if len(f.serverListResponses) > 0 {
		out := f.serverListResponses[0]
		f.serverListResponses = f.serverListResponses[1:]
		return out, nil, nil
	}
	return []*hcloud.Server{}, nil, nil
}

func (f *fakeHcloudClient) ServerCreate(ctx context.Context, opts hcloud.ServerCreateOpts) (hcloud.ServerCreateResult, *hcloud.Response, error) {
	f.serverCreateCalls++
	f.serverCreateLastOpts = &opts
	if f.serverCreateError != nil {
		return hcloud.ServerCreateResult{}, nil, f.serverCreateError
	}
	return f.serverCreateResult, nil, nil
}

func (f *fakeHcloudClient) ServerPoweron(ctx context.Context, server *hcloud.Server) (*hcloud.Action, *hcloud.Response, error) {
	if f.serverPoweronError != nil {
		return nil, nil, f.serverPoweronError
	}
	return f.serverPoweronResult, nil, nil
}

func (f *fakeHcloudClient) ServerDeleteWithResult(ctx context.Context, server *hcloud.Server) (*hcloud.ServerDeleteResult, *hcloud.Response, error) {
	f.serverDeleteCalls++
	if f.serverDeleteError != nil {
		return nil, nil, f.serverDeleteError
	}
	if f.serverDeleteResult == nil {
		return nil, nil, nil
	}
	return f.serverDeleteResult, nil, nil
}

func (f *fakeHcloudClient) FirewallGetByName(ctx context.Context, name string) (*hcloud.Firewall, *hcloud.Response, error) {
	f.firewallByNameCalls++
	if f.firewallGetByNameFn != nil {
		return f.firewallGetByNameFn(ctx, name)
	}
	if f.firewallByNameMap != nil {
		if fw, ok := f.firewallByNameMap[name]; ok {
			return fw, nil, nil
		}
	}
	return nil, nil, nil
}

func (f *fakeHcloudClient) FirewallCreate(ctx context.Context, opts hcloud.FirewallCreateOpts) (hcloud.FirewallCreateResult, *hcloud.Response, error) {
	f.firewallCreateCalls++
	f.firewallCreateLastOpts = &opts
	if f.firewallCreateError != nil {
		return hcloud.FirewallCreateResult{}, nil, f.firewallCreateError
	}
	return f.firewallCreateResult, nil, nil
}

func (f *fakeHcloudClient) FirewallSetRules(ctx context.Context, firewall *hcloud.Firewall, opts hcloud.FirewallSetRulesOpts) ([]*hcloud.Action, *hcloud.Response, error) {
	f.firewallSetRulesCalls++
	if f.firewallSetRulesError != nil {
		return nil, nil, f.firewallSetRulesError
	}
	return nil, nil, nil
}

func (f *fakeHcloudClient) FirewallApplyResources(ctx context.Context, firewall *hcloud.Firewall, resources []hcloud.FirewallResource) ([]*hcloud.Action, *hcloud.Response, error) {
	f.firewallApplyCalls++
	if f.firewallApplyError != nil {
		return nil, nil, f.firewallApplyError
	}
	return nil, nil, nil
}

func (f *fakeHcloudClient) FirewallDelete(ctx context.Context, firewall *hcloud.Firewall) (*hcloud.Response, error) {
	f.firewallDeleteCalls++
	if firewall != nil {
		f.firewallDeleteNames = append(f.firewallDeleteNames, firewall.Name)
		if f.firewallByNameMap != nil {
			delete(f.firewallByNameMap, firewall.Name)
		}
	}
	return nil, nil
}

func (f *fakeHcloudClient) WaitFor(ctx context.Context, actions ...*hcloud.Action) error {
	f.waitForCalls++
	if f.waitForErr != nil {
		return f.waitForErr
	}
	return nil
}

func mustNewProviderWithContext(t *testing.T, cfg Config) *Provider {
	t.Helper()
	var err error
	if cfg.CAKey == "" {
		_, cfg.CAKey, err = mustGenerateSSHKeyPair()
		if err != nil {
			t.Fatalf("failed to generate CA key: %v", err)
		}
	}
	p, err := newProviderWithContext(context.Background(), cfg, nil, &fakeHcloudClient{})
	if err != nil {
		t.Fatalf("failed to build test provider: %v", err)
	}
	return p
}

func makeTestServer(name, ip string, status hcloud.ServerStatus, serverType *hcloud.ServerType) *hcloud.Server {
	if serverType == nil {
		serverType = &hcloud.ServerType{Name: "cpx21"}
	}
	return &hcloud.Server{
		Name:       name,
		Status:     status,
		ServerType: serverType,
		PublicNet: hcloud.ServerPublicNet{
			IPv4: hcloud.ServerPublicNetIPv4{IP: net.ParseIP(ip)},
		},
		Labels: map[string]string{},
	}
}

func makeTestSSHKey(id int64) *hcloud.SSHKey {
	return &hcloud.SSHKey{
		ID:   id,
		Name: fmt.Sprintf("test-%d", id),
	}
}

func mustTestSSHKey(id int64) *hcloud.SSHKey {
	if id == 0 {
		id = 1
	}
	return makeTestSSHKey(id)
}

func mustGenerateSSHKeyPair() (string, string, error) {
	_, private, err := generateSSHKeyPair("test")
	return "", private, err
}

func mustParseCIDRForTest(value string) *net.IPNet {
	ip, network, err := net.ParseCIDR(value)
	if err != nil {
		panic(err)
	}
	if ip == nil {
		panic("invalid ip")
	}
	return network
}

func strPtr(value string) *string {
	return &value
}
