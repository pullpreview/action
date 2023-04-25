module PullPreview
  module Providers
    def self.fetch(name)
      require_relative "./providers/#{name.downcase}"
      const_get(name.capitalize).new
    end
  end
end