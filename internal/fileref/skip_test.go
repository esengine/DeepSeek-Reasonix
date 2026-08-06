package fileref

import (
	"path/filepath"
	"testing"
)

func TestSkipEntryHidesBuildOutputDirectories(t *testing.T) {
	for _, name := range []string{"target", "build", "dist", "__pycache__", ".venv", "node_modules"} {
		if !SkipEntry(name, name, true) {
			t.Errorf("%q is a build output and must stay out of file pickers", name)
		}
	}
}

func TestSkipEntryKeepsSourceDirectories(t *testing.T) {
	for _, name := range []string{"src", "internal", "docs", "targets", "buildkite"} {
		if SkipEntry(name, name, true) {
			t.Errorf("%q is not a build output and must remain browsable", name)
		}
	}
}

func TestSkipEntryIgnoresBuildNamesOnFiles(t *testing.T) {
	if SkipEntry("cmd/build", "build", false) {
		t.Error("a file named build is source, not a build directory")
	}
}

func TestSearchSkipsGeneratedClassFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "main", "java", "App.java"))
	writeFile(t, filepath.Join(root, "target", "classes", "App.class"))

	got := resultPaths(Search(root, "app", 50))
	for _, path := range got {
		if path == "target/classes/App.class" {
			t.Fatalf("generated output leaked into @ results: %v", got)
		}
	}
	if len(got) == 0 {
		t.Fatalf("the source file should still match: %v", got)
	}
}
