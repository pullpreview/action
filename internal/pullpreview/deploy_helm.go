package pullpreview

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	helmReleaseName             = "app"
	helmFailureReportOutputSize = 12000
)

type helmChartSource struct {
	ChartRef     string
	LocalChart   string
	RepoURL      string
	RequiresSync bool
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

	if chartSource.RequiresSync || syncForValues {
		if err := i.syncRemoteAppTree(appPath); err != nil {
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
	remoteChart, err := remoteBindSource(localChart, absAppPath, remoteAppPath)
	if err != nil {
		return helmChartSource{}, fmt.Errorf("chart %s: %w", chart, err)
	}
	return helmChartSource{
		ChartRef:     remoteChart,
		LocalChart:   localChart,
		RequiresSync: true,
	}, nil
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
	if source.RepoURL != "" {
		lines = append(lines,
			fmt.Sprintf("helm repo add pullpreview %s --force-update >/dev/null", shellQuote(source.RepoURL)),
			"helm repo update pullpreview >/dev/null",
		)
	}
	if source.LocalChart != "" {
		lines = append(lines, fmt.Sprintf("helm dependency build %s >/dev/null", shellQuote(source.ChartRef)))
	}

	helmArgs := []string{
		"helm", "upgrade", "--install", helmReleaseName, source.ChartRef,
		"--namespace", namespace,
		"--create-namespace",
		"--wait",
		"--atomic",
	}
	helmArgs = append(helmArgs, valueArgs...)
	lines = append(lines, shellJoin(helmArgs...))

	manifest := i.renderHelmCaddyManifest(namespace, upstreamHost, target.Port)
	lines = append(lines,
		"cat <<'EOF' >/tmp/pullpreview-caddy.yaml",
		manifest,
		"EOF",
		"kubectl apply -f /tmp/pullpreview-caddy.yaml >/dev/null",
		fmt.Sprintf("kubectl rollout status deployment/pullpreview-caddy -n %s --timeout=10m", shellQuote(namespace)),
	)

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

func (i *Instance) renderHelmCaddyManifest(namespace, upstreamHost string, upstreamPort int) string {
	return fmt.Sprintf(`apiVersion: v1
kind: ConfigMap
metadata:
  name: pullpreview-caddy-config
  namespace: %s
data:
  Caddyfile: |
    %s {
      reverse_proxy %s:%d
    }
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
`, namespace, i.PublicDNS(), upstreamHost, upstreamPort, namespace)
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
