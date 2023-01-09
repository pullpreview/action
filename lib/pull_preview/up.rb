require "open-uri"

module PullPreview
  class Up
    def self.run(app_path, opts)
      PullPreview.logger.debug "options=#{opts.to_hash.inspect}"
      tags = Hash[opts[:tags].map{|tag| tag.split(":", 2)}]

      FileUtils.rm_rf("/tmp/app.tar.gz")

      if app_path.start_with?(/^https?/)
        git_url, ref = app_path.split("#", 2)
        ref ||= "master"
        unless system("rm -rf /tmp/app && git clone '#{git_url}' --depth=1 --branch=#{ref} /tmp/app")
          exit 1
        end
        app_path = "/tmp/app"
      end

      aws_region = PullPreview.lightsail.config.region
      instance_name = opts[:name]

      PullPreview.logger.info "Taring up repository at #{app_path.inspect}..."
      unless system("tar czf /tmp/app.tar.gz --exclude .git -C '#{app_path}' .")
        exit 1
      end

      instance = Instance.new(instance_name, opts)

      unless instance.running?
        PullPreview.logger.info "Starting instance name=#{instance.name}"
        azs = PullPreview.lightsail.get_regions(include_availability_zones: true).regions.find do |region|
          region.name == aws_region
        end.availability_zones.map(&:zone_name)

        blueprint_id = PullPreview.lightsail.get_blueprints.blueprints.find do |blueprint|
          blueprint.platform == "LINUX_UNIX" &&
            blueprint.group == "amazon_linux_2" &&
            blueprint.is_active &&
            blueprint.type == "os"
        end.blueprint_id

        bundle_id = PullPreview.lightsail.get_bundles.bundles.find do |bundle|
          if opts[:instance_type].nil? || opts[:instance_type].empty?
            bundle.cpu_count >= 1 &&
              (2..3).include?(bundle.ram_size_in_gb) &&
              bundle.supported_platforms.include?("LINUX_UNIX")
          else
            bundle.bundle_id == opts[:instance_type]
          end
        end.bundle_id

        instance.launch(azs.first, bundle_id, blueprint_id, tags)
        instance.wait_until_running!
        sleep 2
      end

      PullPreview.logger.info "Synchronizing instance name=#{instance.name}"
      instance.open_ports
      instance.wait_until_ssh_ready!
      instance.setup_ssh_access
      instance.setup_update_script
      instance.setup_prepost_scripts

      puts
      puts "To connect to the instance (authorized GitHub users: #{instance.admins.join(", ")}):"
      puts "  ssh #{instance.ssh_address}"
      puts

      PullPreview.logger.info "Preparing to push app tarball (#{(File.size("/tmp/app.tar.gz") / 1024.0**2).round(2)}MB)"
      remote_tarball_path = "/tmp/app-#{Time.now.utc.strftime("%Y%m%d%H%M%S")}.tar.gz"

      unless instance.scp("/tmp/app.tar.gz", remote_tarball_path)
        raise Error, "Unable to copy application content on instance. Aborting."
      end

      PullPreview.logger.info "Launching application..."
      ok = instance.ssh("/tmp/update_script.sh #{remote_tarball_path}")

      puts "::set-output name=url::#{instance.url}"
      puts "::set-output name=host::#{instance.public_ip}"
      puts "::set-output name=username::#{instance.username}"

      puts
      puts "You can access your application at the following URL:"
      puts "  #{instance.url}"
      puts

      puts
      puts "To ssh into the instance (authorized GitHub users: #{instance.admins.join(", ")}):"
      puts "  ssh #{instance.ssh_address}"
      puts

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
