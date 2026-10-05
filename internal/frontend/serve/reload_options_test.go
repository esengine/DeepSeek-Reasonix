package serve

import (
	"testing"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/extension"
	"reasonix/internal/session/control"
)

// A switch and a reload rebuild the same way but for opposite reasons. A model
// or effort switch reuses the serving generation because the extensions did not
// move. A reload happens because they did — so reusing the running sidecars or
// the discovered assembly hands the old code straight back, which is why an
// extension author had to restart the app to see an edit take effect.
func TestReloadDoesNotReuseTheGenerationItWasAskedToReplace(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	s := New(ctrl, bc, config.ServeConfig{})
	owner := extension.NewRuntimeOwner()
	s.lastBuild = &boot.BuildResult{
		Owner:    owner,
		Assembly: &boot.ReusedAssembly{SystemPrompt: "served"},
		Plan:     &extension.RuntimePlan{},
		Snapshot: &extension.RuntimeSnapshot{},
	}

	sw := s.rebuildOptions(ctrl, "m")
	if sw.ReuseAssembly == nil || sw.PreviousPlan == nil || sw.PreviousSnapshot == nil {
		t.Fatalf("a switch must reuse the serving generation, got %+v", sw.RuntimeReload)
	}

	rl := s.reloadOptions(ctrl, "m")
	if rl.ReuseAssembly != nil {
		t.Error("reload reused the discovered assembly; edited skills/commands would not be rescanned")
	}
	if rl.Extensions != nil {
		t.Error("reload reused the running sidecar manager; an edited runtime would keep serving old code")
	}
	if rl.PreviousPlan != nil || rl.PreviousSnapshot != nil || rl.PreviousDispatcher != nil || rl.Graph != nil {
		t.Errorf("reload carried the previous generation forward: %+v", rl.RuntimeReload)
	}
	if rl.Generation != 0 {
		t.Errorf("reload pinned generation %d; a replacement starts its own", rl.Generation)
	}
	// The owner is the session lineage, not the extension state: dropping it
	// would strand the previous generation instead of draining it.
	if rl.Owner != owner {
		t.Error("reload must keep the session lineage's runtime owner")
	}
	// Everything not about extension reuse still has to survive the reload.
	if rl.Model != sw.Model || rl.WorkspaceRoot != sw.WorkspaceRoot || rl.SessionDir != sw.SessionDir {
		t.Errorf("reload lost workspace identity: %+v vs %+v", rl, sw)
	}
}
