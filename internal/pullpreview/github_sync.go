package pullpreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	gh "github.com/google/go-github/v60/github"
	ghclient "github.com/pullpreview/action/internal/github"
	"github.com/pullpreview/action/internal/license"
)

var (
	newGitHubClient = func(ctx context.Context, token string) GitHubAPI {
		return ghclient.NewWithContext(EnsureContext(ctx), token)
	}
	runUpFunc   = RunUp
	runDownFunc = RunDown
)

type GitHubAPI interface {
	ListIssues(repo, label string) ([]*gh.Issue, error)
	GetPullRequest(repo string, number int) (*gh.PullRequest, error)
	ListEnvironments(repo string) ([]*gh.Environment, error)
	ListDeployments(repo, environment, ref string) ([]*gh.Deployment, error)
	CreateDeployment(repo, ref, environment string) (*gh.Deployment, error)
	CreateDeploymentStatus(repo string, deploymentID int64, state string, environmentURL string, autoInactive bool) error
	CreateCommitStatus(repo, sha, state, targetURL, context, description string) error
	DeleteDeployment(repo string, deploymentID int64) error
	DeleteEnvironment(repo, name string) error
	RemoveLabel(repo string, number int, label string) error
	ListIssueComments(repo string, number int) ([]*gh.IssueComment, error)
	CreateIssueComment(repo string, number int, body string) error
	UpdateIssueComment(repo string, commentID int64, body string) error
	ListPullRequests(repo, head string) ([]*gh.PullRequest, error)
	LatestCommitSHA(repo, ref string) (string, error)
	ListCollaborators(repo string) ([]*gh.User, bool, error)
	ListUserPublicKeys(user string) ([]string, error)
}

type GitHubEvent struct {
	Action       string        `json:"action"`
	Label        *GitHubLabel  `json:"label"`
	PullRequest  *GitHubPR     `json:"pull_request"`
	Repository   GitHubRepo    `json:"repository"`
	Organization *GitHubOrg    `json:"organization"`
	Ref          string        `json:"ref"`
	HeadCommit   *GitHubCommit `json:"head_commit"`
	Number       int           `json:"number"`
}

type GitHubCommit struct {
	ID string `json:"id"`
}

type GitHubLabel struct {
	Name string `json:"name"`
}

type GitHubOrg struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
	Type  string `json:"type"`
}

type GitHubRepo struct {
	ID    int64     `json:"id"`
	Name  string    `json:"name"`
	Owner GitHubOrg `json:"owner"`
}

type GitHubPR struct {
	Number int           `json:"number"`
	Head   GitHubPRHead  `json:"head"`
	Base   GitHubPRBase  `json:"base"`
	Labels []GitHubLabel `json:"labels"`
}

type GitHubPRHead struct {
	SHA string `json:"sha"`
	Ref string `json:"ref"`
}

type GitHubPRBase struct {
	Repo GitHubRepo `json:"repo"`
}

type GithubSync struct {
	event    GitHubEvent
	appPath  string
	opts     GithubSyncOptions
	client   GitHubAPI
	provider Provider
	logger   *Logger
	prCache  *gh.PullRequest
	runUp    func(UpOptions, Provider, *Logger) (*Instance, error)
	runDown  func(DownOptions, Provider, *Logger) error
}

func RunGithubSync(opts GithubSyncOptions, provider Provider, logger *Logger) error {
	opts.Context = EnsureContext(opts.Context)
	if opts.Common.Context == nil {
		opts.Common.Context = opts.Context
	} else {
		opts.Common.Context = EnsureContext(opts.Common.Context)
	}
	eventName := os.Getenv("GITHUB_EVENT_NAME")
	if logger != nil {
		logger.Debugf("github_event_name=%s", eventName)
	}
	repo := os.Getenv("GITHUB_REPOSITORY")
	if eventName == "schedule" {
		return runScheduledCleanup(repo, opts, provider, logger)
	}
	path := os.Getenv("GITHUB_EVENT_PATH")
	if path == "" {
		return errors.New("GITHUB_EVENT_PATH not set")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var event GitHubEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}

	client := newGitHubClient(opts.Context, os.Getenv("GITHUB_TOKEN"))
	sync := &GithubSync{event: event, appPath: opts.AppPath, opts: opts, client: client, provider: provider, logger: logger, runUp: runUpFunc, runDown: runDownFunc}
	return sync.Sync()
}

func runScheduledCleanup(repo string, opts GithubSyncOptions, provider Provider, logger *Logger) error {
	opts.Context = EnsureContext(opts.Context)
	client := newGitHubClient(opts.Context, os.Getenv("GITHUB_TOKEN"))
	if logger != nil {
		logger.Infof("[clear_dangling_deployments] start")
	}
	if err := clearDanglingDeployments(repo, opts, provider, client, logger); err != nil {
		return err
	}
	if logger != nil {
		logger.Infof("[clear_outdated_environments] start")
	}
	if err := clearOutdatedEnvironments(repo, opts, provider, client, logger); err != nil {
		return err
	}
	if logger != nil {
		logger.Infof("[clear_outdated_environments] end")
	}
	return nil
}

func prExpired(updatedAt time.Time, ttl string) bool {
	ttl = strings.TrimSpace(ttl)
	if strings.HasSuffix(ttl, "h") {
		hours := mustParseInt(strings.TrimSuffix(ttl, "h"))
		if hours <= 0 {
			return false
		}
		return updatedAt.Before(time.Now().Add(-time.Duration(hours) * time.Hour))
	}
	if strings.HasSuffix(ttl, "d") {
		days := mustParseInt(strings.TrimSuffix(ttl, "d"))
		if days <= 0 {
			return false
		}
		return updatedAt.Before(time.Now().Add(-time.Duration(days) * 24 * time.Hour))
	}
	return false
}

func clearDanglingDeployments(repo string, opts GithubSyncOptions, provider Provider, client GitHubAPI, logger *Logger) error {
	ttl := opts.TTL
	if ttl == "" {
		ttl = "infinite"
	}
	issues, err := client.ListIssues(repo, opts.Label)
	if err != nil {
		return err
	}
	for _, issue := range issues {
		if issue.PullRequestLinks == nil {
			continue
		}
		pr, err := client.GetPullRequest(repo, issue.GetNumber())
		if err != nil {
			continue
		}
		fake := eventFromPR(pr)
		if issue.GetState() == "closed" {
			if logger != nil {
				logger.Warnf("[clear_dangling_deployments] Found dangling %s label for PR#%d. Cleaning up...", opts.Label, pr.GetNumber())
			}
		} else if prExpired(issue.GetUpdatedAt().Time, ttl) {
			if logger != nil {
				logger.Warnf("[clear_dangling_deployments] Found %s label for expired PR#%d (%s). Cleaning up...", opts.Label, pr.GetNumber(), issue.GetUpdatedAt().String())
			}
		} else {
			if logger != nil {
				logger.Warnf("[clear_dangling_deployments] Found %s label for active PR#%d (%s). Not touching.", opts.Label, pr.GetNumber(), issue.GetUpdatedAt().String())
			}
			continue
		}
		sync := &GithubSync{event: fake, appPath: opts.AppPath, opts: opts, client: client, provider: provider, logger: logger, runUp: runUpFunc, runDown: runDownFunc}
		_ = sync.Sync()
	}
	if logger != nil {
		logger.Infof("[clear_dangling_deployments] end")
	}
	return nil
}

func clearOutdatedEnvironments(repo string, opts GithubSyncOptions, provider Provider, client GitHubAPI, logger *Logger) error {
	envs, err := client.ListEnvironments(repo)
	if err != nil {
		return err
	}
	issues, err := client.ListIssues(repo, opts.Label)
	if err != nil {
		return err
	}
	labelledPRs := map[int]struct{}{}
	for _, issue := range issues {
		if issue.PullRequestLinks != nil {
			labelledPRs[issue.GetNumber()] = struct{}{}
		}
	}
	toRemove := []string{}
	for _, env := range envs {
		name := env.GetName()
		prNumber := parsePRNumber(name)
		if prNumber == 0 {
			continue
		}
		if _, ok := labelledPRs[prNumber]; ok {
			continue
		}
		toRemove = append(toRemove, name)
	}
	if logger != nil {
		logger.Warnf("[clear_outdated_environments] Found %d environments to remove: %v.", len(toRemove), toRemove)
	}
	for _, env := range toRemove {
		if logger != nil {
			logger.Warnf("[clear_outdated_environments] Deleting environment %s...", env)
		}
		destroyEnvironment(repo, env, client, logger)
		time.Sleep(5 * time.Second)
	}
	return nil
}

func parsePRNumber(env string) int {
	parts := strings.Split(env, "-")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "pr" {
			return mustParseInt(parts[i+1])
		}
	}
	return 0
}

func mustParseInt(value string) int {
	result := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0
		}
		result = result*10 + int(r-'0')
	}
	return result
}

func destroyEnvironment(repo, environment string, client GitHubAPI, logger *Logger) {
	deploys, err := client.ListDeployments(repo, environment, "")
	if err == nil {
		for _, dep := range deploys {
			_ = client.CreateDeploymentStatus(repo, dep.GetID(), "inactive", "", true)
		}
		for _, dep := range deploys {
			_ = client.DeleteDeployment(repo, dep.GetID())
		}
	}
	if err := client.DeleteEnvironment(repo, environment); err != nil {
		if logger != nil {
			logger.Warnf("Unable to destroy environment %s: %v. This usually means the token lacks environment admin permission. Provide a stronger github_token input or delete it manually.", environment, err)
		}
	}
}

func eventFromPR(pr *gh.PullRequest) GitHubEvent {
	owner := pr.Base.Repo.Owner
	repository := GitHubRepo{ID: pr.Base.Repo.GetID(), Name: pr.Base.Repo.GetName(), Owner: GitHubOrg{Login: owner.GetLogin(), ID: owner.GetID(), Type: owner.GetType()}}
	var org *GitHubOrg
	if strings.EqualFold(owner.GetType(), "Organization") {
		org = &GitHubOrg{Login: owner.GetLogin(), ID: owner.GetID(), Type: owner.GetType()}
	}
	labels := []GitHubLabel{}
	for _, label := range pr.Labels {
		labels = append(labels, GitHubLabel{Name: label.GetName()})
	}
	return GitHubEvent{
		Action:       "closed",
		PullRequest:  &GitHubPR{Number: pr.GetNumber(), Head: GitHubPRHead{SHA: pr.Head.GetSHA(), Ref: pr.Head.GetRef()}, Base: GitHubPRBase{Repo: repository}, Labels: labels},
		Repository:   repository,
		Organization: org,
		Ref:          pr.Head.GetRef(),
		Number:       pr.GetNumber(),
	}
}

func (g *GithubSync) Sync() error {
	if g.runUp == nil {
		g.runUp = runUpFunc
	}
	if g.runDown == nil {
		g.runDown = runDownFunc
	}
	latest := g.latestSHA()
	if latest != "" && g.sha() != latest && os.Getenv("PULLPREVIEW_TEST") == "" {
		if g.logger != nil {
			g.logger.Infof("A newer commit is present. Skipping current run.")
		}
		return nil
	}
	if err := g.validateDeploymentVariant(); err != nil {
		return err
	}
	action := g.guessAction()
	if action == actionIgnored {
		if g.logger != nil {
			g.logger.Infof("Ignoring event %s", action)
		}
		return nil
	}
	if os.Getenv("PULLPREVIEW_TEST") == "" {
		licenseClient := license.New()
		lic, _ := licenseClient.Check(map[string]string{
			"org_id":    fmt.Sprintf("%d", g.orgID()),
			"repo_id":   fmt.Sprintf("%d", g.repoID()),
			"pp_action": string(action),
			"org_slug":  g.orgName(),
			"repo_slug": g.repoName(),
		})
		if g.logger != nil {
			g.logger.Infof(lic.Message)
		}
		if !lic.OK() {
			return errors.New(lic.Message)
		}
	}

	switch action {
	case actionPRDown, actionBranchDown:
		instance := NewInstance(g.instanceName(), g.opts.Common, g.provider, g.logger)
		_ = g.updateGitHubStatus(statusDestroying, "")
		running, _ := instance.Running()
		if running {
			if g.runDown != nil {
				_ = g.runDown(DownOptions{Name: instance.Name}, g.provider, g.logger)
			}
		} else if g.logger != nil {
			g.logger.Warnf("Instance %s already down. Continuing...", instance.Name)
		}
		if g.prClosed() {
			if g.logger != nil {
				g.logger.Infof("Removing label %s from PR#%d...", g.opts.Label, g.prNumber())
			}
			_ = g.client.RemoveLabel(g.repo(), g.prNumber(), g.opts.Label)
		}
		_ = g.updateGitHubStatus(statusDestroyed, "")
		g.writeStepSummary(statusDestroyed, action, "", nil)
	case actionPRUp, actionPRPush, actionBranchPush:
		_ = g.updateGitHubStatus(statusDeploying, "")
		instance := g.buildInstance()
		var upInstance *Instance
		var err error
		if g.runUp != nil {
			upInstance, err = g.runUp(UpOptions{AppPath: g.appPath, Name: instance.Name, Subdomain: instance.Subdomain, Common: instanceToCommon(instance)}, g.provider, g.logger)
		}
		if err != nil {
			_ = g.updateGitHubStatus(statusError, "")
			g.writeStepSummary(statusError, action, "", nil)
			return err
		}
		if upInstance != nil {
			_ = g.updateGitHubStatus(statusDeployed, upInstance.URL())
			g.writeStepSummary(statusDeployed, action, upInstance.URL(), upInstance)
		}
	}
	return nil
}

type actionType string

const (
	actionIgnored    actionType = "ignored"
	actionPRDown     actionType = "pr_down"
	actionBranchDown actionType = "branch_down"
	actionPRUp       actionType = "pr_up"
	actionPRPush     actionType = "pr_push"
	actionBranchPush actionType = "branch_push"
)

type deploymentStatus string

const (
	statusError      deploymentStatus = "error"
	statusDeployed   deploymentStatus = "deployed"
	statusDestroyed  deploymentStatus = "destroyed"
	statusDeploying  deploymentStatus = "deploying"
	statusDestroying deploymentStatus = "destroying"
)

func (g *GithubSync) guessAction() actionType {
	if g.prNumber() == 0 {
		branch := strings.TrimPrefix(g.ref(), "refs/heads/")
		if containsString(g.opts.AlwaysOn, branch) {
			return actionBranchPush
		}
		return actionBranchDown
	}

	if (g.prUnlabeled() && !g.prHasLabel("")) || g.prClosed() {
		return actionPRDown
	}
	if (g.prOpened() || g.prReopened() || g.prLabeled()) && g.prHasLabel("") {
		return actionPRUp
	}
	if g.push() || g.prSynchronize() {
		if g.prHasLabel("") {
			return actionPRPush
		}
		if g.logger != nil {
			g.logger.Infof("Unable to find label %s on PR#%d", g.opts.Label, g.prNumber())
		}
		return actionIgnored
	}
	return actionIgnored
}

func (g *GithubSync) commitStatusFor(status deploymentStatus) string {
	switch status {
	case statusError:
		return "error"
	case statusDeployed, statusDestroyed:
		return "success"
	case statusDeploying, statusDestroying:
		return "pending"
	default:
		return "pending"
	}
}

func (g *GithubSync) deploymentStatusFor(status deploymentStatus) string {
	switch status {
	case statusError:
		return "error"
	case statusDeployed:
		return "success"
	case statusDestroyed:
		return "inactive"
	case statusDeploying:
		return "pending"
	case statusDestroying:
		return ""
	default:
		return ""
	}
}

func (g *GithubSync) updateGitHubStatus(status deploymentStatus, url string) error {
	commitStatus := g.commitStatusFor(status)
	context := "PullPreview"
	if g.deploymentVariant() != "" {
		context = fmt.Sprintf("PullPreview - %s", g.deploymentVariant())
	}
	description := fmt.Sprintf("Environment %s", status)
	if g.logger != nil {
		g.logger.Infof("Setting commit status repo=%s sha=%s status=%s", g.repo(), g.sha(), commitStatus)
	}
	_ = g.client.CreateCommitStatus(g.repo(), g.sha(), commitStatus, url, context, description)
	g.updatePRComment(status, url)

	deploymentStatus := g.deploymentStatusFor(status)
	if deploymentStatus == "" {
		return nil
	}
	if g.logger != nil {
		g.logger.Infof("Setting deployment status repo=%s branch=%s sha=%s status=%s", g.repo(), g.branch(), g.sha(), deploymentStatus)
	}
	deployment := g.deployment()
	_ = g.client.CreateDeploymentStatus(g.repo(), deployment.GetID(), deploymentStatus, url, true)
	if status == statusDestroyed {
		destroyEnvironment(g.repo(), g.instanceName(), g.client, g.logger)
	}
	return nil
}

func (g *GithubSync) updatePRComment(status deploymentStatus, previewURL string) {
	if !g.opts.CommentPR || g.prNumber() == 0 {
		return
	}
	body := g.renderPRComment(status, previewURL)
	if body == "" {
		return
	}
	comments, err := g.client.ListIssueComments(g.repo(), g.prNumber())
	if err != nil {
		if g.logger != nil {
			g.logger.Warnf("Unable to list PR comments for PR#%d: %v", g.prNumber(), err)
		}
		return
	}
	marker := g.prCommentMarker()
	for _, comment := range comments {
		commentBody := comment.GetBody()
		if strings.Contains(commentBody, marker) {
			if err := g.client.UpdateIssueComment(g.repo(), comment.GetID(), body); err != nil && g.logger != nil {
				g.logger.Warnf("Unable to update PR comment for PR#%d: %v", g.prNumber(), err)
			}
			return
		}
	}
	if err := g.client.CreateIssueComment(g.repo(), g.prNumber(), body); err != nil && g.logger != nil {
		g.logger.Warnf("Unable to create PR comment for PR#%d: %v", g.prNumber(), err)
	}
}

func (g *GithubSync) prCommentMarker() string {
	key := g.instanceName()
	if job := g.jobKey(); job != "" {
		key = fmt.Sprintf("%s:%s", key, job)
	}
	return fmt.Sprintf("<!-- pullpreview-status:%s -->", key)
}

func (g *GithubSync) jobName() string {
	return strings.TrimSpace(os.Getenv("GITHUB_JOB"))
}

func (g *GithubSync) jobKey() string {
	job := g.jobName()
	if job == "" {
		return ""
	}
	key := NormalizeName(job)
	if key == "" {
		return "job"
	}
	return key
}

func (g *GithubSync) renderPRComment(status deploymentStatus, previewURL string) string {
	statusText := ""
	switch status {
	case statusDeploying:
		statusText = "⏳ Deploying preview..."
	case statusDeployed:
		statusText = "✅ Deploy successful"
	case statusError:
		statusText = "❌ Deploy failed"
	case statusDestroying:
		statusText = "🧹 Destroying preview..."
	case statusDestroyed:
		statusText = "🗑️ Preview destroyed"
	default:
		return ""
	}
	commit := g.sha()
	if len(commit) > 7 {
		commit = commit[:7]
	}
	preview := "_Pending_"
	if status == statusDestroying {
		preview = "_Destroying_"
	}
	if status == statusDestroyed {
		preview = "_Destroyed_"
	}
	if strings.TrimSpace(previewURL) != "" {
		preview = fmt.Sprintf("[%s](%s)", previewURL, previewURL)
	}
	logs := g.workflowRunURL()
	logsLine := ""
	if logs != "" {
		logsLine = fmt.Sprintf("\n[View logs](%s)\n", logs)
	}
	variantRow := ""
	if variant := g.deploymentVariant(); variant != "" {
		variantRow = fmt.Sprintf("| Variant | `%s` |\n", variant)
	}
	jobRow := ""
	if job := g.jobName(); job != "" {
		jobRow = fmt.Sprintf("| Job | `%s` |\n", job)
	}
	title := fmt.Sprintf("### Deploying %s with [⚡](https://pullpreview.com) PullPreview", g.repoName())
	return fmt.Sprintf(
		"%s\n%s\n\n| Field | Value |\n|---|---|\n| Latest commit | `%s` |\n%s%s| Status | %s |\n| Preview URL | %s |\n%s",
		g.prCommentMarker(),
		title,
		commit,
		variantRow,
		jobRow,
		statusText,
		preview,
		logsLine,
	)
}

func (g *GithubSync) deploymentEnvironmentURL() string {
	server := strings.TrimSuffix(os.Getenv("GITHUB_SERVER_URL"), "/")
	if server == "" {
		return ""
	}
	return fmt.Sprintf(
		"%s/%s/deployments/activity_log?environment=%s",
		server,
		g.repo(),
		url.QueryEscape(g.instanceName()),
	)
}

func (g *GithubSync) statusSummaryText(status deploymentStatus) string {
	switch status {
	case statusDeploying:
		return "Deploying preview"
	case statusDeployed:
		return "Deploy successful"
	case statusError:
		return "Deploy failed"
	case statusDestroying:
		return "Destroying preview"
	case statusDestroyed:
		return "Preview destroyed"
	default:
		return "Status unknown"
	}
}

func (g *GithubSync) renderStepSummary(status deploymentStatus, action actionType, previewURL string, inst *Instance) string {
	commit := g.sha()
	if len(commit) > 7 {
		commit = commit[:7]
	}

	var b strings.Builder
	b.WriteString("## PullPreview Summary\n\n")
	b.WriteString(fmt.Sprintf("- Repository: `%s`\n", g.repo()))
	b.WriteString(fmt.Sprintf("- Branch: `%s`\n", g.branch()))
	b.WriteString(fmt.Sprintf("- Commit: `%s`\n", commit))
	b.WriteString(fmt.Sprintf("- Action: `%s`\n", action))
	b.WriteString(fmt.Sprintf("- Status: `%s`\n", g.statusSummaryText(status)))

	if strings.TrimSpace(previewURL) != "" {
		b.WriteString(fmt.Sprintf("- Preview URL: [%s](%s)\n", previewURL, previewURL))
	}
	if deploymentURL := g.deploymentEnvironmentURL(); deploymentURL != "" {
		b.WriteString(fmt.Sprintf("- Deployment: [%s](%s)\n", g.instanceName(), deploymentURL))
	}
	if logs := g.workflowRunURL(); logs != "" {
		b.WriteString(fmt.Sprintf("- Logs: [%s](%s)\n", logs, logs))
	}

	if inst != nil && status == statusDeployed {
		b.WriteString(fmt.Sprintf("- SSH Username: `%s`\n", inst.Username()))
		b.WriteString(fmt.Sprintf("- SSH IP: `%s`\n", inst.PublicIP()))
		b.WriteString(fmt.Sprintf("- SSH Command: `ssh %s`\n", inst.SSHAddress()))
	}

	b.WriteString("\nPowered by [PullPreview](https://pullpreview.com).\n")
	return b.String()
}

func (g *GithubSync) writeStepSummary(status deploymentStatus, action actionType, previewURL string, inst *Instance) {
	path := strings.TrimSpace(os.Getenv("GITHUB_STEP_SUMMARY"))
	if path == "" {
		return
	}
	content := g.renderStepSummary(status, action, previewURL, inst)
	if strings.TrimSpace(content) == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		if g.logger != nil {
			g.logger.Warnf("Unable to open GITHUB_STEP_SUMMARY file: %v", err)
		}
		return
	}
	defer f.Close()
	if _, err := f.WriteString(content + "\n"); err != nil && g.logger != nil {
		g.logger.Warnf("Unable to write GITHUB_STEP_SUMMARY: %v", err)
	}
}

func (g *GithubSync) workflowRunURL() string {
	server := strings.TrimSuffix(os.Getenv("GITHUB_SERVER_URL"), "/")
	runID := strings.TrimSpace(os.Getenv("GITHUB_RUN_ID"))
	if server == "" || runID == "" {
		return ""
	}
	return fmt.Sprintf("%s/%s/actions/runs/%s", server, g.repo(), runID)
}

func (g *GithubSync) deployment() *gh.Deployment {
	if g.prCache != nil {
		_ = g.prCache
	}
	deploys, err := g.client.ListDeployments(g.repo(), g.instanceName(), g.sha())
	if err == nil && len(deploys) > 0 {
		return deploys[0]
	}
	dep, _ := g.client.CreateDeployment(g.repo(), g.sha(), g.instanceName())
	return dep
}

func (g *GithubSync) orgName() string {
	if g.event.Organization != nil && g.event.Organization.Login != "" {
		return g.event.Organization.Login
	}
	return g.event.Repository.Owner.Login
}

func (g *GithubSync) repoName() string {
	return g.event.Repository.Name
}

func (g *GithubSync) repo() string {
	return fmt.Sprintf("%s/%s", g.orgName(), g.repoName())
}

func (g *GithubSync) repoID() int64 {
	return g.event.Repository.ID
}

func (g *GithubSync) orgID() int64 {
	if g.event.Organization != nil && g.event.Organization.ID != 0 {
		return g.event.Organization.ID
	}
	return g.event.Repository.Owner.ID
}

func (g *GithubSync) ref() string {
	if g.event.Ref != "" {
		return g.event.Ref
	}
	return os.Getenv("GITHUB_REF")
}

func (g *GithubSync) latestSHA() string {
	if g.pullRequest() {
		return g.event.PullRequest.Head.SHA
	}
	sha, _ := g.client.LatestCommitSHA(g.repo(), g.ref())
	return sha
}

func (g *GithubSync) sha() string {
	if g.pullRequest() {
		return g.event.PullRequest.Head.SHA
	}
	if g.event.HeadCommit != nil {
		return g.event.HeadCommit.ID
	}
	return os.Getenv("GITHUB_SHA")
}

func (g *GithubSync) branch() string {
	if g.pullRequest() {
		return g.event.PullRequest.Head.Ref
	}
	return strings.TrimPrefix(g.ref(), "refs/heads/")
}

func (g *GithubSync) expandedAdmins() []string {
	admins, _ := g.expandedAdminsAndKeys()
	return admins
}

func (g *GithubSync) expandedAdminsAndKeys() ([]string, []string) {
	admins := append([]string{}, g.opts.Common.Admins...)
	final := []string{}
	keyLogins := []string{}
	collaboratorsToken := "@collaborators/push"
	needCollabs := false
	for _, admin := range admins {
		admin = strings.TrimSpace(admin)
		if admin == collaboratorsToken {
			needCollabs = true
			continue
		}
		if admin != "" {
			final = append(final, admin)
			keyLogins = append(keyLogins, admin)
		}
	}
	if needCollabs {
		collabs, truncated, err := g.client.ListCollaborators(g.repo())
		if err == nil {
			for _, user := range collabs {
				if user.Permissions == nil || user.Permissions["push"] {
					login := strings.TrimSpace(user.GetLogin())
					if login == "" {
						continue
					}
					final = append(final, login)
					keyLogins = append(keyLogins, login)
				}
			}
			if truncated && g.logger != nil {
				g.logger.Warnf("Found more than 100 collaborators with push access. Only the first 100 will receive SSH access.")
			}
		} else if g.logger != nil {
			g.logger.Warnf("Unable to list collaborators for %s: %v", g.repo(), err)
		}
	}
	final = uniqueStrings(final)
	keys := []string{}
	for _, login := range uniqueStrings(keyLogins) {
		userKeys, err := g.userPublicKeys(login)
		if err != nil {
			if g.logger != nil {
				g.logger.Warnf("Unable to resolve SSH keys for %s: %v", login, err)
			}
			continue
		}
		keys = append(keys, userKeys...)
	}
	return final, uniqueStrings(keys)
}

var sshKeyCacheFilenameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func (g *GithubSync) userPublicKeys(login string) ([]string, error) {
	login = strings.TrimSpace(login)
	if login == "" {
		return nil, nil
	}
	if keys, ok := g.loadCachedUserPublicKeys(login); ok {
		return keys, nil
	}
	keys, err := g.client.ListUserPublicKeys(login)
	if err != nil {
		return nil, err
	}
	keys = uniqueStrings(keys)
	if len(keys) == 0 {
		return keys, nil
	}
	_ = g.saveCachedUserPublicKeys(login, keys)
	return keys, nil
}

func (g *GithubSync) sshKeysCacheDir() string {
	return strings.TrimSpace(os.Getenv("PULLPREVIEW_SSH_KEYS_CACHE_DIR"))
}

func (g *GithubSync) sshKeysCachePath(login string) string {
	dir := g.sshKeysCacheDir()
	if dir == "" {
		return ""
	}
	filename := sshKeyCacheFilenameSanitizer.ReplaceAllString(strings.ToLower(strings.TrimSpace(login)), "-")
	filename = strings.Trim(filename, "-")
	if filename == "" {
		return ""
	}
	return filepath.Join(dir, filename+".keys")
}

func (g *GithubSync) loadCachedUserPublicKeys(login string) ([]string, bool) {
	path := g.sshKeysCachePath(login)
	if path == "" {
		return nil, false
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	keys := []string{}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			keys = append(keys, line)
		}
	}
	keys = uniqueStrings(keys)
	if len(keys) == 0 {
		return nil, false
	}
	if g.logger != nil {
		g.logger.Debugf("Loaded %d SSH key(s) for %s from cache", len(keys), login)
	}
	return keys, true
}

func (g *GithubSync) saveCachedUserPublicKeys(login string, keys []string) error {
	path := g.sshKeysCachePath(login)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	content := strings.Join(uniqueStrings(keys), "\n")
	if content != "" {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	if g.logger != nil {
		g.logger.Debugf("Cached %d SSH key(s) for %s", len(keys), login)
	}
	return nil
}

func (g *GithubSync) pullRequest() bool {
	return g.event.PullRequest != nil
}

func (g *GithubSync) push() bool {
	return !g.pullRequest()
}

func (g *GithubSync) prSynchronize() bool {
	return g.pullRequest() && g.event.Action == "synchronize"
}

func (g *GithubSync) prOpened() bool {
	return g.pullRequest() && g.event.Action == "opened"
}

func (g *GithubSync) prReopened() bool {
	return g.pullRequest() && g.event.Action == "reopened"
}

func (g *GithubSync) prClosed() bool {
	return g.pullRequest() && g.event.Action == "closed"
}

func (g *GithubSync) prLabeled() bool {
	return g.pullRequest() && g.event.Action == "labeled" && g.event.Label != nil && strings.EqualFold(g.event.Label.Name, g.opts.Label)
}

func (g *GithubSync) prUnlabeled() bool {
	return g.pullRequest() && g.event.Action == "unlabeled" && g.event.Label != nil && strings.EqualFold(g.event.Label.Name, g.opts.Label)
}

func (g *GithubSync) prHasLabel(searchedLabel string) bool {
	if g.pr() == nil {
		return false
	}
	label := g.opts.Label
	if searchedLabel != "" {
		label = searchedLabel
	}
	for _, l := range g.pr().Labels {
		if strings.EqualFold(l.Name, label) {
			return true
		}
	}
	return false
}

func (g *GithubSync) prNumber() int {
	if g.pullRequest() {
		return g.event.PullRequest.Number
	}
	if pr := g.prFromRef(); pr != nil {
		return pr.GetNumber()
	}
	return 0
}

func (g *GithubSync) prFromRef() *gh.PullRequest {
	if g.pullRequest() {
		return nil
	}
	if g.prCache != nil {
		return g.prCache
	}
	prs, err := g.client.ListPullRequests(g.repo(), fmt.Sprintf("%s:%s", g.orgName(), g.ref()))
	if err != nil || len(prs) == 0 {
		return nil
	}
	g.prCache = prs[0]
	return g.prCache
}

func (g *GithubSync) pr() *GitHubPR {
	if g.pullRequest() {
		return g.event.PullRequest
	}
	if pr := g.prFromRef(); pr != nil {
		labels := []GitHubLabel{}
		for _, label := range pr.Labels {
			labels = append(labels, GitHubLabel{Name: label.GetName()})
		}
		return &GitHubPR{
			Number: pr.GetNumber(),
			Head:   GitHubPRHead{SHA: pr.Head.GetSHA(), Ref: pr.Head.GetRef()},
			Labels: labels,
		}
	}
	return nil
}

func (g *GithubSync) deploymentVariant() string {
	variant := strings.TrimSpace(g.opts.DeploymentVariant)
	if variant == "" {
		return ""
	}
	if len(variant) > 4 {
		return ""
	}
	return variant
}

func (g *GithubSync) validateDeploymentVariant() error {
	variant := strings.TrimSpace(g.opts.DeploymentVariant)
	if variant != "" && len(variant) > 4 {
		return fmt.Errorf("--deployment-variant must be 4 chars max")
	}
	return nil
}

func (g *GithubSync) instanceName() string {
	parts := []string{"gh", fmt.Sprintf("%d", g.repoID())}
	if g.deploymentVariant() != "" {
		parts = append(parts, g.deploymentVariant())
	}
	if g.prNumber() != 0 {
		parts = append(parts, "pr", fmt.Sprintf("%d", g.prNumber()))
	} else {
		parts = append(parts, "branch", g.branch())
	}
	return NormalizeName(strings.Join(parts, "-"))
}

func (g *GithubSync) instanceSubdomain() string {
	components := []string{}
	if g.deploymentVariant() != "" {
		components = append(components, g.deploymentVariant())
	}
	if g.prNumber() != 0 {
		components = append(components, "pr", fmt.Sprintf("%d", g.prNumber()))
	}
	branch := g.branch()
	if branch != "" {
		parts := strings.Split(branch, "/")
		components = append(components, strings.ToLower(parts[len(parts)-1]))
	}
	return NormalizeName(strings.Join(components, "-"))
}

func (g *GithubSync) defaultInstanceTags() map[string]string {
	tags := map[string]string{
		"repo_name": g.repoName(),
		"repo_id":   fmt.Sprintf("%d", g.repoID()),
		"org_name":  g.orgName(),
		"org_id":    fmt.Sprintf("%d", g.orgID()),
		"version":   Version,
	}
	if g.prNumber() != 0 {
		tags["pr_number"] = fmt.Sprintf("%d", g.prNumber())
	}
	return tags
}

func (g *GithubSync) buildInstance() *Instance {
	common := g.opts.Common
	common.Tags = mergeStringMap(g.defaultInstanceTags(), common.Tags)
	common.Admins, common.AdminPublicKeys = g.expandedAdminsAndKeys()
	instance := NewInstance(g.instanceName(), common, g.provider, g.logger)
	instance.WithSubdomain(g.instanceSubdomain())
	return instance
}

func mergeStringMap(base, extra map[string]string) map[string]string {
	result := map[string]string{}
	for k, v := range base {
		result[k] = v
	}
	for k, v := range extra {
		result[k] = v
	}
	return result
}

func instanceToCommon(inst *Instance) CommonOptions {
	return CommonOptions{
		Admins:          inst.Admins,
		AdminPublicKeys: inst.AdminPublicKeys,
		Context:         inst.Context,
		CIDRs:           inst.CIDRs,
		Registries:      inst.Registries,
		ProxyTLS:        inst.ProxyTLS,
		DNS:             inst.DNS,
		Ports:           inst.Ports,
		InstanceType:    inst.Size,
		DefaultPort:     inst.DefaultPort,
		Tags:            inst.Tags,
		ComposeFiles:    inst.ComposeFiles,
		ComposeOptions:  inst.ComposeOptions,
		PreScript:       inst.PreScript,
	}
}

func containsString(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}
