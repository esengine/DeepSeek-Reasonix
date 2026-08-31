package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
)

func TestRenameSessionResolvesVersionOutsideActiveProject(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectA := t.TempDir()
	projectB := t.TempDir()
	if err := addProject(projectA, "Project A"); err != nil {
		t.Fatal(err)
	}
	if err := addProject(projectB, "Project B"); err != nil {
		t.Fatal(err)
	}
	dirA := desktopSessionDir(projectA)
	dirB := desktopSessionDir(projectB)
	for _, dir := range []string{dirA, dirB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pathA := filepath.Join(dirA, "active.jsonl")
	pathB := filepath.Join(dirB, "history-version.jsonl")
	for _, path := range []string{pathA, pathB} {
		if err := os.WriteFile(path, []byte(`{"role":"user","content":"test"}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	app := NewApp()
	ctrl := control.New(control.Options{SessionDir: dirA, SessionPath: pathA})
	app.setTestCtrl(ctrl, projectA)
	t.Cleanup(ctrl.Close)

	if err := app.RenameSession(pathB, "Project B version note"); err != nil {
		t.Fatalf("rename non-active project version: %v", err)
	}
	meta, ok, err := agent.LoadBranchMeta(pathB)
	if err != nil || !ok || meta.CustomTitle != "Project B version note" {
		t.Fatalf("non-active version note = %+v ok=%v err=%v", meta, ok, err)
	}
	if got := loadSessionTitles(dirB)[filepath.Base(pathB)]; got != "Project B version note" {
		t.Fatalf("project B compatibility title = %q", got)
	}
	if titles := loadSessionTitles(dirA); len(titles) != 0 {
		t.Fatalf("active project title store was mutated: %+v", titles)
	}
}
