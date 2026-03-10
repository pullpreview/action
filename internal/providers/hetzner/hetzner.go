package hetzner

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/pullpreview/action/internal/providers"
	"github.com/pullpreview/action/internal/pullpreview"
)

const (
	// Hardcoded to a location with the highest server-type availability (snapshot: 2026-02-13).
	defaultHetznerLocation    = "nbg1"
	defaultHetznerImage       = "ubuntu-24.04"
	defaultHetznerServerType  = "cpx21"
	defaultHetznerSSHUser     = "root"
	defaultHetznerSSHRetries  = 10
	defaultHetznerSSHInterval = 15 * time.Second
	defaultHetznerSSHCertTTL  = 12 * time.Hour
	hetznerSSHKeyCacheExt     = "json"
)

var hetznerSSHKeyCacheFilenameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
var hetznerLabelSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

const (
	hetznerLabelMaxKeyLength   = 63
	hetznerLabelMaxValueLength = 63
)

type hcloudClient interface {
	SSHKeyCreate(context.Context, hcloud.SSHKeyCreateOpts) (*hcloud.SSHKey, *hcloud.Response, error)
	SSHKeyDelete(context.Context, *hcloud.SSHKey) (*hcloud.Response, error)
	SSHKeyGetByID(context.Context, int64) (*hcloud.SSHKey, *hcloud.Response, error)

	ServerList(context.Context, hcloud.ServerListOpts) ([]*hcloud.Server, *hcloud.Response, error)
	ServerCreate(context.Context, hcloud.ServerCreateOpts) (hcloud.ServerCreateResult, *hcloud.Response, error)
	ServerPoweron(context.Context, *hcloud.Server) (*hcloud.Action, *hcloud.Response, error)
	ServerDeleteWithResult(context.Context, *hcloud.Server) (*hcloud.ServerDeleteResult, *hcloud.Response, error)

	FirewallGetByName(context.Context, string) (*hcloud.Firewall, *hcloud.Response, error)
	FirewallCreate(context.Context, hcloud.FirewallCreateOpts) (hcloud.FirewallCreateResult, *hcloud.Response, error)
	FirewallSetRules(context.Context, *hcloud.Firewall, hcloud.FirewallSetRulesOpts) ([]*hcloud.Action, *hcloud.Response, error)
	FirewallApplyResources(context.Context, *hcloud.Firewall, []hcloud.FirewallResource) ([]*hcloud.Action, *hcloud.Response, error)
	FirewallDelete(context.Context, *hcloud.Firewall) (*hcloud.Response, error)

	WaitFor(context.Context, ...*hcloud.Action) error
}

type hcloudClientAdapter struct {
	client *hcloud.Client
}

func (a hcloudClientAdapter) SSHKeyCreate(ctx context.Context, opts hcloud.SSHKeyCreateOpts) (*hcloud.SSHKey, *hcloud.Response, error) {
	return a.client.SSHKey.Create(ctx, opts)
}

func (a hcloudClientAdapter) SSHKeyDelete(ctx context.Context, key *hcloud.SSHKey) (*hcloud.Response, error) {
	return a.client.SSHKey.Delete(ctx, key)
}

func (a hcloudClientAdapter) SSHKeyGetByID(ctx context.Context, id int64) (*hcloud.SSHKey, *hcloud.Response, error) {
	return a.client.SSHKey.GetByID(ctx, id)
}

func (a hcloudClientAdapter) ServerList(ctx context.Context, opts hcloud.ServerListOpts) ([]*hcloud.Server, *hcloud.Response, error) {
	return a.client.Server.List(ctx, opts)
}

func (a hcloudClientAdapter) ServerCreate(ctx context.Context, opts hcloud.ServerCreateOpts) (hcloud.ServerCreateResult, *hcloud.Response, error) {
	return a.client.Server.Create(ctx, opts)
}

func (a hcloudClientAdapter) ServerPoweron(ctx context.Context, server *hcloud.Server) (*hcloud.Action, *hcloud.Response, error) {
	return a.client.Server.Poweron(ctx, server)
}

func (a hcloudClientAdapter) ServerDeleteWithResult(ctx context.Context, server *hcloud.Server) (*hcloud.ServerDeleteResult, *hcloud.Response, error) {
	return a.client.Server.DeleteWithResult(ctx, server)
}

func (a hcloudClientAdapter) FirewallGetByName(ctx context.Context, name string) (*hcloud.Firewall, *hcloud.Response, error) {
	return a.client.Firewall.GetByName(ctx, name)
}

func (a hcloudClientAdapter) FirewallCreate(ctx context.Context, opts hcloud.FirewallCreateOpts) (hcloud.FirewallCreateResult, *hcloud.Response, error) {
	return a.client.Firewall.Create(ctx, opts)
}

func (a hcloudClientAdapter) FirewallSetRules(ctx context.Context, firewall *hcloud.Firewall, opts hcloud.FirewallSetRulesOpts) ([]*hcloud.Action, *hcloud.Response, error) {
	return a.client.Firewall.SetRules(ctx, firewall, opts)
}

func (a hcloudClientAdapter) FirewallApplyResources(ctx context.Context, firewall *hcloud.Firewall, resources []hcloud.FirewallResource) ([]*hcloud.Action, *hcloud.Response, error) {
	return a.client.Firewall.ApplyResources(ctx, firewall, resources)
}

func (a hcloudClientAdapter) FirewallDelete(ctx context.Context, firewall *hcloud.Firewall) (*hcloud.Response, error) {
	return a.client.Firewall.Delete(ctx, firewall)
}

func (a hcloudClientAdapter) WaitFor(ctx context.Context, actions ...*hcloud.Action) error {
	return a.client.Action.WaitFor(ctx, actions...)
}

var runSSHCommand = func(ctx context.Context, keyFile, certFile, user, host string) ([]byte, error) {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "IdentitiesOnly=yes",
		"-o", "IdentityAgent=none",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=8",
		"-i", keyFile,
	}
	if strings.TrimSpace(certFile) != "" {
		args = append(args, "-o", fmt.Sprintf("CertificateFile=%s", certFile))
	}
	args = append(args,
		fmt.Sprintf("%s@%s", user, host),
		"echo", "ok",
	)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	return cmd.CombinedOutput()
}

type Config struct {
	APIToken        string
	Location        string
	Image           string
	CAKey           string
	SSHUsername     string
	SSHKeysCacheDir string
}

type cachedHetznerSSHCredentials struct {
	InstanceName string `json:"instance_name"`
	PrivateKey   string `json:"private_key"`
	PublicKey    string `json:"public_key"`
	SSHKeyID     int64  `json:"ssh_key_id"`
	SSHKeyName   string `json:"ssh_key_name"`
}

func (c Config) ProviderName() string {
	return "hetzner"
}

func (c Config) ProviderDisplayName() string {
	return "Hetzner Cloud"
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.APIToken) == "" {
		return fmt.Errorf("HCLOUD_TOKEN is required")
	}
	if strings.TrimSpace(c.CAKey) == "" {
		return fmt.Errorf("HETZNER_CA_KEY is required")
	}
	if strings.TrimSpace(c.Location) == "" {
		return fmt.Errorf("location is required")
	}
	if strings.TrimSpace(c.Image) == "" {
		return fmt.Errorf("image is required")
	}
	return nil
}

func ParseConfigFromEnv(env map[string]string) (pullpreview.ProviderConfig, error) {
	token := strings.TrimSpace(env["HCLOUD_TOKEN"])
	location := strings.TrimSpace(env["REGION"])
	if location == "" {
		location = defaultHetznerLocation
	}
	image := strings.TrimSpace(env["IMAGE"])
	if image == "" {
		image = defaultHetznerImage
	}
	caKey := strings.TrimSpace(env["HETZNER_CA_KEY"])
	sshUser := defaultHetznerSSHUser
	sshKeysCacheDir := strings.TrimSpace(env["PULLPREVIEW_SSH_KEYS_CACHE_DIR"])
	cfg := Config{
		APIToken:        token,
		Location:        location,
		Image:           image,
		CAKey:           caKey,
		SSHUsername:     sshUser,
		SSHKeysCacheDir: sshKeysCacheDir,
	}
	if _, _, _, _, err := parseHetznerCAKey(caKey); err != nil {
		return cfg, err
	}
	return cfg, cfg.Validate()
}

func resolveHetznerServerType(raw string) string {
	size := strings.TrimSpace(strings.ToLower(raw))
	switch size {
	case "", "small", "micro":
		return defaultHetznerServerType
	default:
		return size
	}
}

type Provider struct {
	client          hcloudClient
	ctx             context.Context
	location        string
	image           string
	sshUser         string
	caSigner        ssh.Signer
	caPublicKey     string
	sshKeysCacheDir string
	sshRetryCount   int
	sshRetryDelay   time.Duration
	logger          *pullpreview.Logger
}

func New(ctx context.Context, cfg Config, logger *pullpreview.Logger) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	client := hcloud.NewClient(hcloud.WithToken(cfg.APIToken))
	return newProviderWithContext(ctx, cfg, logger, hcloudClientAdapter{client: client})
}

func newProviderWithContext(ctx context.Context, cfg Config, logger *pullpreview.Logger, client hcloudClient) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("client cannot be nil")
	}
	caSigner, caPublicKey, caSource, _, err := parseHetznerCAKey(cfg.CAKey)
	if err != nil {
		return nil, err
	}
	if logger != nil {
		logger.Infof("Hetzner SSH CA pre-check passed (%s)", caSource)
	}
	return &Provider{
		client:          client,
		ctx:             pullpreview.EnsureContext(ctx),
		location:        cfg.Location,
		image:           cfg.Image,
		sshUser:         cfg.SSHUsername,
		caSigner:        caSigner,
		caPublicKey:     caPublicKey,
		sshKeysCacheDir: cfg.SSHKeysCacheDir,
		sshRetryCount:   defaultHetznerSSHRetries,
		sshRetryDelay:   defaultHetznerSSHInterval,
		logger:          logger,
	}, nil
}

func NewWithContext(ctx context.Context, cfg pullpreview.ProviderConfig, logger *pullpreview.Logger) (pullpreview.Provider, error) {
	typed, ok := cfg.(Config)
	if !ok {
		pointer, ok := cfg.(*Config)
		if !ok {
			return nil, fmt.Errorf("invalid hetzner configuration type")
		}
		typed = *pointer
	}
	client := hcloud.NewClient(hcloud.WithToken(typed.APIToken))
	return newProviderWithContext(ctx, typed, logger, hcloudClientAdapter{client: client})
}

func (p *Provider) Name() string {
	return "hetzner"
}

func (p *Provider) DisplayName() string {
	return "Hetzner Cloud"
}

func (p *Provider) SupportsFirewall() bool {
	return true
}

func (p *Provider) SupportsDeploymentTarget(target pullpreview.DeploymentTarget) bool {
	switch pullpreview.NormalizeDeploymentTarget(string(target)) {
	case pullpreview.DeploymentTargetCompose, pullpreview.DeploymentTargetHelm:
		return true
	default:
		return false
	}
}

func (p *Provider) BuildUserData(options pullpreview.UserDataOptions) (string, error) {
	return pullpreview.BuildBootstrapScript(pullpreview.BootstrapOptions{
		AppPath:          options.AppPath,
		Username:         options.Username,
		SSHPublicKeys:    options.SSHPublicKeys,
		DeploymentTarget: options.DeploymentTarget,
		ImageName:        p.image,
		TrustedUserCAKey: p.caPublicKey,
		PropagateRootSSH: true,
	})
}

func (p *Provider) Launch(name string, opts pullpreview.LaunchOptions) (pullpreview.AccessDetails, error) {
	for {
		existing, err := p.serverByName(name)
		if err != nil {
			return pullpreview.AccessDetails{}, err
		}
		if existing == nil {
			return p.createServer(name, opts)
		}
		if reason, mismatch := pullpreview.DeploymentIdentityMismatch(labelsOrEmpty(existing.Labels), opts.Tags); mismatch {
			if p.logger != nil {
				p.logger.Warnf("Existing Hetzner instance %q has incompatible deployment identity (%s); recreating instance", name, reason)
			}
			if err := p.destroyInstanceAndCache(existing, name); err != nil {
				return pullpreview.AccessDetails{}, err
			}
			continue
		}
		if err := p.ensureServerRunning(existing); err != nil {
			return pullpreview.AccessDetails{}, err
		}
		firewalls, err := p.makeFirewall(name, opts.Ports, opts.CIDRs)
		if err != nil {
			return pullpreview.AccessDetails{}, err
		}
		if err := p.ensureServerFirewallAttached(existing, firewalls); err != nil {
			return pullpreview.AccessDetails{}, err
		}
		publicIP := p.publicIPAddress(existing)
		if publicIP == "" {
			if p.logger != nil {
				p.logger.Warnf("Existing Hetzner instance %q is missing public IP; recreating instance", name)
			}
			if err := p.destroyInstanceAndCache(existing, name); err != nil {
				return pullpreview.AccessDetails{}, err
			}
			continue
		}
		privateKey, certKey, err := p.generateSignedAccessCredentials()
		if err != nil {
			return pullpreview.AccessDetails{}, err
		}
		if err := p.validateSSHAccessWithRetry(existing, privateKey, certKey, defaultHetznerSSHRetries); err != nil {
			if p.logger != nil {
				p.logger.Warnf("Existing Hetzner instance %q SSH check failed; recreating instance (%v)", name, err)
			}
			if err := p.destroyInstanceAndCache(existing, name); err != nil {
				return pullpreview.AccessDetails{}, err
			}
			continue
		}
		if p.logger != nil {
			p.logger.Infof("Reusing existing Hetzner server %s with cert-based SSH credentials", name)
		}
		return pullpreview.AccessDetails{
			Username:   p.sshUser,
			IPAddress:  publicIP,
			PrivateKey: strings.TrimSpace(privateKey),
			CertKey:    strings.TrimSpace(certKey),
		}, nil
	}
}

func (p *Provider) createServer(name string, opts pullpreview.LaunchOptions) (pullpreview.AccessDetails, error) {
	publicKey, privateKey, err := generateSSHKeyPair(name)
	if err != nil {
		return pullpreview.AccessDetails{}, err
	}
	keyName := fmt.Sprintf("pullpreview-%s-%d", sanitizeNameForHetzner(name), time.Now().UnixNano())
	sshKey, _, err := p.client.SSHKeyCreate(p.ctx, hcloud.SSHKeyCreateOpts{
		Name:      keyName,
		PublicKey: publicKey,
	})
	if err != nil {
		return pullpreview.AccessDetails{}, err
	}

	serverType := &hcloud.ServerType{Name: resolveHetznerServerType(opts.Size)}
	image := &hcloud.Image{Name: p.image}
	location := &hcloud.Location{Name: p.location}
	firewalls, err := p.makeFirewall(name, opts.Ports, opts.CIDRs)
	if err != nil {
		return pullpreview.AccessDetails{}, p.cleanupFailedCreate(name, sshKey, nil, err)
	}
	labels := sanitizeHetznerLabels(mergeLabels(map[string]string{"stack": pullpreview.StackName}, opts.Tags))

	createOpts := hcloud.ServerCreateOpts{
		Name:             name,
		ServerType:       serverType,
		Image:            image,
		Location:         location,
		SSHKeys:          []*hcloud.SSHKey{sshKey},
		UserData:         opts.UserData,
		Labels:           labels,
		Firewalls:        firewalls,
		Automount:        ptrBool(true),
		StartAfterCreate: ptrBool(true),
	}
	result, _, err := p.client.ServerCreate(p.ctx, createOpts)
	if err != nil {
		return pullpreview.AccessDetails{}, p.cleanupFailedCreate(name, sshKey, nil, err)
	}
	actions := append([]*hcloud.Action{}, result.NextActions...)
	if result.Action != nil {
		actions = append(actions, result.Action)
	}
	if len(actions) > 0 {
		if err := p.client.WaitFor(p.ctx, actions...); err != nil {
			return pullpreview.AccessDetails{}, p.cleanupFailedCreate(name, sshKey, result.Server, err)
		}
	}
	server := result.Server
	if server == nil {
		server, err = p.serverByName(name)
		if err != nil {
			return pullpreview.AccessDetails{}, p.cleanupFailedCreate(name, sshKey, nil, err)
		}
	}
	if server == nil {
		return pullpreview.AccessDetails{}, p.cleanupFailedCreate(name, sshKey, nil, fmt.Errorf("created server not found"))
	}
	publicIP := p.publicIPAddress(server)
	if publicIP == "" {
		return pullpreview.AccessDetails{}, p.cleanupFailedCreate(name, sshKey, server, fmt.Errorf("created server missing public IP"))
	}
	if err := p.validateSSHAccessWithRetry(server, privateKey, "", defaultHetznerSSHRetries); err != nil {
		return pullpreview.AccessDetails{}, p.cleanupFailedCreate(name, sshKey, server, err)
	}
	certPrivateKey, cert, err := p.generateSignedAccessCredentials()
	if err != nil {
		return pullpreview.AccessDetails{}, p.cleanupFailedCreate(name, sshKey, server, err)
	}
	if err := p.validateSSHAccessWithRetry(server, certPrivateKey, cert, defaultHetznerSSHRetries); err != nil {
		return pullpreview.AccessDetails{}, p.cleanupFailedCreate(name, sshKey, server, err)
	}
	if err := p.deleteCloudSSHKeyIfExists(sshKey); err != nil && p.logger != nil {
		p.logger.Warnf("Unable to delete temporary Hetzner SSH key %s: %v", keyName, err)
	}
	if p.logger != nil {
		p.logger.Infof("Created Hetzner server %s with SSH key %s", server.Name, keyName)
	}
	return pullpreview.AccessDetails{
		Username:   p.sshUser,
		IPAddress:  publicIP,
		PrivateKey: certPrivateKey,
		CertKey:    cert,
	}, nil
}

func (p *Provider) validateSSHAccessWithRetry(server *hcloud.Server, privateKey, certKey string, attempts int) error {
	if attempts <= 0 {
		if p.sshRetryCount > 0 {
			attempts = p.sshRetryCount
		} else {
			attempts = 1
		}
	}
	delay := p.sshRetryDelay
	if delay <= 0 {
		delay = defaultHetznerSSHInterval
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := p.validateSSHAccess(server, privateKey, certKey); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if i < attempts-1 {
			if p.logger != nil {
				p.logger.Warnf("SSH access validation failed for %q (attempt %d/%d): %v", strings.TrimSpace(server.Name), i+1, attempts, lastErr)
			}
			time.Sleep(delay)
		}
	}
	return fmt.Errorf("ssh access validation failed for %q after %d attempts: %w", strings.TrimSpace(server.Name), attempts, lastErr)
}

func (p *Provider) Terminate(name string) error {
	server, err := p.serverByName(name)
	if err != nil {
		return err
	}
	if server != nil {
		if err := p.deleteServerAndWait(server); err != nil {
			return err
		}
	}
	p.deleteFirewallForInstance(name)
	p.removeCachedSSHKey(name)
	return nil
}

func (p *Provider) deleteServerAndWait(server *hcloud.Server) error {
	if server == nil {
		return nil
	}
	result, _, err := p.client.ServerDeleteWithResult(p.ctx, server)
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	if result.Action == nil {
		return nil
	}
	return p.client.WaitFor(p.ctx, result.Action)
}

func (p *Provider) deleteFirewallForInstance(name string) {
	firewallName := fmt.Sprintf("pullpreview-%s", sanitizeNameForHetzner(name))
	existing, _, err := p.client.FirewallGetByName(p.ctx, firewallName)
	if err != nil || existing == nil {
		return
	}
	_, _ = p.client.FirewallDelete(p.ctx, existing)
}

func (p *Provider) cleanupFailedCreate(name string, key *hcloud.SSHKey, server *hcloud.Server, cause error) error {
	if server == nil {
		lookup, err := p.serverByName(name)
		if err != nil {
			if key != nil {
				_ = p.deleteCloudSSHKeyIfExists(key)
			}
			p.removeCachedSSHKey(name)
			p.deleteFirewallForInstance(name)
			return cause
		}
		server = lookup
	}
	if err := p.deleteServerAndWait(server); err != nil {
		return fmt.Errorf("create cleanup failed for %q: server delete failed: %w", name, err)
	}
	if key != nil {
		_ = p.deleteCloudSSHKeyIfExists(key)
	}
	p.removeCachedSSHKey(name)
	p.deleteFirewallForInstance(name)
	return cause
}

func (p *Provider) ensureServerRunning(server *hcloud.Server) error {
	if server == nil {
		return nil
	}
	if server.Status == hcloud.ServerStatusRunning {
		return nil
	}
	action, _, err := p.client.ServerPoweron(p.ctx, server)
	if err != nil {
		return err
	}
	if action == nil {
		return nil
	}
	return p.client.WaitFor(p.ctx, action)
}

func (p *Provider) makeFirewall(name string, ports, cidrs []string) ([]*hcloud.ServerCreateFirewall, error) {
	rules, err := parseFirewallRules(ports, cidrs)
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return nil, nil
	}
	firewallName := fmt.Sprintf("pullpreview-%s", sanitizeNameForHetzner(name))
	if existing, _, err := p.client.FirewallGetByName(p.ctx, firewallName); err == nil && existing != nil {
		if !firewallRulesMatch(existing.Rules, rules) {
			if err := p.ensureFirewallRules(existing, rules); err != nil {
				return nil, err
			}
			existing, _, err = p.client.FirewallGetByName(p.ctx, firewallName)
			if err != nil {
				return nil, err
			}
		}
		return []*hcloud.ServerCreateFirewall{{Firewall: *existing}}, nil
	}
	created, _, err := p.client.FirewallCreate(p.ctx, hcloud.FirewallCreateOpts{
		Name:  firewallName,
		Rules: rules,
	})
	if err != nil {
		return nil, err
	}
	if created.Firewall == nil {
		return nil, nil
	}
	return []*hcloud.ServerCreateFirewall{{Firewall: *created.Firewall}}, nil
}

func (p *Provider) ensureServerFirewallAttached(server *hcloud.Server, firewalls []*hcloud.ServerCreateFirewall) error {
	if server == nil || len(firewalls) == 0 {
		return nil
	}
	desired := firewalls[0].Firewall
	if p.serverHasFirewallID(server, desired.ID) {
		return nil
	}
	if desired.ID == 0 {
		return nil
	}
	actions, _, err := p.client.FirewallApplyResources(p.ctx, &desired, []hcloud.FirewallResource{
		{
			Type: hcloud.FirewallResourceTypeServer,
			Server: &hcloud.FirewallResourceServer{
				ID: server.ID,
			},
		},
	})
	if err != nil {
		return err
	}
	if len(actions) == 0 {
		return nil
	}
	return p.client.WaitFor(p.ctx, actions...)
}

func (p *Provider) serverHasFirewallID(server *hcloud.Server, firewallID int64) bool {
	if server == nil || firewallID == 0 {
		return false
	}
	for _, firewallStatus := range server.PublicNet.Firewalls {
		if firewallStatus.Firewall.ID == firewallID {
			return true
		}
	}
	return false
}

func (p *Provider) ensureFirewallRules(firewall *hcloud.Firewall, rules []hcloud.FirewallRule) error {
	if firewall == nil {
		return nil
	}
	actions, _, err := p.client.FirewallSetRules(p.ctx, firewall, hcloud.FirewallSetRulesOpts{
		Rules: rules,
	})
	if err != nil {
		return err
	}
	if len(actions) == 0 {
		return nil
	}
	return p.client.WaitFor(p.ctx, actions...)
}

func firewallRulesMatch(existing, desired []hcloud.FirewallRule) bool {
	return strings.EqualFold(
		strings.Join(sortedFirewallRuleSignatures(existing), "|"),
		strings.Join(sortedFirewallRuleSignatures(desired), "|"),
	)
}

func sortedFirewallRuleSignatures(rules []hcloud.FirewallRule) []string {
	signatures := make([]string, 0, len(rules))
	for _, rule := range rules {
		signatures = append(signatures, firewallRuleSignature(rule))
	}
	sort.Strings(signatures)
	return signatures
}

func firewallRuleSignature(rule hcloud.FirewallRule) string {
	port := ""
	if rule.Port != nil {
		port = strings.TrimSpace(*rule.Port)
	}
	source := sortedCIDRStrings(rule.SourceIPs)
	destination := sortedCIDRStrings(rule.DestinationIPs)
	return strings.Join([]string{
		fmt.Sprintf("dir=%s", strings.ToLower(string(rule.Direction))),
		fmt.Sprintf("proto=%s", strings.ToLower(string(rule.Protocol))),
		fmt.Sprintf("port=%s", port),
		fmt.Sprintf("source=%s", strings.Join(source, ",")),
		fmt.Sprintf("dest=%s", strings.Join(destination, ",")),
	}, "|")
}

func sortedCIDRStrings(networks []net.IPNet) []string {
	values := make([]string, 0, len(networks))
	for _, network := range networks {
		values = append(values, network.String())
	}
	sort.Strings(values)
	return values
}

func (p *Provider) destroyInstanceAndCache(server *hcloud.Server, name string) error {
	if server == nil {
		return nil
	}
	if err := p.deleteServerAndWait(server); err != nil {
		return fmt.Errorf("failed to delete instance %q: %w", name, err)
	}
	p.deleteFirewallForInstance(name)
	p.removeCachedSSHKey(name)
	return nil
}

func (p *Provider) validateSSHAccess(server *hcloud.Server, privateKey, certKey string) error {
	privateKey = strings.TrimSpace(privateKey)
	if privateKey == "" {
		return fmt.Errorf("empty private key")
	}
	publicIP := p.publicIPAddress(server)
	if publicIP == "" {
		return fmt.Errorf("instance %q missing public IP", strings.TrimSpace(server.Name))
	}
	keyFile, err := os.CreateTemp("", "pullpreview-hetzner-key-*")
	if err != nil {
		return err
	}
	if err := keyFile.Close(); err != nil {
		_ = os.Remove(keyFile.Name())
		return err
	}
	if err := os.WriteFile(keyFile.Name(), []byte(privateKey+"\n"), 0600); err != nil {
		_ = os.Remove(keyFile.Name())
		return err
	}
	if err := os.Chmod(keyFile.Name(), 0600); err != nil {
		_ = os.Remove(keyFile.Name())
		return err
	}
	certFile := ""
	if strings.TrimSpace(certKey) != "" {
		certFile = keyFile.Name() + "-cert.pub"
		if err := os.WriteFile(certFile, []byte(strings.TrimSpace(certKey)+"\n"), 0600); err != nil {
			_ = os.Remove(keyFile.Name())
			return err
		}
		defer os.Remove(certFile)
	}
	defer os.Remove(keyFile.Name())

	output, err := runSSHCommand(p.ctx, keyFile.Name(), certFile, p.sshUser, publicIP)
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func validateSSHPrivateKeyFormat(privateKey string) error {
	trimmed := strings.TrimSpace(privateKey)
	if trimmed == "" {
		return fmt.Errorf("cached SSH private key is empty")
	}
	_, err := ssh.ParseRawPrivateKey([]byte(trimmed))
	if err != nil {
		return fmt.Errorf("invalid cached SSH private key: %w", err)
	}
	return nil
}

func (p *Provider) generateSignedAccessCredentials() (string, string, error) {
	_, privateKey, signer, err := generateSSHKeyPairWithSigner("pullpreview")
	if err != nil {
		return "", "", err
	}
	cert, err := generateUserCertificate(p.caSigner, signer, p.sshUser, defaultHetznerSSHCertTTL)
	if err != nil {
		return "", "", err
	}
	return privateKey, cert, nil
}

func generateUserCertificate(caSigner ssh.Signer, userSigner ssh.Signer, principal string, ttl time.Duration) (string, error) {
	if caSigner == nil {
		return "", fmt.Errorf("missing CA signer")
	}
	if userSigner == nil {
		return "", fmt.Errorf("missing user signer")
	}
	publicKey := userSigner.PublicKey()
	if publicKey == nil {
		return "", fmt.Errorf("user signer has no public key")
	}
	cert := &ssh.Certificate{
		Key:             publicKey,
		Serial:          uint64(time.Now().UnixNano()),
		CertType:        ssh.UserCert,
		KeyId:           fmt.Sprintf("pullpreview-%s-%d", sanitizeNameForHetzner(principal), time.Now().UnixNano()),
		ValidPrincipals: []string{principal},
		ValidAfter:      uint64(time.Now().Add(-time.Minute).Unix()),
		ValidBefore:     uint64(time.Now().Add(ttl).Unix()),
	}
	if err := cert.SignCert(rand.Reader, caSigner); err != nil {
		return "", err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(cert))), nil
}

func parseHetznerCAKey(raw string) (ssh.Signer, string, string, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "", "", false, fmt.Errorf("HETZNER_CA_KEY is required")
	}

	caSource := "inline HETZNER_CA_KEY"
	caSourceFromFile := false
	data := []byte(raw)

	if info, err := os.Stat(raw); err == nil {
		if info.IsDir() {
			return nil, "", "", false, fmt.Errorf("HETZNER_CA_KEY %q refers to a directory", raw)
		}
		caSource = raw
		caSourceFromFile = true
		data, err = os.ReadFile(raw)
		if err != nil {
			return nil, "", "", false, fmt.Errorf("failed to read HETZNER_CA_KEY from %q: %w", raw, err)
		}
	}

	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		prefix := "inline HETZNER_CA_KEY"
		if caSourceFromFile {
			prefix = fmt.Sprintf("HETZNER_CA_KEY file %q", caSource)
		}
		return nil, "", caSource, caSourceFromFile, fmt.Errorf("invalid %s: %w", prefix, err)
	}
	publicKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if publicKey == "" {
		errPrefix := "inline HETZNER_CA_KEY"
		if caSourceFromFile {
			errPrefix = fmt.Sprintf("HETZNER_CA_KEY file %q", caSource)
		}
		return nil, "", caSource, caSourceFromFile, fmt.Errorf("invalid %s: unable to derive public key", errPrefix)
	}
	return signer, publicKey, caSource, caSourceFromFile, nil
}

func (p *Provider) cachePath(name string) string {
	dir := strings.TrimSpace(p.sshKeysCacheDir)
	if dir == "" {
		return ""
	}
	filename := strings.Trim(hetznerSSHKeyCacheFilenameSanitizer.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-"), "-")
	if filename == "" {
		filename = "instance"
	}
	return filepath.Join(dir, filename+"."+hetznerSSHKeyCacheExt)
}

func (p *Provider) loadCachedSSHCredentials(name string) (cachedHetznerSSHCredentials, bool) {
	path := p.cachePath(name)
	if path == "" {
		return cachedHetznerSSHCredentials{}, false
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return cachedHetznerSSHCredentials{}, false
	}
	var payload cachedHetznerSSHCredentials
	if err := json.Unmarshal(content, &payload); err != nil {
		return cachedHetznerSSHCredentials{}, false
	}
	payload.InstanceName = strings.TrimSpace(payload.InstanceName)
	if payload.InstanceName == "" {
		payload.InstanceName = strings.TrimSpace(name)
	}
	return payload, strings.TrimSpace(payload.PrivateKey) != ""
}

func (p *Provider) saveCachedSSHCredentials(name string, payload cachedHetznerSSHCredentials) error {
	path := p.cachePath(name)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func (p *Provider) publicIPAddress(server *hcloud.Server) string {
	if server == nil {
		return ""
	}
	if !server.PublicNet.IPv4.IsUnspecified() {
		return server.PublicNet.IPv4.IP.String()
	}
	if !server.PublicNet.IPv6.IsUnspecified() {
		return server.PublicNet.IPv6.IP.String()
	}
	return ""
}

func (p *Provider) deleteCloudSSHKeyIfExists(key *hcloud.SSHKey) error {
	if key == nil {
		return nil
	}
	_, err := p.client.SSHKeyDelete(p.ctx, key)
	return err
}

func (p *Provider) removeCachedSSHKey(name string) {
	path := p.cachePath(name)
	if path == "" {
		return
	}
	cached, ok := p.loadCachedSSHCredentials(name)
	if ok && cached.SSHKeyID != 0 {
		cloudKey, _, err := p.client.SSHKeyGetByID(p.ctx, cached.SSHKeyID)
		if err == nil && cloudKey != nil {
			_, _ = p.client.SSHKeyDelete(p.ctx, cloudKey)
		}
	}
	_ = os.Remove(path)
}

func (p *Provider) Running(name string) (bool, error) {
	server, err := p.serverByName(name)
	if err != nil {
		return false, err
	}
	if server == nil {
		return false, nil
	}
	return server.Status == hcloud.ServerStatusRunning, nil
}

func (p *Provider) ListInstances(tags map[string]string) ([]pullpreview.InstanceSummary, error) {
	servers, _, err := p.client.ServerList(p.ctx, hcloud.ServerListOpts{})
	if err != nil {
		return nil, err
	}
	sanitizedTags := sanitizeHetznerLabels(tags)
	instances := []pullpreview.InstanceSummary{}
	for _, server := range servers {
		if server == nil {
			continue
		}
		if !matchLabels(server.Labels, sanitizedTags) {
			continue
		}
		publicIP := ""
		if !server.PublicNet.IPv4.IsUnspecified() {
			publicIP = server.PublicNet.IPv4.IP.String()
		} else if !server.PublicNet.IPv6.IsUnspecified() {
			publicIP = server.PublicNet.IPv6.IP.String()
		}
		instance := pullpreview.InstanceSummary{
			Name:      server.Name,
			PublicIP:  publicIP,
			Size:      server.ServerType.Name,
			CreatedAt: server.Created,
			Tags:      labelsOrEmpty(server.Labels),
		}
		if server.Location != nil {
			instance.Region = strings.TrimSpace(server.Location.Name)
		}
		instances = append(instances, instance)
	}
	return instances, nil
}

func (p *Provider) Username() string {
	return p.sshUser
}

func (p *Provider) serverByName(name string) (*hcloud.Server, error) {
	servers, _, err := p.client.ServerList(p.ctx, hcloud.ServerListOpts{Name: name})
	if err != nil {
		return nil, err
	}
	for _, server := range servers {
		if server != nil && strings.TrimSpace(server.Name) == strings.TrimSpace(name) {
			return server, nil
		}
	}
	return nil, nil
}

func generateSSHKeyPair(_ string) (string, string, error) {
	public, private, _, err := generateSSHKeyPairWithSigner("")
	if err != nil {
		return "", "", err
	}
	return public, private, nil
}

func generateSSHKeyPairWithSigner(_ string) (string, string, ssh.Signer, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", nil, err
	}
	private := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	if private == nil {
		return "", "", nil, fmt.Errorf("unable to marshal private key")
	}
	public, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		return "", "", nil, err
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		return "", "", nil, err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public))), strings.TrimSpace(string(private)), signer, nil
}

func parseFirewallRules(ports, cidrs []string) ([]hcloud.FirewallRule, error) {
	targetCIDRs := normalizeCIDRs(cidrs)
	portMap := map[string]hcloud.FirewallRule{}
	for _, raw := range ports {
		start, end, protocol, err := parseFirewallPort(raw)
		if err != nil {
			return nil, err
		}
		useCIDRs := targetCIDRs
		port := strconv.Itoa(start)
		if end != start {
			port = fmt.Sprintf("%d-%d", start, end)
		}
		key := fmt.Sprintf("%d-%d/%s", start, end, protocol)
		if _, exists := portMap[key]; exists {
			continue
		}
		portMap[key] = hcloud.FirewallRule{
			Direction: hcloud.FirewallRuleDirectionIn,
			SourceIPs: useCIDRs,
			Protocol:  hcloud.FirewallRuleProtocol(protocol),
			Port:      ptrString(port),
		}
	}
	const sshPort = 22
	sshKey := fmt.Sprintf("%d-%d/tcp", sshPort, sshPort)
	if _, exists := portMap[sshKey]; !exists {
		portMap[sshKey] = hcloud.FirewallRule{
			Direction: hcloud.FirewallRuleDirectionIn,
			SourceIPs: []net.IPNet{*mustParseCIDR("0.0.0.0/0")},
			Protocol:  hcloud.FirewallRuleProtocolTCP,
			Port:      ptrString(strconv.Itoa(sshPort)),
		}
	}
	rules := make([]hcloud.FirewallRule, 0, len(portMap))
	for _, rule := range portMap {
		rules = append(rules, rule)
	}
	return rules, nil
}

func parseFirewallPort(raw string) (int, int, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, 0, "", fmt.Errorf("empty port definition")
	}
	portRange := raw
	protocol := string(hcloud.FirewallRuleProtocolTCP)
	if idx := strings.Index(raw, "/"); idx >= 0 {
		portRange = strings.TrimSpace(raw[:idx])
		protocol = strings.TrimSpace(raw[idx+1:])
	}
	if protocol == "" {
		protocol = string(hcloud.FirewallRuleProtocolTCP)
	}
	protocol = strings.ToLower(protocol)
	if protocol != "tcp" && protocol != "udp" && protocol != "icmp" {
		return 0, 0, "", fmt.Errorf("unsupported protocol %s in port definition %q", protocol, raw)
	}
	if strings.Contains(portRange, "-") {
		parts := strings.SplitN(portRange, "-", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return 0, 0, "", fmt.Errorf("invalid port range %q", raw)
		}
		start, err := mustParsePort(parts[0])
		if err != nil {
			return 0, 0, "", fmt.Errorf("invalid port range %q: %w", raw, err)
		}
		end, err := mustParsePort(parts[1])
		if err != nil {
			return 0, 0, "", fmt.Errorf("invalid port range %q: %w", raw, err)
		}
		return start, end, protocol, nil
	}
	port, err := mustParsePort(portRange)
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid port %q: %w", raw, err)
	}
	return port, port, protocol, nil
}

func mustParsePort(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("empty port")
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if value <= 0 || value > 65535 {
		return 0, fmt.Errorf("invalid port %d", value)
	}
	return value, nil
}

func normalizeCIDRs(raw []string) []net.IPNet {
	normalized := []net.IPNet{}
	if len(raw) == 0 {
		raw = []string{"0.0.0.0/0"}
	}
	seen := map[string]struct{}{}
	for _, value := range raw {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parsed := parseCIDR(value)
		if parsed == nil {
			continue
		}
		key := parsed.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, *parsed)
	}
	if len(normalized) == 0 {
		normalized = append(normalized, *mustParseCIDR("0.0.0.0/0"))
	}
	return normalized
}

func parseCIDR(value string) *net.IPNet {
	if _, parsed, err := net.ParseCIDR(value); err == nil {
		return parsed
	}
	ip := net.ParseIP(value)
	if ip == nil {
		return nil
	}
	mask := net.CIDRMask(32, 32)
	if ip.To4() == nil {
		mask = net.CIDRMask(128, 128)
	}
	return &net.IPNet{IP: ip, Mask: mask}
}

func matchLabels(actual map[string]string, required map[string]string) bool {
	if len(required) == 0 {
		return true
	}
	for key, value := range required {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if actual[key] != value {
			return false
		}
	}
	return true
}

func labelsOrEmpty(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	copied := map[string]string{}
	for k, v := range input {
		copied[k] = v
	}
	return copied
}

func sanitizeHetznerLabels(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	sanitized := map[string]string{}
	for key, value := range input {
		sanitizedKey := sanitizeHetznerLabelPart(key, hetznerLabelMaxKeyLength)
		sanitizedValue := sanitizeHetznerLabelPart(value, hetznerLabelMaxValueLength)
		if sanitizedKey == "" || sanitizedValue == "" {
			continue
		}
		sanitized[sanitizedKey] = sanitizedValue
	}
	return sanitized
}

func sanitizeHetznerLabelPart(value string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	value = hetznerLabelSanitizer.ReplaceAllString(value, "-")
	value = trimHetznerLabelEdges(value)
	if value == "" {
		return ""
	}
	if len(value) > maxLen {
		value = value[:maxLen]
		value = trimHetznerLabelEdges(value)
	}
	if value == "" {
		return ""
	}
	if !isHetznerLabelAlnum(rune(value[0])) || !isHetznerLabelAlnum(rune(value[len(value)-1])) {
		return ""
	}
	return value
}

func trimHetznerLabelEdges(value string) string {
	for len(value) > 0 && !isHetznerLabelAlnum(rune(value[0])) {
		value = value[1:]
	}
	for len(value) > 0 && !isHetznerLabelAlnum(rune(value[len(value)-1])) {
		value = value[:len(value)-1]
	}
	return value
}

func isHetznerLabelAlnum(value rune) bool {
	return (value >= 'a' && value <= 'z') || (value >= '0' && value <= '9')
}

func mergeLabels(base, extra map[string]string) map[string]string {
	merged := map[string]string{}
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	return merged
}

func sanitizeNameForHetzner(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ToLower(name)
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-':
			return r
		default:
			return '-'
		}
	}, name)
	name = strings.Trim(name, "-")
	if name == "" {
		name = "instance"
	}
	return name
}

func ptrString(value string) *string {
	return &value
}

func ptrBool(value bool) *bool {
	return &value
}

func mustParseCIDR(value string) *net.IPNet {
	_, cidr, err := net.ParseCIDR(value)
	if err == nil {
		return cidr
	}
	return &net.IPNet{
		IP:   net.ParseIP("0.0.0.0"),
		Mask: net.CIDRMask(0, 32),
	}
}

func init() {
	_ = providers.RegisterProvider("hetzner", "Hetzner Cloud", ParseConfigFromEnv, NewWithContext)
}
