package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	gh "github.com/google/go-github/v60/github"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	api := gh.NewClient(server.Client())
	baseURL, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatalf("failed to parse base URL: %v", err)
	}
	api.BaseURL = baseURL
	api.UploadURL = baseURL

	return &Client{api: api, ctx: context.Background()}
}

func TestNew(t *testing.T) {
	if New("") == nil {
		t.Fatalf("New(\"\") returned nil")
	}
	if New("token") == nil {
		t.Fatalf("New(token) returned nil")
	}
}

func TestSplitRepo(t *testing.T) {
	owner, repo := splitRepo("org/name")
	if owner != "org" || repo != "name" {
		t.Fatalf("splitRepo(org/name)=(%q,%q)", owner, repo)
	}
	owner, repo = splitRepo("single")
	if owner != "single" || repo != "" {
		t.Fatalf("splitRepo(single)=(%q,%q)", owner, repo)
	}
}

func TestListIssues(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/repos/org/repo/issues" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("labels"); got != "pullpreview" {
			t.Fatalf("unexpected labels query: %q", got)
		}
		_, _ = w.Write([]byte(`[{"number":10}]`))
	})

	issues, err := client.ListIssues("org/repo", "pullpreview")
	if err != nil {
		t.Fatalf("ListIssues() error: %v", err)
	}
	if len(issues) != 1 || issues[0].GetNumber() != 10 {
		t.Fatalf("unexpected issues response: %#v", issues)
	}
}

func TestGetPullRequest(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/org/repo/pulls/4" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"number":4}`))
	})
	pr, err := client.GetPullRequest("org/repo", 4)
	if err != nil {
		t.Fatalf("GetPullRequest() error: %v", err)
	}
	if pr.GetNumber() != 4 {
		t.Fatalf("unexpected PR number: %d", pr.GetNumber())
	}
}

func TestCreateCommitStatus(t *testing.T) {
	var createCommitStatusBody string

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/repos/org/repo/statuses/sha123":
			body, _ := io.ReadAll(r.Body)
			createCommitStatusBody = string(body)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":8}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	err := client.CreateCommitStatus("org/repo", "sha123", "pending", "https://example.test", "PullPreview", "Environment deploying")
	if err != nil {
		t.Fatalf("CreateCommitStatus() error: %v", err)
	}
	if !strings.Contains(createCommitStatusBody, `"state":"pending"`) || !strings.Contains(createCommitStatusBody, `"context":"PullPreview"`) {
		t.Fatalf("unexpected create commit status body: %s", createCommitStatusBody)
	}
}

func TestDeleteOperations(t *testing.T) {
	calls := map[string]bool{}
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		calls[key] = true
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.RemoveLabel("org/repo", 10, "pullpreview"); err != nil {
		t.Fatalf("RemoveLabel() error: %v", err)
	}

	expected := []string{
		"DELETE /repos/org/repo/issues/10/labels/pullpreview",
	}
	for _, key := range expected {
		if !calls[key] {
			t.Fatalf("expected call %q missing; calls=%v", key, calls)
		}
	}
}

func TestIssueCommentOperations(t *testing.T) {
	calls := map[string]string{}
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/org/repo/issues/10/comments":
			_, _ = w.Write([]byte(`[{"id":99,"body":"old"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/org/repo/issues/10/comments":
			body, _ := io.ReadAll(r.Body)
			calls["create"] = string(body)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":100}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/repos/org/repo/issues/comments/99":
			body, _ := io.ReadAll(r.Body)
			calls["update"] = string(body)
			_, _ = w.Write([]byte(`{"id":99}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	comments, err := client.ListIssueComments("org/repo", 10)
	if err != nil {
		t.Fatalf("ListIssueComments() error: %v", err)
	}
	if len(comments) != 1 || comments[0].GetID() != 99 {
		t.Fatalf("unexpected issue comments: %#v", comments)
	}

	if err := client.CreateIssueComment("org/repo", 10, "hello"); err != nil {
		t.Fatalf("CreateIssueComment() error: %v", err)
	}
	if !strings.Contains(calls["create"], `"body":"hello"`) {
		t.Fatalf("unexpected create comment payload: %s", calls["create"])
	}

	if err := client.UpdateIssueComment("org/repo", 99, "updated"); err != nil {
		t.Fatalf("UpdateIssueComment() error: %v", err)
	}
	if !strings.Contains(calls["update"], `"body":"updated"`) {
		t.Fatalf("unexpected update comment payload: %s", calls["update"])
	}
}

func TestPullRequestsCommitsAndCollaborators(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/org/repo/pulls":
			if got := r.URL.Query().Get("head"); got != "org:refs/heads/main" {
				t.Fatalf("unexpected head query: %q", got)
			}
			_, _ = w.Write([]byte(`[{"number":77}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/org/repo/commits":
			if got := r.URL.Query().Get("sha"); got != "refs/heads/main" {
				t.Fatalf("unexpected sha query: %q", got)
			}
			_, _ = w.Write([]byte(`[{"sha":"abc123"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/repos/org/repo/collaborators":
			if got := r.URL.Query().Get("affiliation"); got != "all" {
				t.Fatalf("unexpected affiliation query: %q", got)
			}
			if got := r.URL.Query().Get("permission"); got != "push" {
				t.Fatalf("unexpected permission query: %q", got)
			}
			if got := r.URL.Query().Get("per_page"); got != "100" {
				t.Fatalf("unexpected per_page query: %q", got)
			}
			w.Header().Set("Link", fmt.Sprintf("<http://%s/repos/org/repo/collaborators?page=2>; rel=\"next\"", r.Host))
			_, _ = w.Write([]byte(`[{"login":"alice"},{"login":"bob"}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/users/alice/keys":
			if got := r.URL.Query().Get("per_page"); got != "100" {
				t.Fatalf("unexpected per_page query for user keys: %q", got)
			}
			_, _ = w.Write([]byte(`[{"key":"ssh-ed25519 AAAA alice@dev"},{"key":"ssh-rsa BBBB alice@dev"}]`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	prs, err := client.ListPullRequests("org/repo", "org:refs/heads/main")
	if err != nil {
		t.Fatalf("ListPullRequests() error: %v", err)
	}
	if len(prs) != 1 || prs[0].GetNumber() != 77 {
		t.Fatalf("unexpected PR list: %#v", prs)
	}

	sha, err := client.LatestCommitSHA("org/repo", "refs/heads/main")
	if err != nil {
		t.Fatalf("LatestCommitSHA() error: %v", err)
	}
	if sha != "abc123" {
		t.Fatalf("unexpected sha: %q", sha)
	}

	users, truncated, err := client.ListCollaborators("org/repo")
	if err != nil {
		t.Fatalf("ListCollaborators() error: %v", err)
	}
	if len(users) != 2 || users[0].GetLogin() != "alice" || users[1].GetLogin() != "bob" {
		t.Fatalf("unexpected collaborators: %#v", users)
	}
	if !truncated {
		t.Fatalf("expected collaborators list to be marked as truncated")
	}

	keys, err := client.ListUserPublicKeys("alice")
	if err != nil {
		t.Fatalf("ListUserPublicKeys() error: %v", err)
	}
	if len(keys) != 2 || keys[0] != "ssh-ed25519 AAAA alice@dev" || keys[1] != "ssh-rsa BBBB alice@dev" {
		t.Fatalf("unexpected user keys: %#v", keys)
	}
}

func TestLatestCommitSHAEmptyList(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	})
	sha, err := client.LatestCommitSHA("org/repo", "refs/heads/main")
	if err != nil {
		t.Fatalf("LatestCommitSHA() error: %v", err)
	}
	if sha != "" {
		t.Fatalf("expected empty sha, got %q", sha)
	}
}
