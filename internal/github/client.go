package github

import (
	"context"
	"strings"

	gh "github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

type Client struct {
	api *gh.Client
}

func New(token string) *Client {
	ctx := context.Background()
	if token == "" {
		return &Client{api: gh.NewClient(nil)}
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	client := oauth2.NewClient(ctx, ts)
	return &Client{api: gh.NewClient(client)}
}

func splitRepo(repo string) (string, string) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return repo, ""
}

func (c *Client) ListIssues(repo, label string) ([]*gh.Issue, error) {
	owner, name := splitRepo(repo)
	opts := &gh.IssueListByRepoOptions{State: "all", Labels: []string{label}, ListOptions: gh.ListOptions{PerPage: 100}}
	issues, _, err := c.api.Issues.ListByRepo(context.Background(), owner, name, opts)
	return issues, err
}

func (c *Client) GetPullRequest(repo string, number int) (*gh.PullRequest, error) {
	owner, name := splitRepo(repo)
	pr, _, err := c.api.PullRequests.Get(context.Background(), owner, name, number)
	return pr, err
}

func (c *Client) ListEnvironments(repo string) ([]*gh.Environment, error) {
	owner, name := splitRepo(repo)
	envs, _, err := c.api.Repositories.ListEnvironments(context.Background(), owner, name, &gh.EnvironmentListOptions{
		ListOptions: gh.ListOptions{PerPage: 100},
	})
	if err != nil {
		return nil, err
	}
	return envs.Environments, nil
}

func (c *Client) ListDeployments(repo, environment, ref string) ([]*gh.Deployment, error) {
	owner, name := splitRepo(repo)
	opts := &gh.DeploymentsListOptions{Environment: environment}
	if ref != "" {
		opts.Ref = ref
	}
	deploys, _, err := c.api.Repositories.ListDeployments(context.Background(), owner, name, opts)
	return deploys, err
}

func (c *Client) CreateDeployment(repo, ref, environment string) (*gh.Deployment, error) {
	owner, name := splitRepo(repo)
	requiredContexts := []string{}
	req := &gh.DeploymentRequest{
		Ref:              gh.String(ref),
		AutoMerge:        gh.Bool(false),
		Environment:      gh.String(environment),
		RequiredContexts: &requiredContexts,
	}
	deployment, _, err := c.api.Repositories.CreateDeployment(context.Background(), owner, name, req)
	return deployment, err
}

func (c *Client) CreateDeploymentStatus(repo string, deploymentID int64, state string, environmentURL string, autoInactive bool) error {
	owner, name := splitRepo(repo)
	request := &gh.DeploymentStatusRequest{State: gh.String(state), AutoInactive: gh.Bool(autoInactive)}
	if environmentURL != "" {
		request.EnvironmentURL = gh.String(environmentURL)
	}
	_, _, err := c.api.Repositories.CreateDeploymentStatus(context.Background(), owner, name, deploymentID, request)
	return err
}

func (c *Client) CreateCommitStatus(repo, sha, state, targetURL, statusContext, description string) error {
	owner, name := splitRepo(repo)
	status := &gh.RepoStatus{State: gh.String(state), Context: gh.String(statusContext), Description: gh.String(description)}
	if targetURL != "" {
		status.TargetURL = gh.String(targetURL)
	}
	_, _, err := c.api.Repositories.CreateStatus(context.Background(), owner, name, sha, status)
	return err
}

func (c *Client) DeleteDeployment(repo string, deploymentID int64) error {
	owner, name := splitRepo(repo)
	_, err := c.api.Repositories.DeleteDeployment(context.Background(), owner, name, deploymentID)
	return err
}

func (c *Client) DeleteEnvironment(repo, name string) error {
	owner, repoName := splitRepo(repo)
	_, err := c.api.Repositories.DeleteEnvironment(context.Background(), owner, repoName, name)
	return err
}

func (c *Client) RemoveLabel(repo string, number int, label string) error {
	owner, name := splitRepo(repo)
	_, err := c.api.Issues.RemoveLabelForIssue(context.Background(), owner, name, number, label)
	return err
}

func (c *Client) ListPullRequests(repo, head string) ([]*gh.PullRequest, error) {
	owner, name := splitRepo(repo)
	opts := &gh.PullRequestListOptions{State: "open", Head: head, ListOptions: gh.ListOptions{PerPage: 100}}
	prs, _, err := c.api.PullRequests.List(context.Background(), owner, name, opts)
	return prs, err
}

func (c *Client) LatestCommitSHA(repo, ref string) (string, error) {
	owner, name := splitRepo(repo)
	opts := &gh.CommitsListOptions{SHA: ref, ListOptions: gh.ListOptions{PerPage: 1}}
	commits, _, err := c.api.Repositories.ListCommits(context.Background(), owner, name, opts)
	if err != nil || len(commits) == 0 {
		return "", err
	}
	return commits[0].GetSHA(), nil
}

func (c *Client) ListCollaborators(repo string) ([]*gh.User, error) {
	owner, name := splitRepo(repo)
	users, _, err := c.api.Repositories.ListCollaborators(context.Background(), owner, name, nil)
	return users, err
}
