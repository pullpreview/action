require "erb"
require "ostruct"

module PullPreview
  class Instance
    include Utils

    attr_reader :admins
    attr_reader :cidrs
    attr_reader :compose_files
    attr_reader :compose_options
    attr_reader :default_port
    attr_reader :dns
    attr_reader :name
    attr_reader :subdomain
    attr_reader :ports
    attr_reader :provider
    attr_reader :registries
    attr_reader :size
    attr_reader :tags
    attr_reader :access_details

    class << self
      attr_accessor :client
      attr_accessor :logger
    end

    def self.normalize_name(name)
      name.
        gsub(/[^a-z0-9]/i, "-").
        squeeze("-")[0..60].
        gsub(/(^-|-$)/, "")
    end

    def initialize(name, opts = {})
      @provider = PullPreview.provider
      @name = self.class.normalize_name(name)
      @subdomain = opts[:subdomain] || name
      @admins = opts[:admins] || []
      @cidrs = opts[:cidrs] || ["0.0.0.0/0"]
      @default_port = opts[:default_port] || "80"
      # TODO: normalize
      @ports = (opts[:ports] || []).push(default_port).push("22").uniq.compact
      @compose_files = opts[:compose_files] || ["docker-compose.yml"]
      @compose_options = opts[:compose_options] || ["--build"]
      @registries = opts[:registries] || []
      @dns = opts[:dns]
      @size = opts[:instance_type]
      @ssh_results = []
      @tags = opts[:tags] || {}
    end

    def launch_and_wait_until_ready!
      @access_details = provider.launch!(name, size: size, user_data: user_data, ports: ports, cidrs: cidrs, tags: tags)
      logger.debug "access_details=#{@access_details.inspect}"
      logger.info "Instance is running public_ip=#{public_ip} public_dns=#{public_dns}"
      wait_until_ssh_ready!
    end

    def create_domain_entry
      logger.info "Adding a domain entry public_dns=#{public_dns} and target #{public_ip} in DNS Zone"
      provider.create_domain_entry(dns, public_dns, public_ip)
    end

    def delete_domain_entry
      logger.info "Deleting a domain entry public_dns=#{public_dns} and target #{public_ip} in DNS Zone"
      provider.delete_domain_entry(dns, public_dns, public_ip)
    end

    def terminate!
      if provider.terminate!(name)
        logger.info "Instance successfully destroyed"
      else
        logger.error "Unable to destroy instance"
      end
    end

    def running?
      provider.running?(name)
    end

    def ssh_ready?
      ssh("test -f /etc/pullpreview/ready")
    end

    def public_ip
      access_details.ip_address
    end

    def public_dns
      reserved_space_for_user_subdomain = 8
      # https://community.letsencrypt.org/t/a-certificate-for-a-63-character-domain/78870/4
      remaining_chars_for_subdomain = 62 - reserved_space_for_user_subdomain - dns.size - public_ip.size - "ip".size - ("." * 3).size
      [
        [subdomain[0..remaining_chars_for_subdomain], "ip", public_ip.gsub(".", "-")].join("-").squeeze("-"),
        dns
      ].join(".")
    end

    def url
      scheme = (default_port == "443" ? "https" : "http")
      "#{scheme}://#{public_dns}"
    end

    def username
      provider.username
    end

    def ssh_public_keys
      @ssh_public_keys ||= admins.map do |github_username|
        URI.open("https://github.com/#{github_username}.keys").read.split("\n")
      end.flatten.reject{|key| key.empty?}
    end

    def user_data
      @user_data ||= UserData.new(app_path: remote_app_path, username: username, ssh_public_keys: ssh_public_keys)
    end

    def erb_locals
      OpenStruct.new(
        remote_app_path: remote_app_path,
        compose_files: compose_files,
        compose_options: compose_options,
        public_ip: public_ip,
        public_dns: public_dns,
        admins: admins,
        url: url,
      )
    end

    def github_token
      ENV.fetch("GITHUB_TOKEN", "")
    end

    def github_repository_owner
      ENV.fetch("GITHUB_REPOSITORY_OWNER", "")
    end

    def update_script
      PullPreview.data_dir.join("update_script.sh.erb")
    end

    def update_script_rendered
      ERB.new(File.read(update_script)).result_with_hash(locals: erb_locals)
    end

    def setup_ssh_access
      File.open("/tmp/authorized_keys", "w+") do |f|
        f.write ssh_public_keys.join("\n")
      end
      scp("/tmp/authorized_keys", "/home/#{username}/.ssh/authorized_keys", mode: "0600")
      # in case provider ssh user is different than the one we want to use
      ssh("chown #{username}.#{username} /home/#{username}/.ssh/authorized_keys && chmod 0600 /home/#{username}/.ssh/authorized_keys")
    end

    def setup_update_script
      tmpfile = Tempfile.new("update_script").tap do |f|
        f.write update_script_rendered
      end
      tmpfile.flush
      unless scp(tmpfile.path, "/tmp/update_script.sh", mode: "0755")
        raise Error, "Unable to copy the update script on instance. Aborting."
      end
    end

    def setup_prepost_scripts
      tmpfile = Tempfile.new(["prescript", ".sh"])
      tmpfile.puts "#!/bin/bash -e"
      registries.each_with_index do |registry, index|
        begin
          uri = URI.parse(registry)
          raise Error, "Invalid registry" if uri.host.nil? || uri.scheme != "docker"
          username = uri.user
          password = uri.password
          if password.nil?
            password = username
            username = "doesnotmatter"
          end
          tmpfile.puts 'echo "Logging into %{host}..."' % { host: uri.host }
          # https://docs.github.com/en/packages/guides/using-github-packages-with-github-actions#upgrading-a-workflow-that-accesses-ghcrio
          tmpfile.puts 'echo "%{password}" | docker login "%{host}" -u "%{username}" --password-stdin' % {
            host: uri.host,
            username: username,
            password: password,
          }
        rescue URI::Error, Error => e
          logger.warn "Registry ##{index} is invalid: #{e.message}"
        end
      end
      tmpfile.flush
      unless scp(tmpfile.path, "/tmp/pre_script.sh", mode: "0755")
        raise Error, "Unable to copy the pre script on instance. Aborting."
      end
    end

    def wait_until_ssh_ready!
      if wait_until { logger.info "Waiting for ssh" ; ssh_ready? }
        logger.info "Instance ssh access OK"
      else
        logger.error "Instance ssh access KO"
        raise Error, "Can't connect to instance over SSH. Aborting."
      end
    end

    def scp(source, target, mode: "0644")
      ssh("cat - > #{target} && chmod #{mode} #{target}", input: File.new(source))
    end

    def ssh(command, input: nil)
      key_file_path = "/tmp/tempkey"
      cert_key_path = "/tmp/tempkey-cert.pub"
      File.open(key_file_path, "w+") do |f|
        f.puts access_details.private_key
      end
      if access_details.cert_key
        File.open(cert_key_path, "w+") do |f|
          f.puts access_details.cert_key
        end
      end
      [key_file_path].each{|file| FileUtils.chmod 0600, file}

      cmd = "ssh #{"-v " if logger.level == Logger::DEBUG}-o ServerAliveInterval=15 -o IdentitiesOnly=yes -i #{key_file_path} #{ssh_address} #{ssh_options.join(" ")} '#{command}'"
      if input && input.respond_to?(:path)
        cmd = "cat #{input.path} | #{cmd}"
      end
      logger.debug cmd
      system(cmd).tap {|result| @ssh_results.push([cmd, result])}
    end

    def ssh_address
      access_details.ssh_address
    end

    def ssh_options
      [
        "-o StrictHostKeyChecking=no",
        "-o UserKnownHostsFile=/dev/null",
        "-o LogLevel=ERROR",
        "-o ConnectTimeout=10",
      ]
    end

    private def logger
      PullPreview.logger
    end

    private def remote_app_path
      REMOTE_APP_PATH
    end
  end
end
