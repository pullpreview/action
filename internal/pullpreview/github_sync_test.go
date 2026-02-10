package pullpreview

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	gh "github.com/google/go-github/v60/github"
)

type fakeGitHub struct {
	latestSHA         string
	pullRequestsByRef map[string][]*gh.PullRequest
	pullRequestsByNum map[int]*gh.PullRequest
	issues            []*gh.Issue
	collaborators     []*gh.User
	removedLabels     []string
	removedLabelPRs   []int
	listIssueLabels   []string
	comments          []*gh.IssueComment
	createdComments   []string
	updatedComments   []string
	userPublicKeys    map[string][]string
}

func (f *fakeGitHub) ListIssues(repo, label string) ([]*gh.Issue, error) {
	f.listIssueLabels = append(f.listIssueLabels, label)
	return f.issues, nil
}

func (f *fakeGitHub) GetPullRequest(repo string, number int) (*gh.PullRequest, error) {
	if f.pullRequestsByNum == nil {
		return nil, nil
	}
	return f.pullRequestsByNum[number], nil
}

func (f *fakeGitHub) RemoveLabel(repo string, number int, label string) error {
	f.removedLabels = append(f.removedLabels, label)
	f.removedLabelPRs = append(f.removedLabelPRs, number)
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

type scheduledCleanupProvider struct {
	instances []InstanceSummary
	lastTags  map[string]string
}

func (f *scheduledCleanupProvider) Launch(name string, opts LaunchOptions) (AccessDetails, error) {
	return AccessDetails{}, nil
}

func (f *scheduledCleanupProvider) Terminate(name string) error { return nil }

func (f *scheduledCleanupProvider) Running(name string) (bool, error) { return false, nil }

func (f *scheduledCleanupProvider) ListInstances(tags map[string]string) ([]InstanceSummary, error) {
	f.lastTags = map[string]string{}
	for k, v := range tags {
		f.lastTags[k] = v
	}
	return f.instances, nil
}

func (f *scheduledCleanupProvider) Username() string { return "ec2-user" }

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

func TestGuessActionFromLabeledFixtureWithCustomLabel(t *testing.T) {
	event := loadFixtureEvent(t, "github_event_labeled.json")
	event.Label = &GitHubLabel{Name: "pullpreview-multi-env"}
	event.PullRequest.Labels = []GitHubLabel{{Name: "pullpreview-multi-env"}}
	sync := newSync(event, GithubSyncOptions{Label: "pullpreview-multi-env", DeploymentVariant: "env1", Common: CommonOptions{}}, &fakeGitHub{}, fakeProvider{running: true})
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
	t.Setenv("PULLPREVIEW_GITHUB_JOB_ID", "67890")
	t.Setenv("GITHUB_STEP_SUMMARY", filepath.Join(t.TempDir(), "summary.md"))
	event := loadFixtureEvent(t, "github_event_labeled.json")
	client := &fakeGitHub{latestSHA: event.PullRequest.Head.SHA}
	upCalled := false
	sync := newSync(event, GithubSyncOptions{Label: "pullpreview", Common: CommonOptions{}}, client, fakeProvider{running: true})
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
	if !strings.Contains(client.updatedComments[len(client.updatedComments)-1], "/actions/runs/12345/job/67890") {
		t.Fatalf("expected job URL log link in comment, got %q", client.updatedComments[len(client.updatedComments)-1])
	}
}

func TestSyncLabeledProxyTLSUsesHTTPSURLInComment(t *testing.T) {
	t.Setenv("PULLPREVIEW_TEST", "1")
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_RUN_ID", "12345")

	event := loadFixtureEvent(t, "github_event_labeled.json")
	client := &fakeGitHub{latestSHA: event.PullRequest.Head.SHA}
	sync := newSync(event, GithubSyncOptions{
		Label:  "pullpreview",
		Common: CommonOptions{ProxyTLS: "web:80"},
	}, client, fakeProvider{running: true})

	if err := sync.Sync(); err != nil {
		t.Fatalf("Sync() returned error: %v", err)
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
	sync := newSync(event, GithubSyncOptions{Label: "pullpreview", Common: CommonOptions{}}, client, fakeProvider{running: true})
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

	err := RunGithubSync(GithubSyncOptions{AppPath: "/tmp/app", Label: "pullpreview", Common: CommonOptions{}}, fakeProvider{running: true}, nil)
	if err != nil {
		t.Fatalf("RunGithubSync() error: %v", err)
	}
	if !upCalled {
		t.Fatalf("expected up flow to be executed")
	}
}

func TestRenderPRCommentForErrorState(t *testing.T) {
	event := loadFixtureEvent(t, "github_event_labeled.json")
	sync := newSync(event, GithubSyncOptions{Label: "pullpreview", Common: CommonOptions{}}, &fakeGitHub{}, fakeProvider{running: true})
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
	sync := newSync(event, GithubSyncOptions{Label: "pullpreview", Common: CommonOptions{}}, &fakeGitHub{}, fakeProvider{running: true})
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

func TestRenderPRCommentIncludesVariantAndJob(t *testing.T) {
	t.Setenv("GITHUB_JOB", "deploy_env1")
	event := loadFixtureEvent(t, "github_event_labeled.json")
	sync := newSync(event, GithubSyncOptions{
		Label:             "pullpreview-multi-env",
		DeploymentVariant: "env1",
		Common:            CommonOptions{},
	}, &fakeGitHub{}, fakeProvider{running: true})
	body := sync.renderPRComment(statusDeploying, "")
	if !strings.Contains(body, "| Variant | `env1` |") {
		t.Fatalf("expected variant row in comment body: %q", body)
	}
	if !strings.Contains(body, "| Job | `deploy_env1` |") {
		t.Fatalf("expected job row in comment body: %q", body)
	}
}

func TestUpdatePRCommentTargetsMatchingVariantAndJobMarker(t *testing.T) {
	event := loadFixtureEvent(t, "github_event_labeled.json")
	client := &fakeGitHub{}
	syncEnv1 := newSync(event, GithubSyncOptions{
		Label:             "pullpreview-multi-env",
		DeploymentVariant: "env1",
		Common:            CommonOptions{},
	}, client, fakeProvider{running: true})
	syncEnv2 := newSync(event, GithubSyncOptions{
		Label:             "pullpreview-multi-env",
		DeploymentVariant: "env2",
		Common:            CommonOptions{},
	}, client, fakeProvider{running: true})

	t.Setenv("GITHUB_JOB", "deploy_env1")
	env1Marker := syncEnv1.prCommentMarker()
	t.Setenv("GITHUB_JOB", "deploy_env2")
	env2Marker := syncEnv2.prCommentMarker()
	t.Setenv("GITHUB_JOB", "deploy_env1")

	client.comments = []*gh.IssueComment{
		{ID: gh.Int64(101), Body: gh.String(env1Marker + "\nold env1 body")},
		{ID: gh.Int64(102), Body: gh.String(env2Marker + "\nold env2 body")},
	}

	syncEnv1.updatePRComment(statusDeployed, "https://env1.preview.example")

	if len(client.updatedComments) != 1 {
		t.Fatalf("expected exactly one updated comment, got %d", len(client.updatedComments))
	}
	if !strings.Contains(client.comments[0].GetBody(), "https://env1.preview.example") {
		t.Fatalf("expected env1 comment update, got %q", client.comments[0].GetBody())
	}
	if strings.Contains(client.comments[1].GetBody(), "https://env1.preview.example") {
		t.Fatalf("env2 comment was incorrectly updated: %q", client.comments[1].GetBody())
	}
}

func TestRenderStepSummaryForDeployedState(t *testing.T) {
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_RUN_ID", "777")
	t.Setenv("PULLPREVIEW_GITHUB_JOB_ID", "888")
	event := loadFixtureEvent(t, "github_event_labeled.json")
	sync := newSync(event, GithubSyncOptions{Label: "pullpreview", Common: CommonOptions{}}, &fakeGitHub{}, fakeProvider{running: true})
	inst := NewInstance(sync.instanceName(), CommonOptions{}, fakeProvider{}, nil)
	inst.Access = AccessDetails{Username: "ec2-user", IPAddress: "1.2.3.4"}

	body := sync.renderStepSummary(statusDeployed, actionPRUp, "https://preview.test", inst)
	if !strings.Contains(body, "## PullPreview Summary") {
		t.Fatalf("missing summary header: %q", body)
	}
	if !strings.Contains(body, "- Preview URL: [https://preview.test](https://preview.test)") {
		t.Fatalf("missing preview URL link: %q", body)
	}
	if !strings.Contains(body, "- SSH Command: `ssh ec2-user@1.2.3.4`") {
		t.Fatalf("missing ssh command: %q", body)
	}
	if !strings.Contains(body, "/actions/runs/777/job/888") {
		t.Fatalf("missing job-level logs URL: %q", body)
	}
	if !strings.Contains(body, "Powered by [⚡](https://pullpreview.com) PullPreview.") {
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

func TestClearDanglingDeploymentsDestroysInstancesNotLinkedToActivePROrAlwaysOnBranch(t *testing.T) {
	client := &fakeGitHub{
		issues: []*gh.Issue{
			{
				Number:           gh.Int(10),
				State:            gh.String("open"),
				PullRequestLinks: &gh.PullRequestLinks{},
			},
			{
				Number:           gh.Int(11),
				State:            gh.String("closed"),
				PullRequestLinks: &gh.PullRequestLinks{},
			},
		},
	}
	provider := &scheduledCleanupProvider{
		instances: []InstanceSummary{
			{Name: "gh-1-pr-10", Tags: map[string]string{"pr_number": "10"}},
			{Name: "gh-1-pr-11", Tags: map[string]string{"pr_number": "11"}},
			{Name: "gh-1-branch-main", Tags: map[string]string{"pullpreview_branch": "main"}},
			{Name: "gh-1-branch-feature-x", Tags: map[string]string{}}, // legacy branch instance without branch tag
		},
	}
	destroyed := []string{}
	originalRunDown := runDownFunc
	defer func() { runDownFunc = originalRunDown }()
	runDownFunc = func(opts DownOptions, provider Provider, logger *Logger) error {
		destroyed = append(destroyed, opts.Name)
		return nil
	}
	var logs bytes.Buffer
	logger := NewLogger(LevelInfo)
	logger.base = log.New(&logs, "", 0)

	err := clearDanglingDeployments("org/repo", GithubSyncOptions{
		Label:    "pullpreview-custom",
		AlwaysOn: []string{"main"},
	}, provider, client, logger)
	if err != nil {
		t.Fatalf("clearDanglingDeployments() error: %v", err)
	}

	sort.Strings(destroyed)
	wantDestroyed := []string{"gh-1-branch-feature-x", "gh-1-pr-11"}
	if strings.Join(destroyed, ",") != strings.Join(wantDestroyed, ",") {
		t.Fatalf("unexpected destroyed instances: got=%v want=%v", destroyed, wantDestroyed)
	}
	if provider.lastTags["stack"] != StackName || provider.lastTags["org_name"] != "org" || provider.lastTags["repo_name"] != "repo" {
		t.Fatalf("unexpected repo cleanup list tags: %#v", provider.lastTags)
	}
	if len(client.listIssueLabels) != 1 || client.listIssueLabels[0] != "pullpreview-custom" {
		t.Fatalf("expected custom label lookup, got %v", client.listIssueLabels)
	}
	if len(client.removedLabelPRs) != 1 || client.removedLabelPRs[0] != 11 {
		t.Fatalf("expected closed PR label cleanup for PR#11, got %v", client.removedLabelPRs)
	}
	logOutput := logs.String()
	if !strings.Contains(logOutput, "Active instances: gh-1-branch-main, gh-1-pr-10") {
		t.Fatalf("missing active instances report in logs: %s", logOutput)
	}
	if !strings.Contains(logOutput, "Dangling removed: gh-1-branch-feature-x, gh-1-pr-11") {
		t.Fatalf("missing dangling removed report in logs: %s", logOutput)
	}
}

func TestClearDanglingDeploymentsScopesCleanupByDeploymentVariant(t *testing.T) {
	client := &fakeGitHub{}
	provider := &scheduledCleanupProvider{
		instances: []InstanceSummary{
			{Name: "gh-1-env1-pr-10", Tags: map[string]string{"pr_number": "10", "pullpreview_variant": "env1"}},
			{Name: "gh-1-env2-pr-20", Tags: map[string]string{"pr_number": "20", "pullpreview_variant": "env2"}},
			{Name: "gh-1-env1-pr-30", Tags: map[string]string{}}, // legacy env1 instance without variant tag
			{Name: "gh-1-env2-pr-40", Tags: map[string]string{}}, // legacy env2 instance without variant tag
		},
	}
	destroyed := []string{}
	originalRunDown := runDownFunc
	defer func() { runDownFunc = originalRunDown }()
	runDownFunc = func(opts DownOptions, provider Provider, logger *Logger) error {
		destroyed = append(destroyed, opts.Name)
		return nil
	}

	err := clearDanglingDeployments("org/repo", GithubSyncOptions{
		Label:             "pullpreview",
		DeploymentVariant: "env1",
	}, provider, client, nil)
	if err != nil {
		t.Fatalf("clearDanglingDeployments() error: %v", err)
	}

	sort.Strings(destroyed)
	wantDestroyed := []string{"gh-1-env1-pr-10", "gh-1-env1-pr-30"}
	if strings.Join(destroyed, ",") != strings.Join(wantDestroyed, ",") {
		t.Fatalf("unexpected destroyed instances for env1 cleanup: got=%v want=%v", destroyed, wantDestroyed)
	}
}

func TestClearDanglingDeploymentsWithoutVariantSkipsVariantInstances(t *testing.T) {
	client := &fakeGitHub{}
	provider := &scheduledCleanupProvider{
		instances: []InstanceSummary{
			{Name: "gh-1-pr-10", Tags: map[string]string{"pr_number": "10"}},
			{Name: "gh-1-env1-pr-20", Tags: map[string]string{"pr_number": "20", "pullpreview_variant": "env1"}},
			{Name: "gh-1-env2-pr-30", Tags: map[string]string{}}, // legacy env2 instance without variant tag
		},
	}
	destroyed := []string{}
	originalRunDown := runDownFunc
	defer func() { runDownFunc = originalRunDown }()
	runDownFunc = func(opts DownOptions, provider Provider, logger *Logger) error {
		destroyed = append(destroyed, opts.Name)
		return nil
	}

	err := clearDanglingDeployments("org/repo", GithubSyncOptions{
		Label: "pullpreview",
	}, provider, client, nil)
	if err != nil {
		t.Fatalf("clearDanglingDeployments() error: %v", err)
	}

	sort.Strings(destroyed)
	wantDestroyed := []string{"gh-1-pr-10"}
	if strings.Join(destroyed, ",") != strings.Join(wantDestroyed, ",") {
		t.Fatalf("unexpected destroyed instances for default cleanup: got=%v want=%v", destroyed, wantDestroyed)
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
