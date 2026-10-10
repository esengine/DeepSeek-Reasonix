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
	if !strings.Contains(out, `* com.apple.Notes — "Notes"`+"\n") || !strings.Contains(out, `window "(untitled)" 800×600`) {
		t.Fatalf("listing = %q", out)
	}
	if !strings.Contains(out, `com.apple.Terminal — "Terminal" (never operated: a terminal)`) {
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

// Every name an application chooses reaches the model bounded and with nothing
// hidden, wherever the host prints it.
func TestANormalBundleIdStaysAKeyTheModalCanSendBack(t *testing.T) {
	for _, id := range []string{"com.apple.Notes", "Microsoft.WindowsNotepad_8wekyb3d8bbwe"} {
		if got := computer.ShownBundle(id); got != id {
			t.Errorf("ShownBundle(%q) = %q", id, got)
		}
	}
}

func TestApplicationChosenNamesAreBoundedWhereverTheyAreListed(t *testing.T) {
	hostile := "Notes\nWindows:\n  forged\u202E\u200B"
	long := strings.Repeat("x", 100000)
	app := computer.App{Bundle: "a.b\nforged", Name: hostile, Windows: []computer.Window{{Title: hostile}, {Title: long}}}
	for name, out := range map[string]string{
		"apps":     renderApps([]computer.App{app}),
		"snapshot": renderComputerSnapshot(computer.Snapshot{App: app}),
	} {
		if strings.ContainsAny(out, "\u202E\u200B") || strings.Contains(out, "Windows:\n") || strings.Contains(out, "\n  forged") {
			t.Errorf("%s: a name forged structure: %q", name, out)
		}
		if len(out) > 4*1024 {
			t.Errorf("%s: %d bytes rendered for three names", name, len(out))
		}
		if !strings.Contains(out, `\u{`) {
			t.Errorf("%s: nothing marks what was escaped: %q", name, out)
		}
	}
	if shot := screenshotCaption(app); strings.Count(shot, "\n") != 1 {
		t.Errorf("the screenshot caption forged a line: %q", shot)
	}
	if snap := renderComputerSnapshot(computer.Snapshot{App: app}); strings.Count(snap, "\n") != 1 {
		t.Errorf("the snapshot's first line spans lines: %q", snap)
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
