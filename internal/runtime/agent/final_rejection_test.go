package agent

import (
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/contract"
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

// The sealed verdict answers a frozen plan check against the same baseline.
func TestAnUnclassifiedPlanCheckSettlesItsFrozenCriterion(t *testing.T) {
	plan := checkedPlan()
	a := rejectionAgent(t, &plan)
	command := `python -c "import pay"`
	a.task.ledger.Record(evidence.Receipt{ToolName: "edit_file", Mutation: true, MutationEvidence: evidence.MutationProven, Write: true, Success: true, Paths: []string{"pay.go"}})
	a.task.ledger.Record(evidence.Receipt{ToolName: "bash", Command: command, Mutation: true, Success: true})
	got := a.frozenResults([]contract.Criterion{{ID: "command@x", Source: contract.SourcePlan, Required: true,
		Verifier: contract.Verifier{Kind: contract.VerifierCommand, Identity: evidence.VerificationIdentity(command)}}})
	if len(got) != 1 || !got[0].Satisfied {
		t.Fatalf("frozen = %+v, want the check that ran after the edit satisfied", got)
	}
}

// A plan naming files to change holds an answer that changed nothing, and the
// hold lifts on each legitimate end: the change itself, a declared blocker, a
// wait on the user, or a change a restart carried in.
func TestAPlanNamingFilesHoldsAnAnswerThatChangedNothing(t *testing.T) {
	held := func(a *Agent) bool {
		return strings.Contains(a.finalReadinessCheckFor().reason, "names files to change")
	}
	plan := deliverablePlan()
	if !held(rejectionAgent(t, &plan)) {
		t.Fatal("an answer that changed nothing finished under a plan naming files to change")
	}
	for name, r := range map[string]evidence.Receipt{
		"changed":  {ToolName: "edit_file", Mutation: true, Write: true, Success: true, Paths: []string{"parser.go"}},
		"blocked":  {ToolName: "conclude_blocked", Success: true},
		"awaiting": {ToolName: evidence.UserGateTool, Success: true, Args: []byte(`{"need":"the sample file"}`)},
	} {
		a := rejectionAgent(t, &plan)
		a.task.ledger.Record(r)
		if held(a) {
			t.Fatalf("%s: the deliverable still held the answer", name)
		}
	}
	analysis := deliverablePlan()
	analysis.Steps[0].CandidateFiles = nil
	if held(rejectionAgent(t, &analysis)) {
		t.Fatal("a plan naming no files was held for a change")
	}
	a := rejectionAgent(t, &plan)
	if got := a.appendPlanDeliverableGap(&finalReadinessCheck{}, nil, true); len(got) != 0 {
		t.Fatalf("a change a restart carried in still owed the deliverable: %v", got)
	}
}

// What a run of exactly the plan's check writes is the check's own residue — an
// import writes bytecode, and the scan proves it — so it does not void the check.
// A run that does more than the check still counts as a change.
func TestAPlanCheckDoesNotVoidItselfByWhatItWrites(t *testing.T) {
	const check = `python -c "import pay"`
	plan := checkedPlan()
	plan.Steps[0].Verification = []plancontract.Verification{{Command: check}}
	edit := evidence.Receipt{ToolName: "edit_file", Mutation: true, MutationEvidence: evidence.MutationProven, Write: true, Success: true, Paths: []string{"pay.py"}}
	frozen := []contract.Criterion{{ID: "command@x", Source: contract.SourcePlan, Required: true,
		Verifier: contract.Verifier{Kind: contract.VerifierCommand, Identity: evidence.VerificationIdentity(check)}}}

	a := rejectionAgent(t, &plan)
	a.task.ledger.Record(edit)
	a.task.ledger.Record(evidence.Receipt{ToolName: "bash", Command: check, Mutation: true, MutationEvidence: evidence.MutationProven, Success: true, PathsComplete: true, Paths: []string{"__pycache__/pay.cpython-312.pyc"}, Created: []string{"__pycache__/pay.cpython-312.pyc"}})
	if outstanding := a.outstandingPlanCriteria(); len(outstanding) != 0 {
		t.Fatalf("outstanding = %v, want the check's own bytecode not to void it", outstanding)
	}
	if got := a.frozenResults(frozen); !got[0].Satisfied {
		t.Fatalf("frozen = %+v, want the check satisfied", got)
	}

	b := rejectionAgent(t, &plan)
	b.task.ledger.Record(edit)
	b.task.ledger.Record(evidence.Receipt{ToolName: "bash", Command: check + ` && sed -i s/a/b/ pay.py`, Mutation: true, MutationEvidence: evidence.MutationProven, Success: true, Paths: []string{"pay.py"}})
	if len(b.outstandingPlanCriteria()) == 0 {
		t.Fatal("a run that rewrote source beside the check settled the check it ran before the rewrite")
	}
}

// A check's own residue is not the change a plan naming files is owed.
func TestAPlanChecksResidueIsNotTheDeliverable(t *testing.T) {
	const check = `python -c "import parser"`
	plan := deliverablePlan()
	plan.Steps[0].Verification = []plancontract.Verification{{Command: check}}
	a := rejectionAgent(t, &plan)
	a.task.ledger.Record(evidence.Receipt{ToolName: "bash", Command: check, Mutation: true, MutationEvidence: evidence.MutationProven, Success: true, PathsComplete: true, Paths: []string{"__pycache__/parser.cpython-312.pyc"}, Created: []string{"__pycache__/parser.cpython-312.pyc"}})
	if !strings.Contains(a.finalReadinessCheckFor().reason, "names files to change") {
		t.Fatal("the bytecode a check wrote stood in for the planned change")
	}
}

// Only a check's own runs are left out of its baseline: another check that
// rewrites a file voids a pass taken before it, and a check that never ran is
// not settled because the only write came from a different check.
func TestAnotherPlanChecksWriteVoidsAPass(t *testing.T) {
	const verify, build = "sh verify.sh", "sh build.sh"
	plan := checkedPlan()
	plan.Steps[0].Verification = []plancontract.Verification{{Command: verify}, {Command: build}}
	cmd := func(id, c string) contract.Criterion {
		return contract.Criterion{ID: id, Source: contract.SourcePlan, Required: true,
			Verifier: contract.Verifier{Kind: contract.VerifierCommand, Identity: evidence.VerificationIdentity(c)}}
	}
	frozen := []contract.Criterion{cmd("command@verify", verify), cmd("command@build", build)}

	a := rejectionAgent(t, &plan)
	a.task.ledger.Record(evidence.Receipt{ToolName: "bash", Command: verify, Success: true})
	a.task.ledger.Record(evidence.Receipt{ToolName: "bash", Command: build, Mutation: true, MutationEvidence: evidence.MutationProven, Success: true, PathsComplete: true, Paths: []string{"out.txt"}, Created: []string{"out.txt"}})
	if joined := strings.Join(a.outstandingPlanCriteria(), "; "); !strings.Contains(joined, "run "+verify) {
		t.Fatalf("outstanding = %q, want verify.sh owed after build.sh rewrote out.txt", joined)
	}
	if got := a.frozenResults(frozen); got[0].Satisfied || !got[1].Satisfied {
		t.Fatalf("frozen = %+v, want verify.sh stale and build.sh settled", got)
	}

	b := rejectionAgent(t, &plan)
	b.task.ledger.Record(evidence.Receipt{ToolName: "bash", Command: build, Mutation: true, MutationEvidence: evidence.MutationProven, Success: true, PathsComplete: true, Paths: []string{"out.txt"}, Created: []string{"out.txt"}})
	if got := b.frozenResults(frozen); got[0].Satisfied {
		t.Fatalf("frozen = %+v, want verify.sh unsettled: it never ran and build.sh changed out.txt", got)
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
