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
      @state = "ok"
      @message = "License valid"
      self
    end
  end
end
