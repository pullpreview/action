package pullpreview

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func (f fakeProvider) Name() string {
	return "hetzner"
}

func (f fakeProvider) DisplayName() string {
	return "Hetzner Cloud"
}

func (f fakeProvider) SupportsDeploymentTarget(target DeploymentTarget) bool {
	switch NormalizeDeploymentTarget(string(target)) {
	case DeploymentTargetCompose, DeploymentTargetHelm:
		return true
	default:
		return false
	}
}

type fakeLightsailProvider struct{}

func (f fakeLightsailProvider) Launch(name string, opts LaunchOptions) (AccessDetails, error) {
	return AccessDetails{}, nil
}

func (f fakeLightsailProvider) Terminate(name string) error { return nil }

func (f fakeLightsailProvider) Running(name string) (bool, error) { return false, nil }

func (f fakeLightsailProvider) ListInstances(tags map[string]string) ([]InstanceSummary, error) {
	return nil, nil
}

func (f fakeLightsailProvider) Username() string { return "ec2-user" }

func (f fakeLightsailProvider) Name() string {
	return "lightsail"
}

func (f fakeLightsailProvider) DisplayName() string {
	return "AWS Lightsail"
}

func (f fakeLightsailProvider) SupportsDeploymentTarget(target DeploymentTarget) bool {
	switch NormalizeDeploymentTarget(string(target)) {
	case DeploymentTargetCompose, DeploymentTargetHelm:
		return true
	default:
		return false
	}
}

type scriptCaptureRunner struct {
	args   [][]string
	inputs []string
}

func (r *scriptCaptureRunner) Run(cmd *exec.Cmd) error {
	r.args = append(r.args, append([]string{}, cmd.Args...))
	if cmd.Stdin != nil {
		body, err := io.ReadAll(cmd.Stdin)
		if err != nil {
			return err
		}
		r.inputs = append(r.inputs, string(body))
	} else {
		r.inputs = append(r.inputs, "")
	}
	return nil
}

func TestValidateDeploymentConfigForHelm(t *testing.T) {
	inst := NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "wordpress",
		ChartRepository:  "https://charts.bitnami.com/bitnami",
		ProxyTLS:         "{{ release_name }}-wordpress:80",
	}, fakeProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "root"}

	if err := inst.ValidateDeploymentConfig(); err != nil {
		t.Fatalf("ValidateDeploymentConfig() error: %v", err)
	}
}

func TestValidateDeploymentConfigRejectsHelmWithoutProxyTLS(t *testing.T) {
	inst := NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "wordpress",
		ChartRepository:  "https://charts.bitnami.com/bitnami",
	}, fakeProvider{}, nil)

	if err := inst.ValidateDeploymentConfig(); err == nil || !strings.Contains(err.Error(), "proxy_tls") {
		t.Fatalf("expected proxy_tls validation error, got %v", err)
	}
}

func TestValidateDeploymentConfigRejectsComposeWithHelmOptions(t *testing.T) {
	inst := NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetCompose,
		Chart:            "wordpress",
	}, fakeProvider{}, nil)

	if err := inst.ValidateDeploymentConfig(); err == nil || !strings.Contains(err.Error(), "require deployment_target=helm") {
		t.Fatalf("expected compose/helm validation error, got %v", err)
	}

	inst = NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetCompose,
		ProxyTLSHosts:    []string{"nextcloud.example.test"},
	}, fakeProvider{}, nil)

	if err := inst.ValidateDeploymentConfig(); err == nil || !strings.Contains(err.Error(), "proxy_tls_hosts") {
		t.Fatalf("expected proxy_tls_hosts validation error, got %v", err)
	}
}

func TestValidateDeploymentConfigAcceptsHelmForLightsailProvider(t *testing.T) {
	inst := NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "wordpress",
		ProxyTLS:         "app-wordpress:80",
	}, fakeLightsailProvider{}, nil)

	if err := inst.ValidateDeploymentConfig(); err != nil {
		t.Fatalf("expected lightsail helm validation to pass, got %v", err)
	}
}

func TestValidateDeploymentConfigRejectsHelmSpecificComposeOverrides(t *testing.T) {
	inst := NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "wordpress",
		ProxyTLS:         "app-wordpress:80",
		ComposeFiles:     []string{"docker-compose.preview.yml"},
	}, fakeProvider{}, nil)

	if err := inst.ValidateDeploymentConfig(); err == nil || !strings.Contains(err.Error(), "compose_files") {
		t.Fatalf("expected compose_files validation error, got %v", err)
	}

	inst = NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "wordpress",
		ProxyTLS:         "app-wordpress:80",
		ComposeOptions:   []string{"--no-build"},
	}, fakeProvider{}, nil)

	if err := inst.ValidateDeploymentConfig(); err == nil || !strings.Contains(err.Error(), "compose_options") {
		t.Fatalf("expected compose_options validation error, got %v", err)
	}
}

func TestValidateDeploymentConfigRejectsHelmRegistries(t *testing.T) {
	inst := NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "wordpress",
		ProxyTLS:         "app-wordpress:80",
		Registries:       []string{"docker://alice:secret@ghcr.io"},
	}, fakeProvider{}, nil)

	if err := inst.ValidateDeploymentConfig(); err == nil || !strings.Contains(err.Error(), "registries") {
		t.Fatalf("expected registries validation error, got %v", err)
	}
}

func TestExpandDeploymentValue(t *testing.T) {
	inst := NewInstance("Demo App", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "wordpress",
		ChartRepository:  "https://charts.bitnami.com/bitnami",
		ProxyTLS:         "{{ release_name }}-wordpress:80",
		ProxyTLSHosts: []string{
			"nextcloud.{{ pullpreview_public_dns }}",
			"keycloak.{{ pullpreview_public_dns }}",
		},
		DNS: "rev2.click",
	}, fakeProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "root"}

	got := inst.expandDeploymentValue("https://{{ pullpreview_public_dns }}/{{ namespace }}/{{ release_name }}")
	if got != "https://Demo-App-ip-1-2-3-4.rev2.click/pp-demo-app/app" {
		t.Fatalf("unexpected expanded value: %q", got)
	}
}

func TestResolveHelmChartSourceForLocalChart(t *testing.T) {
	appPath := t.TempDir()
	chartPath := filepath.Join(appPath, "charts", "demo")
	if err := os.MkdirAll(chartPath, 0755); err != nil {
		t.Fatalf("mkdir chart path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartPath, "Chart.yaml"), []byte("apiVersion: v2\nname: demo\nversion: 0.1.0\n"), 0644); err != nil {
		t.Fatalf("write chart: %v", err)
	}

	inst := NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "charts/demo",
		ProxyTLS:         "demo:80",
	}, fakeProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "root"}

	source, err := inst.resolveHelmChartSource(appPath)
	if err != nil {
		t.Fatalf("resolveHelmChartSource() error: %v", err)
	}
	if source.ChartRef != "/app/charts/demo" {
		t.Fatalf("unexpected chart ref: %q", source.ChartRef)
	}
	if !source.RequiresSync {
		t.Fatalf("expected local chart to require sync")
	}
	if !source.SyncAppTree {
		t.Fatalf("expected in-tree chart to sync app tree")
	}
}

func TestResolveHelmChartSourceCollectsRemoteDependencyRepos(t *testing.T) {
	appPath := t.TempDir()
	chartPath := filepath.Join(appPath, "charts", "demo")
	nestedPath := filepath.Join(appPath, "charts", "local-subchart")
	if err := os.MkdirAll(chartPath, 0755); err != nil {
		t.Fatalf("mkdir chart path: %v", err)
	}
	if err := os.MkdirAll(nestedPath, 0755); err != nil {
		t.Fatalf("mkdir nested chart path: %v", err)
	}
	chartYAML := `apiVersion: v2
name: demo
version: 0.1.0
dependencies:
  - name: local-subchart
    repository: file://../local-subchart
    version: 0.1.0
  - name: nextcloud
    repository: https://nextcloud.github.io/helm
    version: 7.0.0
  - name: keycloak
    repository: https://charts.bitnami.com/bitnami
    version: 24.7.5
`
	if err := os.WriteFile(filepath.Join(chartPath, "Chart.yaml"), []byte(chartYAML), 0644); err != nil {
		t.Fatalf("write chart: %v", err)
	}
	nestedChartYAML := `apiVersion: v2
name: local-subchart
version: 0.1.0
dependencies:
  - name: traefik
    repository: https://traefik.github.io/charts
    version: 39.0.5
`
	if err := os.WriteFile(filepath.Join(nestedPath, "Chart.yaml"), []byte(nestedChartYAML), 0644); err != nil {
		t.Fatalf("write nested chart: %v", err)
	}

	inst := NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "charts/demo",
		ProxyTLS:         "demo:80",
	}, fakeProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "root"}

	source, err := inst.resolveHelmChartSource(appPath)
	if err != nil {
		t.Fatalf("resolveHelmChartSource() error: %v", err)
	}
	if len(source.RepoDefs) != 3 {
		t.Fatalf("expected three remote repo defs, got %#v", source.RepoDefs)
	}
	gotRepos := map[string]string{}
	for _, repo := range source.RepoDefs {
		gotRepos[repo.Name] = repo.URL
	}
	wantRepos := map[string]string{
		"nextcloud": "https://nextcloud.github.io/helm",
		"keycloak":  "https://charts.bitnami.com/bitnami",
		"traefik":   "https://traefik.github.io/charts",
	}
	if len(gotRepos) != len(wantRepos) {
		t.Fatalf("unexpected repo defs: %#v", source.RepoDefs)
	}
	for name, url := range wantRepos {
		if gotRepos[name] != url {
			t.Fatalf("expected repo %q=%q, got %#v", name, url, source.RepoDefs)
		}
	}
	if len(source.DependencyBuildRefs) != 1 || source.DependencyBuildRefs[0] != "/app/charts/local-subchart" {
		t.Fatalf("unexpected dependency build refs: %#v", source.DependencyBuildRefs)
	}
}

func TestResolveHelmChartSourceForLocalChartOutsideAppPath(t *testing.T) {
	root := t.TempDir()
	appPath := filepath.Join(root, "app")
	chartPath := filepath.Join(root, "chart")
	if err := os.MkdirAll(appPath, 0755); err != nil {
		t.Fatalf("mkdir app path: %v", err)
	}
	if err := os.MkdirAll(chartPath, 0755); err != nil {
		t.Fatalf("mkdir chart path: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartPath, "Chart.yaml"), []byte("apiVersion: v2\nname: demo\nversion: 0.1.0\n"), 0644); err != nil {
		t.Fatalf("write chart: %v", err)
	}

	inst := NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "../chart",
		ProxyTLS:         "demo:80",
	}, fakeProvider{}, nil)

	source, err := inst.resolveHelmChartSource(appPath)
	if err != nil {
		t.Fatalf("resolveHelmChartSource() error: %v", err)
	}
	if source.ChartRef == "" || !strings.HasPrefix(source.ChartRef, "/app/.pullpreview/charts/") {
		t.Fatalf("unexpected external chart ref: %q", source.ChartRef)
	}
	if !source.RequiresSync {
		t.Fatalf("expected external chart to require sync")
	}
	if source.SyncAppTree {
		t.Fatalf("did not expect external chart to require full app tree sync")
	}
}

func TestResolveHelmChartSourceForRepositoryChart(t *testing.T) {
	inst := NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "wordpress",
		ChartRepository:  "https://charts.bitnami.com/bitnami",
		ProxyTLS:         "app-wordpress:80",
	}, fakeProvider{}, nil)

	source, err := inst.resolveHelmChartSource(t.TempDir())
	if err != nil {
		t.Fatalf("resolveHelmChartSource() error: %v", err)
	}
	if source.ChartRef != "pullpreview/wordpress" {
		t.Fatalf("unexpected chart ref: %q", source.ChartRef)
	}
	if source.RepoURL != "https://charts.bitnami.com/bitnami" {
		t.Fatalf("unexpected repo url: %q", source.RepoURL)
	}
	if source.RequiresSync {
		t.Fatalf("expected repo chart to avoid sync")
	}
}

func TestResolveHelmChartSourceForOCIChart(t *testing.T) {
	inst := NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "oci://registry-1.docker.io/bitnamicharts/wordpress",
		ProxyTLS:         "app-wordpress:80",
	}, fakeProvider{}, nil)

	source, err := inst.resolveHelmChartSource(t.TempDir())
	if err != nil {
		t.Fatalf("resolveHelmChartSource() error: %v", err)
	}
	if source.ChartRef != "oci://registry-1.docker.io/bitnamicharts/wordpress" {
		t.Fatalf("unexpected chart ref: %q", source.ChartRef)
	}
	if source.RepoURL != "" || source.RequiresSync {
		t.Fatalf("unexpected OCI chart source: %#v", source)
	}
}

func TestHelmValueArgsExpandsPlaceholdersAndSyncsValuesFiles(t *testing.T) {
	appPath := t.TempDir()
	for _, path := range []string{
		filepath.Join(appPath, "values.yaml"),
		filepath.Join(appPath, "overrides", "preview.yaml"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir values dir: %v", err)
		}
		if err := os.WriteFile(path, []byte("key: value\n"), 0644); err != nil {
			t.Fatalf("write values file: %v", err)
		}
	}

	inst := NewInstance("Demo App", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "wordpress",
		ChartRepository:  "https://charts.bitnami.com/bitnami",
		ChartValues:      []string{"values.yaml", "overrides/preview.yaml"},
		ChartSet: []string{
			"service.type=ClusterIP",
			"ingress.hostname={{ pullpreview_public_dns }}",
			"url={{ pullpreview_url }}",
		},
		ProxyTLS: "{{ release_name }}-wordpress:80",
		DNS:      "rev2.click",
	}, fakeProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "root"}

	args, requiresSync, err := inst.helmValueArgs(appPath)
	if err != nil {
		t.Fatalf("helmValueArgs() error: %v", err)
	}
	want := []string{
		"--values", "/app/values.yaml",
		"--values", "/app/overrides/preview.yaml",
		"--set", "service.type=ClusterIP",
		"--set", "ingress.hostname=Demo-App-ip-1-2-3-4.rev2.click",
		"--set", "url=https://Demo-App-ip-1-2-3-4.rev2.click:443",
	}
	if len(args) != len(want) {
		t.Fatalf("unexpected helm args length: got=%#v want=%#v", args, want)
	}
	for idx := range want {
		if args[idx] != want[idx] {
			t.Fatalf("unexpected helm arg %d: got=%q want=%q all=%#v", idx, args[idx], want[idx], args)
		}
	}
	if !requiresSync {
		t.Fatalf("expected local values files to require sync")
	}
}

func TestRunHelmDeploymentBuildsExpectedScriptForRepoChart(t *testing.T) {
	inst := NewInstance("Demo App", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "wordpress",
		ChartRepository:  "https://charts.bitnami.com/bitnami",
		ProxyTLS:         "{{ release_name }}-wordpress:80",
		ProxyTLSHosts: []string{
			"nextcloud.{{ pullpreview_public_dns }}",
			"keycloak.{{ pullpreview_public_dns }}",
		},
		DNS: "rev2.click",
	}, fakeProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "root", PrivateKey: "PRIVATE", CertKey: "CERT"}
	runner := &scriptCaptureRunner{}
	inst.Runner = runner

	err := inst.runHelmDeployment(helmChartSource{
		ChartRef: "pullpreview/wordpress",
		RepoURL:  "https://charts.bitnami.com/bitnami",
	}, []string{
		"--values", "/app/values.yaml",
		"--set", "service.type=ClusterIP",
		"--set", "ingress.hostname=Demo-App-ip-1-2-3-4.rev2.click",
	})
	if err != nil {
		t.Fatalf("runHelmDeployment() error: %v", err)
	}
	if len(runner.args) != 1 || len(runner.inputs) != 1 {
		t.Fatalf("expected one ssh invocation, got args=%d inputs=%d", len(runner.args), len(runner.inputs))
	}

	sshArgs := strings.Join(runner.args[0], " ")
	if !strings.Contains(sshArgs, "CertificateFile=") || !strings.Contains(sshArgs, "root@1.2.3.4") || !strings.Contains(sshArgs, "bash -se") {
		t.Fatalf("unexpected ssh args: %s", sshArgs)
	}

	script := runner.inputs[0]
	checks := []string{
		"source /etc/pullpreview/env",
		"export KUBECONFIG=/etc/rancher/k3s/k3s.yaml",
		"kubectl create namespace 'pp-demo-app' --dry-run=client -o yaml | kubectl apply -f - >/dev/null",
		"helm repo add 'pullpreview' 'https://charts.bitnami.com/bitnami' --force-update >/dev/null",
		"helm repo update >/dev/null",
		"'helm' 'upgrade' '--install' 'app' 'pullpreview/wordpress' '--namespace' 'pp-demo-app' '--create-namespace' '--wait' '--atomic' '--timeout' '15m' '--values' '/app/values.yaml' '--set' 'service.type=ClusterIP' '--set' 'ingress.hostname=Demo-App-ip-1-2-3-4.rev2.click'",
		"cat <<'EOF' >/tmp/pullpreview-caddy.yaml",
		"Demo-App-ip-1-2-3-4.rev2.click {",
		"nextcloud.Demo-App-ip-1-2-3-4.rev2.click {",
		"keycloak.Demo-App-ip-1-2-3-4.rev2.click {",
		"reverse_proxy app-wordpress.pp-demo-app.svc.cluster.local:80",
		"kubectl rollout status deployment/pullpreview-caddy -n 'pp-demo-app' --timeout=10m",
	}
	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Fatalf("expected script to contain %q, script:\n%s", check, script)
		}
	}
}

func TestRunHelmDeploymentBuildsDependencyStepForLocalChart(t *testing.T) {
	inst := NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "charts/demo",
		ProxyTLS:         "app-wordpress:80",
	}, fakeProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "root", PrivateKey: "PRIVATE"}
	runner := &scriptCaptureRunner{}
	inst.Runner = runner

	err := inst.runHelmDeployment(helmChartSource{
		ChartRef:   "/app/charts/demo",
		LocalChart: "/tmp/demo",
	}, nil)
	if err != nil {
		t.Fatalf("runHelmDeployment() error: %v", err)
	}
	if len(runner.inputs) != 1 {
		t.Fatalf("expected one ssh script, got %d", len(runner.inputs))
	}
	script := runner.inputs[0]
	if !strings.Contains(script, "helm dependency build '/app/charts/demo' >/dev/null") {
		t.Fatalf("expected helm dependency build for local chart, script:\n%s", script)
	}
	if strings.Contains(script, "helm repo add pullpreview") {
		t.Fatalf("did not expect repo add for local chart, script:\n%s", script)
	}
}

func TestRunHelmDeploymentAddsRemoteReposForLocalChartDependencies(t *testing.T) {
	inst := NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "charts/demo",
		ProxyTLS:         "app-wordpress:80",
	}, fakeProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "root", PrivateKey: "PRIVATE"}
	runner := &scriptCaptureRunner{}
	inst.Runner = runner

	err := inst.runHelmDeployment(helmChartSource{
		ChartRef:            "/app/charts/demo",
		LocalChart:          "/tmp/demo",
		DependencyBuildRefs: []string{"/app/charts/local-subchart"},
		RepoDefs: []helmRepoDefinition{
			{Name: "nextcloud", URL: "https://nextcloud.github.io/helm"},
			{Name: "bitnami", URL: "https://charts.bitnami.com/bitnami"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("runHelmDeployment() error: %v", err)
	}
	script := runner.inputs[0]
	checks := []string{
		"helm repo add 'nextcloud' 'https://nextcloud.github.io/helm' --force-update >/dev/null",
		"helm repo add 'bitnami' 'https://charts.bitnami.com/bitnami' --force-update >/dev/null",
		"helm repo update >/dev/null",
		"helm dependency build '/app/charts/local-subchart' >/dev/null",
		"helm dependency build '/app/charts/demo' >/dev/null",
	}
	for _, check := range checks {
		if !strings.Contains(script, check) {
			t.Fatalf("expected script to contain %q, script:\n%s", check, script)
		}
	}
}

func TestDeployWithHelmSyncsExternalLocalDependencies(t *testing.T) {
	appPath := t.TempDir()
	chartPath := filepath.Join(appPath, "charts", "demo")
	externalDepRoot := filepath.Join(t.TempDir(), "external")
	externalDepPath := filepath.Join(externalDepRoot, "shared-chart")
	if err := os.MkdirAll(chartPath, 0755); err != nil {
		t.Fatalf("mkdir chart path: %v", err)
	}
	if err := os.MkdirAll(externalDepPath, 0755); err != nil {
		t.Fatalf("mkdir external dependency path: %v", err)
	}
	chartYAML := `apiVersion: v2
name: demo
version: 0.1.0
dependencies:
  - name: shared-chart
    repository: file://` + externalDepPath + `
    version: 0.1.0
`
	if err := os.WriteFile(filepath.Join(chartPath, "Chart.yaml"), []byte(chartYAML), 0644); err != nil {
		t.Fatalf("write chart: %v", err)
	}
	if err := os.WriteFile(filepath.Join(externalDepPath, "Chart.yaml"), []byte("apiVersion: v2\nname: shared-chart\nversion: 0.1.0\n"), 0644); err != nil {
		t.Fatalf("write external chart: %v", err)
	}

	inst := NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "charts/demo",
		ProxyTLS:         "demo:80",
	}, fakeProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "root", PrivateKey: "PRIVATE"}
	runner := &scriptCaptureRunner{}
	inst.Runner = runner

	if err := inst.DeployWithHelm(appPath); err != nil {
		t.Fatalf("DeployWithHelm() error: %v", err)
	}
	foundAppSync := false
	foundExternalSync := false
	foundHelmBuild := false
	for idx, args := range runner.args {
		joined := strings.Join(args, " ")
		if len(args) > 0 && args[0] == "rsync" && strings.Contains(joined, remoteAppPath+"/") {
			foundAppSync = true
		}
		if len(args) > 0 && args[0] == "rsync" && strings.Contains(joined, externalHelmChartPath(externalDepPath)) {
			foundExternalSync = true
		}
		if idx < len(runner.inputs) && strings.Contains(runner.inputs[idx], "helm dependency build '"+externalHelmChartPath(externalDepPath)+"' >/dev/null") {
			foundHelmBuild = true
		}
	}
	if !foundAppSync {
		t.Fatalf("expected app tree sync, commands: %#v", runner.args)
	}
	if !foundExternalSync {
		t.Fatalf("expected external dependency sync, commands: %#v", runner.args)
	}
	if !foundHelmBuild {
		t.Fatalf("expected helm dependency build for external dependency, scripts: %#v", runner.inputs)
	}
}

func TestRenderHelmCaddyManifest(t *testing.T) {
	inst := NewInstance("demo", CommonOptions{
		DeploymentTarget: DeploymentTargetHelm,
		Chart:            "wordpress",
		ChartRepository:  "https://charts.bitnami.com/bitnami",
		ProxyTLS:         "app-wordpress:80",
		ProxyTLSHosts:    []string{"nextcloud.{{ pullpreview_public_dns }}", "keycloak.{{ pullpreview_public_dns }}"},
		DNS:              "rev2.click",
	}, fakeProvider{}, nil)
	inst.Access = AccessDetails{IPAddress: "1.2.3.4", Username: "root"}

	manifest := inst.renderHelmCaddyManifest(inst.HelmNamespace(), "app-wordpress.pp-demo.svc.cluster.local", 80)
	if !strings.Contains(manifest, "name: pullpreview-caddy") {
		t.Fatalf("expected caddy deployment in manifest: %s", manifest)
	}
	if !strings.Contains(manifest, "command:\n            - caddy") {
		t.Fatalf("expected caddy command in manifest: %s", manifest)
	}
	if !strings.Contains(manifest, "demo-ip-1-2-3-4.rev2.click") {
		t.Fatalf("expected public DNS in manifest: %s", manifest)
	}
	if !strings.Contains(manifest, "nextcloud.demo-ip-1-2-3-4.rev2.click") {
		t.Fatalf("expected nextcloud host in manifest: %s", manifest)
	}
	if !strings.Contains(manifest, "keycloak.demo-ip-1-2-3-4.rev2.click") {
		t.Fatalf("expected keycloak host in manifest: %s", manifest)
	}
	if !strings.Contains(manifest, "reverse_proxy app-wordpress.pp-demo.svc.cluster.local:80") {
		t.Fatalf("expected reverse proxy upstream in manifest: %s", manifest)
	}
	for _, header := range []string{
		"header_up Host {host}",
		"header_up X-Forwarded-Host {host}",
		"header_up X-Forwarded-Proto https",
		"header_up X-Forwarded-Port 443",
	} {
		if !strings.Contains(manifest, header) {
			t.Fatalf("expected proxy header %q in manifest: %s", header, manifest)
		}
	}
}
