module PullPreview
  module Providers
    class Base
      include Utils

      attr_reader :client

      def username
        "ec2-user"
      end

      def running?(name)
        raise NotImplementedError
      end

      def terminate!(name)
        raise NotImplementedError
      end

      def launch!(name, size:, ssh_public_keys: [], user_data: UserData.new, cidrs: [], ports: [], tags: {})
        raise NotImplementedError
      end

      def setup_firewall(name, cidrs: [], ports: [])
        raise NotImplementedError
      end

      def fetch_access_details(name)
        raise NotImplementedError
      end

      def list_instances(tags: {})
        raise NotImplementedError
      end

      def wait_until_running!(name)
        if wait_until { logger.info "Waiting for instance to be running" ; running?(name) }
          logger.debug "Instance is running"
        else
          logger.error "Timeout while waiting for instance running"
          raise Error, "Instance still not running. Aborting."
        end
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