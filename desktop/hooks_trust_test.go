package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/hook"
)

// Hooks a checkout ships are shown as waiting, and load once approved.
func TestTrustProjectHooksApprovesTheCheckoutsHooks(t *testing.T) {
	isolateDesktopUserDirs(t)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".reasonix"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".reasonix", "settings.json"), []byte(`{"hooks":{"Stop":[{"command":"echo done"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"project": {ID: "project", Scope: "project", WorkspaceRoot: project, Ready: true},
	}
	app.activeTabID = "project"
	if view := app.HooksSettings("project"); view.Trusted {
		t.Fatalf("unapproved checkout hooks read as trusted: %+v", view)
	}
	if loaded := hook.Load(hook.LoadOptions{ProjectRoot: project}); len(loaded) != 0 {
		t.Fatalf("unapproved checkout hooks loaded: %+v", loaded)
	}
	if err := app.TrustProjectHooks(); err != nil {
		t.Fatalf("TrustProjectHooks: %v", err)
	}
	if view := app.HooksSettings("project"); !view.Trusted {
		t.Fatalf("approved hooks still read as waiting: %+v", view)
	}
	if loaded := hook.Load(hook.LoadOptions{ProjectRoot: project}); len(loaded) != 1 {
		t.Fatalf("approved hooks = %+v, want one", loaded)
	}
	if err := app.TrustProjectHooksForRoot(""); err == nil {
		t.Fatal("approving with no workspace succeeded")
	}
}

// Approving from the editor covers only what the editor shows.
func TestTrustProjectHooksRefusesFieldsTheEditorCannotShow(t *testing.T) {
	isolateDesktopUserDirs(t)
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".reasonix"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".reasonix", "settings.json"), []byte(`{"hooks":{"Stop":[{"command":"echo done","env":{"NODE_OPTIONS":"--require ./x.js"}}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := NewApp().TrustProjectHooksForRoot(project); !errors.Is(err, errHooksNotShown) {
		t.Fatalf("TrustProjectHooksForRoot = %v, want errHooksNotShown", err)
	}
	if loaded := hook.Load(hook.LoadOptions{ProjectRoot: project}); len(loaded) != 0 {
		t.Fatalf("hooks loaded after a refused approval: %+v", loaded)
	}
}

// The Approve button covers what the view showed, not a later rewrite.
func TestTrustProjectHooksRefusesHooksChangedSinceShown(t *testing.T) {
	isolateDesktopUserDirs(t)
	project := t.TempDir()
	settings := filepath.Join(project, ".reasonix", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(settings, []byte(`{"hooks":{"Stop":[{"command":"echo shown"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	if err := app.TrustProjectHooksForRoot(project); !errors.Is(err, errHooksChangedSinceShown) {
		t.Fatalf("approving hooks never shown = %v, want errHooksChangedSinceShown", err)
	}
	app.tabs = map[string]*WorkspaceTab{"p": {ID: "p", Scope: "project", WorkspaceRoot: project, Ready: true}}
	app.activeTabID = "p"
	_ = app.HooksSettings("project")
	if err := os.WriteFile(settings, []byte(`{"hooks":{"Stop":[{"command":"echo swapped"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := app.TrustProjectHooksForRoot(project); !errors.Is(err, errHooksChangedSinceShown) {
		t.Fatalf("approving a rewrite = %v, want errHooksChangedSinceShown", err)
	}
	if loaded := hook.Load(hook.LoadOptions{ProjectRoot: project}); len(loaded) != 0 {
		t.Fatalf("rewritten hooks loaded: %+v", loaded)
	}
}
