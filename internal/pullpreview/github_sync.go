package pullpreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	return clearDanglingDeployments(repo, opts, provider, client, logger)
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
	instances, err := provider.ListInstances(repoCleanupTags(repo))
	if err != nil {
		return err
	}
	issues, err := client.ListIssues(repo, opts.Label)
	if err != nil {
		return err
	}
	activePRs := map[string]struct{}{}
	for _, issue := range issues {
		if issue == nil || issue.PullRequestLinks == nil {
			continue
		}
		number := issue.GetNumber()
		if number == 0 {
			continue
		}
		prKey := fmt.Sprintf("%d", number)
		if strings.EqualFold(issue.GetState(), "open") && !prExpired(issue.GetUpdatedAt().Time, ttl) {
			activePRs[prKey] = struct{}{}
			continue
		}
		if logger != nil {
			if strings.EqualFold(issue.GetState(), "closed") {
				logger.Warnf("[clear_dangling_deployments] Found dangling %s label for PR#%d. Cleaning up...", opts.Label, number)
			} else {
				logger.Warnf("[clear_dangling_deployments] Found %s label for expired PR#%d (%s). Cleaning up...", opts.Label, number, issue.GetUpdatedAt().String())
			}
		}
		if err := client.RemoveLabel(repo, number, opts.Label); err != nil && logger != nil {
			logger.Warnf("[clear_dangling_deployments] Unable to remove %s label for PR#%d: %v", opts.Label, number, err)
		}
	}
	activeInstanceNames := []string{}
	removedInstanceNames := []string{}
	for _, inst := range instances {
		if !instanceMatchesCleanupVariant(inst, opts.DeploymentVariant) {
			continue
		}
		ref, ok := cleanupInstanceReference(inst)
		if !ok {
			if logger != nil {
				logger.Warnf("[clear_dangling_deployments] Unable to infer linkage for instance %s. Skipping.", inst.Name)
			}
			continue
		}
		dangling := false
		detail := ""
		if ref.PRNumber != "" {
			if _, exists := activePRs[ref.PRNumber]; !exists {
				dangling = true
				detail = fmt.Sprintf("PR#%s not active/labeled", ref.PRNumber)
			}
		} else {
			dangling = true
			detail = fmt.Sprintf("branch %q not linked to an active PR", ref.Branch)
		}
		if !dangling {
			activeInstanceNames = append(activeInstanceNames, inst.Name)
			continue
		}
		if logger != nil {
			logger.Warnf("[clear_dangling_deployments] Found dangling instance %s (%s). Destroying...", inst.Name, detail)
		}
		if runDownFunc != nil {
			if err := runDownFunc(DownOptions{Name: inst.Name}, provider, logger); err != nil {
				if logger != nil {
					logger.Warnf("[clear_dangling_deployments] Unable to destroy %s: %v", inst.Name, err)
				}
			} else {
				removedInstanceNames = append(removedInstanceNames, inst.Name)
			}
			continue
		}
		if err := provider.Terminate(inst.Name); err != nil {
			if logger != nil {
				logger.Warnf("[clear_dangling_deployments] Unable to destroy %s: %v", inst.Name, err)
			}
		} else {
			removedInstanceNames = append(removedInstanceNames, inst.Name)
		}
	}
	if logger != nil {
		logger.Infof("[clear_dangling_deployments] Active instances: %s", formatCleanupNames(activeInstanceNames))
		logger.Infof("[clear_dangling_deployments] Dangling removed: %s", formatCleanupNames(removedInstanceNames))
		logger.Infof("[clear_dangling_deployments] end")
	}
	return nil
}

func formatCleanupNames(names []string) string {
	names = uniqueStrings(names)
	if len(names) == 0 {
		return "(none)"
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func repoCleanupTags(repo string) map[string]string {
	tags := map[string]string{
		"stack": StackName,
	}
	parts := strings.SplitN(strings.TrimSpace(repo), "/", 2)
	if len(parts) == 2 {
		if org := strings.TrimSpace(parts[0]); org != "" {
			tags["org_name"] = org
		}
		if name := strings.TrimSpace(parts[1]); name != "" {
			tags["repo_name"] = name
		}
	}
	return tags
}

type cleanupInstanceRef struct {
	PRNumber         string
	Branch           string
	BranchNormalized string
}

func cleanupInstanceReference(inst InstanceSummary) (cleanupInstanceRef, bool) {
	prNumber := normalizePRNumber(firstTagValue(inst.Tags, "pr_number"))
	if prNumber != "" {
		return cleanupInstanceRef{PRNumber: prNumber}, true
	}
	branch := firstTagValue(inst.Tags, "pullpreview_branch", "branch")
	if branch != "" {
		return cleanupInstanceRef{Branch: branch, BranchNormalized: NormalizeName(branch)}, true
	}
	parsed, ok := parsePullPreviewInstanceName(inst.Name)
	if !ok {
		return cleanupInstanceRef{}, false
	}
	if parsed.PRNumber != "" {
		return cleanupInstanceRef{PRNumber: parsed.PRNumber}, true
	}
	if parsed.Branch != "" {
		return cleanupInstanceRef{Branch: parsed.Branch, BranchNormalized: parsed.Branch}, true
	}
	return cleanupInstanceRef{}, false
}

func instanceMatchesCleanupVariant(inst InstanceSummary, expectedVariant string) bool {
	expectedVariant = strings.TrimSpace(expectedVariant)
	tagVariant := firstTagValue(inst.Tags, "pullpreview_variant", "deployment_variant")
	if expectedVariant == "" {
		if tagVariant != "" {
			return false
		}
		parsed, ok := parsePullPreviewInstanceName(inst.Name)
		return !ok || parsed.Variant == ""
	}
	if strings.EqualFold(tagVariant, expectedVariant) {
		return true
	}
	if tagVariant != "" {
		return false
	}
	parsed, ok := parsePullPreviewInstanceName(inst.Name)
	return ok && strings.EqualFold(parsed.Variant, expectedVariant)
}

type parsedInstanceName struct {
	Variant  string
	PRNumber string
	Branch   string
}

func parsePullPreviewInstanceName(name string) (parsedInstanceName, bool) {
	parts := strings.Split(strings.TrimSpace(name), "-")
	if len(parts) < 4 || parts[0] != "gh" || !isDigits(parts[1]) {
		return parsedInstanceName{}, false
	}
	if parts[len(parts)-2] == "pr" {
		prNumber := normalizePRNumber(parts[len(parts)-1])
		if prNumber == "" {
			return parsedInstanceName{}, false
		}
		variant := strings.Join(parts[2:len(parts)-2], "-")
		if variant != "" && len(variant) > 4 {
			return parsedInstanceName{}, false
		}
		return parsedInstanceName{Variant: variant, PRNumber: prNumber}, true
	}
	for i := 2; i < len(parts)-1; i++ {
		if parts[i] != "branch" {
			continue
		}
		variant := strings.Join(parts[2:i], "-")
		if variant != "" && len(variant) > 4 {
			continue
		}
		branch := strings.Join(parts[i+1:], "-")
		if branch == "" {
			continue
		}
		return parsedInstanceName{Variant: variant, Branch: branch}, true
	}
	return parsedInstanceName{}, false
}

func normalizePRNumber(value string) string {
	value = strings.TrimSpace(value)
	if !isDigits(value) {
		return ""
	}
	number := mustParseInt(value)
	if number <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", number)
}

func firstTagValue(tags map[string]string, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(tags[key])
		if value != "" {
			return value
		}
	}
	return ""
}

func isDigits(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
			g.logger.Infof("%s", lic.Message)
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
	case actionPRUp, actionPRPush:
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

func (g *GithubSync) updateGitHubStatus(status deploymentStatus, url string) error {
	g.updatePRComment(status, url)
	return nil
}

func (g *GithubSync) updatePRComment(status deploymentStatus, previewURL string) {
	if g.prNumber() == 0 {
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
	if logs := g.workflowRunURL(); logs != "" {
		b.WriteString(fmt.Sprintf("- Logs: [%s](%s)\n", logs, logs))
	}

	if inst != nil && status == statusDeployed {
		b.WriteString(fmt.Sprintf("- SSH Username: `%s`\n", inst.Username()))
		b.WriteString(fmt.Sprintf("- SSH IP: `%s`\n", inst.PublicIP()))
		b.WriteString(fmt.Sprintf("- SSH Command: `ssh %s`\n", inst.SSHAddress()))
	}

	b.WriteString("\nPowered by [⚡](https://pullpreview.com) PullPreview.\n")
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
	jobID := strings.TrimSpace(os.Getenv("PULLPREVIEW_GITHUB_JOB_ID"))
	if jobID != "" {
		return fmt.Sprintf("%s/%s/actions/runs/%s/job/%s", server, g.repo(), runID, jobID)
	}
	return fmt.Sprintf("%s/%s/actions/runs/%s", server, g.repo(), runID)
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
		"repo_name":        g.repoName(),
		"repo_id":          fmt.Sprintf("%d", g.repoID()),
		"org_name":         g.orgName(),
		"org_id":           fmt.Sprintf("%d", g.orgID()),
		"version":          Version,
		"pullpreview_repo": g.repo(),
		"pullpreview_kind": "branch",
	}
	if branch := g.branch(); branch != "" {
		tags["pullpreview_branch"] = branch
	}
	if variant := g.deploymentVariant(); variant != "" {
		tags["pullpreview_variant"] = variant
	}
	if g.prNumber() != 0 {
		tags["pr_number"] = fmt.Sprintf("%d", g.prNumber())
		tags["pullpreview_kind"] = "pr"
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
