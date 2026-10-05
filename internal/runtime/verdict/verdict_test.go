package verdict

import (
	"slices"
	"testing"

	"reasonix/internal/runtime/completion"
	"reasonix/internal/runtime/taskcontract"
	"reasonix/internal/safety/evidence"
)

func contractWith(reqs []taskcontract.Requirement, checks ...taskcontract.Check) *taskcontract.Contract {
	return &taskcontract.Contract{Requirements: reqs, Checks: checks}
}

// A criterion a todo or a citation marked satisfied stays unverifiable: the
// verdict never reads the status a model can set.
func TestClaimedCriterionIsNotSatisfied(t *testing.T) {
	c := contractWith([]taskcontract.Requirement{{ID: "t1", Required: true, Status: taskcontract.Satisfied}})
	res := Evaluate(Input{Contract: c})
	if res.Outcome != Incomplete || res.Obligations[0].Verdict != Unverifiable || res.Obligations[0].Cause != CauseNoVerifier {
		t.Fatalf("result = %+v, want the claimed criterion unverifiable", res)
	}
	d := Classify(completion.VerdictDone, c, res)
	if d.Class != ClassNewStricter || !slices.Contains(d.Reasons, ReasonClaimOnly) {
		t.Fatalf("divergence = %+v, want new_stricter for claim_only", d)
	}
}

func TestAtomicCriterionIsSatisfiedByAHostObservedMutation(t *testing.T) {
	c := taskcontract.Atomic("fix the typo")
	c.Checks = nil
	if res := Evaluate(Input{Contract: c}); res.Outcome != Incomplete || res.Obligations[0].Cause != CauseNotAttempted {
		t.Fatalf("with no mutation: %+v", res)
	}
	receipts := []evidence.Receipt{{ToolName: "edit_file", Success: false, Mutation: true}, {ToolName: "edit_file", Success: true, Mutation: true}}
	if res := Evaluate(Input{Contract: c, Receipts: receipts}); res.Outcome != Completed {
		t.Fatalf("with a successful mutation: %+v", res)
	}
}

func TestCheckStatesMapToVerdicts(t *testing.T) {
	cases := []struct {
		status  taskcontract.Status
		verdict Verdict
		outcome Outcome
	}{
		{taskcontract.Satisfied, Satisfied, Completed},
		{taskcontract.Failed, Unsatisfied, Failed},
		{taskcontract.Stale, Stale, Incomplete},
		{taskcontract.Suppressed, Unverifiable, Incomplete},
		{taskcontract.Pending, Owed, Incomplete},
	}
	for _, tc := range cases {
		c := contractWith(nil, taskcontract.Check{Kind: taskcontract.CheckCommand, Command: "go test ./...", Status: tc.status})
		res := Evaluate(Input{Contract: c})
		if res.Obligations[0].Verdict != tc.verdict || res.Outcome != tc.outcome {
			t.Errorf("status %v: result = %+v, want %s/%s", tc.status, res, tc.verdict, tc.outcome)
		}
	}
}

func TestOutstandingHostObligationKeepsTheTurnIncomplete(t *testing.T) {
	res := Evaluate(Input{HostObligations: []evidence.Obligation{{ID: "unproven_mutation@3", Kind: evidence.ObligationUnprovenMutation}}})
	if res.Outcome != Incomplete || res.Obligations[0].Source != "host:unproven_mutation" {
		t.Fatalf("result = %+v", res)
	}
	if d := Classify(completion.VerdictPartial, nil, res); d.Class != ClassAgree {
		t.Fatalf("a report partial on the same gap agrees: %+v", d)
	}
}

func TestOptionalObligationsDoNotDecide(t *testing.T) {
	c := contractWith([]taskcontract.Requirement{{ID: "nice", Required: false}})
	if res := Evaluate(Input{Contract: c}); res.Outcome != Completed {
		t.Fatalf("an optional criterion decided the outcome: %+v", res)
	}
}

func TestBlockedWinsAndFailureBeatsIncomplete(t *testing.T) {
	failing := contractWith([]taskcontract.Requirement{{ID: "r", Required: true}},
		taskcontract.Check{Kind: taskcontract.CheckCommand, Command: "go test", Status: taskcontract.Failed})
	if res := Evaluate(Input{Contract: failing}); res.Outcome != Failed {
		t.Fatalf("a failed check with an unverifiable criterion: %+v", res)
	}
	if res := Evaluate(Input{Contract: failing, Blocked: true}); res.Outcome != Blocked {
		t.Fatalf("a host-checked block: %+v", res)
	}
}

// A turn that owes nothing and changed nothing has no evidence of completing
// anything: it gets no outcome, which agrees with a report that gave none. The
// same turn with a change the host saw completed what it was asked to.
func TestNothingDeclaredAgreesWithAnUnknownReport(t *testing.T) {
	res := Evaluate(Input{})
	if res.Outcome != NoOutcome || len(res.Obligations) != 0 {
		t.Fatalf("result = %+v, want no outcome", res)
	}
	if d := Classify(completion.VerdictUnknown, nil, res); d.Class != ClassAgree {
		t.Fatalf("divergence = %+v", d)
	}
	if d := Classify(completion.VerdictDone, nil, res); d.Class != ClassNewStricter {
		t.Fatalf("a report calling nothing done must diverge from no outcome: %+v", d)
	}
	changed := Evaluate(Input{Receipts: []evidence.Receipt{{ToolName: "edit_file", Success: true, Write: true}}})
	if changed.Outcome != Completed {
		t.Fatalf("a proven change owing nothing = %+v, want completed", changed)
	}
	failed := Evaluate(Input{Receipts: []evidence.Receipt{{ToolName: "edit_file", Success: false, Write: true}}})
	if failed.Outcome != NoOutcome {
		t.Fatalf("a failed write is no change: %+v", failed)
	}
}

func TestLooserReasonsNameWhatTheReportHeldBack(t *testing.T) {
	c := contractWith([]taskcontract.Requirement{{ID: "r", Required: true, Status: taskcontract.Pending, Auto: true, AutoKind: taskcontract.EvidenceMutation}})
	res := Evaluate(Input{Contract: c, Receipts: []evidence.Receipt{{Success: true, Write: true}}})
	d := Classify(completion.VerdictPartial, c, res)
	if d.Class != ClassNewLooser || !slices.Equal(d.Reasons, []string{ReasonOldGaps, ReasonOldCriteria}) {
		t.Fatalf("divergence = %+v", d)
	}
}

func TestEvaluateIsDeterministic(t *testing.T) {
	in := Input{
		Contract:        contractWith([]taskcontract.Requirement{{ID: "a", Required: true}}, taskcontract.Check{Command: "x", Status: taskcontract.Stale}),
		HostObligations: []evidence.Obligation{{ID: "o@1", Kind: evidence.ObligationStaleVerification}},
	}
	first := Evaluate(in)
	for range 5 {
		if again := Evaluate(in); !slices.Equal(again.Obligations, first.Obligations) || again.Outcome != first.Outcome {
			t.Fatalf("Evaluate varied: %+v vs %+v", again, first)
		}
	}
}

func TestSilentReportIsItsOwnClass(t *testing.T) {
	blocked := Evaluate(Input{Blocked: true})
	if d := Classify(completion.VerdictUnknown, nil, blocked); d.Class != ClassOldSilent || !slices.Equal(d.Reasons, []string{ReasonBlocked}) {
		t.Fatalf("divergence = %+v, want old_silent for blocked", d)
	}
	owed := Evaluate(Input{HostObligations: []evidence.Obligation{{ID: "o@1", Kind: evidence.ObligationUnprovenMutation}}})
	if d := Classify(completion.VerdictUnknown, nil, owed); d.Class != ClassOldSilent || !slices.Equal(d.Reasons, []string{"host:unproven_mutation"}) {
		t.Fatalf("divergence = %+v, want old_silent for an outstanding obligation", d)
	}
}

func TestBlockedAgainstADoneReportNamesTheBlock(t *testing.T) {
	d := Classify(completion.VerdictDone, nil, Evaluate(Input{Blocked: true}))
	if d.Class != ClassNewStricter || !slices.Equal(d.Reasons, []string{ReasonBlocked}) {
		t.Fatalf("divergence = %+v", d)
	}
}

// A host obligation is named by its kind, which is content-free and is what
// decides which old path owes the difference.
func TestStricterReasonNamesTheHostObligationKind(t *testing.T) {
	res := Evaluate(Input{HostObligations: []evidence.Obligation{
		{ID: "stale@1", Kind: evidence.ObligationStaleVerification},
		{ID: "unproven@2", Kind: evidence.ObligationUnprovenMutation},
	}})
	d := Classify(completion.VerdictDone, nil, res)
	if !slices.Equal(d.Reasons, []string{"host:stale_verification", "host:unproven_mutation"}) {
		t.Fatalf("reasons = %v", d.Reasons)
	}
}

func TestFrozenCriteriaAndTheHostObligationsTheyCover(t *testing.T) {
	res := Evaluate(Input{
		Frozen: []Frozen{
			{ID: "command@go test", Source: "project_check", Identity: "go test", Satisfied: false},
			{ID: "test@T", Source: "baseline_test", Identity: "T", Unverifiable: true},
			{ID: "command@go vet", Source: "project_check", Identity: "go vet", Satisfied: true},
		},
		HostObligations: []evidence.Obligation{
			{ID: "baseline_required_check@go test", Kind: evidence.ObligationBaselineCheck},
			{ID: "baseline_test@T", Kind: evidence.ObligationBaselineTest},
			{ID: "missing_project_check@go build", Kind: evidence.ObligationMissingProjectCheck},
		},
	})
	var got []string
	for _, o := range res.Obligations {
		got = append(got, o.ID+"="+string(o.Verdict))
	}
	want := []string{
		"contract@command@go test=owed",
		"contract@test@T=unverifiable",
		"contract@command@go vet=satisfied",
		"missing_project_check@go build=owed",
	}
	if !slices.Equal(got, want) || res.Outcome != Incomplete {
		t.Fatalf("obligations = %v (%s), want %v", got, res.Outcome, want)
	}
	d := Classify(completion.VerdictDone, nil, res)
	if !slices.Equal(d.Reasons, []string{"contract:project_check", "host:missing_project_check", CauseSubjectMissing}) {
		t.Fatalf("reasons = %v", d.Reasons)
	}
}
