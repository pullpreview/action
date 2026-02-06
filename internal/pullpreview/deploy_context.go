package pullpreview

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	composecli "github.com/compose-spec/compose-go/v2/cli"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	dockercommand "github.com/docker/cli/cli/command"
	dockerconfig "github.com/docker/cli/cli/config/configfile"
	dockercfgtypes "github.com/docker/cli/cli/config/types"
	"github.com/docker/cli/cli/connhelper"
	dockerflags "github.com/docker/cli/cli/flags"
	composeapi "github.com/docker/compose/v2/pkg/api"
	composeservice "github.com/docker/compose/v2/pkg/compose"
	dockerclient "github.com/docker/docker/client"
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

	project, err := i.composeProjectForRemoteContext(appPath)
	if err != nil {
		return err
	}
	return i.runComposeOnRemoteContext(project)
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

func (i *Instance) composeProjectForRemoteContext(appPath string) (*composetypes.Project, error) {
	absAppPath, err := filepath.Abs(appPath)
	if err != nil {
		return nil, err
	}

	configPaths := make([]string, 0, len(i.ComposeFiles))
	for _, composeFile := range i.ComposeFiles {
		pathValue := composeFile
		if !filepath.IsAbs(pathValue) {
			pathValue = filepath.Join(absAppPath, composeFile)
		}
		configPaths = append(configPaths, pathValue)
	}

	projectOptions, err := composecli.NewProjectOptions(
		configPaths,
		composecli.WithWorkingDirectory(absAppPath),
		composecli.WithOsEnv,
		composecli.WithEnv([]string{"PWD=" + absAppPath}),
		composecli.WithDotEnv,
		composecli.WithDefaultProfiles(),
		composecli.WithName(dockerProjectName),
		composecli.WithResolvedPaths(true),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to initialize compose loader: %w", err)
	}

	project, err := projectOptions.LoadProject(context.Background())
	if err != nil {
		return nil, fmt.Errorf("unable to load compose project: %w", err)
	}
	if project.Name == "" {
		project.Name = dockerProjectName
	}
	if err := rewriteProjectBindSources(project, absAppPath, remoteAppPath); err != nil {
		return nil, err
	}
	setComposeProjectLabels(project)
	return project, nil
}

func setComposeProjectLabels(project *composetypes.Project) {
	for serviceName, service := range project.Services {
		if service.CustomLabels == nil {
			service.CustomLabels = composetypes.Labels{}
		}
		service.CustomLabels[composeapi.ProjectLabel] = project.Name
		service.CustomLabels[composeapi.ServiceLabel] = serviceName
		service.CustomLabels[composeapi.VersionLabel] = composeapi.ComposeVersion
		service.CustomLabels[composeapi.WorkingDirLabel] = project.WorkingDir
		service.CustomLabels[composeapi.ConfigFilesLabel] = strings.Join(project.ComposeFiles, ",")
		service.CustomLabels[composeapi.OneoffLabel] = "False"
		project.Services[serviceName] = service
	}
}

func rewriteProjectBindSources(project *composetypes.Project, absAppPath, remoteRoot string) error {
	for serviceName, service := range project.Services {
		for idx, volume := range service.Volumes {
			if strings.ToLower(volume.Type) != composetypes.VolumeTypeBind {
				continue
			}
			if strings.TrimSpace(volume.Source) == "" {
				continue
			}
			remoteSource, err := remoteBindSource(volume.Source, absAppPath, remoteRoot)
			if err != nil {
				return fmt.Errorf("service %s bind mount %q: %w", serviceName, volume.Source, err)
			}
			service.Volumes[idx].Source = remoteSource
		}
		project.Services[serviceName] = service
	}
	return nil
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

func (i *Instance) runComposeOnRemoteContext(project *composetypes.Project) error {
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

	registryCredentials := ParseRegistryCredentials(i.Registries, i.Logger)
	dockerConfigDir, cleanupDockerConfig, err := writeDockerConfigDir(registryCredentials)
	if err != nil {
		return err
	}
	defer cleanupDockerConfig()

	service, cleanupService, err := i.newComposeService(keyFile, certFile, dockerConfigDir)
	if err != nil {
		return err
	}
	defer cleanupService()

	runtimeOptions := parseComposeRuntimeOptions(i.ComposeOptions, i.Logger)

	pullErr := error(nil)
	for attempt := 1; attempt <= 5; attempt++ {
		pullErr = service.Pull(context.Background(), project, composeapi.PullOptions{Quiet: runtimeOptions.QuietPull})
		if pullErr == nil {
			break
		}
		if i.Logger != nil {
			i.Logger.Warnf("compose pull failed attempt=%d err=%v", attempt, pullErr)
		}
		time.Sleep(2 * time.Second)
	}
	if pullErr != nil {
		return fmt.Errorf("docker compose pull failed: %w", pullErr)
	}

	create := composeapi.CreateOptions{
		Build:                runtimeOptions.Build,
		RemoveOrphans:        true,
		Recreate:             runtimeOptions.Recreate,
		RecreateDependencies: runtimeOptions.RecreateDependencies,
		Inherit:              runtimeOptions.InheritVolumes,
		QuietPull:            runtimeOptions.QuietPull,
	}
	start := composeapi.StartOptions{
		Project:     project,
		Wait:        true,
		WaitTimeout: runtimeOptions.WaitTimeout,
	}
	if err := service.Up(context.Background(), project, composeapi.UpOptions{Create: create, Start: start}); err != nil {
		return fmt.Errorf("docker compose up failed: %w", err)
	}

	if err := service.Logs(context.Background(), project.Name, stdLogConsumer{}, composeapi.LogOptions{
		Project: project,
		Tail:    "1000",
	}); err != nil {
		return fmt.Errorf("docker compose logs failed: %w", err)
	}
	return nil
}

func (i *Instance) newComposeService(keyFile, certFile, dockerConfigDir string) (composeapi.Service, func(), error) {
	daemonURL := "ssh://" + i.SSHAddress()
	sshFlags := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=10",
		"-i", keyFile,
	}
	if strings.TrimSpace(certFile) != "" {
		sshFlags = append(sshFlags, "-o", "CertificateFile="+certFile)
	}
	helper, err := connhelper.GetConnectionHelperWithSSHOpts(daemonURL, sshFlags)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to initialize ssh docker transport: %w", err)
	}

	apiClient, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost(helper.Host),
		dockerclient.WithDialContext(helper.Dialer),
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("unable to initialize docker api client: %w", err)
	}

	dockerCli, err := dockercommand.NewDockerCli(
		dockercommand.WithInputStream(os.Stdin),
		dockercommand.WithOutputStream(os.Stdout),
		dockercommand.WithErrorStream(os.Stderr),
	)
	if err != nil {
		_ = apiClient.Close()
		return nil, nil, fmt.Errorf("unable to initialize docker cli: %w", err)
	}

	clientOptions := dockerflags.NewClientOptions()
	clientOptions.ConfigDir = dockerConfigDir
	if err := dockerCli.Initialize(clientOptions, dockercommand.WithAPIClient(apiClient)); err != nil {
		_ = apiClient.Close()
		return nil, nil, fmt.Errorf("unable to initialize docker cli client: %w", err)
	}

	cleanup := func() {
		_ = apiClient.Close()
	}
	return composeservice.NewComposeService(dockerCli), cleanup, nil
}

func writeDockerConfigDir(credentials []RegistryCredential) (string, func(), error) {
	dir, err := os.MkdirTemp("", "pullpreview-docker-config-*")
	if err != nil {
		return "", nil, err
	}

	config := dockerconfig.New(filepath.Join(dir, "config.json"))
	config.AuthConfigs = map[string]dockercfgtypes.AuthConfig{}
	for _, cred := range credentials {
		auth := base64.StdEncoding.EncodeToString([]byte(cred.Username + ":" + cred.Password))
		config.AuthConfigs[cred.Host] = dockercfgtypes.AuthConfig{
			Username:      cred.Username,
			Password:      cred.Password,
			Auth:          auth,
			ServerAddress: cred.Host,
		}
	}
	if err := config.Save(); err != nil {
		_ = os.RemoveAll(dir)
		return "", nil, err
	}

	cleanup := func() {
		_ = os.RemoveAll(dir)
	}
	return dir, cleanup, nil
}

type composeRuntimeOptions struct {
	Build                *composeapi.BuildOptions
	Recreate             string
	RecreateDependencies string
	InheritVolumes       bool
	QuietPull            bool
	WaitTimeout          time.Duration
}

func parseComposeRuntimeOptions(composeOptions []string, logger *Logger) composeRuntimeOptions {
	options := composeRuntimeOptions{
		Build:                nil,
		Recreate:             composeapi.RecreateDiverged,
		RecreateDependencies: composeapi.RecreateDiverged,
		InheritVolumes:       true,
		QuietPull:            true,
		WaitTimeout:          10 * time.Minute,
	}

	for idx := 0; idx < len(composeOptions); idx++ {
		value := strings.TrimSpace(composeOptions[idx])
		if value == "" {
			continue
		}
		switch {
		case value == "--build":
			options.Build = &composeapi.BuildOptions{}
		case value == "--no-build":
			options.Build = nil
		case value == "--force-recreate":
			options.Recreate = composeapi.RecreateForce
		case value == "--no-recreate":
			options.Recreate = composeapi.RecreateNever
			options.RecreateDependencies = composeapi.RecreateNever
		case value == "--always-recreate-deps":
			options.RecreateDependencies = composeapi.RecreateForce
		case value == "--renew-anon-volumes" || value == "-V":
			options.InheritVolumes = false
		case value == "--quiet-pull":
			options.QuietPull = true
		case value == "--remove-orphans" || value == "--wait" || value == "--detach" || value == "-d":
			// Always set by the action.
		case strings.HasPrefix(value, "--wait-timeout="):
			raw := strings.TrimPrefix(value, "--wait-timeout=")
			timeout, err := strconv.Atoi(raw)
			if err == nil && timeout > 0 {
				options.WaitTimeout = time.Duration(timeout) * time.Second
			}
		case value == "--wait-timeout":
			if idx+1 >= len(composeOptions) {
				continue
			}
			next := strings.TrimSpace(composeOptions[idx+1])
			idx++
			timeout, err := strconv.Atoi(next)
			if err == nil && timeout > 0 {
				options.WaitTimeout = time.Duration(timeout) * time.Second
			}
		default:
			if logger != nil {
				logger.Warnf("Ignoring unsupported compose option in context mode: %s", value)
			}
		}
	}

	return options
}

type stdLogConsumer struct{}

func (stdLogConsumer) Register(container string) {}

func (stdLogConsumer) Log(containerName, message string) {
	printComposeLog(containerName, message)
}

func (stdLogConsumer) Err(containerName, message string) {
	printComposeLog(containerName, message)
}

func (stdLogConsumer) Status(containerName, message string) {
	printComposeLog(containerName, message)
}

func printComposeLog(containerName, message string) {
	text := strings.TrimSpace(message)
	if text == "" {
		return
	}
	if strings.TrimSpace(containerName) == "" {
		fmt.Println(text)
		return
	}
	fmt.Printf("%s | %s\n", containerName, text)
}
