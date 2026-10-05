package serve

import (
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// A model or effort switch rebuilds the runtime. Before this, the replacement
// resolved its own root from the process working directory and listed the
// global session dir, so flipping effort swapped the sidebar for another
// project's conversations and continued the transcript into the global dir.
func TestRebuildOptionsKeepsWorkspace(t *testing.T) {
	root := testenv.TempDir(t)
	sessions := filepath.Join(root, "sessions")
	ctrl := control.New(control.Options{WorkspaceRoot: root, SessionDir: sessions})
	defer ctrl.Close()
	s := New(ctrl, NewBroadcaster(), config.ServeConfig{})

	opts := s.rebuildOptions(s.Controller(), "deepseek/deepseek-v4")

	if opts.WorkspaceRoot != root {
		t.Errorf("WorkspaceRoot = %q, want %q", opts.WorkspaceRoot, root)
	}
	if opts.SessionDir != sessions {
		t.Errorf("SessionDir = %q, want %q", opts.SessionDir, sessions)
	}
	if opts.Model != "deepseek/deepseek-v4" {
		t.Errorf("Model = %q, want the switch target", opts.Model)
	}
}

func TestRebuildOptionsWithoutController(t *testing.T) {
	s := New(control.New(control.Options{}), NewBroadcaster(), config.ServeConfig{})
	if got := s.rebuildOptions(nil, "ref"); got.WorkspaceRoot != "" || got.SessionDir != "" {
		t.Errorf("rebuildOptions(nil) = %+v, want no workspace to inherit", got)
	}
}
