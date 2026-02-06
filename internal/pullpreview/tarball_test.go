package pullpreview

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateTarballExcludesGitDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "docker-compose.yml"), []byte("services: {}"), 0644); err != nil {
		t.Fatalf("failed writing compose file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0755); err != nil {
		t.Fatalf("failed creating nested dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "nested", "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatalf("failed writing nested file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatalf("failed creating .git dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("gitconfig"), 0644); err != nil {
		t.Fatalf("failed writing .git config: %v", err)
	}

	tarball, cleanup, err := CreateTarball(root)
	if err != nil {
		t.Fatalf("CreateTarball() error: %v", err)
	}
	defer cleanup()

	entries, err := readTarEntries(tarball)
	if err != nil {
		t.Fatalf("failed reading tarball entries: %v", err)
	}

	if !entries["docker-compose.yml"] {
		t.Fatalf("expected docker-compose.yml entry, got %#v", entries)
	}
	if !entries["nested/file.txt"] {
		t.Fatalf("expected nested/file.txt entry, got %#v", entries)
	}
	if entries[".git/config"] || entries[".git"] {
		t.Fatalf("expected .git entries to be excluded, got %#v", entries)
	}
}

func readTarEntries(path string) (map[string]bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	gz, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	reader := tar.NewReader(gz)
	result := map[string]bool{}
	for {
		header, err := reader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		result[header.Name] = true
	}
	return result, nil
}
