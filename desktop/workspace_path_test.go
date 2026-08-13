package main

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/control"
)

func TestResolveWorkspacePathForTab(t *testing.T) {
	parentRoot := robustTempDir(t)
	childRoot := filepath.Join(parentRoot, "child")
	if err := os.MkdirAll(childRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(childRoot, "shared.txt"), []byte("child"), 0o644); err != nil {
		t.Fatal(err)
	}
	app := &App{tabs: map[string]*WorkspaceTab{
		"child": {ID: "child", Scope: "project", WorkspaceRoot: childRoot},
	}}

	absolutePath, err := app.ResolveWorkspacePathForTab("child", "shared.txt")
	if err != nil || absolutePath != filepath.Join(childRoot, "shared.txt") {
		t.Fatalf("ResolveWorkspacePathForTab(child) = (%q, %v)", absolutePath, err)
	}
	if _, err := app.ResolveWorkspacePathForTab("missing", "shared.txt"); err == nil {
		t.Fatal("ResolveWorkspacePathForTab accepted a missing tab")
	}
	if _, err := app.ResolveWorkspacePathForTab("child", "../parent-only.txt"); err == nil {
		t.Fatal("ResolveWorkspacePathForTab accepted a path outside the requested tab workspace")
	}
}

func TestResolveWorkspacePathForTabExternalFolder(t *testing.T) {
	external := filepath.Join(robustTempDir(t), "Folder With Spaces")
	if err := os.MkdirAll(filepath.Join(external, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	expectedExternal := external
	if resolved, err := filepath.EvalSymlinks(external); err == nil {
		expectedExternal = resolved
	}
	ctrl := &control.Controller{}
	token, _, err := ctrl.RegisterExternalFolderRef(external)
	if err != nil {
		t.Fatalf("RegisterExternalFolderRef: %v", err)
	}
	app := &App{tabs: map[string]*WorkspaceTab{
		"project": {ID: "project", WorkspaceRoot: robustTempDir(t), Ctrl: ctrl},
	}}

	absolutePath, err := app.ResolveWorkspacePathForTab("project", token+"/src/outside.txt")
	want := filepath.Join(expectedExternal, "src", "outside.txt")
	if err != nil || absolutePath != want {
		t.Fatalf("ResolveWorkspacePathForTab external = (%q, %v), want %q", absolutePath, err, want)
	}
}
