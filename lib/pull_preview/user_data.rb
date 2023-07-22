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
      result << "#!/bin/bash -xe"
      ssh_public_keys.each do |ssh_public_key|
        result << %{echo '#{ssh_public_keys}' >> /home/#{username}/.ssh/authorized_keys}
      end
      result << "mkdir -p #{app_path} && chown -R #{username}.#{username} #{app_path}"
      result << "echo 'cd #{app_path}' > /etc/profile.d/pullpreview.sh"
      result << "test -s /swapfile || ( fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile && echo '/swapfile none swap sw 0 0' | tee -a /etc/fstab )"
      result << "sysctl vm.swappiness=10 && sysctl vm.vfs_cache_pressure=50"
      result << "echo 'vm.swappiness=10' | tee -a /etc/sysctl.conf"
      result << "echo 'vm.vfs_cache_pressure=50' | tee -a /etc/sysctl.conf"
      result << "curl -fsSL https://get.docker.com -o - | sh"
      result << "systemctl restart docker"
      result << %{echo -e '#!/bin/sh\nexec docker compose "$@"' > /usr/local/bin/docker-compose}
      result << "chmod +x /usr/local/bin/docker-compose"
      result << "usermod -aG docker #{username}"
      result << "echo 'docker image prune -a --filter=\"until=72h\" --force' > /etc/cron.daily/docker-prune && chmod a+x /etc/cron.daily/docker-prune"
      result << "mkdir -p /etc/pullpreview && touch /etc/pullpreview/ready && chown -R #{username}.#{username} /etc/pullpreview"
      result
    end

    def to_s
      instructions.join("\n")
    end
  end
end
