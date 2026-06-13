package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveTerminalStartDirDirectory(t *testing.T) {
	base := t.TempDir()
	dev := filepath.Join(base, "dev")
	if err := os.MkdirAll(dev, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := resolveTerminalStartDir(base, "dev")
	if err != nil {
		t.Fatalf("resolveTerminalStartDir(dev) err = %v", err)
	}
	if got != dev {
		t.Fatalf("resolveTerminalStartDir(dev) = %q, want %q", got, dev)
	}
}

func TestResolveTerminalStartDirFileUsesParent(t *testing.T) {
	base := t.TempDir()
	readme := filepath.Join(base, "README.md")
	if err := os.WriteFile(readme, []byte("# test"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveTerminalStartDir(base, "README.md")
	if err != nil {
		t.Fatalf("resolveTerminalStartDir(README.md) err = %v", err)
	}
	if got != base {
		t.Fatalf("resolveTerminalStartDir(README.md) = %q, want %q", got, base)
	}
}

func TestResolveTerminalStartDirNestedFile(t *testing.T) {
	base := t.TempDir()
	nestedDir := filepath.Join(base, "docs")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nestedFile := filepath.Join(nestedDir, "guide.md")
	if err := os.WriteFile(nestedFile, []byte("guide"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := resolveTerminalStartDir(base, "docs/guide.md")
	if err != nil {
		t.Fatalf("resolveTerminalStartDir(docs/guide.md) err = %v", err)
	}
	if got != nestedDir {
		t.Fatalf("resolveTerminalStartDir(docs/guide.md) = %q, want %q", got, nestedDir)
	}
}

func TestResolveTerminalStartDirOutsideWorkspace(t *testing.T) {
	base := t.TempDir()
	_, err := resolveTerminalStartDir(base, "../outside")
	if err == nil {
		t.Fatal("resolveTerminalStartDir(../outside) err = nil, want outside workspace")
	}
	if err != errTerminalOutsideWorkspace {
		t.Fatalf("resolveTerminalStartDir(../outside) err = %v, want %v", err, errTerminalOutsideWorkspace)
	}
}

func TestStartTerminalRelativeInvalidPath(t *testing.T) {
	base := t.TempDir()
	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"project": {ID: "project", Scope: "project", WorkspaceRoot: base, Ready: true},
	}
	app.activeTabID = "project"

	got := app.startTerminalRelative("../outside")
	if got != "invalid path (outside workspace)" {
		t.Fatalf("startTerminalRelative(../outside) = %q, want invalid path (outside workspace)", got)
	}
}

func TestTerminalSessionsLifecycle(t *testing.T) {
	ts := newTerminalSessions()
	id := ts.add(&terminalSession{})
	if id != "term-0" {
		t.Fatalf("add() = %q, want term-0", id)
	}
	if ts.get(id) == nil {
		t.Fatal("get() returned nil for added session")
	}
	ts.remove(id)
	if ts.get(id) != nil {
		t.Fatal("get() returned session after remove")
	}
}
