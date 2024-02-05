module PullPreview
  class UserData
    attr_reader :app_path, :ssh_public_keys, :username

    def initialize(app_path:, ssh_public_keys:, username: "ec2-user")
      @app_path = app_path
      @ssh_public_keys = ssh_public_keys
      @username = username
    end

    def instructions
      result = []
      result << "#!/bin/bash"
      result << "set -xe ; set -o pipefail"
      if ssh_public_keys.any?
        result << %{echo '#{ssh_public_keys.join("\n")}' > /home/#{username}/.ssh/authorized_keys}
      end
      result << "mkdir -p #{app_path} && chown -R #{username}.#{username} #{app_path}"
      result << "echo 'cd #{app_path}' > /etc/profile.d/pullpreview.sh"
      result << "test -s /swapfile || ( fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile && echo '/swapfile none swap sw 0 0' | tee -a /etc/fstab )"

      # for Amazon Linux 2023, which by default creates a /tmp mount that is too small
      result << "systemctl disable --now tmp.mount"
      result << "systemctl mask tmp.mount"

      result << "sysctl vm.swappiness=10 && sysctl vm.vfs_cache_pressure=50"
      result << "echo 'vm.swappiness=10' | tee -a /etc/sysctl.conf"
      result << "echo 'vm.vfs_cache_pressure=50' | tee -a /etc/sysctl.conf"
      result << "yum install -y docker"
      result << %{curl -L "https://github.com/docker/compose/releases/download/v2.18.1/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose}
      result << "chmod +x /usr/local/bin/docker-compose"
      result << "usermod -aG docker #{username}"
      result << "systemctl restart docker"
      result << "echo 'docker system prune -f && docker image prune -a --filter=\"until=96h\" --force' > /etc/cron.daily/docker-prune && chmod a+x /etc/cron.daily/docker-prune"
      result << "mkdir -p /etc/pullpreview && touch /etc/pullpreview/ready && chown -R #{username}.#{username} /etc/pullpreview"
      result
    end

    def to_s
      instructions.join("\n")
    end
  end
end
