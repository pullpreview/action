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
	composeFailureReportOutputLimit  = 8000
)

type composePSContainer struct {
	Service  string
	Name     string
	State    string
	Health   string
	ExitCode int
}

type bindMountSync struct {
	LocalSource  string
	RemoteSource string
	IsDir        bool
}

func (i *Instance) DeployWithDockerContext(appPath string) error {
	pullpreviewEnv, err := i.writeRemoteEnvFile()
	if err != nil {
		return err
	}
	composeConfig, syncPlan, err := i.composeConfigForRemoteContext(appPath, pullpreviewEnv)
	if err != nil {
		return err
	}
	if err := i.syncRemoteBindMountSources(syncPlan); err != nil {
		return err
	}
	if err := i.runRemotePreScript(appPath); err != nil {
		return err
	}
	return i.runComposeOnRemoteContext(composeConfig)
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

func (i *Instance) runRemotePreScript(appPath string) error {
	script, err := i.inlinePreScript(appPath)
	if err != nil {
		return err
	}
	command := fmt.Sprintf("cd %s && bash -se", remoteAppPath)
	return i.SSH(command, bytes.NewBufferString(script))
}

func (i *Instance) composeConfigForRemoteContext(appPath string, pullpreviewEnv map[string]string) ([]byte, []bindMountSync, error) {
	absAppPath, err := filepath.Abs(appPath)
	if err != nil {
		return nil, nil, err
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
		return nil, nil, fmt.Errorf("unable to render compose config: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}

	syncPlan, err := collectBindMountSyncs(stdout.Bytes(), absAppPath, remoteAppPath)
	if err != nil {
		return nil, nil, err
	}

	rewritten, err := rewriteRelativeBindSources(stdout.Bytes(), absAppPath, remoteAppPath)
	if err != nil {
		return nil, nil, err
	}
	finalConfig, err := applyProxyTLS(rewritten, i.ProxyTLS, i.PublicDNS(), i.Logger)
	if err != nil {
		return nil, nil, err
	}
	return finalConfig, syncPlan, nil
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

func collectBindMountSyncs(composeConfigJSON []byte, absAppPath, remoteRoot string) ([]bindMountSync, error) {
	var config map[string]any
	if err := json.Unmarshal(composeConfigJSON, &config); err != nil {
		return nil, fmt.Errorf("unable to parse compose config for bind mount sync: %w", err)
	}

	rawServices, ok := config["services"].(map[string]any)
	if !ok {
		return nil, nil
	}

	seen := map[string]struct{}{}
	syncs := []bindMountSync{}
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

			localSource := source
			if !filepath.IsAbs(localSource) {
				localSource = filepath.Join(absAppPath, localSource)
			}
			localSource = filepath.Clean(localSource)

			remoteSource, err := remoteBindSource(localSource, absAppPath, remoteRoot)
			if err != nil {
				return nil, fmt.Errorf("service %s bind mount %q: %w", serviceName, source, err)
			}

			info, err := os.Stat(localSource)
			if err != nil {
				return nil, fmt.Errorf("service %s bind mount %q: %w", serviceName, source, err)
			}

			key := localSource + "=>" + remoteSource
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			syncs = append(syncs, bindMountSync{
				LocalSource:  localSource,
				RemoteSource: remoteSource,
				IsDir:        info.IsDir(),
			})
		}
	}

	sort.Slice(syncs, func(i, j int) bool {
		if syncs[i].RemoteSource == syncs[j].RemoteSource {
			return syncs[i].LocalSource < syncs[j].LocalSource
		}
		return syncs[i].RemoteSource < syncs[j].RemoteSource
	})
	return syncs, nil
}

func (i *Instance) syncRemoteBindMountSources(syncPlan []bindMountSync) error {
	if len(syncPlan) == 0 {
		if i.Logger != nil {
			i.Logger.Infof("No bind mounts detected in compose config; skipping source sync")
		}
		return nil
	}
	if i.Logger != nil {
		i.Logger.Infof("Syncing %d bind mount source path(s) to remote host", len(syncPlan))
	}
	if err := i.ensureRemoteBindMountTargets(syncPlan); err != nil {
		return err
	}
	for _, entry := range syncPlan {
		if i.Logger != nil {
			i.Logger.Infof("Rsync bind mount local=%s remote=%s", entry.LocalSource, entry.RemoteSource)
		}
		if err := i.rsyncBindMount(entry); err != nil {
			return err
		}
	}
	return nil
}

func (i *Instance) ensureRemoteBindMountTargets(syncPlan []bindMountSync) error {
	user := i.Username()
	remoteDirs := map[string]struct{}{
		remoteAppPath: {},
	}
	for _, entry := range syncPlan {
		if entry.IsDir {
			remoteDirs[entry.RemoteSource] = struct{}{}
			continue
		}
		remoteDirs[path.Dir(entry.RemoteSource)] = struct{}{}
	}
	ordered := make([]string, 0, len(remoteDirs))
	for dir := range remoteDirs {
		ordered = append(ordered, dir)
	}
	sort.Strings(ordered)
	quoted := make([]string, 0, len(ordered))
	for _, dir := range ordered {
		quoted = append(quoted, shellQuote(dir))
	}
	command := fmt.Sprintf(
		"sudo mkdir -p %s && sudo chown %s.%s %s",
		strings.Join(quoted, " "),
		user,
		user,
		strings.Join(quoted, " "),
	)
	return i.SSH(command, nil)
}

func (i *Instance) rsyncBindMount(entry bindMountSync) error {
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

	sshArgs := []string{
		"ssh",
		"-o", "ServerAliveInterval=15",
		"-o", "IdentitiesOnly=yes",
		"-i", keyFile,
	}
	if certFile != "" {
		sshArgs = append(sshArgs, "-o", "CertificateFile="+certFile)
	}
	sshArgs = append(sshArgs, i.SSHOptions()...)

	source := entry.LocalSource
	remote := fmt.Sprintf("%s:%s", i.SSHAddress(), entry.RemoteSource)
	if entry.IsDir {
		source = ensureTrailingSlash(source)
		remote += "/"
	}

	cmd := exec.CommandContext(i.Context, "rsync",
		"-az",
		"--delete",
		"--links",
		"--omit-dir-times",
		"--no-perms",
		"--no-owner",
		"--no-group",
		"-e", strings.Join(sshArgs, " "),
		source,
		remote,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := i.Runner.Run(cmd); err != nil {
		return fmt.Errorf("rsync %s -> %s failed: %w", entry.LocalSource, entry.RemoteSource, err)
	}
	return nil
}

func ensureTrailingSlash(value string) string {
	if strings.HasSuffix(value, string(filepath.Separator)) {
		return value
	}
	return value + string(filepath.Separator)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (i *Instance) inlinePreScript(appPath string) (string, error) {
	lines := []string{
		"set -a",
		fmt.Sprintf("source %s", remoteEnvPath),
		"set +a",
	}
	for _, registry := range ParseRegistryCredentials(i.Registries, i.Logger) {
		lines = append(lines,
			fmt.Sprintf("echo \"Logging into %s...\"", registry.Host),
			fmt.Sprintf("echo \"%s\" | docker login \"%s\" -u \"%s\" --password-stdin", registry.Password, registry.Host, registry.Username),
		)
	}
	if strings.TrimSpace(i.PreScript) == "" {
		return strings.Join(lines, "\n") + "\n", nil
	}

	pathValue := i.PreScript
	if !filepath.IsAbs(pathValue) {
		pathValue = filepath.Join(appPath, pathValue)
	}
	content, err := os.ReadFile(pathValue)
	if err != nil {
		return "", fmt.Errorf("unable to read pre_script at %s: %w", i.PreScript, err)
	}

	lines = append(lines, fmt.Sprintf("echo \"Running pre_script %s inline over SSH...\"", i.PreScript))
	lines = append(lines, string(content))
	return strings.Join(lines, "\n") + "\n", nil
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
		i.emitComposeFailureReport(env, contextName, upArgs, err)
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

func (i *Instance) runDockerCommandOnContextCapture(env []string, contextName string, args ...string) (string, error) {
	cmdArgs := []string{"--context", contextName}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.CommandContext(i.Context, "docker", cmdArgs...)
	cmd.Env = env
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	output := mergeCommandOutput(stdout.String(), stderr.String())
	if err != nil {
		return output, fmt.Errorf("docker %s failed: %w", strings.Join(args, " "), err)
	}
	return output, nil
}

func (i *Instance) emitComposeFailureReport(env []string, contextName string, upArgs []string, upErr error) {
	report := i.composeFailureReport(env, contextName, upArgs, upErr)
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

func (i *Instance) composeFailureReport(env []string, contextName string, upArgs []string, upErr error) string {
	diagnostics := []string{}
	projectFilter := "label=com.docker.compose.project=" + dockerProjectName

	dockerPSOutput, dockerPSErr := i.runDockerCommandOnContextCapture(env, contextName, "ps", "-a", "--filter", projectFilter)
	if dockerPSErr != nil {
		diagnostics = append(diagnostics, fmt.Sprintf("Unable to run docker ps -a on runner context: %v", dockerPSErr))
		dockerPSOutput = ""
	}

	dockerPSJSONOutput, dockerPSJSONErr := i.runDockerCommandOnContextCapture(
		env,
		contextName,
		"ps",
		"-a",
		"--filter",
		projectFilter,
		"--format",
		"{{json .}}",
	)
	if dockerPSJSONErr != nil {
		diagnostics = append(diagnostics, fmt.Sprintf("Unable to run docker ps -a --format '{{json .}}' on runner context: %v", dockerPSJSONErr))
	}

	containers := []composePSContainer{}
	if dockerPSJSONErr == nil && strings.TrimSpace(dockerPSJSONOutput) != "" {
		parsed, err := parseDockerPSOutput(dockerPSJSONOutput)
		if err != nil {
			diagnostics = append(diagnostics, fmt.Sprintf("Unable to parse docker ps output: %v", err))
		} else {
			containers = parsed
		}
	}

	failed := selectFailedContainers(containers)
	return renderComposeFailureReport(i, upArgs, upErr, containers, failed, dockerPSOutput, diagnostics)
}

func parseDockerPSOutput(raw string) ([]composePSContainer, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []composePSContainer{}, nil
	}

	containers := []composePSContainer{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row struct {
			Names  string `json:"Names"`
			Status string `json:"Status"`
			Labels string `json:"Labels"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, err
		}
		state, health, exitCode := parseDockerPSStatus(row.Status)
		service := dockerPSLabelValue(row.Labels, "com.docker.compose.service")
		if service == "" {
			service = strings.TrimSpace(row.Names)
		}
		containers = append(containers, composePSContainer{
			Service:  service,
			Name:     strings.TrimSpace(row.Names),
			State:    state,
			Health:   health,
			ExitCode: exitCode,
		})
	}
	return containers, nil
}

func dockerPSLabelValue(labels string, key string) string {
	for _, pair := range strings.Split(labels, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[0]) == key {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

func parseDockerPSStatus(status string) (string, string, int) {
	s := strings.ToLower(strings.TrimSpace(status))
	state := "unknown"
	switch {
	case strings.HasPrefix(s, "up "):
		state = "running"
	case strings.HasPrefix(s, "exited "):
		state = "exited"
	case strings.HasPrefix(s, "created"):
		state = "created"
	case strings.HasPrefix(s, "restarting"):
		state = "restarting"
	case strings.HasPrefix(s, "paused"):
		state = "paused"
	case strings.HasPrefix(s, "dead"):
		state = "dead"
	case strings.HasPrefix(s, "removing"):
		state = "removing"
	}

	health := ""
	switch {
	case strings.Contains(s, "(unhealthy)"):
		health = "unhealthy"
	case strings.Contains(s, "(healthy)"):
		health = "healthy"
	case strings.Contains(s, "(health: starting)"):
		health = "starting"
	}

	exitCode := 0
	if strings.HasPrefix(s, "exited (") {
		rest := strings.TrimPrefix(s, "exited (")
		if idx := strings.Index(rest, ")"); idx > 0 {
			if code, err := strconv.Atoi(strings.TrimSpace(rest[:idx])); err == nil {
				exitCode = code
			}
		}
	}
	return state, health, exitCode
}

func selectFailedContainers(containers []composePSContainer) []composePSContainer {
	out := []composePSContainer{}
	seen := map[string]bool{}
	for _, container := range containers {
		if !container.isFailed() {
			continue
		}
		key := failedContainerLogKey(container)
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

func failedContainerLogKey(container composePSContainer) string {
	name := strings.TrimSpace(container.Name)
	if name != "" {
		return name
	}
	return strings.TrimSpace(container.Service)
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
	containers []composePSContainer,
	failed []composePSContainer,
	psOutput string,
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

	b.WriteString(renderContainerHealthOverview(containers, failed))

	b.WriteString("### Connect and Troubleshoot\n\n")
	b.WriteString("Diagnostics below are captured on the runner via Docker context.\n\n")
	if strings.TrimSpace(ssh) != "" {
		b.WriteString("```bash\n")
		b.WriteString(fmt.Sprintf("ssh %s\n", ssh))
		b.WriteString("# then on the instance:\n")
		b.WriteString("docker ps -a\n")
		b.WriteString("```\n\n")
	} else {
		b.WriteString("```bash\n")
		b.WriteString("docker ps -a\n")
		b.WriteString("```\n\n")
	}

	if strings.TrimSpace(psOutput) != "" {
		b.WriteString("### `docker ps -a` (runner context)\n\n")
		b.WriteString("```text\n")
		b.WriteString(truncateReportOutput(psOutput, composeFailureReportOutputLimit))
		b.WriteString("\n```\n\n")
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

func renderContainerHealthOverview(containers []composePSContainer, failed []composePSContainer) string {
	var b strings.Builder
	b.WriteString("### Container Health Overview\n\n")
	if len(containers) == 0 {
		b.WriteString("Container status could not be determined.\n\n")
		return b.String()
	}

	runningCount := 0
	unhealthyCount := 0
	for _, container := range containers {
		if strings.EqualFold(strings.TrimSpace(container.State), "running") {
			runningCount++
		}
		if strings.EqualFold(strings.TrimSpace(container.Health), "unhealthy") {
			unhealthyCount++
		}
	}

	b.WriteString(fmt.Sprintf(
		"- Total containers: `%d`\n- Running: `%d`\n- Failed: `%d`\n- Unhealthy: `%d`\n\n",
		len(containers),
		runningCount,
		len(failed),
		unhealthyCount,
	))

	for _, container := range containers {
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

		status := "ok"
		if container.isFailed() {
			status = "failed"
		}
		b.WriteString(fmt.Sprintf("- `%s` (`%s`) state=`%s` health=`%s` exit_code=`%d` status=`%s`\n", service, name, state, health, container.ExitCode, status))
	}
	b.WriteString("\n")
	return b.String()
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
