package serve

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// adoptedPane is the window's own runtime: assembled by the host, then handed
// to the hub.
func adoptedPane(t *testing.T, mode string) (*Hub, *Server, *Runtime) {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	closeSharedCatalogsOnCleanup(t)
	writeLocalProviderConfig(t)
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, SessionDir: testenv.TempDir(t)})
	ctrl.SetToolApprovalMode(mode)
	srv := New(ctrl, bc, config.ServeConfig{})
	h := NewHub(HubOptions{})
	t.Cleanup(h.Shutdown)
	rt, err := h.Adopt(srv, bc)
	if err != nil {
		t.Fatalf("adopt: %v", err)
	}
	return h, srv, rt
}

// The posture is a window setting, and a window outlives any one conversation.
// Reading it off whichever pane was first meant closing that conversation put
// the next one back to ask — an approval prompt on a tool the person had
// already waved through, with nothing on screen saying why.
func TestANewPaneKeepsThePostureAfterTheOneThatSetItCloses(t *testing.T) {
	h, srv, first := adoptedPane(t, control.ToolApprovalAsk)

	srv.applyApprovalMode(control.ToolApprovalYolo)
	if err := h.Close(first.ID); err != nil {
		t.Fatalf("close: %v", err)
	}

	next, err := h.Open(context.Background(), OpenRequest{Root: testenv.TempDir(t)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := next.Server.Controller().ToolApprovalMode(); got != control.ToolApprovalYolo {
		t.Fatalf("pane opened in %q, want the %q the composer was left on", got, control.ToolApprovalYolo)
	}
}

// Nobody has touched the composer yet, so the posture is still the one the host
// launched in — on the desktop, the setting read out of the user's config.
func TestAPaneOpensInThePostureTheHostLaunchedIn(t *testing.T) {
	h, _, _ := adoptedPane(t, control.ToolApprovalAuto)

	next, err := h.Open(context.Background(), OpenRequest{Root: testenv.TempDir(t)})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if got := next.Server.Controller().ToolApprovalMode(); got != control.ToolApprovalAuto {
		t.Fatalf("pane opened in %q, want the launch posture %q", got, control.ToolApprovalAuto)
	}
}

// writeLocalProviderConfig gives Open a model to assemble against. The base URL
// is unreachable on purpose: these panes are never asked to run a turn.
func writeLocalProviderConfig(t *testing.T) {
	t.Helper()
	path := config.UserConfigPath()
	if path == "" {
		t.Fatal("user config path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `default_model = "default/shared-chat"

[[providers]]
name = "default"
kind = "openai"
base_url = "http://127.0.0.1:1/v1"
models = ["shared-chat"]
default = "shared-chat"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
