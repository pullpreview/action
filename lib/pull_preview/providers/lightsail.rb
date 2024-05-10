module PullPreview
  module Providers
    class Lightsail
      include Utils

      attr_reader :client

      SIZES = {
        "XXS" => "nano",
        "XS" => "micro",
        "S" => "small",
        "M" => "medium",
        "L" => "large",
        "XL" => "xlarge",
        "2XL" => "2xlarge",
      }

      def initialize
        require "aws-sdk-lightsail"
        @aws_region = ENV.fetch("AWS_REGION", "us-east-1")
        @client = Aws::Lightsail::Client.new(region: @aws_region)
      end

      def running?(name)
        resp = client.get_instance_state(instance_name: name)
        resp.state.name == "running"
      rescue Aws::Lightsail::Errors::NotFoundException
        false
      end

      def create_domain_entry(dns, public_dns, public_ip)
        domain_entry = {
          name: public_dns,
          target: public_ip,
          type: "A"
        }

        begin
          resp = client.create_domain_entry(domain_name: dns, domain_entry: domain_entry)
          resp.operation.status == "Succeeded"
        rescue Aws::Lightsail::Errors::NotFoundException
          false
        rescue Aws::Lightsail::Errors::InvalidInputException
          false
        rescue StandardError
          false
        end
      end

      def delete_domain_entry(dns, public_dns, public_ip)
        domain_entry = {
          name: public_dns,
          target: public_ip,
          type: "A"
        }

        begin
          resp = client.delete_domain_entry(domain_name: dns, domain_entry: domain_entry)
          resp.operation.status == "Succeeded"
        rescue Aws::Lightsail::Errors::NotFoundException
          false
        rescue Aws::Lightsail::Errors::InvalidInputException
          false
        rescue StandardError
          false
        end
      end

      def terminate!(name)
        operation = client.delete_instance(instance_name: name).operations.first
        if operation.error_code.nil?
          true
        else
          raise Error, "An error occurred while destroying the instance: #{operation.error_code} (#{operation.error_details})"
        end
      end

      def launch!(name, size:, ssh_public_keys: [], user_data: UserData.new, cidrs: [], ports: [], tags: {})
        unless running?(name)
          launch_or_restore_from_snapshot(name, user_data: user_data, size: size, ssh_public_keys: ssh_public_keys, tags: tags)
          sleep 2
          wait_until_running!(name)
        end
        setup_firewall(name, cidrs: cidrs, ports: ports)
        fetch_access_details(name)
      end

      def launch_or_restore_from_snapshot(name, user_data:, size:, ssh_public_keys: [], tags: {})
        params = {
          instance_names: [name],
          availability_zone: availability_zones.first,
          bundle_id: bundle_id(size),
          tags: {stack: PullPreview::STACK_NAME}.merge(tags).map{|(k,v)| {key: k.to_s, value: v.to_s}},
        }

        if latest_snapshot(name)
          logger.info "Found snapshot to restore from: #{latest_snapshot.name}"
          logger.info "Creating new instance name=#{name}..."
          client.create_instances_from_snapshot(params.merge({
            user_data: user_data.to_s,
            instance_snapshot_name: latest_snapshot.name,
          }))
        else
          logger.info "Creating new instance name=#{name}..."
          client.create_instances(params.merge({
            user_data: user_data.to_s,
            blueprint_id: blueprint_id
          }))
        end
      end

      def wait_until_running!(name)
        if wait_until { logger.info "Waiting for instance to be running" ; running?(name) }
          logger.debug "Instance is running"
        else
          logger.error "Timeout while waiting for instance running"
          raise Error, "Instance still not running. Aborting."
        end
      end

      def setup_firewall(name, cidrs: [], ports: [])
        client.put_instance_public_ports({
          port_infos: ports.map do |port_definition|
            port_range, protocol = port_definition.split("/", 2)
            protocol ||= "tcp"
            port_range_start, port_range_end = port_range.split("-", 2)
            port_range_end ||= port_range_start
            cidrs_to_use = cidrs
            if port_range_start.to_i == 22
              # allow SSH from anywhere
              cidrs_to_use = ["0.0.0.0/0"]
            end
            {
              from_port: port_range_start.to_i,
              to_port: port_range_end.to_i,
              protocol: protocol, # accepts tcp, all, udp
              cidrs: cidrs_to_use,
            }
          end,
          instance_name: name
        })
      end

      def fetch_access_details(name)
        result = client.get_instance_access_details({
          instance_name: name,
          protocol: "ssh", # accepts ssh, rdp
        }).access_details
        AccessDetails.new(username: result.username, ip_address: result.ip_address, cert_key: result.cert_key, private_key: result.private_key)
      end

      def latest_snapshot(name)
        client.get_instance_snapshots.instance_snapshots.sort{|a,b| b.created_at <=> a.created_at}.find do |snap|
          snap.state == "available" && snap.from_instance_name == name
        end
      end

      def list_instances(tags: {})
        next_page_token = nil
        begin
          result = client.get_instances(next_page_token: next_page_token)
          next_page_token = result.next_page_token
          result.instances.each do |instance|
            matching_tags = Hash[instance.tags.select{|tag| tags.keys.include?(tag.key)}.map{|tag| [tag.key, tag.value]}]
            if matching_tags == tags
              yield(OpenStruct.new(
                name: instance.name,
                public_ip: instance.public_ip_address,
                size: SIZES.invert.fetch(instance.bundle_id, instance.bundle_id),
                region: instance.location.region_name,
                zone: instance.location.availability_zone,
                created_at: instance.created_at,
                tags: instance.tags,
              ))
            end
          end
        end while not next_page_token.nil?
      end

      def availability_zones
        azs = client.get_regions(include_availability_zones: true).regions.find do |region|
          region.name == @aws_region
        end.availability_zones.map(&:zone_name)
      end

      def blueprint_id
        blueprint_id = client.get_blueprints.blueprints.find do |blueprint|
          blueprint.platform == "LINUX_UNIX" &&
            blueprint.group == "amazon_linux_2023" &&
            blueprint.is_active &&
            blueprint.type == "os"
        end.blueprint_id
      end

      def username
        "ec2-user"
      end

      def bundle_id(size = "M")
        instance_type = SIZES.fetch(size, size.sub("_2_0", ""))
        bundle_id = client.get_bundles.bundles.find do |bundle|
          if instance_type.nil? || instance_type.empty?
            bundle.cpu_count >= 1 &&
              (2..3).include?(bundle.ram_size_in_gb) &&
              bundle.supported_platforms.include?("LINUX_UNIX")
          else
            bundle.instance_type == instance_type
          end
        end.bundle_id
      end

      private def remote_app_path
        PullPreview::REMOTE_APP_PATH
      end

      private def logger
        PullPreview.logger
      end
    end
  end
end
