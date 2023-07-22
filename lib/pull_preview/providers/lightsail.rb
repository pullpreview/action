module PullPreview
  module Providers
    class Lightsail < Base
      SIZES = {
        "nano" => "nano",
        "micro" => "micro",
        "small" => "small",
        "medium" => "medium",
        "large" => "large",
        "xlarge" => "xlarge",
        "2xlarge" => "2xlarge",
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

      def terminate!(name)
        operation = client.delete_instance(instance_name: name).operations.first
        if operation.error_code.nil?
          true
        else
          raise Error, "An error occurred while destroying the instance: #{operation.error_code} (#{operation.error_details})"
        end
      end

      def launch!(name, size:, user_data: UserData.new, cidrs: [], ports: [], tags: {})
        unless running?(name)
          launch_or_restore_from_snapshot(name, user_data: user_data, size: size, tags: tags)
          sleep 2
          wait_until_running!(name)
        end
        setup_firewall(name, cidrs: cidrs, ports: ports)
        fetch_access_details(name)
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

      private def setup_firewall(name, cidrs: [], ports: [])
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

      private def fetch_access_details(name)
        result = client.get_instance_access_details({
          instance_name: name,
          protocol: "ssh", # accepts ssh, rdp
        }).access_details
        AccessDetails.new(username: result.username, ip_address: result.ip_address, cert_key: result.cert_key, private_key: result.private_key)
      end

      private def launch_or_restore_from_snapshot(name, user_data:, size:, tags: {})
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

      private def latest_snapshot(name)
        client.get_instance_snapshots.instance_snapshots.sort{|a,b| b.created_at <=> a.created_at}.find do |snap|
          snap.state == "available" && snap.from_instance_name == name
        end
      end

      private def availability_zones
        azs = client.get_regions(include_availability_zones: true).regions.find do |region|
          region.name == @aws_region
        end.availability_zones.map(&:zone_name)
      end

      private def blueprint_id
        blueprint_id = client.get_blueprints.blueprints.find do |blueprint|
          blueprint.platform == "LINUX_UNIX" &&
            blueprint.group == "amazon_linux_2" &&
            blueprint.is_active &&
            blueprint.type == "os"
        end.blueprint_id
      end

      private def bundle_id(size = "medium")
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
    end
  end
end