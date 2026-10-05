package serve

import (
	"path/filepath"
	"testing"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
)

type countingSink struct {
	inner event.Sink
	seen  int
}

func (c *countingSink) Emit(e event.Event) {
	c.seen++
	c.inner.Emit(e)
}

func serverWithDecoratedPane(t *testing.T) (*Server, *countingSink) {
	t.Helper()
	root := testenv.TempDir(t)
	ctrl := control.New(control.Options{WorkspaceRoot: root, SessionDir: filepath.Join(root, "sessions")})
	t.Cleanup(ctrl.Close)
	bc := NewBroadcaster()
	pane := &countingSink{inner: bc}
	s := New(ctrl, bc, config.ServeConfig{})
	s.SetPaneSink(pane)
	return s, pane
}

// A rebuild wired to the bare broadcaster strands the host's observers on the
// previous TurnDone — a switch is refused while a turn runs, so that is the
// last thing they saw — and nothing corrects it for the pane's whole life. The
// tray said "nothing running" through a fourteen-minute turn, and saving an
// API key was enough to get there (#9448).
func TestRebuiltRuntimesKeepThePaneDecoration(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts func(*testing.T, *Server) boot.Options
	}{
		{"model, effort or extension rebuild", func(t *testing.T, s *Server) boot.Options {
			return s.rebuildOptions(s.Controller(), "deepseek/deepseek-v4")
		}},
		{"workspace switch", func(t *testing.T, s *Server) boot.Options {
			return s.workspaceOptions(testenv.TempDir(t), "deepseek/deepseek-v4")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, pane := serverWithDecoratedPane(t)
			tc.opts(t, s).Sink.Emit(event.Event{Kind: event.TurnStarted})
			if pane.seen != 1 {
				t.Fatalf("the pane's decoration saw %d of the rebuilt runtime's events, want the one it emitted", pane.seen)
			}
		})
	}
}

// A host that decorated nothing is served by the broadcaster itself.
func TestRebuildFallsBackToTheBroadcaster(t *testing.T) {
	root := testenv.TempDir(t)
	ctrl := control.New(control.Options{WorkspaceRoot: root, SessionDir: filepath.Join(root, "sessions")})
	defer ctrl.Close()
	bc := NewBroadcaster()
	s := New(ctrl, bc, config.ServeConfig{})
	if got := s.rebuildOptions(s.Controller(), "ref").Sink; got != event.Sink(bc) {
		t.Fatalf("rebuild sink = %v, want the broadcaster", got)
	}
}
