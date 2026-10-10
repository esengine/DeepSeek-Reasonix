package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/platform/computer"
	"reasonix/internal/safety/permission"
)

func TestComputerToolsWithoutAHelperSayItIsUnavailable(t *testing.T) {
	ctx := context.Background()
	if _, err := (computerRead{}).Execute(ctx, json.RawMessage(`{"what":"apps"}`)); computer.CodeOf(err) != computer.CodeUnavailable {
		t.Fatalf("computer_read unbound = %v, want %s", err, computer.CodeUnavailable)
	}
	if _, err := (computerAct{}).Execute(ctx, json.RawMessage(`{"app":"com.apple.Notes","steps":[{"action":"key","key":"Enter"}]}`)); computer.CodeOf(err) != computer.CodeUnavailable {
		t.Fatalf("computer_act unbound = %v, want %s", err, computer.CodeUnavailable)
	}
	for _, tool := range ComputerTools(nil) {
		if ComputerBound(tool) {
			t.Errorf("%s reports a helper it does not have", tool.Name())
		}
	}
}

func TestApplicationListMarksTheOnesNeverOperated(t *testing.T) {
	out := renderApps([]computer.App{
		{Bundle: "com.apple.Notes", Name: "Notes", Active: true, Windows: []computer.Window{{Title: "", Bounds: computer.Rect{Width: 800, Height: 600}}}},
		{Bundle: "com.apple.Terminal", Name: "Terminal"},
	})
	if !strings.Contains(out, "* com.apple.Notes — Notes\n") || !strings.Contains(out, `window "(untitled)" 800×600`) {
		t.Fatalf("listing = %q", out)
	}
	if !strings.Contains(out, "com.apple.Terminal — Terminal (never operated: a terminal)") {
		t.Fatalf("a refused application is not marked: %q", out)
	}
}

// The subject says which of the two a call is: operating the application
// through its own actions, or taking the pointer the person is holding.
func TestThePointerStepsNameThemselvesInTheApproval(t *testing.T) {
	ctx := context.Background()
	through := computerAct{}.PermissionArgs(ctx, json.RawMessage(`{"app":"com.apple.Notes","steps":[{"action":"click","ref":"a1"}]}`))
	if got := permission.Subject(through); got != "com.apple.Notes" {
		t.Errorf("accessibility steps named %q", got)
	}
	taking := computerAct{}.PermissionArgs(ctx, json.RawMessage(`{"app":"com.apple.Notes","steps":[{"action":"click","ref":"a1"},{"action":"pointer_drag","x":1,"y":2,"to_x":3,"to_y":4}]}`))
	if got := permission.Subject(taking); got != permission.ComputerPointerPrefix+"com.apple.Notes" {
		t.Errorf("a run that takes the pointer named %q", got)
	}
}

// A modal is said before the tree, from the snapshot's structure, so the model
// knows why the rest of the window takes no input.
func TestASnapshotLeadsWithTheModalThatHoldsTheInput(t *testing.T) {
	out := renderComputerSnapshot(computer.Snapshot{
		App:    computer.App{Bundle: "notepad.exe", Name: "Notepad"},
		Lines:  []string{`- window "Untitled" [a1]`, `  - window "Error" [a34] modal`},
		Modals: []computer.Modal{{Ref: "a34", Title: "Error", Blocks: "Untitled"}},
	})
	lines := strings.Split(out, "\n")
	if len(lines) < 3 || !strings.HasPrefix(lines[1], `Blocked: the modal "Error" [a34] over "Untitled" holds this application's input`) {
		t.Fatalf("snapshot = %q", out)
	}
}

func TestEachStepSaysWhatIsKnownOfItsEffect(t *testing.T) {
	modal := &computer.Modal{Ref: "a34", Title: "Error"}
	for _, c := range []struct {
		effect computer.Effect
		want   string
	}{
		{computer.Effect{Class: computer.EffectConfirmed, Evidence: []computer.Evidence{computer.EvidenceValueReadback}}, " — took effect (value read back)"},
		{computer.Effect{Class: computer.EffectSuspectedNoop, Evidence: []computer.Evidence{computer.EvidenceValueUnchanged}}, " — no effect seen (value unchanged)"},
		{computer.Effect{Class: computer.EffectUnverifiable}, " — effect not verified"},
		{computer.Effect{Class: computer.EffectUnverifiable, BlockedBy: modal}, ` — effect not verified, into the modal "Error" [a34]`},
		{computer.Effect{}, ""},
	} {
		if got := renderEffect(c.effect); got != c.want {
			t.Errorf("%+v renders %q, want %q", c.effect, got, c.want)
		}
	}
}
