require "json"
require "net/http"

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
      begin
        response = Net::HTTP.get_response(uri)
        if response.code == "200"
          @state = "ok"
          @message = response.body
        else
          @state = "ko"
          @message = response.body
        end
      rescue Exception => e
        PullPreview.logger.warn "License server unreachable - #{e.message}"
        @state = "ok"
        @message = "License server unreachable. Continuing..."
      end
      self
    end
  end
end
