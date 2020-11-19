require "octokit"

module PullPreview
  class SyncWithGithub
    attr_reader :github_context
    attr_reader :app_path
    # CLI options, already parsed
    attr_reader :opts
    attr_reader :always_on

    LABEL = "pullpreview"

    def self.run(app_path, opts)
      github_event_path = ENV.fetch("GITHUB_EVENT_PATH")
      # https://developer.github.com/v3/activity/events/types/#pushevent
      # https://help.github.com/en/actions/reference/events-that-trigger-workflows
      github_context = JSON.parse(File.read(github_event_path))
      PullPreview.logger.debug "github_context = #{github_context.inspect}"
      self.new(github_context, app_path, opts).sync!
    end

    def self.clear_deployments_for(repo, environment, force: false)
      deploys = PullPreview.octokit.list_deployments(repo, environment: environment, per_page: 100)
      if force
        # make sure all deploys are marked as inactive first
        deploys.each do |deployment|
          PullPreview.octokit.create_deployment_status(
            deployment.url,
            "inactive",
            headers: { accept: "application/vnd.github.ant-man-preview+json" }
          )
        end
      end
      deploys.each do |deployment|
        PullPreview.octokit.delete(deployment.url)
      end
    rescue => e
      PullPreview.logger.warn "Unable to clear deployments for environment #{environment.inspect}: #{e.message}"
    end

    def initialize(github_context, app_path, opts)
      @github_context = github_context
      @app_path = app_path
      @opts = opts
      @always_on = opts.delete(:always_on)
    end

    def octokit
      PullPreview.octokit
    end

    def sync!
      if sha != latest_sha && !ENV.fetch("PULLPREVIEW_TEST", nil)
        PullPreview.logger.info "A newer commit is present. Skipping current run."
        return true
      end

      pp_action = guess_action_from_event
      license = PullPreview::License.new(org_id, repo_id, pp_action).fetch!
      PullPreview.logger.info license.message

      case pp_action
      when :pr_down, :branch_down
        instance = Instance.new(instance_name)
        unless instance.running?
          PullPreview.logger.info "Ignoring event"
          return true
        end
        update_github_status(:destroying)
        Down.run(name: instance_name)
        if pr_closed?
          PullPreview.logger.info "Removing label #{LABEL} from PR##{pr_number}..."
          octokit.remove_label(repo, pr_number, LABEL)
        end
        update_github_status(:destroyed)
      when :pr_up, :pr_push, :branch_push
        unless license.ok?
          raise LicenseError, license.message
        end
        update_github_status(:deploying)
        tags = default_instance_tags.push(*opts[:tags]).uniq
        instance = Up.run(
          app_path,
          opts.merge(name: instance_name, subdomain: instance_subdomain, tags: tags)
        )
        update_github_status(:deployed, instance.url)
      else
        PullPreview.logger.info "Ignoring event"
      end
    rescue => e
      update_github_status(:error)
      raise e
    end

    def guess_action_from_event
      if pr_number.nil?
        branch = ref.sub("refs/heads/", "")
        if always_on.include?(branch)
          return :branch_push
        else
          return :branch_down
        end
      end

      # In case of labeled & unlabeled, we recheck what the PR currently has for
      # labels since actions don't execute in the order they are triggered
      if (pr_unlabeled? && !pr_has_label?(LABEL)) || (pr_closed? && pr_has_label?(LABEL))
        return :pr_down
      end

      if pr_labeled? && pr_has_label?(LABEL)
        return :pr_up
      end

      if push? || pr_synchronize?
        if pr_has_label?(LABEL)
          action = :pr_push
        else
          PullPreview.logger.info "Unable to find label #{LABEL} on PR##{pr_number}"
          return :ignored
        end
      end
    end

    def commit_status_for(status)
      case status
      when :error
        :error
      when :deployed, :destroyed
        :success
      when :deploying, :destroying
        :pending
      end
    end

    def deployment_status_for(status)
      case status
      when :error
        :error
      when :deployed
        :success
      when :destroyed
        :inactive
      when :deploying
        :pending
      when :destroying
        nil
      end
    end

    def update_github_status(status, url = nil)
      commit_status = commit_status_for(status)
      # https://developer.github.com/v3/repos/statuses/#create-a-status
      commit_status_params = {
        context: "PullPreview",
        description: "Environment #{status.to_s}"
      }
      commit_status_params.merge!(target_url: url) if url
      PullPreview.logger.info "Setting commit status for repo=#{repo.inspect}, sha=#{sha.inspect}, status=#{commit_status.inspect}, params=#{commit_status_params.inspect}"
      octokit.create_status(
        repo,
        sha,
        commit_status.to_s,
        commit_status_params
      )

      deployment_status = deployment_status_for(status)
      unless deployment_status.nil?
        deployment_status_params = {
          headers: {accept: "application/vnd.github.ant-man-preview+json"},
          auto_inactive: true
        }
        deployment_status_params.merge!(environment_url: url) if url
        PullPreview.logger.info "Setting deployment status for repo=#{repo.inspect}, branch=#{branch.inspect}, sha=#{sha.inspect}, status=#{deployment_status.inspect}, params=#{deployment_status_params.inspect}"
        octokit.create_deployment_status(deployment.url, deployment_status.to_s, deployment_status_params)
        if status == :destroyed
          self.class.clear_deployments_for(repo, instance_name)
        end
      end
    end

    def organization?
      github_context.has_key?("organization")
    end

    def org_name
      if organization?
        github_context["organization"]["login"]
      else
        github_context["repository"]["owner"]["login"]
      end
    end

    def repo_name
      github_context["repository"]["name"]
    end

    def repo
      [org_name, repo_name].join("/")
    end

    def repo_id
      github_context["repository"]["id"]
    end

    def org_id
      if organization?
        github_context["organization"]["id"]
      else
        github_context["repository"]["owner"]["id"]
      end
    end

    def ref
      github_context["ref"]
    end

    def latest_sha
      @latest_sha ||= if pull_request?
        pr.head.sha
      else
        octokit.list_commits(repo, ref).first.sha
      end
    end

    def sha
      if pull_request?
        github_context["pull_request"]["head"]["sha"]
      else
        github_context["head_commit"]["id"]
      end
    end

    def branch
      if pull_request?
        github_context["pull_request"]["head"]["ref"]
      else
        github_context["ref"].sub("refs/heads/", "")
      end
    end

    def deployment
      @deployment ||= (find_deployment || create_deployment)
    end

    def find_deployment
      octokit.list_deployments(repo, environment: instance_name, ref: sha).first
    end

    def create_deployment
      octokit.create_deployment(
        repo,
        sha,
        auto_merge: false,
        environment: instance_name,
        required_contexts: []
      )
    end

    def pull_request?
      github_context.has_key?("pull_request")
    end

    def push?
      !pull_request?
    end

    def pr_synchronize?
      pull_request? &&
        github_context["action"] == "synchronize"
    end

    def pr_closed?
      pull_request? &&
        github_context["action"] == "closed"
    end

    def pr_labeled?
      pull_request? &&
        github_context["action"] == "labeled" &&
        github_context["label"]["name"] == LABEL
    end

    def pr_unlabeled?
      pull_request? &&
        github_context["action"] == "unlabeled" &&
        github_context["label"]["name"] == LABEL
    end

    def pr_has_label?(searched_label)
      pr.labels.find{|label| label.name.downcase == searched_label.downcase}
    end

    def pr_number
      @pr_number ||= if pull_request?
        github_context["number"]
      elsif pr_from_ref
        pr_from_ref[:number]
      else
        nil
      end
    end

    # only used to retrieve the PR when the event is push
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

    def instance_name
      @instance_name ||= begin
        name = if pr_number
          ["gh", repo_id, "pr", pr_number].join("-")
        else
          # push on branch without PR
          ["gh", repo_id, "branch", branch].join("-")
        end
        Instance.normalize_name(name)
      end
    end

    def instance_subdomain
      Instance.normalize_name(["gh", repo_id, "branch", branch].join("-"))
    end

    def default_instance_tags
      [
        ["repo_name", repo_name],
        ["repo_id", repo_id],
        ["org_name", org_name],
        ["org_id", org_id],
        ["version", PullPreview::VERSION],
      ].map{|tag| tag.join(":")}
    end
  end
end
