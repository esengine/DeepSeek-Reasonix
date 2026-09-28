package boot

import (
	"context"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent/testutil"
)

// TestGoalOnlyToolsLeaveTheSchemaWhenUnreachable is an effect test at the
// provider boundary: a turn that carries no goal recorder does not carry
// update_goal either, whether the assembly could ever arm one or not. Shipping
// it regardless bought a definition the model calls and the host rejects —
// measured across real sessions, 121 calls and 116 refusals.
func TestGoalOnlyToolsLeaveTheSchemaWhenUnreachable(t *testing.T) {
	for _, tc := range []struct {
		name        string
		unreachable bool
		wantGoal    bool
	}{
		{name: "reachable assembly, no goal armed", unreachable: false, wantGoal: false},
		{name: "unreachable assembly", unreachable: true, wantGoal: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateConfigHome(t)
			dir := robustTempDir(t)
			t.Chdir(dir)

			registerBootTokenProfileTestProvider()
			prov := testutil.NewMock("goal-surface", testutil.Turn{Text: "done"})
			setBootTokenProfileTestProvider(t, prov)
			writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)
			approveWorkspace(t, dir)

			ctrl, err := Build(context.Background(), Options{
				Sink:                 event.Discard,
				GoalTurnsUnreachable: tc.unreachable,
			})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}
			defer ctrl.Close()
			if err := ctrl.Run(context.Background(), "go"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			reqs := prov.Requests()
			if len(reqs) != 1 {
				t.Fatalf("requests = %d, want 1", len(reqs))
			}
			if got := requestHasTool(reqs[0], "update_goal"); got != tc.wantGoal {
				t.Fatalf("update_goal present = %v, want %v; tools=%v",
					got, tc.wantGoal, toolSchemaNames(reqs[0].Tools))
			}
			// The capability stays registered either way: what changed is the
			// provider surface for this turn, not what the session can reach.
			registered := false
			for _, e := range ctrl.AllToolContractEntries() {
				if e.Name == "update_goal" {
					registered = true
				}
			}
			if !registered {
				t.Fatalf("update_goal left the registry; only the turn surface should narrow")
			}
			// The execution surface is untouched either way.
			for _, want := range []string{"bash", "read_file", "edit_file", "write_file"} {
				if !requestHasTool(reqs[0], want) {
					t.Fatalf("missing execution tool %q; tools=%v", want, toolSchemaNames(reqs[0].Tools))
				}
			}
		})
	}
}
