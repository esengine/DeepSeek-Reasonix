package boot

import (
	"context"
	"os"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/platform/browser"
)

// A model switch rebuilds the runtime; the agent's browser, and the tabs it has
// open, belong to the session rather than to one generation of it.
func TestRebuildKeepsTheBrowserSession(t *testing.T) {
	isolateConfigHome(t)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	writeUserConfig(t, "[browser]\nexecutable = \"/nonexistent/chrome\"\n")
	fenceBootTestHistoryCatalog(t)
	old, err := Build(context.Background(), Options{WorkspaceRoot: workspace, Sink: event.Discard, Stderr: os.Stderr})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	session := old.BrowserSession()
	if session == nil {
		t.Fatal("an enabled browser gave the controller no session")
	}
	res, err := Rebuild(context.Background(), old, Options{WorkspaceRoot: workspace, Sink: event.Discard, Stderr: os.Stderr})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if res.Controller.BrowserSession() != session {
		t.Fatal("the rebuilt runtime started a new browser session")
	}
	old.ReleaseResources()
	open := func() browser.Code {
		_, err := session.Open(context.Background(), "https://example.com/", "", false)
		return browser.CodeOf(err)
	}
	if got := open(); got != browser.CodeEngineMissing {
		t.Fatalf("after the old runtime released it, Open = %s, want the session still open (%s)", got, browser.CodeEngineMissing)
	}
	res.Controller.Close()
	if got := open(); got != browser.CodeEngineFailed {
		t.Fatalf("after the last owner closed, Open = %s, want the closed session's %s", got, browser.CodeEngineFailed)
	}
}
