package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"reasonix/internal/safety/evidence"
)

// taskCarryOver names the fields restartLedger hands to the next task in the
// same session. Everything else must come back zeroed, which is what makes
// "one ledger, one task, one bill" a property of the type rather than of the
// call sites that used to clear these fields one by one.
var taskCarryOver = map[string]bool{
	"scopeID":    true,
	"checkpoint": true,
	"ledger":     true, // identity, not contents: executeOne hands the pointer to every tool context
}

func TestTaskRuntimeRestartCarriesScopeAndResetsAccounting(t *testing.T) {
	ledger := evidence.NewLedger()
	before := &taskRuntime{
		scopeID:        "scope-1",
		checkpoint:     evidence.DeliveryCheckpoint{ScopeID: "scope-1"},
		ledger:         ledger,
		outcome:        evidence.NewOutcomeTracker(),
		budget:         runBudget{rounds: 4, requests: 9, cost: 1.5, limit: TaskBudget{}},
		witness:        map[string][]string{"tally.go": {"total += x"}},
		workspaceProse: &workspaceProseCache{state: workspaceProseState{epoch: 1, proseOnly: true}},
	}
	after := *before
	after.restartLedger()

	if after.witness != nil {
		t.Errorf("witness = %v, want the previous task's changes to prove nothing here", after.witness)
	}
	if after.workspaceProse == nil || after.workspaceProse == before.workspaceProse || after.workspaceProse.state.epoch != 0 {
		t.Error("workspace prose cache retained evidence from the previous task")
	}

	if after.scopeID != "scope-1" || after.checkpoint.ScopeID != "scope-1" {
		t.Errorf("scope = %q/%q, want it carried: beginRunTurn owns the scope transition", after.scopeID, after.checkpoint.ScopeID)
	}
	if after.ledger != ledger {
		t.Error("ledger pointer replaced; tool contexts hold it for the length of a call")
	}
	if after.budget.rounds != 0 || after.budget.requests != 0 || after.budget.cost != 0 {
		t.Errorf("budget = %+v, want a fresh bill for the new task", after.budget)
	}
	if after.outcome == nil || after.outcome == before.outcome {
		t.Error("outcome tracker not replaced; the shadow scorer must not span tasks")
	}
}

// taskRestarted names the fields the test above asserts a new task starts
// from. Together with taskCarryOver it must cover taskRuntime exactly, so a
// field added to the type fails here until someone states which side it is on.
var taskRestarted = map[string]bool{
	"outcome": true,
	"budget":  true,
	"witness": true,
	// A new task begins under whatever the tree says then, so what an earlier
	// one was held to is not carried into it.
	"baselineCriteria": true,
	// Revisions count one plan's history. A new task plans again from nothing.
	"todoRevs": true,
	// What the walk could not finish before this task is not what it cannot
	// finish during it: an install is what puts a tree over the limit and a
	// clean is what takes it back under, and both happen inside one task.
	"overScanLimit": true,
	// The capture answers for the workspace one task began under. A new task
	// begins under whatever the tree says then, alongside baselineCriteria.
	"criteriaEpoch":  true,
	"workspaceProse": true,
	// A new task is accepted under a new contract; the cache goes with the
	// record name restartLedger clears from the carried checkpoint.
	"contract":       true,
	"contractRecord": true,
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

func TestTaskRuntimeRestartStartsANewContract(t *testing.T) {
	before := &taskRuntime{
		checkpoint:     evidence.DeliveryCheckpoint{ScopeID: "scope-1", Contract: "sha256:rev"},
		ledger:         evidence.NewLedger(),
		contractRecord: "sha256:rev",
	}
	after := *before
	after.restartLedger()
	if after.checkpoint.Contract != "" || after.contractRecord != "" || after.contract != nil {
		t.Fatalf("checkpoint.Contract=%q record=%q, want a new task to start without the last one's contract", after.checkpoint.Contract, after.contractRecord)
	}
	if after.checkpoint.ScopeID != "scope-1" {
		t.Fatal("the scope still carries")
	}
}
