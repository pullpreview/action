package main

import "testing"

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
