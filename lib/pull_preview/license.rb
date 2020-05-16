require "json"

module PullPreview
  class License
    attr_reader :state, :message

    def initialize(org_id, repo_id, action)
      @org_id = org_id
      @repo_id = repo_id
      @action = action
    end

    def ok?
      state == "ok"
    end

    def params
      {org_id: @org_id, repo_id: @repo_id, pp_action: @action}
    end

    def fetch!
      response = PullPreview.faraday.get(LICENSE_STATUS_URL, params)
      if response.status.to_s == "200"
        payload = JSON.parse(response.body)
        @state = payload["state"]
        @message = payload["message"].to_s
      else
        @state = "error"
        @message = "Error while fetching the license status"
      end
      self
    rescue Faraday::Error => e
      logger.warn "Got the following error while fetching the license: #{e.message}"
      self
    rescue StandardError => e
      logger.warn "Got the following error while checking the license: #{e.message}"
      self
    end
  end
end
