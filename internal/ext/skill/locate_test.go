package skill

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
)

// locate is read-only and reads code only; what makes it fast is the step cap
// its tool asks for and the prompt that spends each step on parallel searches.
func TestLocateIsAReadOnlyCodeReader(t *testing.T) {
	sk, ok := New(Options{HomeDir: testenv.TempDir(t)}).Read("locate")
	if !ok {
		t.Fatal("locate is not a built-in")
	}
	if sk.RunAs != RunSubagent || !sk.ReadOnly {
		t.Fatalf("locate = runAs %q readOnly %v, want a read-only subagent", sk.RunAs, sk.ReadOnly)
	}
	for _, name := range sk.AllowedTools {
		if !slices.Contains([]string{"read_file", "ls", "glob", "grep", "code_index"}, name) {
			t.Fatalf("locate may call %q, which is not a code reader", name)
		}
	}
}

// The locate tool caps its child at four rounds; explore keeps the default
// budget, so the cap is per entry point and never a property of the profile.
func TestLocateToolCapsItsRunAtFourSteps(t *testing.T) {
	got := map[string]int{}
	runner := func(_ context.Context, sk Skill, _ string, opts SubagentRunOptions) (string, error) {
		got[sk.Name] = opts.MaxSteps
		return "internal/x.go:10-20 — here", nil
	}
	for _, tl := range BuiltinSubagentTools(New(Options{HomeDir: testenv.TempDir(t)}), runner) {
		if tl.Name() != "locate" && tl.Name() != "explore" {
			continue
		}
		if _, err := tl.Execute(context.Background(), json.RawMessage(`{"task":"where is the retry policy"}`)); err != nil {
			t.Fatalf("%s: %v", tl.Name(), err)
		}
	}
	if got["locate"] != 4 || got["explore"] != 0 {
		t.Fatalf("step caps = %v, want locate 4 and explore the default (0)", got)
	}
}
