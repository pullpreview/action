package pullpreview

import (
	"bytes"
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

const remoteAppPath = "/app"

type Runner interface {
	Run(cmd *exec.Cmd) error
}

type SystemRunner struct{}

func (r SystemRunner) Run(cmd *exec.Cmd) error {
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type Instance struct {
	Name           string
	Subdomain      string
	Admins         []string
	CIDRs          []string
	ComposeFiles   []string
	ComposeOptions []string
	DefaultPort    string
	DNS            string
	Ports          []string
	ProxyTLS       string
	Provider       Provider
	Registries     []string
	Size           string
	Tags           map[string]string
	PreScript      string
	Access         AccessDetails
	Logger         *Logger
	Runner         Runner
}

func NewInstance(name string, opts CommonOptions, provider Provider, logger *Logger) *Instance {
	normalized := NormalizeName(name)
	defaultPort := defaultString(opts.DefaultPort, "80")
	proxyTLS := strings.TrimSpace(opts.ProxyTLS)
	if proxyTLS != "" {
		if defaultPort != "443" && logger != nil {
			logger.Warnf("proxy_tls=%q enabled: overriding default_port=%s to 443", proxyTLS, defaultPort)
		}
		defaultPort = "443"
	}
	return &Instance{
		Name:           normalized,
		Subdomain:      NormalizeName(name),
		Admins:         opts.Admins,
		CIDRs:          defaultSlice(opts.CIDRs, []string{"0.0.0.0/0"}),
		ComposeFiles:   defaultSlice(opts.ComposeFiles, []string{"docker-compose.yml"}),
		ComposeOptions: defaultSlice(opts.ComposeOptions, []string{"--build"}),
		DefaultPort:    defaultPort,
		DNS:            defaultString(opts.DNS, "my.preview.run"),
		Ports:          opts.Ports,
		ProxyTLS:       proxyTLS,
		Provider:       provider,
		Registries:     opts.Registries,
		Size:           opts.InstanceType,
		Tags:           defaultMap(opts.Tags),
		PreScript:      opts.PreScript,
		Logger:         logger,
		Runner:         SystemRunner{},
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

func (i *Instance) LaunchAndWait() error {
	userData := UserData{AppPath: remoteAppPath, SSHPublicKeys: i.SSHPublicKeys(), Username: i.Username()}
	access, err := i.Provider.Launch(i.Name, LaunchOptions{
		Size:     i.Size,
		UserData: userData.Script(),
		Ports:    i.PortsWithDefaults(),
		CIDRs:    i.CIDRs,
		Tags:     i.Tags,
	})
	if err != nil {
		return err
	}
	i.Access = access
	if i.Logger != nil {
		i.Logger.Infof("Instance is running public_ip=%s", i.PublicIP())
	}
	if ok := WaitUntil(30, 5*time.Second, func() bool {
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
		if proxyTLSEnabled && firewallRuleTargetsPort(port, 80) {
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
	content := strings.Join(keys, "\n") + "\n"
	return i.SCP(bytes.NewBufferString(content), fmt.Sprintf("/home/%s/.ssh/authorized_keys", i.Username()), "0600")
}

func (i *Instance) SetupPreScript() error {
	script := BuildPreScript(i.Registries, i.PreScript, i.Logger)
	return i.SCP(bytes.NewBufferString(script), "/tmp/pre_script.sh", "0755")
}

func (i *Instance) SCP(input io.Reader, target, mode string) error {
	command := fmt.Sprintf("cat - > %s && chmod %s %s", target, mode, target)
	return i.SSH(command, input)
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

	args := []string{}
	if i.Logger != nil && i.Logger.level <= LevelDebug {
		args = append(args, "-v")
	}
	args = append(args,
		"-o", "ServerAliveInterval=15",
		"-o", "IdentitiesOnly=yes",
		"-i", keyFile,
	)
	args = append(args, i.SSHOptions()...)
	args = append(args, i.SSHAddress())
	args = append(args, command)

	cmd := exec.Command("ssh", args...)
	cmd.Stdin = input
	if input == nil {
		cmd.Stdin = os.Stdin
	}
	return i.Runner.Run(cmd)
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
	command := fmt.Sprintf("chown %s.%s /home/%s/.ssh/authorized_keys && chmod 0600 /home/%s/.ssh/authorized_keys", i.Username(), i.Username(), i.Username(), i.Username())
	return i.SSH(command, nil)
}

func (i *Instance) UpdateFromTarball(appPath, tarballPath string) error {
	return i.DeployWithDockerContext(appPath, tarballPath)
}

func (i *Instance) SetupScripts() error {
	if err := i.SetupSSHAccess(); err != nil {
		return err
	}
	if err := i.EnsureRemoteAuthorizedKeysOwner(); err != nil {
		return err
	}
	if err := i.SetupPreScript(); err != nil {
		return err
	}
	return nil
}

func (i *Instance) LocalTarballPath(appPath string) (string, func(), error) {
	return CreateTarball(appPath)
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
		cmd := exec.Command("git", "clone", gitURL, "--depth=1", "--branch", ref, tmpDir)
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
