package pullpreview

import (
	"strings"
)

type UserData struct {
	AppPath       string
	SSHPublicKeys []string
	Username      string
}

func (u UserData) Script() string {
	homeDir := HomeDirForUser(u.Username)
	lines := []string{
		"#!/bin/bash",
		"set -xe ; set -o pipefail",
	}
	if len(u.SSHPublicKeys) > 0 {
		lines = append(lines, "echo '"+strings.Join(u.SSHPublicKeys, "\n")+"' > "+homeDir+"/.ssh/authorized_keys")
	}
	lines = append(lines,
		"mkdir -p "+u.AppPath+" && chown -R "+u.Username+":"+u.Username+" "+u.AppPath,
		"echo 'cd "+u.AppPath+"' > /etc/profile.d/pullpreview.sh",
		"test -s /swapfile || ( fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile && echo '/swapfile none swap sw 0 0' | tee -a /etc/fstab )",
		"systemctl disable --now tmp.mount",
		"systemctl mask tmp.mount",
		"sysctl vm.swappiness=10 && sysctl vm.vfs_cache_pressure=50",
		"echo 'vm.swappiness=10' | tee -a /etc/sysctl.conf",
		"echo 'vm.vfs_cache_pressure=50' | tee -a /etc/sysctl.conf",
		"yum install -y docker",
		"curl -L \"https://github.com/docker/compose/releases/download/v2.18.1/docker-compose-$(uname -s)-$(uname -m)\" -o /usr/local/bin/docker-compose",
		"chmod +x /usr/local/bin/docker-compose",
		"usermod -aG docker "+u.Username,
		"systemctl restart docker",
		"echo 'docker system prune -f && docker image prune -a --filter=\"until=96h\" --force' > /etc/cron.daily/docker-prune && chmod a+x /etc/cron.daily/docker-prune",
		"mkdir -p /etc/pullpreview && touch /etc/pullpreview/ready && chown -R "+u.Username+":"+u.Username+" /etc/pullpreview",
	)
	return strings.Join(lines, "\n")
}
