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
	cfgRaw, err := ParseConfigFromEnv(map[string]string{
		"HCLOUD_TOKEN":        "abc",
		"HETZNER_LOCATION":    "fra1",
		"HETZNER_IMAGE":       "debian-12",
		"HETZNER_SERVER_TYPE": "cx11",
		"HETZNER_USERNAME":    "admin",
	})
	if err != nil {
		t.Fatalf("ParseConfigFromEnv() error: %v", err)
	}
	cfg := cfgRaw.(Config)
	if cfg.APIToken != "abc" {
		t.Fatalf("token = %q, want %q", cfg.APIToken, "abc")
	}
	if cfg.Location != "fra1" || cfg.Image != "debian-12" || cfg.ServerType != "cx11" || cfg.SSHUsername != "admin" {
		t.Fatalf("unexpected parsed config: %#v", cfg)
	}

	cfgRaw, err = ParseConfigFromEnv(map[string]string{
		"HETZNER_API_TOKEN": "fallback",
	})
	if err != nil {
		t.Fatalf("ParseConfigFromEnv() error: %v", err)
	}
	cfg = cfgRaw.(Config)
	if cfg.APIToken != "fallback" {
		t.Fatalf("expected fallback token")
	}
	if cfg.Location != defaultHetznerLocation || cfg.Image != defaultHetznerImage || cfg.ServerType != defaultHetznerServerType || cfg.SSHUsername != defaultHetznerSSHUser {
		t.Fatalf("expected defaults, got %#v", cfg)
	}

	if _, err := ParseConfigFromEnv(map[string]string{}); err == nil {
		t.Fatalf("expected missing token error")
	}
}

func TestBuildUserDataBranchesAndPaths(t *testing.T) {
	p := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           "ubuntu-24.04",
		ServerType:      defaultHetznerServerType,
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
	if !strings.Contains(script, "chown -R ec2-user:ec2-user /home/ec2-user/.ssh") {
		t.Fatalf("missing user path + chown: %s", script)
	}
	if !strings.Contains(script, "chmod 0700 /home/ec2-user/.ssh && chmod 0600 /home/ec2-user/.ssh/authorized_keys") {
		t.Fatalf("missing ssh key file protection setup: %s", script)
	}
	if !strings.Contains(script, "echo 'ssh-ed25519 AAA\nssh-rsa BBB' > /home/ec2-user/.ssh/authorized_keys") {
		t.Fatalf("missing authorized_keys injection: %s", script)
	}

	noKeyScript, err := p.BuildUserData(pullpreview.UserDataOptions{
		AppPath:  "/app",
		Username: "ec2-user",
	})
	if err != nil {
		t.Fatalf("BuildUserData() error: %v", err)
	}
	if strings.Contains(noKeyScript, "authorized_keys") {
		t.Fatalf("did not expect authorized key setup: %s", noKeyScript)
	}

	debian := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           "debian-12",
		ServerType:      defaultHetznerServerType,
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

func TestHetznerLaunchLifecycleRecreateWhenCacheMissing(t *testing.T) {
	cacheDir := t.TempDir()
	provider := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           defaultHetznerImage,
		ServerType:      defaultHetznerServerType,
		SSHUsername:     defaultHetznerSSHUser,
		SSHKeysCacheDir: cacheDir,
	})

	existing := makeTestServer("gh-1-pr-1", "198.51.100.1", hcloud.ServerStatusRunning, nil)
	created := makeTestServer("gh-1-pr-1", "203.0.113.10", hcloud.ServerStatusRunning, nil)
	key := makeTestSSHKey(1)
	client := &fakeHcloudClient{
		serverListResponses: [][]*hcloud.Server{{
			existing,
		}, nil},
		sshKeyCreateResult: key,
		serverCreateResult: hcloud.ServerCreateResult{
			Server: created,
		},
	}
	provider.client = client

	access, err := provider.Launch("gh-1-pr-1", pullpreview.LaunchOptions{})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}
	if access.IPAddress != created.PublicNet.IPv4.IP.String() {
		t.Fatalf("unexpected access ip: %#v", access.IPAddress)
	}
	if client.serverListCalls != 2 {
		t.Fatalf("expected two server list calls, got %d", client.serverListCalls)
	}
	if client.serverDeleteCalls != 1 {
		t.Fatalf("expected one delete call for recreate path, got %d", client.serverDeleteCalls)
	}
	if client.serverCreateCalls != 1 {
		t.Fatalf("expected one create call, got %d", client.serverCreateCalls)
	}
}

func TestHetznerLaunchLifecycleRecreateWhenCacheInvalid(t *testing.T) {
	cacheDir := t.TempDir()
	provider := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           defaultHetznerImage,
		ServerType:      defaultHetznerServerType,
		SSHUsername:     defaultHetznerSSHUser,
		SSHKeysCacheDir: cacheDir,
	})
	instance := "gh-1-pr-1"
	existing := makeTestServer(instance, "198.51.100.1", hcloud.ServerStatusRunning, nil)
	created := makeTestServer(instance, "203.0.113.10", hcloud.ServerStatusRunning, nil)
	key := makeTestSSHKey(1)
	client := &fakeHcloudClient{
		serverListResponses: [][]*hcloud.Server{{existing}, nil},
		sshKeyCreateResult:  key,
		serverCreateResult:  hcloud.ServerCreateResult{Server: created},
	}
	provider.client = client
	if err := provider.saveCachedSSHCredentials(instance, cachedHetznerSSHCredentials{
		InstanceName: instance,
		PrivateKey:   "invalid",
	}); err != nil {
		t.Fatalf("setup cache: %v", err)
	}

	_, err := provider.Launch(instance, pullpreview.LaunchOptions{})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}
	if client.serverDeleteCalls != 1 {
		t.Fatalf("expected one delete call for invalid cache path, got %d", client.serverDeleteCalls)
	}
	if client.serverCreateCalls != 1 {
		t.Fatalf("expected one create call for invalid cache path, got %d", client.serverCreateCalls)
	}
}

func TestHetznerLaunchLifecycleRecreateWhenPublicIPMissing(t *testing.T) {
	cacheDir := t.TempDir()
	provider := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           defaultHetznerImage,
		ServerType:      defaultHetznerServerType,
		SSHUsername:     defaultHetznerSSHUser,
		SSHKeysCacheDir: cacheDir,
	})
	instance := "gh-1-pr-1"
	existing := &hcloud.Server{Name: instance, Status: hcloud.ServerStatusRunning}
	created := makeTestServer(instance, "203.0.113.10", hcloud.ServerStatusRunning, nil)
	key := makeTestSSHKey(1)
	client := &fakeHcloudClient{
		serverListResponses: [][]*hcloud.Server{{existing}, nil},
		sshKeyCreateResult:  key,
		serverCreateResult:  hcloud.ServerCreateResult{Server: created},
	}
	provider.client = client
	_, private, _ := mustGenerateSSHKeyPair()
	if err := provider.saveCachedSSHCredentials(instance, cachedHetznerSSHCredentials{
		InstanceName: instance,
		PrivateKey:   private,
	}); err != nil {
		t.Fatalf("setup cache: %v", err)
	}

	_, err := provider.Launch(instance, pullpreview.LaunchOptions{})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}
	if client.serverDeleteCalls != 1 {
		t.Fatalf("expected one delete call when public IP missing, got %d", client.serverDeleteCalls)
	}
	if client.serverCreateCalls != 1 {
		t.Fatalf("expected one create call when public IP missing, got %d", client.serverCreateCalls)
	}
}

func TestHetznerLaunchLifecycleRecreateWhenSSHPrecheckFails(t *testing.T) {
	cacheDir := t.TempDir()
	provider := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           defaultHetznerImage,
		ServerType:      defaultHetznerServerType,
		SSHUsername:     defaultHetznerSSHUser,
		SSHKeysCacheDir: cacheDir,
	})
	instance := "gh-1-pr-1"
	_, private, _ := mustGenerateSSHKeyPair()
	existing := makeTestServer(instance, "198.51.100.1", hcloud.ServerStatusRunning, nil)
	created := makeTestServer(instance, "203.0.113.10", hcloud.ServerStatusRunning, nil)
	client := &fakeHcloudClient{
		serverListResponses: [][]*hcloud.Server{{existing}, nil},
		serverCreateResult:  hcloud.ServerCreateResult{Server: created},
		sshKeyCreateResult:  mustTestSSHKey(12),
	}
	provider.client = client
	if err := provider.saveCachedSSHCredentials(instance, cachedHetznerSSHCredentials{
		InstanceName: instance,
		PrivateKey:   private,
	}); err != nil {
		t.Fatalf("setup cache: %v", err)
	}
	originalRunSSHCommand := runSSHCommand
	defer func() { runSSHCommand = originalRunSSHCommand }()
	runSSHCommand = func(context.Context, string, string, string) ([]byte, error) {
		return nil, fmt.Errorf("ssh unavailable")
	}

	_, err := provider.Launch(instance, pullpreview.LaunchOptions{})
	if err != nil {
		t.Fatalf("Launch() error: %v", err)
	}
	if client.serverDeleteCalls != 1 {
		t.Fatalf("expected one delete call on SSH precheck failure, got %d", client.serverDeleteCalls)
	}
	if client.serverCreateCalls != 1 {
		t.Fatalf("expected one create call on SSH precheck failure, got %d", client.serverCreateCalls)
	}
}

func TestHetznerCreateFailureCleansUpServerAndKey(t *testing.T) {
	cacheDir := t.TempDir()
	provider := mustNewProviderWithContext(t, Config{
		APIToken:        "token",
		Location:        defaultHetznerLocation,
		Image:           defaultHetznerImage,
		ServerType:      defaultHetznerServerType,
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
		ServerType:      defaultHetznerServerType,
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

	serverCreateResult hcloud.ServerCreateResult
	serverCreateError  error

	serverDeleteResult *hcloud.ServerDeleteResult
	serverDeleteError  error

	serverPoweronResult *hcloud.Action
	serverPoweronError  error

	firewallByNameCalls int
	firewallGetByNameFn func(context.Context, string) (*hcloud.Firewall, *hcloud.Response, error)
	firewallByNameMap   map[string]*hcloud.Firewall

	firewallCreateResult hcloud.FirewallCreateResult
	firewallCreateError  error
	firewallCreateCalls  int

	firewallSetRulesCalls int
	firewallSetRulesError error
	firewallApplyCalls    int
	firewallApplyError    error

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
	p, err := newProviderWithContext(context.Background(), cfg, nil, &fakeHcloudClient{})
	if err != nil {
		t.Fatalf("failed to build test provider: %v", err)
	}
	return p
}

func makeTestServer(name, ip string, status hcloud.ServerStatus, serverType *hcloud.ServerType) *hcloud.Server {
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
