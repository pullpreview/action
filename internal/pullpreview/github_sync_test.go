package pullpreview

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gh "github.com/google/go-github/v60/github"
)

type fakeGitHub struct {
	latestSHA           string
	commitStatuses      []string
	commitStatusURLs    []string
	deploymentStates    []string
	deployments         []*gh.Deployment
	pullRequestsByRef   map[string][]*gh.PullRequest
	pullRequestsByNum   map[int]*gh.PullRequest
	issues              []*gh.Issue
	environments        []*gh.Environment
	collaborators       []*gh.User
	removedLabels       []string
	deletedDeployments  []int64
	deletedEnvironments []string
	comments            []*gh.IssueComment
	createdComments     []string
	updatedComments     []string
	userPublicKeys      map[string][]string
}

func (f *fakeGitHub) ListIssues(repo, label string) ([]*gh.Issue, error) {
	return f.issues, nil
}

func (f *fakeGitHub) GetPullRequest(repo string, number int) (*gh.PullRequest, error) {
	if f.pullRequestsByNum == nil {
		return nil, nil
	}
	return f.pullRequestsByNum[number], nil
}

func (f *fakeGitHub) ListEnvironments(repo string) ([]*gh.Environment, error) {
	return f.environments, nil
}

func (f *fakeGitHub) ListDeployments(repo, environment, ref string) ([]*gh.Deployment, error) {
	return f.deployments, nil
}

func (f *fakeGitHub) CreateDeployment(repo, ref, environment string) (*gh.Deployment, error) {
	id := int64(len(f.deployments) + 1)
	dep := &gh.Deployment{ID: gh.Int64(id)}
	f.deployments = []*gh.Deployment{dep}
	return dep, nil
}

func (f *fakeGitHub) CreateDeploymentStatus(repo string, deploymentID int64, state string, environmentURL string, autoInactive bool) error {
	f.deploymentStates = append(f.deploymentStates, state)
	return nil
}

func (f *fakeGitHub) CreateCommitStatus(repo, sha, state, targetURL, context, description string) error {
	f.commitStatuses = append(f.commitStatuses, state)
	f.commitStatusURLs = append(f.commitStatusURLs, targetURL)
	return nil
}

func (f *fakeGitHub) DeleteDeployment(repo string, deploymentID int64) error {
	f.deletedDeployments = append(f.deletedDeployments, deploymentID)
	return nil
}

func (f *fakeGitHub) DeleteEnvironment(repo, name string) error {
	f.deletedEnvironments = append(f.deletedEnvironments, name)
	return nil
}

func (f *fakeGitHub) RemoveLabel(repo string, number int, label string) error {
	f.removedLabels = append(f.removedLabels, label)
	return nil
}

func (f *fakeGitHub) ListIssueComments(repo string, number int) ([]*gh.IssueComment, error) {
	return f.comments, nil
}

func (f *fakeGitHub) CreateIssueComment(repo string, number int, body string) error {
	f.createdComments = append(f.createdComments, body)
	id := int64(len(f.comments) + 1)
	f.comments = append(f.comments, &gh.IssueComment{ID: gh.Int64(id), Body: gh.String(body)})
	return nil
}

func (f *fakeGitHub) UpdateIssueComment(repo string, commentID int64, body string) error {
	f.updatedComments = append(f.updatedComments, body)
	for _, comment := range f.comments {
		if comment.GetID() == commentID {
			comment.Body = gh.String(body)
			break
		}
	}
	return nil
}

func (f *fakeGitHub) ListPullRequests(repo, head string) ([]*gh.PullRequest, error) {
	if f.pullRequestsByRef == nil {
		return nil, nil
	}
	return f.pullRequestsByRef[head], nil
}

func (f *fakeGitHub) LatestCommitSHA(repo, ref string) (string, error) {
	return f.latestSHA, nil
}

func (f *fakeGitHub) ListCollaborators(repo string) ([]*gh.User, bool, error) {
	return f.collaborators, false, nil
}

func (f *fakeGitHub) ListUserPublicKeys(user string) ([]string, error) {
	if f.userPublicKeys == nil {
		return nil, nil
	}
	return f.userPublicKeys[user], nil
}

type fakeProvider struct {
	running bool
}

func (f fakeProvider) Launch(name string, opts LaunchOptions) (AccessDetails, error) {
	return AccessDetails{Username: "ec2-user", IPAddress: "1.2.3.4"}, nil
}

func (f fakeProvider) Terminate(name string) error { return nil }

func (f fakeProvider) Running(name string) (bool, error) { return f.running, nil }

func (f fakeProvider) ListInstances(tags map[string]string) ([]InstanceSummary, error) {
	return nil, nil
}

func (f fakeProvider) Username() string { return "ec2-user" }

func loadFixtureEvent(t *testing.T, filename string) GitHubEvent {
	t.Helper()
	path := filepath.Join("..", "..", "test", "fixtures", filename)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", filename, err)
	}
	var event GitHubEvent
	if err := json.Unmarshal(content, &event); err != nil {
		t.Fatalf("failed to parse fixture %s: %v", filename, err)
	}
	return event
}

func newSync(event GitHubEvent, opts GithubSyncOptions, client *fakeGitHub, provider Provider) *GithubSync {
	return &GithubSync{
		event:    event,
		appPath:  "/tmp/app",
		opts:     opts,
		client:   client,
		provider: provider,
		runUp: func(opts UpOptions, provider Provider, logger *Logger) (*Instance, error) {
			inst := NewInstance(opts.Name, opts.Common, provider, logger)
			inst.Access = AccessDetails{Username: "ec2-user", IPAddress: "1.2.3.4"}
			return inst, nil
		},
		runDown: func(opts DownOptions, provider Provider, logger *Logger) error { return nil },
	}
}

func TestGuessActionFromLabeledFixture(t *testing.T) {
	event := loadFixtureEvent(t, "github_event_labeled.json")
	sync := newSync(event, GithubSyncOptions{Label: "pullpreview", Common: CommonOptions{}}, &fakeGitHub{}, fakeProvider{running: true})
	if got := sync.guessAction(); got != actionPRUp {
		t.Fatalf("guessAction()=%s, want %s", got, actionPRUp)
	}
}

func TestGuessActionFromPushFixtureWithPR(t *testing.T) {
	event := loadFixtureEvent(t, "github_event_push.json")
	client := &fakeGitHub{
		latestSHA: event.HeadCommit.ID,
		pullRequestsByRef: map[string][]*gh.PullRequest{
			"pullpreview:refs/heads/test-action": {
				&gh.PullRequest{
					Number: gh.Int(10),
					Head:   &gh.PullRequestBranch{SHA: gh.String(event.HeadCommit.ID), Ref: gh.String("test-action")},
					Labels: []*gh.Label{{Name: gh.String("pullpreview")}},
				},
			},
		},
	}
	sync := newSync(event, GithubSyncOptions{Label: "pullpreview", Common: CommonOptions{}}, client, fakeProvider{running: true})
	if got := sync.guessAction(); got != actionPRPush {
		t.Fatalf("guessAction()=%s, want %s", got, actionPRPush)
	}
}

func TestGuessActionFromSoloPushAlwaysOn(t *testing.T) {
	event := loadFixtureEvent(t, "github_event_push_solo_organization.json")
	client := &fakeGitHub{latestSHA: event.HeadCommit.ID}
	sync := newSync(event, GithubSyncOptions{Label: "pullpreview", AlwaysOn: []string{"dev"}, Common: CommonOptions{}}, client, fakeProvider{running: true})
	if got := sync.guessAction(); got != actionBranchPush {
		t.Fatalf("guessAction()=%s, want %s", got, actionBranchPush)
	}
}

func TestSyncLabeledFixtureRunsUp(t *testing.T) {
	t.Setenv("PULLPREVIEW_TEST", "1")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_RUN_ID", "12345")
	t.Setenv("GITHUB_STEP_SUMMARY", filepath.Join(t.TempDir(), "summary.md"))
	event := loadFixtureEvent(t, "github_event_labeled.json")
	client := &fakeGitHub{latestSHA: event.PullRequest.Head.SHA}
	upCalled := false
	sync := newSync(event, GithubSyncOptions{Label: "pullpreview", CommentPR: true, Common: CommonOptions{}}, client, fakeProvider{running: true})
	sync.runUp = func(opts UpOptions, provider Provider, logger *Logger) (*Instance, error) {
		upCalled = true
		inst := NewInstance(opts.Name, opts.Common, provider, logger)
		inst.Access = AccessDetails{Username: "ec2-user", IPAddress: "1.2.3.4"}
		return inst, nil
	}
	if err := sync.Sync(); err != nil {
		t.Fatalf("Sync() returned error: %v", err)
	}
	if !upCalled {
		t.Fatalf("expected runUp to be called")
	}
	if len(client.commitStatuses) < 2 {
		t.Fatalf("expected at least two commit statuses, got %v", client.commitStatuses)
	}
	if client.commitStatuses[0] != "pending" || client.commitStatuses[len(client.commitStatuses)-1] != "success" {
		t.Fatalf("unexpected commit statuses: %v", client.commitStatuses)
	}
	if len(client.createdComments) != 1 {
		t.Fatalf("expected initial PR comment creation, got %d", len(client.createdComments))
	}
	if len(client.updatedComments) == 0 {
		t.Fatalf("expected PR comment update on deployed state")
	}
	if !strings.Contains(client.updatedComments[len(client.updatedComments)-1], "✅ Deploy successful") {
		t.Fatalf("expected successful deploy text in comment, got %q", client.updatedComments[len(client.updatedComments)-1])
	}
	if !strings.Contains(client.updatedComments[len(client.updatedComments)-1], "[⚡](https://pullpreview.com) PullPreview") {
		t.Fatalf("expected pullpreview lightning link in comment title, got %q", client.updatedComments[len(client.updatedComments)-1])
	}
}

func TestSyncLabeledProxyTLSUsesHTTPSURLInStatusAndComment(t *testing.T) {
	t.Setenv("PULLPREVIEW_TEST", "1")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_RUN_ID", "12345")

	event := loadFixtureEvent(t, "github_event_labeled.json")
	client := &fakeGitHub{latestSHA: event.PullRequest.Head.SHA}
	sync := newSync(event, GithubSyncOptions{
		Label:     "pullpreview",
		CommentPR: true,
		Common:    CommonOptions{ProxyTLS: "web:80"},
	}, client, fakeProvider{running: true})

	if err := sync.Sync(); err != nil {
		t.Fatalf("Sync() returned error: %v", err)
	}
	if len(client.commitStatusURLs) == 0 {
		t.Fatalf("expected commit status URLs to be recorded")
	}

	foundHTTPS := false
	for _, value := range client.commitStatusURLs {
		if strings.HasPrefix(value, "https://") {
			foundHTTPS = true
			break
		}
	}
	if !foundHTTPS {
		t.Fatalf("expected at least one https commit status URL, got %v", client.commitStatusURLs)
	}
	if len(client.updatedComments) == 0 {
		t.Fatalf("expected PR comment update on deployed state")
	}
	if !strings.Contains(client.updatedComments[len(client.updatedComments)-1], "https://") {
		t.Fatalf("expected https preview URL in PR comment, got %q", client.updatedComments[len(client.updatedComments)-1])
	}
}

func TestSyncClosedPRRunsDownAndRemovesLabel(t *testing.T) {
	t.Setenv("PULLPREVIEW_TEST", "1")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_RUN_ID", "12345")
	t.Setenv("GITHUB_STEP_SUMMARY", filepath.Join(t.TempDir(), "summary.md"))
	event := GitHubEvent{
		Action: "closed",
		PullRequest: &GitHubPR{
			Number: 10,
			Head:   GitHubPRHead{SHA: "abc", Ref: "feature"},
			Base:   GitHubPRBase{Repo: GitHubRepo{ID: 1, Name: "repo", Owner: GitHubOrg{Login: "org", ID: 2, Type: "Organization"}}},
			Labels: []GitHubLabel{{Name: "pullpreview"}},
		},
		Repository:   GitHubRepo{ID: 1, Name: "repo", Owner: GitHubOrg{Login: "org", ID: 2, Type: "Organization"}},
		Organization: &GitHubOrg{Login: "org", ID: 2, Type: "Organization"},
	}
	client := &fakeGitHub{latestSHA: "abc"}
	downCalled := false
	sync := newSync(event, GithubSyncOptions{Label: "pullpreview", CommentPR: true, Common: CommonOptions{}}, client, fakeProvider{running: true})
	sync.runDown = func(opts DownOptions, provider Provider, logger *Logger) error {
		downCalled = true
		return nil
	}
	if err := sync.Sync(); err != nil {
		t.Fatalf("Sync() returned error: %v", err)
	}
	if !downCalled {
		t.Fatalf("expected runDown to be called")
	}
	if len(client.removedLabels) == 0 || client.removedLabels[0] != "pullpreview" {
		t.Fatalf("expected pullpreview label removal, got %v", client.removedLabels)
	}
	if len(client.createdComments) != 1 {
		t.Fatalf("expected a destroying PR comment to be created")
	}
	if !strings.Contains(client.createdComments[0], "🧹 Destroying preview...") {
		t.Fatalf("unexpected destroying PR comment body: %q", client.createdComments[0])
	}
	if len(client.updatedComments) == 0 {
		t.Fatalf("expected destroyed PR comment update")
	}
	if !strings.Contains(client.updatedComments[len(client.updatedComments)-1], "🗑️ Preview destroyed") {
		t.Fatalf("unexpected destroyed PR comment body: %q", client.updatedComments[len(client.updatedComments)-1])
	}
}

func TestExpandedAdminsIncludesCollaboratorsWithPush(t *testing.T) {
	event := loadFixtureEvent(t, "github_event_labeled.json")
	client := &fakeGitHub{
		collaborators: []*gh.User{
			{Login: gh.String("alice"), Permissions: map[string]bool{"push": true}},
			{Login: gh.String("bob"), Permissions: map[string]bool{"push": false}},
			{Login: gh.String("team-user")},
		},
	}
	sync := newSync(event, GithubSyncOptions{Label: "pullpreview", Common: CommonOptions{Admins: []string{"@collaborators/push", "manual"}}}, client, fakeProvider{running: true})
	admins := sync.expandedAdmins()
	if len(admins) != 3 {
		t.Fatalf("expected 3 admins, got %v", admins)
	}
	if admins[0] != "manual" && admins[1] != "manual" && admins[2] != "manual" {
		t.Fatalf("expected manual admin to be preserved, got %v", admins)
	}
	if admins[0] != "alice" && admins[1] != "alice" && admins[2] != "alice" {
		t.Fatalf("expected push collaborator to be included, got %v", admins)
	}
	if admins[0] != "team-user" && admins[1] != "team-user" && admins[2] != "team-user" {
		t.Fatalf("expected team-derived collaborator to be included, got %v", admins)
	}
}

func TestValidateDeploymentVariant(t *testing.T) {
	sync := newSync(loadFixtureEvent(t, "github_event_labeled.json"), GithubSyncOptions{
		Label:             "pullpreview",
		DeploymentVariant: "abcdef",
		Common:            CommonOptions{},
	}, &fakeGitHub{}, fakeProvider{running: true})

	if err := sync.validateDeploymentVariant(); err == nil {
		t.Fatalf("expected validation error for long deployment variant")
	}
}

func TestClearOutdatedEnvironmentsRemovesDanglingPREnvironments(t *testing.T) {
	client := &fakeGitHub{
		environments: []*gh.Environment{
			{Name: gh.String("gh-1-pr-10")},
			{Name: gh.String("gh-1-pr-99")},
			{Name: gh.String("gh-1-branch-main")},
		},
		issues: []*gh.Issue{
			{Number: gh.Int(10), PullRequestLinks: &gh.PullRequestLinks{}},
		},
	}
	err := clearOutdatedEnvironments("org/repo", GithubSyncOptions{Label: "pullpreview"}, fakeProvider{}, client, nil)
	if err != nil {
		t.Fatalf("clearOutdatedEnvironments() error: %v", err)
	}
	if len(client.deletedEnvironments) != 1 || client.deletedEnvironments[0] != "gh-1-pr-99" {
		t.Fatalf("unexpected deleted environments: %v", client.deletedEnvironments)
	}
}

func TestRunGithubSyncFromEnvironmentRunsUpForLabeledPR(t *testing.T) {
	t.Setenv("PULLPREVIEW_TEST", "1")
	event := loadFixtureEvent(t, "github_event_labeled.json")
	path := writeFixtureToTempEventFile(t, event)
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_EVENT_PATH", path)
	t.Setenv("GITHUB_REPOSITORY", "pullpreview/action")
	t.Setenv("GITHUB_REF", "refs/heads/test-action")

	client := &fakeGitHub{latestSHA: event.PullRequest.Head.SHA}
	originalClientFactory := newGitHubClient
	originalRunUp := runUpFunc
	originalRunDown := runDownFunc
	defer func() {
		newGitHubClient = originalClientFactory
		runUpFunc = originalRunUp
		runDownFunc = originalRunDown
	}()
	newGitHubClient = func(ctx context.Context, token string) GitHubAPI { return client }
	upCalled := false
	runUpFunc = func(opts UpOptions, provider Provider, logger *Logger) (*Instance, error) {
		upCalled = true
		inst := NewInstance(opts.Name, opts.Common, provider, logger)
		inst.Access = AccessDetails{Username: "ec2-user", IPAddress: "1.2.3.4"}
		return inst, nil
	}
	runDownFunc = func(opts DownOptions, provider Provider, logger *Logger) error { return nil }

	err := RunGithubSync(GithubSyncOptions{AppPath: "/tmp/app", Label: "pullpreview", CommentPR: true, Common: CommonOptions{}}, fakeProvider{running: true}, nil)
	if err != nil {
		t.Fatalf("RunGithubSync() error: %v", err)
	}
	if !upCalled {
		t.Fatalf("expected up flow to be executed")
	}
}

func TestRenderPRCommentForErrorState(t *testing.T) {
	event := loadFixtureEvent(t, "github_event_labeled.json")
	sync := newSync(event, GithubSyncOptions{Label: "pullpreview", CommentPR: true, Common: CommonOptions{}}, &fakeGitHub{}, fakeProvider{running: true})
	body := sync.renderPRComment(statusError, "")
	if !strings.Contains(body, "❌ Deploy failed") {
		t.Fatalf("unexpected error comment body: %q", body)
	}
	if !strings.Contains(body, sync.prCommentMarker()) {
		t.Fatalf("missing marker in rendered comment")
	}
}

func TestRenderPRCommentForDestroyedState(t *testing.T) {
	event := loadFixtureEvent(t, "github_event_labeled.json")
	sync := newSync(event, GithubSyncOptions{Label: "pullpreview", CommentPR: true, Common: CommonOptions{}}, &fakeGitHub{}, fakeProvider{running: true})
	body := sync.renderPRComment(statusDestroyed, "")
	if !strings.Contains(body, "🗑️ Preview destroyed") {
		t.Fatalf("unexpected destroyed comment body: %q", body)
	}
	if !strings.Contains(body, "| Preview URL | _Destroyed_ |") {
		t.Fatalf("expected destroyed preview marker in comment body: %q", body)
	}
	if !strings.Contains(body, sync.prCommentMarker()) {
		t.Fatalf("missing marker in rendered comment")
	}
	if !strings.Contains(body, "[⚡](https://pullpreview.com) PullPreview") {
		t.Fatalf("missing pullpreview lightning title: %q", body)
	}
}

func TestRenderStepSummaryForDeployedState(t *testing.T) {
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_RUN_ID", "777")
	event := loadFixtureEvent(t, "github_event_labeled.json")
	sync := newSync(event, GithubSyncOptions{Label: "pullpreview", CommentPR: true, Common: CommonOptions{}}, &fakeGitHub{}, fakeProvider{running: true})
	inst := NewInstance(sync.instanceName(), CommonOptions{}, fakeProvider{}, nil)
	inst.Access = AccessDetails{Username: "ec2-user", IPAddress: "1.2.3.4"}

	body := sync.renderStepSummary(statusDeployed, actionPRUp, "https://preview.test", inst)
	if !strings.Contains(body, "## PullPreview Summary") {
		t.Fatalf("missing summary header: %q", body)
	}
	if !strings.Contains(body, "- Preview URL: [https://preview.test](https://preview.test)") {
		t.Fatalf("missing preview URL link: %q", body)
	}
	if !strings.Contains(body, "/deployments/activity_log?environment=") {
		t.Fatalf("missing deployment link: %q", body)
	}
	if !strings.Contains(body, "- SSH Command: `ssh ec2-user@1.2.3.4`") {
		t.Fatalf("missing ssh command: %q", body)
	}
	if !strings.Contains(body, "Powered by [PullPreview](https://pullpreview.com).") {
		t.Fatalf("missing powered by line: %q", body)
	}
}

func TestRunGithubSyncFromEnvironmentRunsDownForBranchPushWithoutAlwaysOn(t *testing.T) {
	t.Setenv("PULLPREVIEW_TEST", "1")
	event := loadFixtureEvent(t, "github_event_push_solo_organization.json")
	path := writeFixtureToTempEventFile(t, event)
	t.Setenv("GITHUB_EVENT_NAME", "push")
	t.Setenv("GITHUB_EVENT_PATH", path)
	t.Setenv("GITHUB_REPOSITORY", "pullpreview/action")
	t.Setenv("GITHUB_REF", event.Ref)

	client := &fakeGitHub{latestSHA: event.HeadCommit.ID}
	originalClientFactory := newGitHubClient
	originalRunUp := runUpFunc
	originalRunDown := runDownFunc
	defer func() {
		newGitHubClient = originalClientFactory
		runUpFunc = originalRunUp
		runDownFunc = originalRunDown
	}()
	newGitHubClient = func(ctx context.Context, token string) GitHubAPI { return client }
	runUpFunc = func(opts UpOptions, provider Provider, logger *Logger) (*Instance, error) {
		t.Fatalf("runUp should not be called for branch down action")
		return nil, nil
	}
	downCalled := false
	runDownFunc = func(opts DownOptions, provider Provider, logger *Logger) error {
		downCalled = true
		return nil
	}

	err := RunGithubSync(GithubSyncOptions{AppPath: "/tmp/app", Label: "pullpreview", Common: CommonOptions{}}, fakeProvider{running: true}, nil)
	if err != nil {
		t.Fatalf("RunGithubSync() error: %v", err)
	}
	if !downCalled {
		t.Fatalf("expected down flow to be executed")
	}
}

func writeFixtureToTempEventFile(t *testing.T, event GitHubEvent) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "event.json")
	content, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed marshalling event: %v", err)
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed writing temp event file: %v", err)
	}
	return path
}
