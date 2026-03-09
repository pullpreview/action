package main

import (
	"context"
	"flag"
	"testing"

	"github.com/pullpreview/action/internal/pullpreview"
)

func TestDefaultUpNameFromLocalPath(t *testing.T) {
	got := defaultUpName("path/to/example-app")
	want := "local-example-app"
	if got != want {
		t.Fatalf("defaultUpName()=%q, want %q", got, want)
	}
}

func TestDefaultUpNameFromURL(t *testing.T) {
	got := defaultUpName("https://github.com/pullpreview/action.git#main")
	want := "local-action-git"
	if got != want {
		t.Fatalf("defaultUpName()=%q, want %q", got, want)
	}
}

func TestSplitCommaList(t *testing.T) {
	got := splitCommaList("a, b,,c")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("unexpected split result: %#v", got)
	}
}

func TestParseTags(t *testing.T) {
	got := parseTags([]string{"repo:action,org:pullpreview", "repo:override"})
	if got["repo"] != "override" || got["org"] != "pullpreview" {
		t.Fatalf("unexpected tags: %#v", got)
	}
}

func TestSplitLeadingPositional(t *testing.T) {
	first, rest := splitLeadingPositional([]string{"examples/wordpress", "--registries", "docker://token@ghcr.io"})
	if first != "examples/wordpress" {
		t.Fatalf("unexpected first positional: %q", first)
	}
	if len(rest) != 2 || rest[0] != "--registries" {
		t.Fatalf("unexpected remaining args: %#v", rest)
	}
}

func TestSplitLeadingPositionalWhenFlagsFirst(t *testing.T) {
	first, rest := splitLeadingPositional([]string{"--registries", "docker://token@ghcr.io", "examples/wordpress"})
	if first != "" {
		t.Fatalf("expected no leading positional when flags are first, got %q", first)
	}
	if len(rest) != 3 {
		t.Fatalf("unexpected remaining args: %#v", rest)
	}
}

func TestRegisterCommonFlagsParsesHelmOptions(t *testing.T) {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	values := registerCommonFlags(fs)
	if err := fs.Parse([]string{
		"--provider", "hetzner",
		"--deployment-target", "helm",
		"--chart", "wordpress",
		"--chart-repository", "https://charts.bitnami.com/bitnami",
		"--chart-values", "values.yaml,values.preview.yaml",
		"--chart-set", "image.tag=123,ingress.host={{ release_name }}.preview.run",
	}); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	opts := values.ToOptions(context.Background())
	if opts.ProviderName != "hetzner" {
		t.Fatalf("expected provider name hetzner, got %q", opts.ProviderName)
	}
	if opts.DeploymentTarget != pullpreview.DeploymentTargetHelm {
		t.Fatalf("expected helm deployment target, got %q", opts.DeploymentTarget)
	}
	if opts.Chart != "wordpress" {
		t.Fatalf("unexpected chart: %q", opts.Chart)
	}
	if opts.ChartRepository != "https://charts.bitnami.com/bitnami" {
		t.Fatalf("unexpected chart repository: %q", opts.ChartRepository)
	}
	if len(opts.ChartValues) != 2 || opts.ChartValues[0] != "values.yaml" || opts.ChartValues[1] != "values.preview.yaml" {
		t.Fatalf("unexpected chart values: %#v", opts.ChartValues)
	}
	if len(opts.ChartSet) != 2 {
		t.Fatalf("unexpected chart set values: %#v", opts.ChartSet)
	}
}

func TestRegisterCommonFlagsDefaultsToCompose(t *testing.T) {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	values := registerCommonFlags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	opts := values.ToOptions(context.Background())
	if opts.DeploymentTarget != pullpreview.DeploymentTargetCompose {
		t.Fatalf("expected compose deployment target by default, got %q", opts.DeploymentTarget)
	}
	if len(opts.ComposeFiles) != 1 || opts.ComposeFiles[0] != "docker-compose.yml" {
		t.Fatalf("unexpected compose files: %#v", opts.ComposeFiles)
	}
	if len(opts.ComposeOptions) != 1 || opts.ComposeOptions[0] != "--build" {
		t.Fatalf("unexpected compose options: %#v", opts.ComposeOptions)
	}
	if len(opts.ChartValues) != 0 || len(opts.ChartSet) != 0 {
		t.Fatalf("expected empty helm options by default, got values=%#v set=%#v", opts.ChartValues, opts.ChartSet)
	}
}

func TestRegisterCommonFlagsNormalizesDeploymentTarget(t *testing.T) {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	values := registerCommonFlags(fs)
	if err := fs.Parse([]string{"--deployment-target", "HeLm"}); err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	opts := values.ToOptions(context.Background())
	if opts.DeploymentTarget != pullpreview.DeploymentTargetHelm {
		t.Fatalf("expected normalized helm target, got %q", opts.DeploymentTarget)
	}
}
