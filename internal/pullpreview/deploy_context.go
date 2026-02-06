package pullpreview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	remoteEnvPath     = "/etc/pullpreview/env"
	dockerProjectName = "app"
)

func (i *Instance) DeployWithDockerContext(appPath, tarballPath string) error {
	if err := i.syncRemoteAppFromTarball(tarballPath); err != nil {
		return err
	}
	if err := i.writeRemoteEnvFile(); err != nil {
		return err
	}
	if err := i.runRemotePreScript(); err != nil {
		return err
	}

	composeConfig, err := i.composeConfigForRemoteContext(appPath)
	if err != nil {
		return err
	}
	return i.runComposeOnRemoteContext(composeConfig)
}

func (i *Instance) syncRemoteAppFromTarball(tarballPath string) error {
	remotePath := fmt.Sprintf("/tmp/app-%d.tar.gz", time.Now().UTC().Unix())
	file, err := os.Open(tarballPath)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := i.SCP(file, remotePath, "0644"); err != nil {
		return err
	}
	user := i.Username()
	command := fmt.Sprintf(
		"sudo rm -rf %s && sudo mkdir -p %s && sudo chown -R %s.%s %s && tar xzf %s -C %s && rm -f %s",
		remoteAppPath, remoteAppPath, user, user, remoteAppPath, remotePath, remoteAppPath, remotePath,
	)
	return i.SSH(command, nil)
}

func (i *Instance) writeRemoteEnvFile() error {
	firstRun := "true"
	if i.SSH(fmt.Sprintf("test -f %s", remoteEnvPath), nil) == nil {
		firstRun = "false"
	}

	content := strings.Join([]string{
		fmt.Sprintf("PULLPREVIEW_PUBLIC_DNS=%s", i.PublicDNS()),
		fmt.Sprintf("PULLPREVIEW_PUBLIC_IP=%s", i.PublicIP()),
		fmt.Sprintf("PULLPREVIEW_URL=%s", i.URL()),
		fmt.Sprintf("PULLPREVIEW_FIRST_RUN=%s", firstRun),
		fmt.Sprintf("COMPOSE_FILE=%s", strings.Join(i.ComposeFiles, ":")),
		"",
	}, "\n")
	if err := i.SCP(bytes.NewBufferString(content), "/tmp/pullpreview_env", "0644"); err != nil {
		return err
	}
	user := i.Username()
	command := fmt.Sprintf(
		"sudo mkdir -p /etc/pullpreview && sudo mv /tmp/pullpreview_env %s && sudo chown %s.%s %s && sudo chmod 0644 %s",
		remoteEnvPath, user, user, remoteEnvPath, remoteEnvPath,
	)
	return i.SSH(command, nil)
}

func (i *Instance) runRemotePreScript() error {
	command := fmt.Sprintf("cd %s && set -a && source %s && set +a && /tmp/pre_script.sh", remoteAppPath, remoteEnvPath)
	return i.SSH(command, nil)
}

func (i *Instance) composeConfigForRemoteContext(appPath string) ([]byte, error) {
	absAppPath, err := filepath.Abs(appPath)
	if err != nil {
		return nil, err
	}

	args := []string{"compose"}
	for _, composeFile := range i.ComposeFiles {
		pathValue := composeFile
		if !filepath.IsAbs(pathValue) {
			pathValue = filepath.Join(absAppPath, composeFile)
		}
		args = append(args, "-f", pathValue)
	}
	args = append(args, "config", "--format", "json")

	cmd := exec.Command("docker", args...)
	cmd.Dir = absAppPath
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("unable to render compose config: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	rewritten, err := rewriteRelativeBindSources(stdout.Bytes(), absAppPath, remoteAppPath)
	if err != nil {
		return nil, err
	}
	return rewritten, nil
}

func rewriteRelativeBindSources(composeConfigJSON []byte, absAppPath, remoteRoot string) ([]byte, error) {
	var config map[string]any
	if err := json.Unmarshal(composeConfigJSON, &config); err != nil {
		return nil, fmt.Errorf("unable to parse compose config: %w", err)
	}

	rawServices, ok := config["services"].(map[string]any)
	if !ok {
		return composeConfigJSON, nil
	}

	for serviceName, rawService := range rawServices {
		service, ok := rawService.(map[string]any)
		if !ok {
			continue
		}
		rawVolumes, ok := service["volumes"].([]any)
		if !ok {
			continue
		}

		for _, rawVolume := range rawVolumes {
			volume, ok := rawVolume.(map[string]any)
			if !ok {
				continue
			}
			typeValue, _ := volume["type"].(string)
			if strings.ToLower(typeValue) != "bind" {
				continue
			}
			source, _ := volume["source"].(string)
			if strings.TrimSpace(source) == "" {
				continue
			}
			remoteSource, err := remoteBindSource(source, absAppPath, remoteRoot)
			if err != nil {
				return nil, fmt.Errorf("service %s bind mount %q: %w", serviceName, source, err)
			}
			volume["source"] = remoteSource
		}
	}

	return json.Marshal(config)
}

func remoteBindSource(source, absAppPath, remoteRoot string) (string, error) {
	resolvedSource := source
	if !filepath.IsAbs(resolvedSource) {
		resolvedSource = filepath.Join(absAppPath, resolvedSource)
	}
	resolvedSource = filepath.Clean(resolvedSource)
	absAppPath = filepath.Clean(absAppPath)

	rel, err := filepath.Rel(absAppPath, resolvedSource)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return remoteRoot, nil
	}
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("absolute bind mount outside app_path is unsupported in context mode")
	}
	return path.Join(remoteRoot, filepath.ToSlash(rel)), nil
}

func (i *Instance) runComposeOnRemoteContext(composeConfig []byte) error {
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

	hostAlias := fmt.Sprintf("pullpreview-%s", i.Name)
	restoreSSHConfig, err := injectSSHHostAlias(hostAlias, i.PublicIP(), i.Username(), keyFile, certFile)
	if err != nil {
		return err
	}
	defer restoreSSHConfig()

	contextName := fmt.Sprintf("pullpreview-%s-%d", i.Name, time.Now().UnixNano())
	env := os.Environ()

	createContext := exec.Command("docker", "context", "create", contextName, "--docker", "host=ssh://"+hostAlias)
	createContext.Env = env
	createContext.Stdout = os.Stdout
	createContext.Stderr = os.Stderr
	if err := createContext.Run(); err != nil {
		return fmt.Errorf("unable to create docker context: %w", err)
	}
	defer func() {
		removeCmd := exec.Command("docker", "context", "rm", "-f", contextName)
		removeCmd.Env = env
		removeCmd.Stdout = os.Stdout
		removeCmd.Stderr = os.Stderr
		_ = removeCmd.Run()
	}()

	credentials := ParseRegistryCredentials(i.Registries, i.Logger)
	if err := loginRegistriesOnRunner(credentials, env); err != nil {
		return err
	}

	pullErr := error(nil)
	for attempt := 1; attempt <= 5; attempt++ {
		pullErr = i.runComposeCommandWithConfig(env, contextName, composeConfig, "pull", "-q")
		if pullErr == nil {
			break
		}
	}
	if pullErr != nil {
		return pullErr
	}

	upArgs := []string{"up", "--wait", "--remove-orphans", "-d"}
	upArgs = append(upArgs, i.ComposeOptions...)
	if err := i.runComposeCommandWithConfig(env, contextName, composeConfig, upArgs...); err != nil {
		return err
	}

	if err := i.runComposeCommandWithConfig(env, contextName, composeConfig, "logs", "--tail", "1000"); err != nil {
		return err
	}
	return nil
}

func loginRegistriesOnRunner(credentials []RegistryCredential, env []string) error {
	for _, cred := range credentials {
		cmd := exec.Command("docker", "login", cred.Host, "-u", cred.Username, "--password-stdin")
		cmd.Env = env
		cmd.Stdin = strings.NewReader(cred.Password + "\n")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("docker login %s failed: %w", cred.Host, err)
		}
	}
	return nil
}

func injectSSHHostAlias(hostAlias, hostName, userName, keyFile, certFile string) (func(), error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return nil, err
	}
	configPath := filepath.Join(sshDir, "config")
	originalContent, readErr := os.ReadFile(configPath)
	fileExisted := readErr == nil
	if readErr != nil && !os.IsNotExist(readErr) {
		return nil, readErr
	}

	lines := []string{
		fmt.Sprintf("Host %s", hostAlias),
		fmt.Sprintf("  HostName %s", hostName),
		fmt.Sprintf("  User %s", userName),
		fmt.Sprintf("  IdentityFile %s", keyFile),
		"  IdentitiesOnly yes",
		"  ServerAliveInterval 15",
		"  StrictHostKeyChecking no",
		"  UserKnownHostsFile /dev/null",
		"  LogLevel ERROR",
		"  ConnectTimeout 10",
	}
	if strings.TrimSpace(certFile) != "" {
		lines = append(lines, fmt.Sprintf("  CertificateFile %s", certFile))
	}

	newContent := append([]byte{}, originalContent...)
	if len(newContent) > 0 && !bytes.HasSuffix(newContent, []byte("\n")) {
		newContent = append(newContent, '\n')
	}
	newContent = append(newContent, []byte(strings.Join(lines, "\n")+"\n")...)
	if err := os.WriteFile(configPath, newContent, 0600); err != nil {
		return nil, err
	}

	cleanup := func() {
		if fileExisted {
			_ = os.WriteFile(configPath, originalContent, 0600)
			return
		}
		_ = os.Remove(configPath)
	}
	return cleanup, nil
}

func (i *Instance) runComposeCommandWithConfig(env []string, contextName string, composeConfig []byte, args ...string) error {
	cmdArgs := []string{
		"--context", contextName,
		"compose",
		"--project-name", dockerProjectName,
		"-f", "-",
	}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command("docker", cmdArgs...)
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(composeConfig)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}
