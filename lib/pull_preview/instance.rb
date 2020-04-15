module PullPreview
  class Instance
    attr_reader :name
    attr_reader :authorized_users

    class << self
      attr_accessor :client
      attr_accessor :logger
    end

    def initialize(name, authorized_users = [])
      @name = name
      @authorized_users = authorized_users
      logger.info "Instance name=#{@name}"
    end

    def ssh_public_keys
      @ssh_public_keys ||= authorized_users.map do |github_username|
        URI.open("https://github.com/#{github_username}.keys").read.split("\n")
      end.flatten.reject{|key| key.empty?}
    end

    def running?
      resp = client.get_instance_state(instance_name: name)
      resp.state.name == "running"
    rescue Aws::Lightsail::Errors::NotFoundException
      @instance_details = nil
      false
    end

    def ssh_ready?
      ssh("test -f /etc/pullpreview/ready")
    end

    def launch(az, bundle_id, blueprint_id, tags = {})
      logger.debug "Instance launching ssh_public_keys=#{ssh_public_keys.inspect}"

      client.create_instances({
        instance_names: [name],
        availability_zone: az,
        blueprint_id: blueprint_id,
        bundle_id: bundle_id,
        user_data: [
          %{echo '#{ssh_public_keys.join("\n")}' > /home/ec2-user/.ssh/authorized_keys},
          "mkdir -p #{REMOTE_APP_PATH} && chown -R ec2-user.ec2-user #{REMOTE_APP_PATH}",
          "echo 'cd #{REMOTE_APP_PATH}' > /etc/profile.d/pullpreview.sh",
          "yum install -y docker",
          %{curl -L "https://github.com/docker/compose/releases/download/1.25.4/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose},
          "chmod +x /usr/local/bin/docker-compose",
          "usermod -aG docker ec2-user",
          "service docker start",
          "mkdir -p /etc/pullpreview && touch /etc/pullpreview/ready",
        ].join(" && "),
        tags: {stack: STACK_NAME}.merge(tags).map{|(k,v)| {key: k.to_s, value: v.to_s}},
      })
    end

    def destroy!
      operation = client.delete_instance(instance_name: name).operations.first
      if operation.error_code.nil?
        PullPreview.logger.info "Instance successfully destroyed"
      else
        raise Error, "An error occurred while destroying the instance: #{operation.error_code} (#{operation.error_details})"
      end
    end

    def setup_ssh_access
      File.open("/tmp/authorized_keys", "w+") do |f|
        f.write ssh_public_keys.join("\n")
      end
      scp("/tmp/authorized_keys", "/home/ec2-user/.ssh/authorized_keys", mode: "0600")
    end

    def wait_until_running!
      if wait_until { logger.info "Waiting for instance to be running" ; running? }
        logger.info "Instance is running public_ip=#{public_ip}"
      else
        logger.error "Timeout while waiting for instance running"
        raise Error, "Instance still not running. Aborting."
      end
    end

    def wait_until_ssh_ready!
      if wait_until { logger.info "waiting for ssh" ; ssh_ready? }
        logger.info "instance ssh access OK"
      else
        logger.error "instance ssh access KO"
        raise Error, "Can't connect to instance over SSH. Aborting."
      end
    end

    def wait_until(max_retries = 20, interval = 3, &block)
      result = true
      retries = 0
      until block.call
        retries += 1
        if retries >= max_retries
          result = false
          break
        end
        sleep interval
      end
      result 
    end

    def open_ports(ports)
      client.put_instance_public_ports({
        port_infos: ports.map do |port_definition|
          port_range, protocol = port_definition.split("/", 2)
          protocol ||= "tcp"
          port_range_start, port_range_end = port_range.split("-", 2)
          port_range_end ||= port_range_start
          {
            from_port: port_range_start.to_i,
            to_port: port_range_end.to_i,
            protocol: protocol, # accepts tcp, all, udp
          }
        end,
        instance_name: name
      })
    end

    def scp(source, target, mode: "0644")
      ssh("cat - > #{target} && chmod #{mode} #{target}", input: File.new(source))
    end

    def ssh(command, input: nil)
      cert_key_path = "/tmp/tempkey-cert.pub"
      key_file_path = "/tmp/tempkey"
      File.open(cert_key_path, "w+") do |f|
        f.write access_details.cert_key
      end
      File.open(key_file_path, "w+") do |f|
        f.write access_details.private_key
      end
      [key_file_path, cert_key_path].each{|file| FileUtils.chmod 0600, file}
      cmd = "ssh -i #{key_file_path} #{ssh_address} #{ssh_options.join(" ")} '#{command}'"
      if input && input.respond_to?(:path)
        cmd = "cat #{input.path} | #{cmd}"
      end
      logger.debug cmd
      system(cmd)
    end

    def username
      access_details.username
    end

    def public_ip
      access_details.ip_address
    end

    def url
      "http://#{public_ip}"
    end

    def ssh_address
      [username, public_ip].compact.join("@")
    end

    def ssh_options
      [
        "-o StrictHostKeyChecking=no",
        "-o UserKnownHostsFile=/dev/null",
        "-o LogLevel=ERROR",
        "-o ConnectTimeout=10",
      ]
    end

    def instance_details
      @instance_details ||= client.get_instance(instance_name: name).instance
    end

    def access_details
      @access_details ||= client.get_instance_access_details({
        instance_name: name,
        protocol: "ssh", # accepts ssh, rdp
      }).access_details
    end

    def client
      PullPreview.lightsail
    end

    def logger
      PullPreview.logger
    end
  end
end
