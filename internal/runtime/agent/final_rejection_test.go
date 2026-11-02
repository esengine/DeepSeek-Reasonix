package agent

import (
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/plancontract"
	"reasonix/internal/safety/evidence"
)

func rejectionAgent(t *testing.T, plan *plancontract.Plan) *Agent {
	t.Helper()
	a := New(nil, tool.NewRegistry(), sessionstore.NewSession(""), Options{}, nil)
	a.resetTurnEvidence()
	a.turn.turnInput = "fix the retry race"
	a.SetPlanContract(plan)
	return a
}

func checkedPlan() plancontract.Plan {
	plan := criterionPlan()
	plan.Steps[0].Verification = []plancontract.Verification{{Command: "go test ./pay/"}}
	return plan.Normalize()
}

// The gate has to name what is missing: a criterion whose step names a check
// that has not passed since the change, with the check that settles it.
func TestUnprovenPlanCriterionBlocksTheFinalAnswer(t *testing.T) {
	plan := checkedPlan()
	a := rejectionAgent(t, &plan)
	a.task.ledger.Record(evidence.Receipt{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"pay.go"}})

	joined := strings.Join(a.outstandingPlanCriteria(), "; ")
	if !strings.Contains(joined, "retries no longer double-charge") || !strings.Contains(joined, "run go test ./pay/") {
		t.Fatalf("outstanding = %q, want the criterion and the check that settles it", joined)
	}
}

// A citation is the model's word: it does not settle a criterion whose step
// names a check, and a criterion no command checks is not held at all.
func TestACitationSettlesNoPlanCriterion(t *testing.T) {
	plan := checkedPlan()
	a := rejectionAgent(t, &plan)
	a.task.ledger.Record(evidence.Receipt{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"pay.go"}})
	for _, c := range plan.Steps[0].Acceptance {
		a.task.ledger.Record(completeStepReceipt(t, c.ID, "manual", ""))
	}
	if len(a.outstandingPlanCriteria()) == 0 {
		t.Fatal("manual citations settled criteria whose step names a check")
	}
	unchecked := criterionPlan()
	b := rejectionAgent(t, &unchecked)
	b.task.ledger.Record(evidence.Receipt{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"pay.go"}})
	if outstanding := b.outstandingPlanCriteria(); len(outstanding) != 0 {
		t.Fatalf("outstanding = %v, want criteria no command checks left to the receipt, not the gate", outstanding)
	}
}

// Once the step's check passes after the change the gate lets go, and a later
// change reopens it: a pass from before the last change proves nothing now.
func TestPlanCheckMustPassAfterTheLatestChange(t *testing.T) {
	plan := checkedPlan()
	a := rejectionAgent(t, &plan)
	edit := evidence.Receipt{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"pay.go"}}
	a.task.ledger.Record(edit)
	zero := 0
	a.task.ledger.Record(evidence.Receipt{ToolName: "bash", Command: "go test ./pay/", Success: true, ExitCode: &zero})
	if outstanding := a.outstandingPlanCriteria(); len(outstanding) != 0 {
		t.Fatalf("outstanding = %v, want none once the step's check passed after the change", outstanding)
	}
	a.task.ledger.Record(edit)
	if len(a.outstandingPlanCriteria()) == 0 {
		t.Fatal("a change after the check must reopen the criteria")
	}
}

// An unplanned turn must not inherit a contract it never agreed to.
func TestUnplannedTurnHasNoOutstandingCriteria(t *testing.T) {
	a := rejectionAgent(t, nil)
	a.task.ledger.Record(evidence.Receipt{ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"pay.go"}})
	if outstanding := a.outstandingPlanCriteria(); len(outstanding) != 0 {
		t.Fatalf("outstanding = %v, want none without an approved plan", outstanding)
	}
}

// A plan's check that the host cannot classify is not a change of its own: were
// it one, a `python -c` check would owe itself a later run and never settle.
func TestAnUnclassifiedPlanCheckSettlesItsCriterion(t *testing.T) {
	plan := checkedPlan()
	plan.Steps[0].Verification = []plancontract.Verification{{Command: `python -c "import pay"`}}
	a := rejectionAgent(t, &plan)
	a.task.ledger.Record(evidence.Receipt{ToolName: "edit_file", Mutation: true, MutationEvidence: evidence.MutationProven, Write: true, Success: true, Paths: []string{"pay.go"}})
	a.task.ledger.Record(evidence.Receipt{ToolName: "bash", Command: `python -c "import pay"`, Mutation: true, Success: true})
	if outstanding := a.outstandingPlanCriteria(); len(outstanding) != 0 {
		t.Fatalf("outstanding = %v, want the check that ran after the edit to settle its criteria", outstanding)
	}
}

// A check's write is residue only while every file it wrote is one this turn
// created and it stays off what the plan names and what the turn's other writes
// touched; a list no complete walk established proves none of that.
func TestAPlanChecksWriteToGuardedFilesVoidsIt(t *testing.T) {
	const check = "sh check.sh"
	plan := checkedPlan()
	plan.Steps[0].CandidateFiles = []string{"src.txt"}
	plan.Steps[0].Verification = []plancontract.Verification{{Command: check}}
	run := func(paths ...string) evidence.Receipt {
		return evidence.Receipt{ToolName: "bash", Command: check, Mutation: true, MutationEvidence: evidence.MutationProven, Success: true, PathsComplete: true, Paths: paths, Created: paths}
	}
	rewrote := func(paths ...string) evidence.Receipt {
		r := run(paths...)
		r.Created = nil
		return r
	}
	watched := run(".cache/stamp")
	watched.PathsComplete = false
	for _, tc := range []struct {
		name  string
		other []evidence.Receipt
		ran   evidence.Receipt
		owed  bool
	}{
		{"residue outside everything", nil, run(".cache/stamp"), false},
		{"a file that was there before the turn", nil, rewrote("lib.txt"), true},
		{"a file an earlier run deleted", []evidence.Receipt{rewrote("lib.txt")}, run("lib.txt"), true},
		{"a new file beside one that was there", nil, evidence.Receipt{ToolName: "bash", Command: check, Mutation: true, MutationEvidence: evidence.MutationProven, Success: true, PathsComplete: true, Paths: []string{".cache/stamp", "lib.txt"}, Created: []string{".cache/stamp"}}, true},
		{"a file the plan names", nil, run("src.txt"), true},
		{"a file another write touched", []evidence.Receipt{{ToolName: "write_file", Mutation: true, MutationEvidence: evidence.MutationProven, Write: true, Success: true, Paths: []string{"notes.txt"}}}, run("notes.txt"), true},
		{"a list no complete walk established", nil, watched, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := rejectionAgent(t, &plan)
			for _, r := range tc.other {
				a.task.ledger.Record(r)
			}
			a.task.ledger.Record(tc.ran)
			if got := len(a.outstandingPlanCriteria()) > 0; got != tc.owed {
				t.Fatalf("owed = %v, want %v", got, tc.owed)
			}
		})
	}
}
