module PullPreview
  class Down
    def self.run(opts)
      instance = Instance.new(opts[:name])
      PullPreview.logger.info "Destroying instance name=#{instance.name}"
      instance.destroy!
    end
  end
end
