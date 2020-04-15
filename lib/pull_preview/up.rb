require "open-uri"
require 'slop'

module PullPreview
  class Up
    def self.run(cli_args)
      opts = Slop.parse do |o|
        o.banner = "Usage: pullpreview up path/to/app [options]"
        o.string '--name', 'Unique name for the environment'
        o.array '--admins', 'Logins of GitHub users that will have their SSH key installed on the instance'
        o.array '--compose-files', 'Compose files to use when running docker-compose up', default: ["docker-compose.yml"]
        o.bool '-v', '--verbose', 'Enable verbose mode'
        o.on '--help' do
          puts o
          exit
        end
      end

      if opts.verbose?
        PullPreview.logger.level = Logger::DEBUG
      end

      if opts[:name].to_s.empty?
        puts opts
        exit 1
      end

      PullPreview.logger.debug "CLI options=#{opts.to_hash.inspect}"

      repo_path = opts.arguments.first
      if repo_path.nil?
        puts opts
        exit 1
      end

      FileUtils.rm_rf("/tmp/app.tar.gz")

      if repo_path.start_with?(/^https?/)
        git_url, ref = repo_path.split("#", 2)
        ref ||= "master"
        unless system("rm -rf /tmp/app && git clone '#{git_url}' --depth=1 --branch=#{ref} /tmp/app")
          exit 1
        end
        repo_path = "/tmp/app"
      end

      aws_region = PullPreview.lightsail.config.region
      compose_files = opts[:compose_files]
      authorized_users = opts[:admins]
      instance_name = opts[:name].gsub(/[^a-z0-9]/i, "-").squeeze("-")[0..60]
      instance_name.gsub!(/(^-|-$)/, "")

      PullPreview.logger.info "Taring up repository at #{repo_path.inspect}..."
      unless system("tar czf /tmp/app.tar.gz -C '#{repo_path}' .")
        exit 1
      end

      instance = Instance.new(instance_name, authorized_users)

      unless instance.running?
        azs = PullPreview.lightsail.get_regions(include_availability_zones: true).regions.find do |region|
          region.name == aws_region
        end.availability_zones.map(&:zone_name)

        blueprint_id = PullPreview.lightsail.get_blueprints.blueprints.find do |blueprint|
          blueprint.platform == "LINUX_UNIX" &&
            blueprint.group == "amazon-linux" &&
            blueprint.is_active &&
            blueprint.type == "os"
        end.blueprint_id

        bundle_id = PullPreview.lightsail.get_bundles.bundles.find do |bundle|
          bundle.cpu_count >= 1 &&
            (1...3).include?(bundle.ram_size_in_gb) &&
            bundle.supported_platforms.include?("LINUX_UNIX")
        end.bundle_id

        instance.launch(azs.sample, bundle_id, blueprint_id)
        instance.wait_until_running!
        sleep 2
      end

      instance.open_ports(
        ["22", "80/tcp", "443/tcp", "1000-10000/tcp"]
      )

      instance.wait_until_ssh_ready!
      instance.setup_ssh_access

      puts
      puts "To connect to the instance (authorized GitHub users: #{instance.authorized_users.join(", ")}):"
      puts "  ssh #{instance.ssh_address}"
      puts

      PullPreview.logger.info "Preparing to push app tarball (#{(File.size("/tmp/app.tar.gz") / 1024.0**2).round(2)}MB)"
      unless instance.scp("/tmp/app.tar.gz", "/tmp/app.tar.gz")
        PullPreview.logger.error "Unable to copy application content on instance. Aborting."
        exit 1
      end

      instance.ssh("rm -f #{REMOTE_APP_PATH}/docker-compose.* && tar xzf /tmp/app.tar.gz -C #{REMOTE_APP_PATH} && cd #{REMOTE_APP_PATH} && docker-compose #{compose_files.map{|f| ["-f", f]}.flatten.join(" ")} up --build --remove-orphans -d && sleep 10 && docker-compose logs")

      puts
      puts "To connect to the instance (authorized GitHub users: #{instance.authorized_users.join(", ")}):"
      puts "  ssh #{instance.ssh_address}"
      puts

      puts
      puts "Then to view the logs:"
      puts "  docker-compose logs --tail 1000 -f"
      puts

      instance
    end
  end
end
