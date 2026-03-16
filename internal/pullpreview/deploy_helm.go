package pullpreview

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	helmReleaseName             = "app"
	helmFailureReportOutputSize = 12000
	helmDeployTimeout           = "15m"
)

type helmChartSource struct {
	ChartRef            string
	LocalChart          string
	RepoURL             string
	RepoDefs            []helmRepoDefinition
	RequiresSync        bool
	SyncAppTree         bool
	DependencyBuildRefs []string
	ExtraSyncPaths      []helmSyncPath
}

type helmRepoDefinition struct {
	Name string
	URL  string
}

type helmSyncPath struct {
	Local  string
	Remote string
}

func (i *Instance) HelmNamespace() string {
	return kubernetesName("pp-" + i.Name)
}

func kubernetesName(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	name = strings.Join(strings.FieldsFunc(name, func(r rune) bool { return r == '-' }), "-")
	if name == "" {
		name = "app"
	}
	if len(name) <= 63 {
		return name
	}
	sum := fmt.Sprintf("%x", sha1.Sum([]byte(name)))
	return strings.Trim(name[:54], "-") + "-" + sum[:8]
}

func (i *Instance) deploymentPlaceholderReplacer() *strings.Replacer {
	return strings.NewReplacer(
		"{{ pullpreview_url }}", i.URL(),
		"{{pullpreview_url}}", i.URL(),
		"{{ pullpreview_public_dns }}", i.PublicDNS(),
		"{{pullpreview_public_dns}}", i.PublicDNS(),
		"{{ pullpreview_public_ip }}", i.PublicIP(),
		"{{pullpreview_public_ip}}", i.PublicIP(),
		"{{ namespace }}", i.HelmNamespace(),
		"{{namespace}}", i.HelmNamespace(),
		"{{ release_name }}", helmReleaseName,
		"{{release_name}}", helmReleaseName,
	)
}

func (i *Instance) expandDeploymentValue(value string) string {
	return i.deploymentPlaceholderReplacer().Replace(value)
}

func (i *Instance) DeployWithHelm(appPath string) error {
	if _, err := i.writeRemoteEnvFile(); err != nil {
		return err
	}

	chartSource, err := i.resolveHelmChartSource(appPath)
	if err != nil {
		return err
	}
	valueArgs, syncForValues, err := i.helmValueArgs(appPath)
	if err != nil {
		return err
	}

	if chartSource.SyncAppTree || syncForValues {
		if err := i.syncRemoteAppTree(appPath); err != nil {
			return err
		}
	}
	if chartSource.RequiresSync && chartSource.LocalChart != "" && !chartSource.SyncAppTree {
		if err := i.syncRemotePath(chartSource.LocalChart, chartSource.ChartRef); err != nil {
			return err
		}
	}
	for _, syncPath := range chartSource.ExtraSyncPaths {
		if err := i.syncRemotePath(syncPath.Local, syncPath.Remote); err != nil {
			return err
		}
	}
	if err := i.runRemotePreScript(appPath); err != nil {
		return err
	}
	if err := i.runHelmDeployment(chartSource, valueArgs); err != nil {
		i.emitHelmFailureReport()
		return err
	}
	return nil
}

func (i *Instance) resolveHelmChartSource(appPath string) (helmChartSource, error) {
	chart := strings.TrimSpace(i.Chart)
	if strings.HasPrefix(chart, "oci://") {
		if strings.TrimSpace(i.ChartRepository) != "" {
			return helmChartSource{}, fmt.Errorf("chart_repository is not supported with OCI chart references")
		}
		return helmChartSource{ChartRef: chart}, nil
	}
	if repoURL := strings.TrimSpace(i.ChartRepository); repoURL != "" {
		return helmChartSource{
			ChartRef: fmt.Sprintf("pullpreview/%s", strings.TrimLeft(chart, "/")),
			RepoURL:  repoURL,
			RepoDefs: []helmRepoDefinition{{Name: "pullpreview", URL: repoURL}},
		}, nil
	}

	absAppPath, err := filepath.Abs(appPath)
	if err != nil {
		return helmChartSource{}, err
	}
	localChart := chart
	if !filepath.IsAbs(localChart) {
		localChart = filepath.Join(absAppPath, localChart)
	}
	localChart = filepath.Clean(localChart)
	if _, err := os.Stat(localChart); err != nil {
		return helmChartSource{}, fmt.Errorf("unable to access chart %s: %w", chart, err)
	}
	repoDefs, dependencyPaths, err := helmDependencyInputs(localChart)
	if err != nil {
		return helmChartSource{}, fmt.Errorf("chart %s: %w", chart, err)
	}
	buildRefs := make([]string, 0, len(dependencyPaths))
	extraSyncPaths := []helmSyncPath{}
	for _, dependencyPath := range dependencyPaths {
		if pathWithinRoot(absAppPath, dependencyPath) {
			remoteRef, err := remoteBindSource(dependencyPath, absAppPath, remoteAppPath)
			if err != nil {
				return helmChartSource{}, fmt.Errorf("chart %s: %w", chart, err)
			}
			buildRefs = append(buildRefs, remoteRef)
			continue
		}

		remoteRef := externalHelmChartPath(dependencyPath)
		buildRefs = append(buildRefs, remoteRef)
		extraSyncPaths = append(extraSyncPaths, helmSyncPath{
			Local:  dependencyPath,
			Remote: remoteRef,
		})
	}
	if pathWithinRoot(absAppPath, localChart) {
		remoteChart, err := remoteBindSource(localChart, absAppPath, remoteAppPath)
		if err != nil {
			return helmChartSource{}, fmt.Errorf("chart %s: %w", chart, err)
		}
		return helmChartSource{
			ChartRef:            remoteChart,
			LocalChart:          localChart,
			RepoDefs:            repoDefs,
			RequiresSync:        true,
			SyncAppTree:         true,
			DependencyBuildRefs: buildRefs,
			ExtraSyncPaths:      extraSyncPaths,
		}, nil
	}
	remoteChart := externalHelmChartPath(localChart)
	return helmChartSource{
		ChartRef:            remoteChart,
		LocalChart:          localChart,
		RepoDefs:            repoDefs,
		RequiresSync:        true,
		DependencyBuildRefs: buildRefs,
		ExtraSyncPaths: append(extraSyncPaths, helmSyncPath{
			Local:  localChart,
			Remote: remoteChart,
		}),
	}, nil
}

type localHelmChartMetadata struct {
	Dependencies []localHelmChartDependency `yaml:"dependencies"`
}

type localHelmChartDependency struct {
	Name       string `yaml:"name"`
	Repository string `yaml:"repository"`
}

func helmDependencyInputs(chartPath string) ([]helmRepoDefinition, []string, error) {
	visited := map[string]bool{}
	repos := []helmRepoDefinition{}
	buildPaths := []string{}
	urlToName := map[string]string{}
	usedNames := map[string]bool{}

	var walk func(string) error
	walk = func(path string) error {
		path = filepath.Clean(path)
		if visited[path] {
			return nil
		}
		visited[path] = true

		metadata, err := loadLocalHelmChartMetadata(path)
		if err != nil {
			return err
		}

		for _, dep := range metadata.Dependencies {
			repoURL := strings.TrimSpace(dep.Repository)
			switch {
			case strings.HasPrefix(repoURL, "http://") || strings.HasPrefix(repoURL, "https://"):
				if existing, ok := urlToName[repoURL]; ok {
					repos = append(repos, helmRepoDefinition{Name: existing, URL: repoURL})
					continue
				}
				name := uniqueHelmRepoName(dep.Name, repoURL, usedNames)
				urlToName[repoURL] = name
				usedNames[name] = true
				repos = append(repos, helmRepoDefinition{Name: name, URL: repoURL})
			case strings.HasPrefix(repoURL, "file://"):
				dependencyPath := strings.TrimSpace(strings.TrimPrefix(repoURL, "file://"))
				if dependencyPath == "" {
					return fmt.Errorf("chart %s: empty file:// dependency for %q", path, dep.Name)
				}
				if !filepath.IsAbs(dependencyPath) {
					dependencyPath = filepath.Join(path, dependencyPath)
				}
				if err := walk(dependencyPath); err != nil {
					return err
				}
			}
		}

		buildPaths = append(buildPaths, path)
		return nil
	}

	if err := walk(chartPath); err != nil {
		return nil, nil, err
	}
	if len(buildPaths) > 0 {
		buildPaths = buildPaths[:len(buildPaths)-1]
	}
	return uniqueHelmRepoDefinitions(repos), buildPaths, nil
}

func loadLocalHelmChartMetadata(chartPath string) (localHelmChartMetadata, error) {
	chartYAMLPath := filepath.Join(chartPath, "Chart.yaml")
	data, err := os.ReadFile(chartYAMLPath)
	if err != nil {
		return localHelmChartMetadata{}, fmt.Errorf("read %s: %w", chartYAMLPath, err)
	}

	var metadata localHelmChartMetadata
	if err := yaml.Unmarshal(data, &metadata); err != nil {
		return localHelmChartMetadata{}, fmt.Errorf("parse %s: %w", chartYAMLPath, err)
	}
	return metadata, nil
}

func uniqueHelmRepoName(preferred, repoURL string, used map[string]bool) string {
	candidate := sanitizeRemotePathComponent(preferred)
	if candidate == "" || candidate == "chart" {
		candidate = sanitizeRemotePathComponent(repoURL)
	}
	if candidate == "" || candidate == "chart" {
		candidate = "repo"
	}
	if !used[candidate] {
		return candidate
	}
	sum := fmt.Sprintf("%x", sha1.Sum([]byte(repoURL)))
	for idx := 0; ; idx++ {
		suffix := sum[:6]
		if idx > 0 {
			suffix = fmt.Sprintf("%s-%d", suffix, idx)
		}
		name := fmt.Sprintf("%s-%s", candidate, suffix)
		if !used[name] {
			return name
		}
	}
}

func uniqueHelmRepoDefinitions(values []helmRepoDefinition) []helmRepoDefinition {
	seen := map[string]bool{}
	result := make([]helmRepoDefinition, 0, len(values))
	for _, value := range values {
		key := value.Name + "\x00" + value.URL
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}

func pathWithinRoot(root, candidate string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func externalHelmChartPath(localChart string) string {
	sum := fmt.Sprintf("%x", sha1.Sum([]byte(filepath.Clean(localChart))))
	name := sanitizeRemotePathComponent(filepath.Base(localChart))
	return remoteAppPath + "/.pullpreview/charts/" + sum[:12] + "/" + name
}

func sanitizeRemotePathComponent(value string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(value) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	name := strings.Trim(b.String(), "-.")
	if name == "" {
		return "chart"
	}
	return name
}

func (i *Instance) helmValueArgs(appPath string) ([]string, bool, error) {
	if len(i.ChartValues) == 0 && len(i.ChartSet) == 0 {
		return nil, false, nil
	}

	absAppPath, err := filepath.Abs(appPath)
	if err != nil {
		return nil, false, err
	}

	args := []string{}
	requiresSync := false
	for _, raw := range i.ChartValues {
		valuePath := strings.TrimSpace(raw)
		if valuePath == "" {
			continue
		}
		if !filepath.IsAbs(valuePath) {
			valuePath = filepath.Join(absAppPath, valuePath)
		}
		valuePath = filepath.Clean(valuePath)
		if _, err := os.Stat(valuePath); err != nil {
			return nil, false, fmt.Errorf("unable to access chart values file %s: %w", raw, err)
		}
		remotePath, err := remoteBindSource(valuePath, absAppPath, remoteAppPath)
		if err != nil {
			return nil, false, fmt.Errorf("chart values %s: %w", raw, err)
		}
		args = append(args, "--values", remotePath)
		requiresSync = true
	}
	for _, raw := range i.ChartSet {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		args = append(args, "--set", i.expandDeploymentValue(value))
	}
	return args, requiresSync, nil
}

func (i *Instance) syncRemoteAppTree(appPath string) error {
	absAppPath, err := filepath.Abs(appPath)
	if err != nil {
		return err
	}
	info, err := os.Stat(absAppPath)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("app_path %s must be a directory for deployment_target=helm", appPath)
	}
	if i.Logger != nil {
		i.Logger.Infof("Syncing app directory to remote host local=%s remote=%s", absAppPath, remoteAppPath)
	}
	if err := i.ensureRemoteAppRoot(); err != nil {
		return err
	}

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

	cmd := exec.CommandContext(i.Context, "rsync",
		"-az",
		"--delete",
		"--links",
		"--omit-dir-times",
		"--no-perms",
		"--no-owner",
		"--no-group",
		"--exclude=.git/",
		"-e", strings.Join(sshArgs, " "),
		ensureTrailingSlash(absAppPath),
		fmt.Sprintf("%s:%s/", i.SSHAddress(), remoteAppPath),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := i.Runner.Run(cmd); err != nil {
		return fmt.Errorf("rsync %s -> %s failed: %w", absAppPath, remoteAppPath, err)
	}
	return nil
}

func (i *Instance) ensureRemoteAppRoot() error {
	user := i.Username()
	command := fmt.Sprintf(
		"sudo mkdir -p %s && sudo chown %s:%s %s",
		shellQuote(remoteAppPath),
		user,
		user,
		shellQuote(remoteAppPath),
	)
	return i.SSH(command, nil)
}

func (i *Instance) runHelmDeployment(source helmChartSource, valueArgs []string) error {
	namespace := i.HelmNamespace()
	target, err := parseProxyTLSTarget(i.expandDeploymentValue(i.ProxyTLS))
	if err != nil {
		return err
	}
	upstreamHost := target.Service
	if !strings.Contains(upstreamHost, ".") {
		upstreamHost = fmt.Sprintf("%s.%s.svc.cluster.local", upstreamHost, namespace)
	}

	lines := []string{
		"set -euo pipefail",
		"set -a",
		fmt.Sprintf("source %s", remoteEnvPath),
		"set +a",
		"export KUBECONFIG=/etc/rancher/k3s/k3s.yaml",
		fmt.Sprintf("kubectl create namespace %s --dry-run=client -o yaml | kubectl apply -f - >/dev/null", shellQuote(namespace)),
	}
	manifest := i.renderHelmCaddyManifest(namespace, upstreamHost, target.Port)
	lines = append(lines,
		"cat <<'EOF' >/tmp/pullpreview-caddy.yaml",
		manifest,
		"EOF",
		"kubectl apply -f /tmp/pullpreview-caddy.yaml >/dev/null",
		fmt.Sprintf("kubectl rollout status deployment/pullpreview-caddy -n %s --timeout=10m", shellQuote(namespace)),
	)
	repoDefs := source.RepoDefs
	if len(repoDefs) == 0 && strings.TrimSpace(source.RepoURL) != "" {
		repoDefs = []helmRepoDefinition{{Name: "pullpreview", URL: source.RepoURL}}
	}
	if len(repoDefs) > 0 {
		for _, repo := range repoDefs {
			lines = append(lines, fmt.Sprintf("helm repo add %s %s --force-update >/dev/null", shellQuote(repo.Name), shellQuote(repo.URL)))
		}
		lines = append(lines, "helm repo update >/dev/null")
	}
	if source.LocalChart != "" {
		for _, ref := range source.DependencyBuildRefs {
			lines = append(lines, fmt.Sprintf("helm dependency build %s >/dev/null", shellQuote(ref)))
		}
		lines = append(lines, fmt.Sprintf("helm dependency build %s >/dev/null", shellQuote(source.ChartRef)))
	}

	helmArgs := []string{
		"helm", "upgrade", "--install", helmReleaseName, source.ChartRef,
		"--namespace", namespace,
		"--create-namespace",
		"--wait",
		"--atomic",
		"--timeout", helmDeployTimeout,
	}
	helmArgs = append(helmArgs, valueArgs...)
	lines = append(lines, shellJoin(helmArgs...))

	if i.Logger != nil {
		i.Logger.Infof("Deploying Helm release=%s namespace=%s chart=%s", helmReleaseName, namespace, source.ChartRef)
	}
	return i.SSH("bash -se", bytes.NewBufferString(strings.Join(lines, "\n")+"\n"))
}

func shellJoin(args ...string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func (i *Instance) helmProxyTLSPublicHosts() []string {
	hosts := []string{i.PublicDNS()}
	for _, raw := range i.ProxyTLSHosts {
		hosts = append(hosts, i.expandDeploymentValue(raw))
	}
	return uniqueStrings(hosts)
}

func (i *Instance) renderHelmCaddyManifest(namespace, upstreamHost string, upstreamPort int) string {
	var caddySites strings.Builder
	for _, host := range i.helmProxyTLSPublicHosts() {
		caddySites.WriteString(fmt.Sprintf(`    %s {
      reverse_proxy %s:%d {
        header_up Host {host}
        header_up X-Forwarded-Host {host}
        header_up X-Forwarded-Proto https
        header_up X-Forwarded-Port 443
      }
    }
`, host, upstreamHost, upstreamPort))
	}

	return fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: pullpreview-caddy-config
  namespace: %s
data:
  Caddyfile: |
%s
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: pullpreview-caddy
  namespace: %s
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: pullpreview-caddy
  template:
    metadata:
      labels:
        app: pullpreview-caddy
    spec:
      hostNetwork: true
      dnsPolicy: ClusterFirstWithHostNet
      containers:
        - name: caddy
          image: caddy:2-alpine
          command:
            - caddy
          args:
            - run
            - --config
            - /etc/caddy/Caddyfile
            - --adapter
            - caddyfile
          ports:
            - containerPort: 80
              hostPort: 80
              name: http
            - containerPort: 443
              hostPort: 443
              name: https
          volumeMounts:
            - name: config
              mountPath: /etc/caddy/Caddyfile
              subPath: Caddyfile
            - name: data
              mountPath: /data
            - name: runtime
              mountPath: /config
      volumes:
        - name: config
          configMap:
            name: pullpreview-caddy-config
        - name: data
          hostPath:
            path: /var/lib/pullpreview/caddy-data
            type: DirectoryOrCreate
        - name: runtime
          hostPath:
            path: /var/lib/pullpreview/caddy-config
            type: DirectoryOrCreate
`, namespace, caddySites.String(), namespace)
}

func (i *Instance) emitHelmFailureReport() {
	namespace := i.HelmNamespace()
	script := strings.Join([]string{
		"set +e",
		"export KUBECONFIG=/etc/rancher/k3s/k3s.yaml",
		fmt.Sprintf("echo '---- helm status (%s/%s) ----'", namespace, helmReleaseName),
		fmt.Sprintf("helm status %s -n %s", shellQuote(helmReleaseName), shellQuote(namespace)),
		fmt.Sprintf("echo '---- kubectl get pods,svc,events (%s) ----'", namespace),
		fmt.Sprintf("kubectl get pods,svc -n %s -o wide", shellQuote(namespace)),
		fmt.Sprintf("kubectl get events -n %s --sort-by=.lastTimestamp | tail -n 50", shellQuote(namespace)),
		fmt.Sprintf("echo '---- failing workload describe (%s) ----'", namespace),
		fmt.Sprintf("for pod in $(kubectl get pods -n %s --no-headers 2>/dev/null | awk '$2 !~ /^([0-9]+)\\/\\1$/ {print $1}'); do kubectl describe pod -n %s \"$pod\"; kubectl logs -n %s \"$pod\" --all-containers --tail=200; done", shellQuote(namespace), shellQuote(namespace), shellQuote(namespace)),
	}, "\n") + "\n"
	output, err := i.SSHOutput("bash -se", bytes.NewBufferString(script))
	if strings.TrimSpace(output) != "" {
		if len(output) > helmFailureReportOutputSize {
			output = output[len(output)-helmFailureReportOutputSize:]
		}
		fmt.Fprintln(os.Stderr, output)
	}
	if err != nil && i.Logger != nil {
		i.Logger.Warnf("Unable to capture Helm diagnostics: %v", err)
	}
}
