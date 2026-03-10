package pullpreview

import (
	"strings"
	"testing"
)

func TestBuildBootstrapScriptComposeIncludesSharedRuntime(t *testing.T) {
	script, err := BuildBootstrapScript(BootstrapOptions{
		AppPath:          "/app",
		Username:         "ec2-user",
		SSHPublicKeys:    []string{"ssh-ed25519 AAA", "ssh-rsa BBB"},
		DeploymentTarget: DeploymentTargetCompose,
		ImageName:        "ubuntu-24.04",
		PropagateRootSSH: true,
	})
	if err != nil {
		t.Fatalf("BuildBootstrapScript() error: %v", err)
	}

	checks := []string{
		"#!/usr/bin/env bash",
		"cp /root/.ssh/authorized_keys /home/ec2-user/.ssh/authorized_keys",
		"echo 'ssh-ed25519 AAA\nssh-rsa BBB' >> /home/ec2-user/.ssh/authorized_keys",
		"mkdir -p /app && chown -R ec2-user:ec2-user /app",
		"IMAGE_NAME=\"ubuntu-24.04\"",
		"if command -v apt-get >/dev/null 2>&1; then",
		"elif command -v dnf >/dev/null 2>&1; then",
		"elif command -v yum >/dev/null 2>&1; then",
		"docker-compose-plugin",
		"usermod -aG docker ec2-user",
		"docker system prune -f",
	}
	for _, fragment := range checks {
		if !strings.Contains(script, fragment) {
			t.Fatalf("expected compose bootstrap to contain %q, script:\n%s", fragment, script)
		}
	}
	if strings.Contains(script, "INSTALL_K3S_EXEC='server --disable traefik") {
		t.Fatalf("did not expect k3s install in compose bootstrap:\n%s", script)
	}
}

func TestBuildBootstrapScriptComposeOnAmazonLinuxUsesNativeDockerPackage(t *testing.T) {
	script, err := BuildBootstrapScript(BootstrapOptions{
		AppPath:          "/app",
		Username:         "ec2-user",
		DeploymentTarget: DeploymentTargetCompose,
		ImageName:        "amazon-linux-2023",
		PropagateRootSSH: true,
	})
	if err != nil {
		t.Fatalf("BuildBootstrapScript() error: %v", err)
	}

	checks := []string{
		"dnf -y install dnf-plugins-core curl",
		"if echo \"$IMAGE_NAME\" | grep -Eiq 'amazon[- ]linux'; then",
		"yum -y install docker",
		"https://github.com/docker/compose/releases/latest/download/docker-compose-linux-${compose_arch}",
		"docker compose version",
	}
	for _, fragment := range checks {
		if !strings.Contains(script, fragment) {
			t.Fatalf("expected amazon linux compose bootstrap to contain %q, script:\n%s", fragment, script)
		}
	}
}

func TestBuildBootstrapScriptHelmIncludesReadableKubeconfig(t *testing.T) {
	script, err := BuildBootstrapScript(BootstrapOptions{
		AppPath:          "/app",
		Username:         "ec2-user",
		DeploymentTarget: DeploymentTargetHelm,
		ImageName:        "amazon-linux-2023",
		PropagateRootSSH: true,
	})
	if err != nil {
		t.Fatalf("BuildBootstrapScript() error: %v", err)
	}

	checks := []string{
		"--write-kubeconfig-mode 0644",
		"get-helm-3",
		"mkdir -p /home/ec2-user/.kube",
		"cp /etc/rancher/k3s/k3s.yaml /home/ec2-user/.kube/config",
		"chown -R ec2-user:ec2-user /home/ec2-user/.kube",
	}
	for _, fragment := range checks {
		if !strings.Contains(script, fragment) {
			t.Fatalf("expected helm bootstrap to contain %q, script:\n%s", fragment, script)
		}
	}
	if strings.Contains(script, "docker-compose-plugin") {
		t.Fatalf("did not expect docker compose install in helm bootstrap:\n%s", script)
	}
}

func TestBuildBootstrapScriptSupportsProviderHooks(t *testing.T) {
	script, err := BuildBootstrapScript(BootstrapOptions{
		AppPath:          "/app",
		Username:         "root",
		DeploymentTarget: DeploymentTargetCompose,
		ImageName:        "ubuntu-24.04",
		HostTuning: []string{
			"echo tuning-one",
			"echo tuning-two",
		},
		TrustedUserCAKey: "ssh-ed25519 TEST",
		PropagateRootSSH: true,
	})
	if err != nil {
		t.Fatalf("BuildBootstrapScript() error: %v", err)
	}

	for _, fragment := range []string{"echo tuning-one", "echo tuning-two", "TrustedUserCAKeys /etc/ssh/pullpreview-user-ca.pub"} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("expected bootstrap hook fragment %q, script:\n%s", fragment, script)
		}
	}
	if strings.Contains(script, "get-helm-3") {
		t.Fatalf("did not expect helm installer in compose bootstrap with hooks, script:\n%s", script)
	}
}

func TestUserDataScriptUsesSharedComposeBootstrap(t *testing.T) {
	script := UserData{
		AppPath:       "/app",
		SSHPublicKeys: []string{"ssh-ed25519 ROOT"},
		Username:      "root",
	}.Script()

	if !strings.Contains(script, "echo 'ssh-ed25519 ROOT' >> /root/.ssh/authorized_keys") {
		t.Fatalf("expected shared user data to populate authorized_keys, script:\n%s", script)
	}
	if !strings.Contains(script, "docker-compose-plugin") {
		t.Fatalf("expected shared compose bootstrap in user data, script:\n%s", script)
	}
}
