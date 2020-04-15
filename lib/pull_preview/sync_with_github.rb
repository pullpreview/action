require "octokit"

module PullPreview
  class SyncWithGithub
    LABEL = "pullpreview"

    def self.run(cli_args)
      github_event_path = ENV.fetch("GITHUB_EVENT_PATH")
      # https://developer.github.com/v3/activity/events/types/#pushevent
      # https://help.github.com/en/actions/reference/events-that-trigger-workflows
      github_context = JSON.parse(File.read(github_event_path))
      github_token = ENV.fetch("GITHUB_TOKEN")
      octokit = Octokit::Client.new(access_token: github_token)

      PullPreview.logger.debug "context = #{github_context.inspect}"

      self.new(octokit, github_context).sync!(cli_args)
    end

    attr_reader :octokit, :github_context

    def initialize(octokit, github_context)
      @octokit = octokit
      @github_context = github_context
    end

    def sync!(cli_args)
      if pr_number.nil?
        PullPreview.logger.error "Unable to find a matching PR for ref #{ref.inspect}"
        exit 1
      end

      if unlabeled?
        update_github_status(:destroying)
        Down.run(cli_args)
        update_github_status(:destroyed)
      elsif labeled?
        update_github_status(:deploying)
        instance = Up.run(cli_args)
        update_github_status(:deployed, instance.url)
      elsif push?
        if pr.labels.map {|label| label.name.downcase}.include?(LABEL)
          update_github_status(:deploying)
          PullPreview.logger.info "Found label #{LABEL} on PR##{pr_number}"
          instance = Up.run(cli_args)
          update_github_status(:deployed, instance.url)
        else
          PullPreview.logger.info "Unable to find label on PR##{pr_number}"
        end
      else
        PullPreview.logger.info "Ignoring event"
      end
    rescue => e
      PullPreview.logger.error "Got error: #{e.message}"
      PullPreview.logger.debug e.backtrace.join("\n")
      update_github_status(:error)
    end

    def github_status_for(status)
      case status
      when :error
        :error
      when :deployed, :destroyed
        :success
      when :deploying, :destroying
        :pending
      end
    end

    def update_github_status(status, url = nil)
      github_status = github_status_for(status)
      # https://developer.github.com/v3/repos/statuses/#create-a-status
      params = {
        context: "PullPreview",
        description: "Preview environment #{status.to_s}"
      }
      params.merge!(target_url: url) if url
      octokit.create_status(
        repo,
        sha,
        github_status.to_s,
        params
      )
    end

    def org_name
      if pull_request?
        github_context["organization"]["login"]
      else
        github_context["repository"]["organization"]
      end
    end

    def repo_name
      github_context["repository"]["name"]
    end

    def repo
      [org_name, repo_name].join("/")
    end

    def ref
      github_context["ref"]
    end

    def sha
      pr.head.sha
    end

    def labeled?
      pull_request? &&
        github_context["action"] == "labeled" &&
        github_context["label"]["name"] == LABEL
    end

    def unlabeled?
      pull_request? &&
        github_context["action"] == "unlabeled" &&
        github_context["label"]["name"] == LABEL
    end

    def pull_request?
      github_context.has_key?("pull_request")
    end

    def push?
      !github_context.has_key?("pull_request")
    end

    def pr_number
      @pr_number ||= if pull_request?
        github_context["number"]
      else
        pr_from_ref.number
      end
    end

    def pr_from_ref
      @pr_from_ref ||= octokit.pull_requests(
        repo,
        state: "open",
        head: [org_name, ref].join(":")
      ).first.tap{|o| @pr = o if o }
    end

    def pr
      @pr ||= octokit.pull_request(
        repo,
        pr_number
      )
    end
  end
end
