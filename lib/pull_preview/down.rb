module PullPreview
  class Down
    def self.run(opts)
      instance = Instance.new(opts[:name])
      instance.destroy!
    end
  end
end
