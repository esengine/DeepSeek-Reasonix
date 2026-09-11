package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateBlankProjectCreatesOneChildDirectory(t *testing.T) {
	parent := t.TempDir()
	want := filepath.Join(parent, "new-project")

	got, err := createBlankProject(parent, "  new-project  ")
	if err != nil {
		t.Fatalf("createBlankProject: %v", err)
	}
	if got != want {
		t.Fatalf("createBlankProject path = %q, want %q", got, want)
	}
	if info, err := os.Stat(got); err != nil {
		t.Fatalf("stat created project: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("created path is not a directory: %s", got)
	}
}

func TestCreateBlankProjectRejectsUnsafeNames(t *testing.T) {
	parent := t.TempDir()
	tests := []string{"", "   ", ".", "..", "nested/project", `nested\project`, "bad\nname"}
	for _, name := range tests {
		t.Run(strings.ReplaceAll(name, "/", "_"), func(t *testing.T) {
			if got, err := createBlankProject(parent, name); err == nil {
				t.Fatalf("createBlankProject(%q) = %q, want error", name, got)
			}
		})
	}
	if entries, err := os.ReadDir(parent); err != nil {
		t.Fatal(err)
	} else if len(entries) != 0 {
		t.Fatalf("unsafe names created entries: %+v", entries)
	}
}

func TestCreateBlankProjectRejectsInvalidParent(t *testing.T) {
	parent := t.TempDir()
	file := filepath.Join(parent, "file.txt")
	if err := os.WriteFile(file, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, invalidParent := range []string{"", filepath.Join(parent, "missing"), file} {
		if got, err := createBlankProject(invalidParent, "project"); err == nil {
			t.Fatalf("createBlankProject(%q) = %q, want error", invalidParent, got)
		}
	}
}

func TestCreateBlankProjectNeverOverwritesExistingFolder(t *testing.T) {
	parent := t.TempDir()
	existing := filepath.Join(parent, "existing")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(existing, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got, err := createBlankProject(parent, "existing"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("createBlankProject existing = %q, %v; want already exists error", got, err)
	}
	if body, err := os.ReadFile(marker); err != nil {
		t.Fatalf("existing marker was removed: %v", err)
	} else if string(body) != "keep" {
		t.Fatalf("existing marker changed to %q", body)
	}
}
