package github

import (
	"context"
	"strings"

	gh "github.com/google/go-github/v60/github"
	"golang.org/x/oauth2"
)

type Client struct {
	api *gh.Client
	ctx context.Context
}

func New(token string) *Client {
	return NewWithContext(context.Background(), token)
}

func NewWithContext(ctx context.Context, token string) *Client {
	if ctx == nil {
		ctx = context.Background()
	}
	if token == "" {
		return &Client{api: gh.NewClient(nil), ctx: ctx}
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	client := oauth2.NewClient(ctx, ts)
	return &Client{api: gh.NewClient(client), ctx: ctx}
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
	issues, _, err := c.api.Issues.ListByRepo(c.ctx, owner, name, opts)
	return issues, err
}

func (c *Client) GetPullRequest(repo string, number int) (*gh.PullRequest, error) {
	owner, name := splitRepo(repo)
	pr, _, err := c.api.PullRequests.Get(c.ctx, owner, name, number)
	return pr, err
}

func (c *Client) CreateCommitStatus(repo, sha, state, targetURL, statusContext, description string) error {
	owner, name := splitRepo(repo)
	status := &gh.RepoStatus{State: gh.String(state), Context: gh.String(statusContext), Description: gh.String(description)}
	if targetURL != "" {
		status.TargetURL = gh.String(targetURL)
	}
	_, _, err := c.api.Repositories.CreateStatus(c.ctx, owner, name, sha, status)
	return err
}

func (c *Client) RemoveLabel(repo string, number int, label string) error {
	owner, name := splitRepo(repo)
	_, err := c.api.Issues.RemoveLabelForIssue(c.ctx, owner, name, number, label)
	return err
}

func (c *Client) ListIssueComments(repo string, number int) ([]*gh.IssueComment, error) {
	owner, name := splitRepo(repo)
	comments, _, err := c.api.Issues.ListComments(c.ctx, owner, name, number, &gh.IssueListCommentsOptions{
		ListOptions: gh.ListOptions{PerPage: 100},
	})
	return comments, err
}

func (c *Client) CreateIssueComment(repo string, number int, body string) error {
	owner, name := splitRepo(repo)
	_, _, err := c.api.Issues.CreateComment(c.ctx, owner, name, number, &gh.IssueComment{
		Body: gh.String(body),
	})
	return err
}

func (c *Client) UpdateIssueComment(repo string, commentID int64, body string) error {
	owner, name := splitRepo(repo)
	_, _, err := c.api.Issues.EditComment(c.ctx, owner, name, commentID, &gh.IssueComment{
		Body: gh.String(body),
	})
	return err
}

func (c *Client) ListPullRequests(repo, head string) ([]*gh.PullRequest, error) {
	owner, name := splitRepo(repo)
	opts := &gh.PullRequestListOptions{State: "open", Head: head, ListOptions: gh.ListOptions{PerPage: 100}}
	prs, _, err := c.api.PullRequests.List(c.ctx, owner, name, opts)
	return prs, err
}

func (c *Client) LatestCommitSHA(repo, ref string) (string, error) {
	owner, name := splitRepo(repo)
	opts := &gh.CommitsListOptions{SHA: ref, ListOptions: gh.ListOptions{PerPage: 1}}
	commits, _, err := c.api.Repositories.ListCommits(c.ctx, owner, name, opts)
	if err != nil || len(commits) == 0 {
		return "", err
	}
	return commits[0].GetSHA(), nil
}

func (c *Client) ListCollaborators(repo string) ([]*gh.User, bool, error) {
	owner, name := splitRepo(repo)
	opts := &gh.ListCollaboratorsOptions{
		Affiliation: "all",
		Permission:  "push",
		ListOptions: gh.ListOptions{PerPage: 100},
	}
	users, resp, err := c.api.Repositories.ListCollaborators(c.ctx, owner, name, opts)
	if err != nil {
		return nil, false, err
	}
	return users, resp != nil && resp.NextPage != 0, nil
}

func (c *Client) ListUserPublicKeys(user string) ([]string, error) {
	keys, _, err := c.api.Users.ListKeys(c.ctx, user, &gh.ListOptions{PerPage: 100})
	if err != nil {
		return nil, err
	}
	result := []string{}
	for _, key := range keys {
		value := strings.TrimSpace(key.GetKey())
		if value != "" {
			result = append(result, value)
		}
	}
	return result, nil
}
