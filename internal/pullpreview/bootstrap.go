package pullpreview

import (
	"fmt"
	"strings"
)

type BootstrapOptions struct {
	AppPath          string
	Username         string
	SSHPublicKeys    []string
	DeploymentTarget DeploymentTarget
	ImageName        string
	HostTuning       []string
	TrustedUserCAKey string
	PropagateRootSSH bool
}

type UserData struct {
	AppPath       string
	SSHPublicKeys []string
	Username      string
}

func (u UserData) Script() string {
	script, _ := BuildBootstrapScript(BootstrapOptions{
		AppPath:          u.AppPath,
		Username:         u.Username,
		SSHPublicKeys:    u.SSHPublicKeys,
		DeploymentTarget: DeploymentTargetCompose,
		PropagateRootSSH: true,
	})
	return script
}

func BuildBootstrapScript(opts BootstrapOptions) (string, error) {
	target := NormalizeDeploymentTarget(string(opts.DeploymentTarget))
	if target == "" {
		target = DeploymentTargetCompose
	}
	if err := target.Validate(); err != nil {
		return "", err
	}

	username := strings.TrimSpace(opts.Username)
	if username == "" {
		username = "root"
	}
	appPath := strings.TrimSpace(opts.AppPath)
	if appPath == "" {
		appPath = remoteAppPath
	}
	homeDir := HomeDirForUser(username)

	lines := []string{
		"#!/usr/bin/env bash",
		"set -xe ; set -o pipefail",
		fmt.Sprintf("mkdir -p %s/.ssh", homeDir),
	}
	if opts.PropagateRootSSH && username != "root" {
		lines = append(lines,
			"if [ -f /root/.ssh/authorized_keys ]; then",
			fmt.Sprintf("  cp /root/.ssh/authorized_keys %s/.ssh/authorized_keys", homeDir),
			"fi",
		)
	}
	if len(opts.SSHPublicKeys) > 0 {
		lines = append(lines, fmt.Sprintf("echo '%s' >> %s/.ssh/authorized_keys", strings.Join(opts.SSHPublicKeys, "\n"), homeDir))
	}
	if username != "root" || len(opts.SSHPublicKeys) > 0 {
		lines = append(lines,
			fmt.Sprintf("chown -R %s:%s %s/.ssh", username, username, homeDir),
			fmt.Sprintf("chmod 0700 %s/.ssh && chmod 0600 %s/.ssh/authorized_keys", homeDir, homeDir),
		)
	}
	lines = append(lines,
		fmt.Sprintf("mkdir -p %s && chown -R %s:%s %s", appPath, username, username, appPath),
		"mkdir -p /etc/profile.d",
		fmt.Sprintf("echo 'cd %s' > /etc/profile.d/pullpreview.sh", appPath),
	)
	lines = append(lines, opts.HostTuning...)
	lines = append(lines, sharedBootstrapPackagePrep(strings.TrimSpace(opts.ImageName))...)
	switch target {
	case DeploymentTargetHelm:
		lines = append(lines, sharedHelmRuntime(homeDir, username)...)
	default:
		lines = append(lines, sharedComposeRuntime(username)...)
	}
	if strings.TrimSpace(opts.TrustedUserCAKey) != "" {
		lines = append(lines,
			"mkdir -p /etc/ssh/sshd_config.d",
			fmt.Sprintf("cat <<'EOF' > /etc/ssh/pullpreview-user-ca.pub\n%s\nEOF", strings.TrimSpace(opts.TrustedUserCAKey)),
			"cat <<'EOF' > /etc/ssh/sshd_config.d/pullpreview.conf",
			"TrustedUserCAKeys /etc/ssh/pullpreview-user-ca.pub",
			"EOF",
			"systemctl restart ssh || systemctl restart sshd || true",
		)
	}
	lines = append(lines,
		"mkdir -p /etc/pullpreview && touch /etc/pullpreview/ready",
		fmt.Sprintf("chown -R %s:%s /etc/pullpreview", username, username),
	)
	return strings.Join(lines, "\n"), nil
}

func sharedBootstrapPackagePrep(imageName string) []string {
	return []string{
		fmt.Sprintf("IMAGE_NAME=%q", imageName),
		"if command -v apt-get >/dev/null 2>&1; then",
		"  mkdir -p /etc/apt/keyrings",
		"  install -m 0755 -d /etc/apt/keyrings",
		"  apt-get update",
		"  apt-get install -y ca-certificates curl gnupg lsb-release",
		"elif command -v dnf >/dev/null 2>&1; then",
		"  dnf -y install dnf-plugins-core curl",
		"elif command -v yum >/dev/null 2>&1; then",
		"  yum -y install yum-utils curl",
		"else",
		"  echo \"unsupported OS family; expected apt, dnf, or yum\"",
		"  exit 1",
		"fi",
	}
}

func sharedComposeRuntime(username string) []string {
	lines := []string{
		"if command -v apt-get >/dev/null 2>&1; then",
		"  if echo \"$IMAGE_NAME\" | grep -iq ubuntu; then",
		"    DISTRO=ubuntu",
		"  else",
		"    DISTRO=debian",
		"  fi",
		"  curl -fsSL https://download.docker.com/linux/$DISTRO/gpg -o /etc/apt/keyrings/docker.asc",
		"  chmod a+r /etc/apt/keyrings/docker.asc",
		"  echo \"deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/$DISTRO $(lsb_release -cs) stable\" > /etc/apt/sources.list.d/docker.list",
		"  apt-get update",
		"  apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin",
		"elif command -v dnf >/dev/null 2>&1; then",
		"  if echo \"$IMAGE_NAME\" | grep -Eiq 'amazon[- ]linux'; then",
		"    yum -y install docker",
		"  else",
		"    dnf config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo",
		"    dnf -y install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin",
		"  fi",
		"elif command -v yum >/dev/null 2>&1; then",
		"  if echo \"$IMAGE_NAME\" | grep -Eiq 'amazon[- ]linux'; then",
		"    yum -y install docker",
		"  else",
		"    yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo",
		"    yum -y install docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin",
		"  fi",
		"fi",
		"if ! docker compose version >/dev/null 2>&1; then",
		"  case \"$(uname -m)\" in",
		"    x86_64|amd64) compose_arch=x86_64 ;;",
		"    aarch64|arm64) compose_arch=aarch64 ;;",
		"    *) echo \"unsupported compose architecture $(uname -m)\"; exit 1 ;;",
		"  esac",
		"  mkdir -p /usr/local/lib/docker/cli-plugins",
		"  curl -fsSL \"https://github.com/docker/compose/releases/latest/download/docker-compose-linux-${compose_arch}\" -o /usr/local/lib/docker/cli-plugins/docker-compose",
		"  chmod +x /usr/local/lib/docker/cli-plugins/docker-compose",
		"fi",
	}
	if strings.TrimSpace(username) != "" && username != "root" {
		lines = append(lines, fmt.Sprintf("usermod -aG docker %s", username))
	}
	lines = append(lines,
		"docker compose version",
		"systemctl enable --now docker || systemctl restart docker",
		"echo 'docker system prune -f && docker image prune -a --filter=\"until=96h\" --force' > /etc/cron.daily/docker-prune && chmod a+x /etc/cron.daily/docker-prune",
	)
	return lines
}

func sharedHelmRuntime(homeDir, username string) []string {
	return []string{
		"curl -sfL https://get.k3s.io | INSTALL_K3S_EXEC='server --disable traefik --write-kubeconfig-mode 0644' sh -",
		"curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash",
		fmt.Sprintf("mkdir -p %s/.kube", homeDir),
		fmt.Sprintf("cp /etc/rancher/k3s/k3s.yaml %s/.kube/config", homeDir),
		fmt.Sprintf("chown -R %s:%s %s/.kube", username, username, homeDir),
		"export KUBECONFIG=/etc/rancher/k3s/k3s.yaml",
		"until kubectl get nodes >/dev/null 2>&1; do sleep 5; done",
		"until kubectl get nodes -o jsonpath='{range .items[*]}{range .status.conditions[?(@.type==\"Ready\")]}{.status}{\"\\n\"}{end}{end}' | grep -q True; do sleep 5; done",
	}
}
