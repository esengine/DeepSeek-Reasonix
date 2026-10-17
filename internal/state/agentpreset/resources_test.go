package agentpreset

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRetainedDirectorySurvivesSourceChanges(t *testing.T) {
	s := New(t.TempDir(), t.TempDir())
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, ".git"), []byte("gitdir: private worktree metadata"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "playbook.md"), []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	asset, err := CaptureDirectory(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(asset.Files) != 1 || asset.Files[0].Path != "playbook.md" {
		t.Fatal("resource snapshot retained worktree metadata")
	}
	if err := os.WriteFile(filepath.Join(source, "playbook.md"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := s.Materialize(asset)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "playbook.md"))
	if err != nil || string(b) != "original" {
		t.Fatalf("asset %s %v", b, err)
	}
}

func TestAssetCannotEscapeItsManagedDirectory(t *testing.T) {
	s := New(t.TempDir(), t.TempDir())
	for _, file := range []AssetFile{{Path: "../outside", Data: []byte("bad")}, {Path: "link", Link: "../../outside"}} {
		if _, err := s.Materialize(DirectorySnapshot{Files: []AssetFile{file}}); err == nil {
			t.Fatalf("escaped asset: %+v", file)
		}
	}
}

func TestHookBundleRetainsSiblingImportsAfterSourceDeletion(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, ".git", "config"), []byte("private VCS metadata"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"entry.py": "from helper import value", "helper.py": "value = 'original'"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	asset, paths, err := CaptureHookResources([]string{filepath.Join(source, "entry.py")})
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range asset.Files {
		if filepath.Base(file.Path) == "config" {
			t.Fatal("hook bundle retained VCS metadata")
		}
	}
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	root, err := New(t.TempDir(), t.TempDir()).Materialize(asset)
	if err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, paths[filepath.Join(source, "entry.py")])
	helper, err := os.ReadFile(filepath.Join(filepath.Dir(entry), "helper.py"))
	if err != nil || string(helper) != "value = 'original'" {
		t.Fatalf("sibling import changed: %s %v", helper, err)
	}
}
