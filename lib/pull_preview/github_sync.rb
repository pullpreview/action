require "octokit"

module PullPreview
  class GithubSync
    attr_reader :github_context
    attr_reader :app_path

    # CLI options, already parsed
    attr_reader :opts
    attr_reader :always_on
    attr_reader :label

    def self.run(app_path, opts)
      github_event_name = ENV.fetch("GITHUB_EVENT_NAME")
      PullPreview.logger.debug "github_event_name = #{github_event_name.inspect}"

      if ["schedule"].include?(github_event_name)
        clear_dangling_deployments(ENV.fetch("GITHUB_REPOSITORY"), app_path, opts)
        clear_outdated_environments(ENV.fetch("GITHUB_REPOSITORY"), app_path, opts)
        return
      end

      github_event_path = ENV.fetch("GITHUB_EVENT_PATH")
      # https://developer.github.com/v3/activity/events/types/#pushevent
      # https://help.github.com/en/actions/reference/events-that-trigger-workflows
      github_context = JSON.parse(File.read(github_event_path))
      PullPreview.logger.debug "github_context = #{github_context.inspect}"
      self.new(github_context, app_path, opts).sync!
    end

    def self.pr_expired?(last_updated_at, ttl)
      ttl = ttl.to_s
      if ttl.end_with?("h")
        return last_updated_at <= Time.now - ttl.sub("h", "").to_i * 3600
      elsif ttl.end_with?("d")
        return last_updated_at <= Time.now - ttl.sub("d", "").to_i * 3600 * 24
      else
        false
      end
    end

    # Go over closed pull requests that are still labelled as "pullpreview", and force the removal of the corresponding environments
    # This happens sometimes, when a pull request is closed, but the environment is not destroyed due to some GitHub Action hiccup.
    def self.clear_dangling_deployments(repo, app_path, opts)
      ttl = opts[:ttl] || "infinite"
      PullPreview.logger.info "[clear_dangling_deployments] start"
      label = opts[:label]
      pr_issues_labeled = PullPreview.octokit.get("repos/#{repo}/issues", labels: label, pulls: true, state: "all", per_page: 100)
      pr_issues_labeled.each do |pr_issue|
        pr = PullPreview.octokit.get(pr_issue.pull_request.url)
        fake_github_context = OpenStruct.new(
          action: "closed",
          number: pr.number,
          pull_request: pr,
          ref: pr.head.ref,
          repository: pr.base.repo,
        )
        if pr.base.repo.owner.type == "Organization"
          fake_github_context.organization = pr.base.repo.owner
        end
        if pr_issue.state == "closed"
          PullPreview.logger.warn "[clear_dangling_deployments] Found dangling #{label} label for PR##{pr.number}. Cleaning up..."
        elsif pr_expired?(pr_issue.updated_at, ttl)
          PullPreview.logger.warn "[clear_dangling_deployments] Found #{label} label for expired PR##{pr.number} (#{pr_issue.updated_at}). Cleaning up..."
        else
          PullPreview.logger.warn "[clear_dangling_deployments] Found #{label} label for active PR##{pr.number} (#{pr_issue.updated_at}). Not touching."
          next
        end
        new(fake_github_context, app_path, opts).sync!
      end

      PullPreview.logger.info "[clear_dangling_deployments] end"
    end

    # Clear any outdated environments, which have no corresponding PR anymore.
    def self.clear_outdated_environments(repo, app_path, opts)
      label = opts[:label]
      environments_to_remove = Set.new
      pr_numbers_with_label_assigned = PullPreview.octokit.get("repos/#{repo}/issues", labels: label, pulls: true, state: "all", per_page: 100).map(&:number)
      PullPreview.octokit.list_environments(repo, per_page: 100).environments.each do |env|
        # regexp must match `pr-`. We don't want to destroy branch environments (`branch-`)
        environment = env.name
        pr_number = environment.match(/gh\-(\d+)\-pr\-(\d+)/)&.captures&.last.to_i
        next if pr_number.zero?
        # don't do anything if the corresponding PR still has the label
        next if pr_numbers_with_label_assigned.include?(pr_number)
        environments_to_remove.add environment
      end

      PullPreview.logger.warn "[clear_outdated_environments] Found #{environments_to_remove.size} environments to remove: #{environments_to_remove}."

      environments_to_remove.each do |environment|
        PullPreview.logger.warn "[clear_outdated_environments] Deleting environment #{environment}..."
        destroy_environment(repo, environment)
        sleep 5
      end
    end

    def self.destroy_environment(repo, environment)
      deploys = PullPreview.octokit.list_deployments(repo, environment: environment, per_page: 100)
      # make sure all deploys are marked as inactive first
      deploys.each do |deployment|
        PullPreview.octokit.create_deployment_status(
          deployment.url,
          "inactive",
          headers: { accept: "application/vnd.github.ant-man-preview+json" }
        )
      end
      deploys.each do |deployment|
        PullPreview.octokit.delete(deployment.url)
      end
      # This requires repository permission, which the GitHub Action token cannot get, so cannot delete the environment unfortunately
      # PullPreview.octokit.delete_environment(repo, environment)
    rescue => e
      PullPreview.logger.warn "Unable to destroy environment #{environment.inspect}: #{e.message}"
    end

    def initialize(github_context, app_path, opts = {})
      @github_context = github_context
      @app_path = app_path
      @label = opts.delete(:label)
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

      pp_action = guess_action_from_event.to_sym
      license = PullPreview::License.new(org_id, repo_id, pp_action, org_slug: org_name, repo_slug: repo_name).fetch!
      PullPreview.logger.info license.message

      unless license.ok?
        raise LicenseError, license.message
      end

      case pp_action
      when :pr_down, :branch_down
        instance = Instance.new(instance_name)
        update_github_status(:destroying)
        tags = default_instance_tags.push(*opts[:tags]).uniq
        if instance.running?
          Down.run(opts.merge(name: instance_name, subdomain: instance_subdomain, tags: tags))

        else
          PullPreview.logger.warn "Instance #{instance_name.inspect} already down. Continuing..."
        end
        if pr_closed?
          PullPreview.logger.info "Removing label #{label} from PR##{pr_number}..."
          begin
            octokit.remove_label(repo, pr_number, label)
          rescue Octokit::NotFound
            # ignore errors when removing absent labels
            true
          end
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
          opts.merge(name: instance_name, subdomain: instance_subdomain, tags: tags, admins: expanded_admins)
        )
        update_github_status(:deployed, instance.url)
      else
        PullPreview.logger.info "Ignoring event #{pp_action.inspect}"
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
      if (pr_unlabeled? && !pr_has_label?) || pr_closed?
        return :pr_down
      end

      if pr_labeled? && pr_has_label?
        return :pr_up
      end

      if push? || pr_synchronize?
        if pr_has_label?
          return :pr_push
        else
          PullPreview.logger.info "Unable to find label #{label} on PR##{pr_number}"
          return :ignored
        end
      end

      :ignored
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
        context: ["PullPreview", deployment_variant].compact.join(" - "),
        description: ["Environment", status].join(" ")
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
          self.class.destroy_environment(repo, instance_name)
        end
      end
    end

    def organization?
      github_context["organization"]
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
      github_context["ref"] || ENV.fetch("GITHUB_REF")
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
        github_context.dig("head_commit", "id") || ENV.fetch("GITHUB_SHA")
      end
    end

    def branch
      if pull_request?
        github_context["pull_request"]["head"]["ref"]
      else
        ref.sub("refs/heads/", "")
      end
    end

    def expanded_admins
      collaborators_with_push = "@collaborators/push"
      admins = opts[:admins].dup.map(&:strip)
      if admins.include?(collaborators_with_push)
        admins.delete(collaborators_with_push)
        admins.push(*octokit.collaborators(repo).select{|c| c.permissions&.push}.map(&:login))
      end
      admins.uniq
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
      github_context["pull_request"]
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
        github_context["label"]["name"] == label
    end

    def pr_unlabeled?
      pull_request? &&
        github_context["action"] == "unlabeled" &&
        github_context["label"]["name"] == label
    end

    def pr_has_label?(searched_label = nil)
      pr.labels.find{|l| l.name.downcase == (searched_label || label).downcase}
    end

    def pr_number
      @pr_number ||= if pull_request?
        github_context["pull_request"]["number"]
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

    def deployment_variant
      variant = opts[:deployment_variant].to_s
      return nil if variant == ""
      raise Error, "--deployment-variant must be 4 chars max" if variant.size > 4
      variant
    end

    def instance_name
      @instance_name ||= begin
        name = if pr_number
          ["gh", repo_id, deployment_variant, "pr", pr_number].compact.join("-")
        else
          # push on branch without PR
          ["gh", repo_id, deployment_variant, "branch", branch].compact.join("-")
        end
        Instance.normalize_name(name)
      end
    end

    def instance_subdomain
      @instance_subdomain ||= begin
        components = []
        components.push(deployment_variant) if deployment_variant
        components.push(*["pr", pr_number]) if pr_number
        components.push(branch.split("/").last)
        Instance.normalize_name(components.join("-"))
      end
    end

    def default_instance_tags
      tags = [
        ["repo_name", repo_name],
        ["repo_id", repo_id],
        ["org_name", org_name],
        ["org_id", org_id],
        ["version", PullPreview::VERSION],
      ]
      if pr_number
        tags << ["pr_number", pr_number]
      end
      tags.map{|tag| tag.join(":")}
    end
  end
end
