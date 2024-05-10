module PullPreview
  class Down
    def self.run(opts)
      instance = Instance.new(opts[:name], opts)
      PullPreview.logger.info "Deleting the subdomain to DNS Zone"
      instance.delete_domain_entry

      PullPreview.logger.info "Destroying instance name=#{instance.name}"
      instance.terminate!
    end
  end
end
