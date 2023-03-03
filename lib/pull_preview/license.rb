require "json"
require 'open-uri'

module PullPreview
  class License
    attr_reader :state, :message

    def initialize(org_id, repo_id, action, details = {})
      @org_id = org_id
      @repo_id = repo_id
      @action = action
      @details = details
    end

    def ok?
      state == "ok"
    end

    def params
      @details.merge({org_id: @org_id, repo_id: @repo_id, pp_action: @action})
    end

    def fetch!
      uri = URI("https://app.pullpreview.com/licenses/check")
      uri.query = URI.encode_www_form(params)
      result = ""
      begin
        result = open(uri.to_s)
      rescue StandardError => e
        result = "OK - License server unreachable"
      end
      if result =~ /^OK/
        @state = "ok"
        @message = result
      else
        @state = "ko"
        @message = result
      end
      self
    end
  end
end
