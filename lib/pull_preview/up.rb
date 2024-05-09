require "open-uri"

module PullPreview
  class Up
    def self.run(app_path, opts)
      STDOUT.sync = true
      STDERR.sync = true

      PullPreview.logger.debug "options=#{opts.to_hash.inspect}"
      opts[:tags] = Hash[opts[:tags].map{|tag| tag.split(":", 2)}]

      FileUtils.rm_rf("/tmp/app.tar.gz")

      if app_path.start_with?(/^https?/)
        git_url, ref = app_path.split("#", 2)
        ref ||= "master"
        unless system("rm -rf /tmp/app && git clone '#{git_url}' --depth=1 --branch=#{ref} /tmp/app")
          exit 1
        end
        app_path = "/tmp/app"
      end

      instance_name = opts[:name]

      PullPreview.logger.info "Taring up repository at #{app_path.inspect}..."
      unless system("tar czf /tmp/app.tar.gz --exclude .git -C '#{app_path}' .")
        exit 1
      end

      instance = Instance.new(instance_name, opts)
      PullPreview.logger.info "Starting instance name=#{instance.name}"
      instance.launch_and_wait_until_ready!

      PullPreview.logger.info "Setting up the domain entry to DNS Zone"
      instance.create_domain_entry

      PullPreview.logger.info "Synchronizing instance name=#{instance.name}"
      instance.setup_ssh_access
      instance.setup_update_script
      instance.setup_prepost_scripts

      connection_instructions = [
        "",
        "To connect to the instance (authorized GitHub users: #{instance.admins.join(", ")}):",
        "  ssh #{instance.ssh_address}",
        ""
      ].join("\n")

      heartbeat = Thread.new do
        loop do
          puts connection_instructions
          sleep 10
        end
      end

      PullPreview.logger.info "Preparing to push app tarball (#{(File.size("/tmp/app.tar.gz") / 1024.0**2).round(2)}MB)"
      remote_tarball_path = "/tmp/app-#{Time.now.utc.strftime("%Y%m%d%H%M%S")}.tar.gz"

      unless instance.scp("/tmp/app.tar.gz", remote_tarball_path)
        raise Error, "Unable to copy application content on instance. Aborting."
      end

      PullPreview.logger.info "Launching application..."
      ok = instance.ssh("/tmp/update_script.sh #{remote_tarball_path}")

      heartbeat.kill

      if github_output_file = ENV["GITHUB_OUTPUT"]
        File.open(github_output_file, "a") do |f|
          f.puts "url=#{instance.url}"
          f.puts "host=#{instance.public_ip}"
          f.puts "username=#{instance.username}"
        end
      end

      puts
      puts "You can access your application at the following URL:"
      puts "  #{instance.url}"
      puts

      puts connection_instructions

      puts
      puts "Then to view the logs:"
      puts "  docker-compose logs --tail 1000 -f"
      puts

      if ok
        instance
      else
        raise Error, "Trying to launch the application failed. Please see the logs above to troubleshoot the issue and for informations on how to connect to the instance"
      end
    end
  end
end
