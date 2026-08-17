package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"reasonix/internal/evidence"
)

// taskCarryOver names the fields restartLedger hands to the next task in the
// same session. Everything else must come back zeroed, which is what makes
// "one ledger, one task, one bill" a property of the type rather than of the
// call sites that used to clear these fields one by one.
var taskCarryOver = map[string]bool{
	"scopeID":        true,
	"checkpoint":     true,
	"repeatFailures": true,
	"repeatScope":    true,
	"ledger":         true, // identity, not contents: executeOne hands the pointer to every tool context
}

func TestTaskRuntimeRestartCarriesScopeAndResetsAccounting(t *testing.T) {
	ledger := evidence.NewLedger()
	before := &taskRuntime{
		scopeID:        "scope-1",
		checkpoint:     evidence.DeliveryCheckpoint{ScopeID: "scope-1"},
		ledger:         ledger,
		outcome:        evidence.NewOutcomeTracker(),
		budget:         runBudget{rounds: 4, requests: 9, cost: 1.5, limit: TaskBudget{}},
		ebm:            ebmState{fired: true, captured: true},
		governor:       governorState{engaged: true, noticed: true},
		repeatFailures: map[string]repeatFailureRecord{"sig": {count: 2}},
		repeatScope:    "scope-1",
	}
	after := *before
	after.restartLedger()

	if after.scopeID != "scope-1" || after.checkpoint.ScopeID != "scope-1" {
		t.Errorf("scope = %q/%q, want it carried: beginRunTurn owns the scope transition", after.scopeID, after.checkpoint.ScopeID)
	}
	if after.repeatScope != "scope-1" || len(after.repeatFailures) != 1 {
		t.Errorf("repeat failures = %q/%d, want them carried: prepareScope decides what survives", after.repeatScope, len(after.repeatFailures))
	}
	if after.ledger != ledger {
		t.Error("ledger pointer replaced; tool contexts hold it for the length of a call")
	}
	if after.budget.rounds != 0 || after.budget.requests != 0 || after.budget.cost != 0 {
		t.Errorf("budget = %+v, want a fresh bill for the new task", after.budget)
	}
	if after.ebm != (ebmState{}) || after.governor != (governorState{}) {
		t.Errorf("ebm/governor = %+v/%+v, want both reset with the ledger", after.ebm, after.governor)
	}
	if after.outcome == nil || after.outcome == before.outcome {
		t.Error("outcome tracker not replaced; the shadow scorer must not span tasks")
	}
}

// taskRestarted names the fields the test above asserts a new task starts
// from. Together with taskCarryOver it must cover taskRuntime exactly, so a
// field added to the type fails here until someone states which side it is on.
var taskRestarted = map[string]bool{
	"outcome":  true,
	"budget":   true,
	"ebm":      true,
	"governor": true,
}

// restartLedger is one assignment, so an unlisted field resets by default —
// the safe direction. The risk is the other one: a field that quietly ends up
// carried, or reset with nothing asserting it. Both lists are therefore
// checked against the struct rather than trusted.
func TestTaskRuntimeLifetimeListsCoverTheStruct(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "taskstate.go", nil, 0)
	if err != nil {
		t.Fatalf("parse taskstate.go: %v", err)
	}
	fields := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Name.Name != "taskRuntime" {
			return true
		}
		st, ok := spec.Type.(*ast.StructType)
		if !ok {
			return false
		}
		for _, field := range st.Fields.List {
			for _, name := range field.Names {
				fields[name.Name] = true
			}
		}
		return false
	})
	if len(fields) == 0 {
		t.Fatal("taskRuntime has no fields; the guard would pass vacuously")
	}
	for _, list := range []map[string]bool{taskCarryOver, taskRestarted} {
		for name := range list {
			if !fields[name] {
				t.Errorf("the lifetime lists name %q, which taskRuntime no longer has", name)
			}
		}
	}
	for name := range fields {
		switch {
		case taskCarryOver[name] && taskRestarted[name]:
			t.Errorf("taskRuntime.%s is listed as both carried and restarted", name)
		case !taskCarryOver[name] && !taskRestarted[name]:
			t.Errorf("taskRuntime.%s is on neither list; decide whether a new task keeps it and assert that above", name)
		}
	}
}

func TestTaskRuntimePrepareScopeKeepsOnlyRecheckableFailures(t *testing.T) {
	records := func() map[string]repeatFailureRecord {
		return map[string]repeatFailureRecord{
			"stale":  {count: 2, stateRecheck: true},
			"solved": {count: 1},
		}
	}
	cases := []struct {
		name    string
		scoped  bool
		scopeID string
		want    []string
	}{
		{"same scope keeps the anchors still worth re-checking", true, "scope-1", []string{"stale"}},
		{"a new scope is a new task", true, "scope-2", nil},
		{"an unscoped run keeps nothing", false, "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := taskRuntime{repeatFailures: records(), repeatScope: "scope-1"}
			task.prepareScope(tc.scoped, tc.scopeID)
			if len(task.repeatFailures) != len(tc.want) {
				t.Fatalf("repeatFailures = %v, want %v", task.repeatFailures, tc.want)
			}
			for _, sig := range tc.want {
				if _, ok := task.repeatFailures[sig]; !ok {
					t.Errorf("repeatFailures lost %q", sig)
				}
			}
			if tc.scoped && task.repeatScope != tc.scopeID {
				t.Errorf("repeatScope = %q, want %q", task.repeatScope, tc.scopeID)
			}
			if !tc.scoped && task.repeatScope != "" {
				t.Errorf("repeatScope = %q, want it cleared", task.repeatScope)
			}
		})
	}
}
