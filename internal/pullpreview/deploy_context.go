package pullpreview

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	remoteEnvPath     = "/etc/pullpreview/env"
	dockerProjectName = "app"

	composeFailureReportServiceLimit = 5
	composeFailureReportLogTail      = 120
	composeFailureReportOutputLimit  = 8000
)

type composePSContainer struct {
	Service  string
	Name     string
	State    string
	Health   string
	ExitCode int
}

func (i *Instance) DeployWithDockerContext(appPath, tarballPath string) error {
	if err := i.syncRemoteAppFromTarball(tarballPath); err != nil {
		return err
	}
	pullpreviewEnv, err := i.writeRemoteEnvFile()
	if err != nil {
		return err
	}
	if err := i.runRemotePreScript(); err != nil {
		return err
	}

	composeConfig, err := i.composeConfigForRemoteContext(appPath, pullpreviewEnv)
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

func (i *Instance) runRemotePreScript() error {
	command := fmt.Sprintf("cd %s && set -a && source %s && set +a && /tmp/pre_script.sh", remoteAppPath, remoteEnvPath)
	return i.SSH(command, nil)
}

func (i *Instance) composeConfigForRemoteContext(appPath string, pullpreviewEnv map[string]string) ([]byte, error) {
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

	cmd := exec.CommandContext(i.Context, "docker", args...)
	cmd.Dir = absAppPath
	cmd.Env = mergeEnvironment(os.Environ(), pullpreviewEnv)
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
	return applyProxyTLS(rewritten, i.ProxyTLS, i.PublicDNS(), i.Logger)
}

func (i *Instance) writeRemoteEnvFile() (map[string]string, error) {
	firstRun := "true"
	if i.SSH(fmt.Sprintf("test -f %s", remoteEnvPath), nil) == nil {
		firstRun = "false"
	}
	envValues := i.pullpreviewEnvValues(firstRun)

	content := strings.Join([]string{
		fmt.Sprintf("PULLPREVIEW_PUBLIC_DNS=%s", envValues["PULLPREVIEW_PUBLIC_DNS"]),
		fmt.Sprintf("PULLPREVIEW_PUBLIC_IP=%s", envValues["PULLPREVIEW_PUBLIC_IP"]),
		fmt.Sprintf("PULLPREVIEW_URL=%s", envValues["PULLPREVIEW_URL"]),
		fmt.Sprintf("PULLPREVIEW_FIRST_RUN=%s", envValues["PULLPREVIEW_FIRST_RUN"]),
		fmt.Sprintf("COMPOSE_FILE=%s", envValues["COMPOSE_FILE"]),
		"",
	}, "\n")
	if err := i.SCP(bytes.NewBufferString(content), "/tmp/pullpreview_env", "0644"); err != nil {
		return nil, err
	}
	user := i.Username()
	command := fmt.Sprintf(
		"sudo mkdir -p /etc/pullpreview && sudo mv /tmp/pullpreview_env %s && sudo chown %s.%s %s && sudo chmod 0644 %s",
		remoteEnvPath, user, user, remoteEnvPath, remoteEnvPath,
	)
	if err := i.SSH(command, nil); err != nil {
		return nil, err
	}
	return envValues, nil
}

func (i *Instance) pullpreviewEnvValues(firstRun string) map[string]string {
	return map[string]string{
		"PULLPREVIEW_PUBLIC_DNS": i.PublicDNS(),
		"PULLPREVIEW_PUBLIC_IP":  i.PublicIP(),
		"PULLPREVIEW_URL":        i.URL(),
		"PULLPREVIEW_FIRST_RUN":  firstRun,
		"COMPOSE_FILE":           strings.Join(i.ComposeFiles, ":"),
	}
}

func mergeEnvironment(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return append([]string{}, base...)
	}
	result := append([]string{}, base...)
	keyIndex := map[string]int{}
	for i, entry := range result {
		if idx := strings.Index(entry, "="); idx > 0 {
			keyIndex[entry[:idx]] = i
		}
	}
	for key, value := range overrides {
		pair := fmt.Sprintf("%s=%s", key, value)
		if idx, ok := keyIndex[key]; ok {
			result[idx] = pair
			continue
		}
		keyIndex[key] = len(result)
		result = append(result, pair)
	}
	return result
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

	createContext := exec.CommandContext(i.Context, "docker", "context", "create", contextName, "--docker", "host=ssh://"+hostAlias)
	createContext.Env = env
	createContext.Stdout = os.Stdout
	createContext.Stderr = os.Stderr
	if err := createContext.Run(); err != nil {
		return fmt.Errorf("unable to create docker context: %w", err)
	}
	defer func() {
		removeCmd := exec.CommandContext(i.Context, "docker", "context", "rm", "-f", contextName)
		removeCmd.Env = env
		removeCmd.Stdout = os.Stdout
		removeCmd.Stderr = os.Stderr
		_ = removeCmd.Run()
	}()

	credentials := ParseRegistryCredentials(i.Registries, i.Logger)
	if err := loginRegistriesOnRunner(i.Context, credentials, env); err != nil {
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
		i.emitComposeFailureReport(env, contextName, composeConfig, upArgs, err)
		return err
	}

	if err := i.runComposeCommandWithConfig(env, contextName, composeConfig, "logs", "--tail", "1000"); err != nil {
		return err
	}
	return nil
}

func loginRegistriesOnRunner(ctx context.Context, credentials []RegistryCredential, env []string) error {
	ctx = ensureContext(ctx)
	for _, cred := range credentials {
		cmd := exec.CommandContext(ctx, "docker", "login", cred.Host, "-u", cred.Username, "--password-stdin")
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
	cmdArgs := composeCommandArgs(contextName, args...)
	cmd := exec.CommandContext(i.Context, "docker", cmdArgs...)
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(composeConfig)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

func composeCommandArgs(contextName string, args ...string) []string {
	cmdArgs := []string{
		"--context", contextName,
		"compose",
		"--project-name", dockerProjectName,
		"-f", "-",
	}
	cmdArgs = append(cmdArgs, args...)
	return cmdArgs
}

func (i *Instance) runComposeCommandCaptureWithConfig(env []string, contextName string, composeConfig []byte, args ...string) (string, error) {
	cmd := exec.CommandContext(i.Context, "docker", composeCommandArgs(contextName, args...)...)
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(composeConfig)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := mergeCommandOutput(stdout.String(), stderr.String())
	if err != nil {
		return output, fmt.Errorf("docker compose %s failed: %w", strings.Join(args, " "), err)
	}
	return output, nil
}

func mergeCommandOutput(stdout, stderr string) string {
	stdout = strings.TrimSpace(stdout)
	stderr = strings.TrimSpace(stderr)
	if stdout == "" {
		return stderr
	}
	if stderr == "" {
		return stdout
	}
	return stdout + "\n" + stderr
}

func (i *Instance) emitComposeFailureReport(env []string, contextName string, composeConfig []byte, upArgs []string, upErr error) {
	report := i.composeFailureReport(env, contextName, composeConfig, upArgs, upErr)
	if strings.TrimSpace(report) == "" {
		return
	}
	if i.Logger != nil {
		i.Logger.Warnf("%s", report)
	} else {
		fmt.Println(report)
	}
	appendStepSummary(report, i.Logger)
}

func (i *Instance) composeFailureReport(env []string, contextName string, composeConfig []byte, upArgs []string, upErr error) string {
	diagnostics := []string{}
	psOutput, psErr := i.runComposeCommandCaptureWithConfig(env, contextName, composeConfig, "ps", "-a", "--format", "json")
	if psErr != nil {
		diagnostics = append(diagnostics, fmt.Sprintf("Unable to run docker compose ps -a --format json: %v", psErr))
		psOutput, psErr = i.runComposeCommandCaptureWithConfig(env, contextName, composeConfig, "ps", "-a")
		if psErr != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("Unable to run docker compose ps -a: %v", psErr))
		}
	}

	containers := []composePSContainer{}
	if strings.TrimSpace(psOutput) != "" {
		parsed, err := parseComposePSOutput(psOutput)
		if err != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("Unable to parse docker compose ps output: %v", err))
		} else {
			containers = parsed
		}
	}

	failed := selectFailedContainers(containers)
	logTargets := failed
	if len(logTargets) == 0 {
		logTargets = containers
	}
	if len(logTargets) > composeFailureReportServiceLimit {
		logTargets = logTargets[:composeFailureReportServiceLimit]
	}

	serviceLogs := map[string]string{}
	for _, container := range logTargets {
		service := strings.TrimSpace(container.Service)
		if service == "" {
			continue
		}
		if _, seen := serviceLogs[service]; seen {
			continue
		}
		logOutput, err := i.runComposeCommandCaptureWithConfig(
			env,
			contextName,
			composeConfig,
			"logs",
			"--tail",
			strconv.Itoa(composeFailureReportLogTail),
			service,
		)
		if err != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("Unable to collect logs for service %s: %v", service, err))
		}
		serviceLogs[service] = strings.TrimSpace(logOutput)
	}

	return renderComposeFailureReport(i, upArgs, upErr, failed, psOutput, serviceLogs, diagnostics)
}

func parseComposePSOutput(raw string) ([]composePSContainer, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty output")
	}

	objects := []map[string]any{}
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &objects); err != nil {
			return nil, err
		}
	} else {
		for _, line := range strings.Split(raw, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var object map[string]any
			if err := json.Unmarshal([]byte(line), &object); err != nil {
				return nil, err
			}
			objects = append(objects, object)
		}
	}
	if len(objects) == 0 {
		return nil, fmt.Errorf("no containers found")
	}

	containers := make([]composePSContainer, 0, len(objects))
	for _, object := range objects {
		containers = append(containers, composePSContainer{
			Service:  composePSFieldString(object, "Service", "service"),
			Name:     composePSFieldString(object, "Name", "name"),
			State:    composePSFieldString(object, "State", "Status", "state"),
			Health:   composePSFieldString(object, "Health", "health"),
			ExitCode: composePSFieldInt(object, "ExitCode", "exit_code"),
		})
	}
	return containers, nil
}

func composePSFieldString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case float64:
			return strconv.Itoa(int(typed))
		case json.Number:
			return typed.String()
		}
	}
	return ""
}

func composePSFieldInt(values map[string]any, keys ...string) int {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case float64:
			return int(typed)
		case int:
			return typed
		case int64:
			return int(typed)
		case json.Number:
			if parsed, err := typed.Int64(); err == nil {
				return int(parsed)
			}
		case string:
			if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil {
				return parsed
			}
		}
	}
	return 0
}

func selectFailedContainers(containers []composePSContainer) []composePSContainer {
	out := []composePSContainer{}
	seen := map[string]bool{}
	for _, container := range containers {
		if !container.isFailed() {
			continue
		}
		key := strings.TrimSpace(container.Service)
		if key == "" {
			key = strings.TrimSpace(container.Name)
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, container)
		if len(out) == composeFailureReportServiceLimit {
			break
		}
	}
	return out
}

func (c composePSContainer) isFailed() bool {
	state := strings.ToLower(strings.TrimSpace(c.State))
	health := strings.ToLower(strings.TrimSpace(c.Health))
	if health == "unhealthy" {
		return true
	}
	if state == "" {
		return c.ExitCode != 0
	}
	if state != "running" {
		return true
	}
	return c.ExitCode != 0
}

func renderComposeFailureReport(
	instance *Instance,
	upArgs []string,
	upErr error,
	failed []composePSContainer,
	psOutput string,
	serviceLogs map[string]string,
	diagnostics []string,
) string {
	ssh := ""
	previewURL := ""
	admins := "none"
	if instance != nil {
		ssh = instance.SSHAddress()
		previewURL = instance.URL()
		if len(instance.Admins) > 0 {
			admins = strings.Join(instance.Admins, ", ")
		}
	}

	var b strings.Builder
	b.WriteString("## PullPreview Troubleshooting Report\n\n")
	b.WriteString(fmt.Sprintf("`docker compose %s` failed during deployment.\n\n", strings.Join(upArgs, " ")))
	if upErr != nil {
		b.WriteString(fmt.Sprintf("- Deploy error: `%s`\n", upErr.Error()))
	}
	if strings.TrimSpace(previewURL) != "" {
		b.WriteString(fmt.Sprintf("- Preview URL: `%s`\n", previewURL))
	}
	if strings.TrimSpace(ssh) != "" {
		b.WriteString(fmt.Sprintf("- SSH Command: `ssh %s`\n", ssh))
	}
	b.WriteString(fmt.Sprintf("- Authorized GitHub users: `%s`\n\n", admins))

	b.WriteString("### Failed Containers\n\n")
	if len(failed) == 0 {
		b.WriteString("No failed containers were detected automatically.\n\n")
	} else {
		for _, container := range failed {
			service := strings.TrimSpace(container.Service)
			if service == "" {
				service = "(unknown service)"
			}
			name := strings.TrimSpace(container.Name)
			if name == "" {
				name = "(unknown container)"
			}
			state := strings.TrimSpace(container.State)
			if state == "" {
				state = "unknown"
			}
			health := strings.TrimSpace(container.Health)
			if health == "" {
				health = "n/a"
			}
			b.WriteString(fmt.Sprintf("- `%s` (`%s`) state=`%s` health=`%s` exit_code=`%d`\n", service, name, state, health, container.ExitCode))
		}
		b.WriteString("\n")
	}

	b.WriteString("### Connect and Troubleshoot\n\n")
	b.WriteString("```bash\n")
	if strings.TrimSpace(ssh) != "" {
		b.WriteString(fmt.Sprintf("ssh %s\n", ssh))
	}
	b.WriteString("cd /app\n")
	b.WriteString("docker compose ps -a\n")
	if len(failed) == 0 {
		b.WriteString("docker compose logs --tail 200\n")
	} else {
		for _, container := range failed {
			service := strings.TrimSpace(container.Service)
			if service == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("docker compose logs --tail 200 %s\n", service))
		}
	}
	b.WriteString("docker compose config\n")
	b.WriteString("```\n\n")

	if strings.TrimSpace(psOutput) != "" {
		b.WriteString("### `docker compose ps -a`\n\n")
		b.WriteString("```text\n")
		b.WriteString(truncateReportOutput(psOutput, composeFailureReportOutputLimit))
		b.WriteString("\n```\n\n")
	}

	if len(serviceLogs) > 0 {
		services := orderedServiceLogKeys(failed, serviceLogs)
		for _, service := range services {
			logOutput := strings.TrimSpace(serviceLogs[service])
			if logOutput == "" {
				continue
			}
			b.WriteString(fmt.Sprintf("### Recent Logs: `%s`\n\n", service))
			b.WriteString("```text\n")
			b.WriteString(truncateReportOutput(logOutput, composeFailureReportOutputLimit))
			b.WriteString("\n```\n\n")
		}
	}

	if len(diagnostics) > 0 {
		b.WriteString("### Diagnostics Notes\n\n")
		for _, line := range diagnostics {
			b.WriteString(fmt.Sprintf("- %s\n", line))
		}
		b.WriteString("\n")
	}

	return strings.TrimSpace(b.String())
}

func truncateReportOutput(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	suffix := "\n... (truncated)"
	limit := maxLen - len(suffix)
	if limit < 0 {
		limit = 0
	}
	return strings.TrimSpace(value[:limit]) + suffix
}

func orderedServiceLogKeys(failed []composePSContainer, serviceLogs map[string]string) []string {
	seen := map[string]bool{}
	ordered := []string{}
	for _, container := range failed {
		service := strings.TrimSpace(container.Service)
		if service == "" || seen[service] {
			continue
		}
		ordered = append(ordered, service)
		seen[service] = true
	}

	remaining := []string{}
	for service := range serviceLogs {
		service = strings.TrimSpace(service)
		if service == "" || seen[service] {
			continue
		}
		remaining = append(remaining, service)
	}
	sort.Strings(remaining)
	return append(ordered, remaining...)
}

func appendStepSummary(content string, logger *Logger) {
	path := strings.TrimSpace(os.Getenv("GITHUB_STEP_SUMMARY"))
	if path == "" || strings.TrimSpace(content) == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		if logger != nil {
			logger.Warnf("Unable to open GITHUB_STEP_SUMMARY file: %v", err)
		}
		return
	}
	defer f.Close()
	if _, err := f.WriteString(strings.TrimSpace(content) + "\n\n"); err != nil && logger != nil {
		logger.Warnf("Unable to write GITHUB_STEP_SUMMARY: %v", err)
	}
}
