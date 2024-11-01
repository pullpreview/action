module PullPreview
  class Down
    def self.run(opts)
      instance = Instance.new(opts[:name], opts)

      # Call the launch so we can get the instance attributes (ip, dns etc)
      instance.launch_and_wait_until_ready!

      PullPreview.logger.info "Deleting the subdomain to DNS Zone"
      instance.delete_domain_entry

      PullPreview.logger.info "Destroying instance name=#{instance.name}"
      instance.terminate!
    end
  end
end
