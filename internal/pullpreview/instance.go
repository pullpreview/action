package pullpreview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	remoteAppPath              = "/app"
	instanceSSHReadyInterval   = 5 * time.Second
	instanceSSHReadyWaitWindow = 5 * time.Minute
	sshReadyDiagnosticCommand  = `if test -f /etc/pullpreview/ready; then
  echo ready-marker-present
  exit 0
fi
echo ready-marker-missing
if command -v cloud-init >/dev/null 2>&1; then
  echo "-- cloud-init status --"
  sudo -n cloud-init status --long 2>/dev/null || cloud-init status --long 2>/dev/null || sudo -n cloud-init status 2>/dev/null || cloud-init status 2>/dev/null || true
fi
if sudo -n test -f /var/log/cloud-init-output.log 2>/dev/null || test -f /var/log/cloud-init-output.log; then
  echo "-- cloud-init-output tail --"
  sudo -n tail -n 40 /var/log/cloud-init-output.log 2>/dev/null || tail -n 40 /var/log/cloud-init-output.log 2>/dev/null || true
fi
exit 1`
)

type Runner interface {
	Run(cmd *exec.Cmd) error
}

type SystemRunner struct{}

func (r SystemRunner) Run(cmd *exec.Cmd) error {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

var runSSHCombinedOutput = func(cmd *exec.Cmd) ([]byte, error) {
	return cmd.CombinedOutput()
}

type Instance struct {
	Name             string
	Subdomain        string
	Admins           []string
	AdminPublicKeys  []string
	Context          context.Context
	DeploymentTarget DeploymentTarget
	CIDRs            []string
	ComposeFiles     []string
	ComposeOptions   []string
	Chart            string
	ChartRepository  string
	ChartValues      []string
	ChartSet         []string
	DefaultPort      string
	DNS              string
	Ports            []string
	ProxyTLS         string
	Provider         Provider
	Registries       []string
	Size             string
	Tags             map[string]string
	PreScript        string
	Access           AccessDetails
	Logger           *Logger
	Runner           Runner
}

func NewInstance(name string, opts CommonOptions, provider Provider, logger *Logger) *Instance {
	normalized := NormalizeName(name)
	target := NormalizeDeploymentTarget(string(opts.DeploymentTarget))
	defaultPort := defaultString(opts.DefaultPort, "80")
	proxyTLS := strings.TrimSpace(opts.ProxyTLS)
	if proxyTLS != "" {
		if defaultPort != "443" && logger != nil {
			logger.Warnf("proxy_tls=%q enabled: overriding default_port=%s to 443", proxyTLS, defaultPort)
		}
		defaultPort = "443"
	}
	return &Instance{
		Name:             normalized,
		Subdomain:        NormalizeName(name),
		Admins:           opts.Admins,
		AdminPublicKeys:  opts.AdminPublicKeys,
		Context:          ensureContext(opts.Context),
		DeploymentTarget: target,
		CIDRs:            defaultSlice(opts.CIDRs, []string{"0.0.0.0/0"}),
		ComposeFiles:     defaultSlice(opts.ComposeFiles, []string{"docker-compose.yml"}),
		ComposeOptions:   defaultSlice(opts.ComposeOptions, []string{"--build"}),
		Chart:            strings.TrimSpace(opts.Chart),
		ChartRepository:  strings.TrimSpace(opts.ChartRepository),
		ChartValues:      opts.ChartValues,
		ChartSet:         opts.ChartSet,
		DefaultPort:      defaultPort,
		DNS:              defaultString(opts.DNS, "my.preview.run"),
		Ports:            opts.Ports,
		ProxyTLS:         proxyTLS,
		Provider:         provider,
		Registries:       opts.Registries,
		Size:             opts.InstanceType,
		Tags:             defaultMap(opts.Tags),
		PreScript:        opts.PreScript,
		Logger:           logger,
		Runner:           SystemRunner{},
	}
}

func (i *Instance) WithSubdomain(subdomain string) {
	if subdomain == "" {
		return
	}
	i.Subdomain = NormalizeName(subdomain)
}

func defaultSlice(value, fallback []string) []string {
	if len(value) == 0 {
		return fallback
	}
	return value
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func defaultMap(value map[string]string) map[string]string {
	if value == nil {
		return map[string]string{}
	}
	return value
}

func providerName(provider Provider) string {
	if metadata, ok := provider.(ProviderMetadata); ok {
		return strings.ToLower(strings.TrimSpace(metadata.Name()))
	}
	return ""
}

func providerSupportsDeploymentTarget(provider Provider, target DeploymentTarget) bool {
	if supported, ok := provider.(SupportsDeploymentTarget); ok {
		return supported.SupportsDeploymentTarget(target)
	}
	return NormalizeDeploymentTarget(string(target)) == DeploymentTargetCompose
}

func sameStringSlice(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for idx := range got {
		if strings.TrimSpace(got[idx]) != strings.TrimSpace(want[idx]) {
			return false
		}
	}
	return true
}

func (i *Instance) ValidateDeploymentConfig() error {
	if err := i.DeploymentTarget.Validate(); err != nil {
		return err
	}

	switch i.DeploymentTarget {
	case DeploymentTargetCompose:
		if strings.TrimSpace(i.Chart) != "" || strings.TrimSpace(i.ChartRepository) != "" || len(i.ChartValues) > 0 || len(i.ChartSet) > 0 {
			return fmt.Errorf("chart, chart_repository, chart_values, and chart_set require deployment_target=helm")
		}
	case DeploymentTargetHelm:
		if !providerSupportsDeploymentTarget(i.Provider, DeploymentTargetHelm) {
			return fmt.Errorf("deployment_target=helm is unsupported for provider=%s", providerName(i.Provider))
		}
		if strings.TrimSpace(i.Chart) == "" {
			return fmt.Errorf("deployment_target=helm requires chart")
		}
		if strings.TrimSpace(i.ProxyTLS) == "" {
			return fmt.Errorf("deployment_target=helm requires proxy_tls")
		}
		if len(uniqueStrings(i.Registries)) > 0 {
			return fmt.Errorf("registries is unsupported with deployment_target=helm")
		}
		if len(i.ComposeFiles) > 0 && !sameStringSlice(i.ComposeFiles, []string{"docker-compose.yml"}) {
			return fmt.Errorf("compose_files is unsupported with deployment_target=helm")
		}
		if len(i.ComposeOptions) > 0 && !sameStringSlice(i.ComposeOptions, []string{"--build"}) {
			return fmt.Errorf("compose_options is unsupported with deployment_target=helm")
		}
	default:
		return fmt.Errorf("unsupported deployment target %q", i.DeploymentTarget)
	}
	return nil
}

func (i *Instance) LaunchAndWait() error {
	if i.Logger != nil {
		i.Logger.Infof("Creating or restoring instance name=%s size=%s", i.Name, i.Size)
	}

	userData := UserData{
		AppPath:       remoteAppPath,
		SSHPublicKeys: i.SSHPublicKeys(),
		Username:      i.Username(),
	}.Script()
	if provider, ok := i.Provider.(UserDataProvider); ok {
		generatedUserData, err := provider.BuildUserData(UserDataOptions{
			AppPath:          remoteAppPath,
			DeploymentTarget: i.DeploymentTarget,
			SSHPublicKeys:    i.SSHPublicKeys(),
			Username:         i.Username(),
		})
		if err != nil {
			return err
		}
		userData = generatedUserData
	}
	access, err := i.Provider.Launch(i.Name, LaunchOptions{
		Size:     i.Size,
		UserData: userData,
		Ports:    i.PortsWithDefaults(),
		CIDRs:    i.CIDRs,
		Tags:     i.Tags,
	})
	if err != nil {
		return err
	}
	i.Access = access
	if i.Logger != nil {
		i.Logger.Infof(
			"Instance created name=%s public_ip=%s username=%s",
			i.Name,
			i.PublicIP(),
			i.Username(),
		)
	}
	if ok := WaitUntilContext(i.Context, pollAttemptsForWindow(instanceSSHReadyWaitWindow, instanceSSHReadyInterval), instanceSSHReadyInterval, func() bool {
		if i.Logger != nil {
			i.Logger.Infof(
				"Waiting for SSH username=%s ip=%s ssh=\"ssh %s\"",
				i.Username(),
				i.PublicIP(),
				i.SSHAddress(),
			)
		}
		return i.SSHReady()
	}); !ok {
		if i.Logger != nil {
			if diagErr := i.SSHReadyDiagnostic(); diagErr != nil {
				i.Logger.Warnf("SSH readiness diagnostics: %v", diagErr)
			}
		}
		return errors.New("can't connect to instance over SSH")
	}
	if i.Logger != nil {
		i.Logger.Infof("Instance ssh access OK")
	}
	return nil
}

func (i *Instance) Terminate() error {
	return i.Provider.Terminate(i.Name)
}

func (i *Instance) Running() (bool, error) {
	return i.Provider.Running(i.Name)
}

func (i *Instance) SSHReady() bool {
	return i.SSH("test -f /etc/pullpreview/ready", nil) == nil
}

func (i *Instance) SSHReadyDiagnostic() error {
	output, err := i.SSHOutput(sshReadyDiagnosticCommand, nil)
	if err == nil {
		return nil
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, output)
}

func (i *Instance) PublicIP() string {
	return i.Access.IPAddress
}

func (i *Instance) PublicDNS() string {
	return PublicDNS(i.Subdomain, i.DNS, i.PublicIP())
}

func (i *Instance) URL() string {
	scheme := "http"
	if i.DefaultPort == "443" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s:%s", scheme, i.PublicDNS(), i.DefaultPort)
}

func (i *Instance) Username() string {
	if i.Access.Username != "" {
		return i.Access.Username
	}
	return i.Provider.Username()
}

func (i *Instance) PortsWithDefaults() []string {
	proxyTLSEnabled := strings.TrimSpace(i.ProxyTLS) != ""
	ports := []string{}
	for _, port := range i.Ports {
		if proxyTLSEnabled && i.DeploymentTarget != DeploymentTargetHelm && firewallRuleTargetsPort(port, 80) {
			continue
		}
		ports = append(ports, port)
	}
	ports = append(ports, i.DefaultPort, "22")
	return uniqueStrings(ports)
}

func firewallRuleTargetsPort(rule string, port int) bool {
	value := strings.TrimSpace(rule)
	if value == "" {
		return false
	}
	if idx := strings.Index(value, "/"); idx >= 0 {
		value = value[:idx]
	}
	if strings.Contains(value, ":") {
		parts := strings.Split(value, ":")
		if len(parts) > 0 {
			value = parts[len(parts)-1]
		}
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	return parsed == port
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		result = append(result, v)
	}
	return result
}

func (i *Instance) SSHPublicKeys() []string {
	if len(i.AdminPublicKeys) > 0 {
		return uniqueStrings(i.AdminPublicKeys)
	}

	keys := []string{}
	client := http.Client{Timeout: 10 * time.Second}
	for _, admin := range i.Admins {
		admin = strings.TrimSpace(admin)
		if admin == "" {
			continue
		}
		url := fmt.Sprintf("https://github.com/%s.keys", admin)
		resp, err := client.Get(url)
		if err != nil {
			if i.Logger != nil {
				i.Logger.Warnf("Unable to fetch SSH keys for %s: %v", admin, err)
			}
			continue
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				keys = append(keys, line)
			}
		}
	}
	return keys
}

func (i *Instance) SetupSSHAccess() error {
	keys := i.SSHPublicKeys()
	if len(keys) == 0 {
		return nil
	}
	content := strings.Join(keys, "\n") + "\n"
	homeDir := HomeDirForUser(i.Username())
	return i.appendRemoteFile(bytes.NewBufferString(content), fmt.Sprintf("%s/.ssh/authorized_keys", homeDir), "0600")
}

func (i *Instance) SetupPreScript() error {
	script := BuildPreScript(i.Registries, i.PreScript, i.Logger)
	return i.SCP(bytes.NewBufferString(script), "/tmp/pre_script.sh", "0755")
}

func (i *Instance) SCP(input io.Reader, target, mode string) error {
	command := fmt.Sprintf("cat - > %s && chmod %s %s", target, mode, target)
	return i.SSH(command, input)
}

func (i *Instance) appendRemoteFile(input io.Reader, target, mode string) error {
	command := fmt.Sprintf("cat - >> %s && chmod %s %s", target, mode, target)
	return i.SSH(command, input)
}

func (i *Instance) sshArgs(keyFile, certFile string) []string {
	args := []string{}
	if i.Logger != nil && i.Logger.level <= LevelDebug {
		args = append(args, "-v")
	}
	args = append(args,
		"-o", "ServerAliveInterval=15",
		"-o", "IdentitiesOnly=yes",
		"-i", keyFile,
	)
	if strings.TrimSpace(certFile) != "" {
		args = append(args, "-o", "CertificateFile="+certFile)
	}
	args = append(args, i.SSHOptions()...)
	args = append(args, i.SSHAddress())
	return args
}

func (i *Instance) SSH(command string, input io.Reader) error {
	keyFile, certFile, err := i.writeTempKeys()
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(keyFile)
		if certFile != "" {
			_ = os.Remove(certFile)
		}
	}()

	args := i.sshArgs(keyFile, certFile)
	args = append(args, command)

	cmd := exec.CommandContext(i.Context, "ssh", args...)
	cmd.Stdin = input
	return i.Runner.Run(cmd)
}

func (i *Instance) SSHOutput(command string, input io.Reader) (string, error) {
	keyFile, certFile, err := i.writeTempKeys()
	if err != nil {
		return "", err
	}
	defer func() {
		_ = os.Remove(keyFile)
		if certFile != "" {
			_ = os.Remove(certFile)
		}
	}()

	args := i.sshArgs(keyFile, certFile)
	args = append(args, command)

	cmd := exec.CommandContext(i.Context, "ssh", args...)
	cmd.Stdin = input
	output, err := runSSHCombinedOutput(cmd)
	return string(output), err
}

func (i *Instance) SSHAddress() string {
	username := i.Username()
	if username == "" {
		return i.PublicIP()
	}
	return fmt.Sprintf("%s@%s", username, i.PublicIP())
}

func (i *Instance) SSHOptions() []string {
	return []string{
		"-o", "BatchMode=yes",
		"-o", "IdentityAgent=none",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=10",
	}
}

func (i *Instance) writeTempKeys() (string, string, error) {
	keyFile, err := os.CreateTemp("", "pullpreview-key-*")
	if err != nil {
		return "", "", err
	}
	if _, err := keyFile.WriteString(i.Access.PrivateKey + "\n"); err != nil {
		_ = keyFile.Close()
		return "", "", err
	}
	if err := keyFile.Close(); err != nil {
		return "", "", err
	}
	if err := os.Chmod(keyFile.Name(), 0600); err != nil {
		return "", "", err
	}

	certFile := ""
	if strings.TrimSpace(i.Access.CertKey) != "" {
		certFile = keyFile.Name() + "-cert.pub"
		if err := os.WriteFile(certFile, []byte(i.Access.CertKey+"\n"), 0600); err != nil {
			return "", "", err
		}
	}

	return keyFile.Name(), certFile, nil
}

func (i *Instance) EnsureRemoteAuthorizedKeysOwner() error {
	homeDir := HomeDirForUser(i.Username())
	command := fmt.Sprintf("chown %s:%s %s/.ssh/authorized_keys && chmod 0600 %s/.ssh/authorized_keys", i.Username(), i.Username(), homeDir, homeDir)
	return i.SSH(command, nil)
}

func (i *Instance) DeployApp(appPath string) error {
	switch i.DeploymentTarget {
	case DeploymentTargetHelm:
		return i.DeployWithHelm(appPath)
	case DeploymentTargetCompose:
		return i.DeployWithDockerContext(appPath)
	default:
		return fmt.Errorf("unsupported deployment target %q", i.DeploymentTarget)
	}
}

func (i *Instance) SetupScripts() error {
	if err := i.SetupSSHAccess(); err != nil {
		return err
	}
	if len(i.SSHPublicKeys()) > 0 {
		if err := i.EnsureRemoteAuthorizedKeysOwner(); err != nil {
			return err
		}
	}
	return nil
}

func (i *Instance) CloneIfURL(appPath string) (string, func(), error) {
	if strings.HasPrefix(appPath, "http://") || strings.HasPrefix(appPath, "https://") {
		parts := strings.SplitN(appPath, "#", 2)
		gitURL := parts[0]
		ref := "master"
		if len(parts) == 2 && parts[1] != "" {
			ref = parts[1]
		}
		tmpDir, err := os.MkdirTemp("", "pullpreview-app-*")
		if err != nil {
			return "", nil, err
		}
		cleanup := func() { _ = os.RemoveAll(tmpDir) }
		cmd := exec.CommandContext(i.Context, "git", "clone", gitURL, "--depth=1", "--branch", ref, tmpDir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := i.Runner.Run(cmd); err != nil {
			cleanup()
			return "", nil, err
		}
		return tmpDir, cleanup, nil
	}
	return appPath, func() {}, nil
}

func (i *Instance) AppSizeMB(path string) float64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return float64(info.Size()) / 1024.0 / 1024.0
}
