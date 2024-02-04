module PullPreview
  class Down
    def self.run(opts)
      instance = Instance.new(opts[:name])
      PullPreview.logger.info "Destroying instance name=#{instance.name}"
      instance.terminate!

      PullPreview.logger.info "Deleting the subdomain to DNS Zone"
      instance.delete_dns_entry
    end
  end
end
