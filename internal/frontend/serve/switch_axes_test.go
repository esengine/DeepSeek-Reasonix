package serve

import (
	"context"
	"testing"

	"reasonix/internal/session/control"
)

// A model or effort switch rebuilds the runtime, and serve used to re-apply
// only the session authorizations: the replacement came up with default
// approval and plan modes, so flipping effort silently turned YOLO and plan
// mode back off. boot.ApplyRuntimeMigration is the one definition of what a
// rebuild carries, and serve now goes through it.
func TestSwitchModelKeepsSessionAxes(t *testing.T) {
	bc := NewBroadcaster()
	old := control.New(control.Options{Sink: bc})
	defer old.Close()
	old.SetToolApprovalMode("yolo")
	old.SetPlanMode(true)

	s := &Server{ctrl: old, bc: bc}
	var built *control.Controller
	s.buildController = func(context.Context, string) (*control.Controller, error) {
		built = control.New(control.Options{Sink: bc})
		return built, nil
	}

	if err := s.switchModel(context.Background(), "next-model"); err != nil {
		t.Fatalf("switchModel: %v", err)
	}
	if built == nil {
		t.Fatal("no replacement controller was built")
	}
	defer built.Close()

	if got := built.ToolApprovalMode(); got != "yolo" {
		t.Errorf("tool approval mode after switch = %q, want yolo", got)
	}
	if !built.PlanMode() {
		t.Error("plan mode was reset by the switch")
	}
}
