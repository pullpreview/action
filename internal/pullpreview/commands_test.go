package pullpreview

import (
	"testing"
	"time"
)

type providerSpy struct {
	terminatedName string
	lastListTags   map[string]string
	instances      []InstanceSummary
}

func (p *providerSpy) Launch(name string, opts LaunchOptions) (AccessDetails, error) {
	return AccessDetails{}, nil
}

func (p *providerSpy) Terminate(name string) error {
	p.terminatedName = name
	return nil
}

func (p *providerSpy) Running(name string) (bool, error) {
	return false, nil
}

func (p *providerSpy) ListInstances(tags map[string]string) ([]InstanceSummary, error) {
	p.lastListTags = tags
	return p.instances, nil
}

func (p *providerSpy) Username() string {
	return "ec2-user"
}

func TestRunDownNormalizesInstanceName(t *testing.T) {
	spy := &providerSpy{}
	err := RunDown(DownOptions{Name: "My Feature Branch"}, spy, nil)
	if err != nil {
		t.Fatalf("RunDown() error: %v", err)
	}
	if spy.terminatedName != "My-Feature-Branch" {
		t.Fatalf("unexpected terminated name: %q", spy.terminatedName)
	}
}

func TestRunListValidatesInput(t *testing.T) {
	spy := &providerSpy{}
	err := RunList(ListOptions{}, spy, nil)
	if err == nil {
		t.Fatalf("expected error when org/repo are empty")
	}
}

func TestRunListBuildsProviderTagFilters(t *testing.T) {
	spy := &providerSpy{
		instances: []InstanceSummary{
			{Name: "gh-1-pr-2", PublicIP: "1.2.3.4", CreatedAt: time.Unix(0, 0)},
		},
	}
	err := RunList(ListOptions{Org: "pullpreview", Repo: "action"}, spy, nil)
	if err != nil {
		t.Fatalf("RunList() error: %v", err)
	}
	if spy.lastListTags["stack"] != StackName || spy.lastListTags["org_name"] != "pullpreview" || spy.lastListTags["repo_name"] != "action" {
		t.Fatalf("unexpected list tags: %#v", spy.lastListTags)
	}
}
