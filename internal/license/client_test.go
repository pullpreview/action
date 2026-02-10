package license

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResultOK(t *testing.T) {
	if !(Result{State: "ok"}.OK()) {
		t.Fatalf("expected ok state to be true")
	}
	if (Result{State: "ko"}).OK() {
		t.Fatalf("expected ko state to be false")
	}
}

func TestCheckSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/licenses/check" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("repo_id"); got != "2" {
			t.Fatalf("unexpected query repo_id=%q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok from server"))
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL}
	result, err := client.Check(map[string]string{"org_id": "1", "repo_id": "2"})
	if err != nil {
		t.Fatalf("Check() returned error: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected ok result, got %#v", result)
	}
	if result.Message != "ok from server" {
		t.Fatalf("unexpected message: %q", result.Message)
	}
}

func TestCheckNon200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("license denied"))
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL}
	result, err := client.Check(map[string]string{"org_id": "1"})
	if err != nil {
		t.Fatalf("Check() returned error: %v", err)
	}
	if result.OK() {
		t.Fatalf("expected ko result, got %#v", result)
	}
	if result.Message != "license denied" {
		t.Fatalf("unexpected message: %q", result.Message)
	}
}

func TestCheckInvalidBaseURLGracefulFallback(t *testing.T) {
	client := &Client{BaseURL: "://bad-url"}
	result, err := client.Check(map[string]string{"org_id": "1"})
	if err != nil {
		t.Fatalf("Check() returned error: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected ok fallback, got %#v", result)
	}
	if !strings.Contains(result.Message, "License server unreachable") {
		t.Fatalf("unexpected message: %q", result.Message)
	}
}

func TestCheckUnreachableGracefulFallback(t *testing.T) {
	client := &Client{BaseURL: "http://127.0.0.1:1"}
	result, err := client.Check(map[string]string{"org_id": "1"})
	if err != nil {
		t.Fatalf("Check() returned error: %v", err)
	}
	if !result.OK() {
		t.Fatalf("expected ok fallback, got %#v", result)
	}
	if !strings.Contains(result.Message, "License server unreachable") {
		t.Fatalf("unexpected message: %q", result.Message)
	}
}
