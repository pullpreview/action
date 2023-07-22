module PullPreview
  module Providers
    # Using this provider deploys environements hosted by pullpreview.com
    # This requires to set a valid PULLPREVIEW_LICENSE environment variable
    class Hosted < Base
      def initialize
        require "faraday"
        @client ||= Faraday.new(
          url: PullPreview.api_uri.to_s,
          headers: {'Accept' => 'application/json'},
          request: { timeout: 5 * 60 },
        ) do |conn|
          conn.request  :json
          conn.request :authorization, :Token, PullPreview.license
          conn.response  :json
          if PullPreview.logger.level.zero?
            conn.response :logger, PullPreview.logger, bodies: true, headers: true
          end
        end
      end

      def running?(name)
        response = @client.get("/api/servers/#{name}")
        response.success? && response.body["status"] == "launched"
      end

      def terminate!(name)
        response = @client.delete("/api/servers/#{name}")
        raise Error, response.body["message"] unless response.success? || response.status == 404
        true
      end

      def launch!(name, size: "small", user_data: UserData.new, cidrs: [], ports: [], tags: {})
        unless running?(name)
          launch_or_restore_from_snapshot(name, user_data: user_data, size: size, cidrs: cidrs, ports: ports, tags: tags)
          sleep 2
          wait_until_running!(name)
        end
        fetch_access_details(name)
      end

      private def launch_or_restore_from_snapshot(name, size: "small", user_data: UserData.new, cidrs: [], ports: [], tags: {})
        response = @client.post("/api/servers", {
          name: name,
          size: size,
          user_data: user_data,
          cidrs: cidrs,
          ports: ports,
          tags: tags,
        })
        raise Error, response.body["message"] unless response.success?
        raise Error, "Failed to launch" unless response.body["status"] == "launched"
      end

      private def fetch_access_details(name)
        response = @client.get("/api/servers/#{name}")
        raise Error, response.body["message"] unless response.success?
        AccessDetails.new(
          username: response.body["username"],
          ip_address: response.body["ipv4"],
          private_key: response.body["private_key"]
        )
      end

      def list_instances(tags: {})
        response = @client.get("/api/servers", tags: tags)
        raise Error, response.body["message"] unless response.success?
        response.body["servers"].each do |server|
          yield OpenStruct.new(
            name: server["name"],
            public_ip: server["ipv4"],
            size: server["size"],
            region: server["region"],
            zone: server["zone"],
            created_at: server["created_at"],
            tags: server["tags"] || {},
          )
        end
      end
    end
  end
end