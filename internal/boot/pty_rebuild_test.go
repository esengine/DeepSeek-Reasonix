package boot

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/pty"
)

func TestRebuildReusesPTYManager(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))

	workspace := t.TempDir()
	fenceBootTestHistoryCatalog(t)

	mgr := pty.NewManager(workspace)
	old := control.New(control.Options{
		PTY:   mgr,
		Sink:  event.Discard,
		Label: "test-pty-rebuild",
	})

	sess, err := old.PTY().Start(context.Background(), pty.StartOptions{
		ID:  "rebuild-sess",
		Cwd: workspace,
	})
	if err != nil {
		t.Fatalf("start PTY session on old controller: %v", err)
	}
	if !sess.IsRunning() {
		t.Fatalf("expected session to be running initially")
	}

	res, err := Rebuild(context.Background(), old, Options{
		WorkspaceRoot: workspace,
		Sink:          event.Discard,
		RequireKey:    false,
		Stderr:        os.Stderr,
	})
	if err != nil {
		t.Fatalf("rebuild failed: %v", err)
	}

	// Close old controller; must NOT kill active PTY session migrated to the replacement
	old.Close()
	time.Sleep(50 * time.Millisecond)

	newCtrl := res.Controller
	if newCtrl.PTY() != mgr {
		t.Fatalf("replacement controller must share the old PTY manager")
	}
	if !sess.IsRunning() {
		t.Fatalf("active PTY session was killed when old controller closed across rebuild")
	}

	// Verify session can still be written to from the replacement controller
	out, err := sess.Write(context.Background(), "echo PTY_SURVIVED_REBUILD\n", 800*time.Millisecond)
	if err != nil {
		t.Fatalf("write to surviving PTY session: %v", err)
	}
	t.Logf("PTY output after rebuild:\n%s", out)

	// Finally, closing the active controller should terminate the session
	newCtrl.Close()
	time.Sleep(50 * time.Millisecond)
	if sess.IsRunning() {
		t.Fatalf("expected PTY session to terminate when active controller closes")
	}
}
