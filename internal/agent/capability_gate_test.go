package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/agentpreset"
	"reasonix/internal/evidence"
	"reasonix/internal/taskpolicy"
	"reasonix/internal/tool"
)

// Every production turn freezes a TaskPolicy before the first request, so the
// gate must hold with one installed — not only against the zero value the
// other cases in this file construct.
func TestDeliveryReviewGateHoldsUnderFrozenTaskPolicy(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/permission/gate.go"}`), true, evidence.ToolFacts{}))

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	reg.Add(fakeTool{name: "security_review", readOnly: true})

	a := &Agent{deliveryProfile: true, task: taskRuntime{ledger: ledger}, svc: agentServices{tools: reg}}
	a.projectSensitivePaths = []string{"internal/permission/**"}
	a.turn.policy = taskpolicy.Derive(taskpolicy.Input{Preset: agentpreset.Delivery})
	a.turn.policySet = true

	if got := a.reviewGateFailure(); !strings.Contains(got, "high-risk") {
		t.Fatalf("gate under a frozen policy = %q, want high-risk review demand", got)
	}

	// Undeclared, the same edit is ordinary production code: the host reads no
	// sensitivity out of a path's spelling, and ordinary code buys no reviewer.
	a.projectSensitivePaths = nil
	if got := a.reviewGateFailure(); got != "" {
		t.Fatalf("undeclared gate = %q, want no structured-review demand", got)
	}
}

func TestDeliveryReviewGateExplainsOpaqueMutationRecovery(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName:         "bash",
		Success:          true,
		Mutation:         true,
		MutationEvidence: evidence.MutationProven,
		Command:          "printf hi > out.log",
	})

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	reg.Add(fakeTool{name: "security_review", readOnly: true})
	a := &Agent{deliveryProfile: true, task: taskRuntime{ledger: ledger}, svc: agentServices{tools: reg}}

	got := a.reviewGateFailure()
	for _, want := range []string{"high-risk", "reported no file paths", "reviewed_paths"} {
		if !strings.Contains(got, want) {
			t.Fatalf("review gate = %q, want %q", got, want)
		}
	}
	if strings.HasSuffix(got, "covering: ") {
		t.Fatalf("review gate must not end with empty coverage: %q", got)
	}

	ledger.Record(grantedReviewReceipt(evidence.ReviewKindReview, `{
		"kind":"review",
		"verdict":"pass",
		"reviewed_paths":["internal/agent/agent.go"],
		"findings":[]
	}`))
	got = a.reviewGateFailure()
	if !strings.Contains(got, "security_review") || !strings.Contains(got, "reported no file paths") {
		t.Fatalf("security review gate = %q, want opaque-mutation recovery guidance", got)
	}

	ledger.Record(grantedReviewReceipt(evidence.ReviewKindSecurity, `{
		"kind":"security",
		"verdict":"pass",
		"reviewed_paths":["internal/agent/agent.go"],
		"findings":[]
	}`))
	if got := a.reviewGateFailure(); got != "" {
		t.Fatalf("review gate = %q after both reports, want ready", got)
	}
}

func TestNonDeliveryProfileNeverRequiresStructuredReview(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/permission/gate.go"}`), true, evidence.ToolFacts{}))

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	reg.Add(fakeTool{name: "security_review", readOnly: true})
	a := &Agent{deliveryProfile: false, task: taskRuntime{ledger: ledger}, svc: agentServices{tools: reg}}

	if got := a.reviewGateFailure(); got != "" {
		t.Fatalf("non-Delivery review gate = %q, want disabled", got)
	}
}

func TestDeliveryReviewGateHighRiskStillRequiresSecurityReview(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/permission/gate.go"}`), true, evidence.ToolFacts{}))

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	reg.Add(fakeTool{name: "security_review", readOnly: true})
	a := &Agent{deliveryProfile: true, task: taskRuntime{ledger: ledger}, svc: agentServices{tools: reg}}
	a.projectSensitivePaths = []string{"internal/permission/**"}

	if got := a.reviewGateFailure(); !strings.Contains(got, "high-risk") {
		t.Fatalf("review gate = %q, want high-risk review demand", got)
	}

	ledger.Record(grantedReviewReceipt(evidence.ReviewKindReview, `{
		"kind":"review",
		"verdict":"pass",
		"reviewed_paths":["internal/permission/gate.go"],
		"findings":[]
	}`))
	if got := a.reviewGateFailure(); !strings.Contains(got, "security_review") {
		t.Fatalf("security review gate = %q, want security_review demand", got)
	}

	ledger.Record(grantedReviewReceipt(evidence.ReviewKindSecurity, `{
		"kind":"security",
		"verdict":"pass",
		"reviewed_paths":["internal/permission/gate.go"],
		"findings":[]
	}`))
	if got := a.reviewGateFailure(); got != "" {
		t.Fatalf("review gate = %q after both reports, want ready", got)
	}
}

// Ordinary production code buys no independent reviewer: the branch that used
// to demand one accepted self-inspection instead, so the cheap side was always
// taken and the demand never bound.
func TestReviewGateBuysNoReviewerForOrdinaryProductionCode(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/agent/parser.go"}`), true, evidence.ToolFacts{}))

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	reg.Add(fakeTool{name: "security_review", readOnly: true})
	a := &Agent{deliveryProfile: true, task: taskRuntime{ledger: ledger}, svc: agentServices{tools: reg}}

	if got := a.reviewGateFailure(); got != "" {
		t.Fatalf("medium-risk gate = %q, want no structured-review demand", got)
	}

	// The same edit under a path the project declared sensitive still buys both.
	a.projectSensitivePaths = []string{"internal/agent/**"}
	if got := a.reviewGateFailure(); !strings.Contains(got, "high-risk") {
		t.Fatalf("declared-sensitive gate = %q, want the high-risk demand", got)
	}
}

// A review that ran on its own is still read. Warnings used to be collected
// only inside the branch that demanded one, so a warn verdict at a risk level
// nobody demanded a review for was gathered nowhere.
func TestWarningsAreReadFromAnyReviewTheTurnRan(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/agent/parser.go"}`), true, evidence.ToolFacts{}))
	ledger.Record(evidence.Receipt{ToolName: "review_report", Success: true, Args: json.RawMessage(`{
		"kind":"review",
		"verdict":"warn",
		"reviewed_paths":["internal/agent/parser.go"],
		"findings":[{"severity":"warn","summary":"error path has no test"}]
	}`)})

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	a := &Agent{deliveryProfile: true, task: taskRuntime{ledger: ledger}, svc: agentServices{tools: reg}}

	if got := a.reviewGateFailure(); got != "" {
		t.Fatalf("warn must not block: %q", got)
	}
	if len(a.ReviewWarnings()) == 0 {
		t.Fatal("a warn verdict the gate did not ask for was still dropped")
	}
}

func TestDeliveryReviewGateDefersToParentInSubagents(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/permission/gate.go"}`), true, evidence.ToolFacts{}))

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	reg.Add(fakeTool{name: "security_review", readOnly: true})
	a := &Agent{agentConfig: agentConfig{subagentDepth: 1}, deliveryProfile: true, task: taskRuntime{ledger: ledger}, svc: agentServices{tools: reg}}

	// Inside a sub-agent the structured-review contract belongs to the parent,
	// which receives the child's mutation receipts via mergeChildEvidence. The
	// child must not wedge against a review_report demand it may be unable to
	// satisfy.
	if got := a.reviewGateFailure(); got != "" {
		t.Fatalf("subagent review gate = %q, want deferred to parent", got)
	}
}

// The change set is what has not been reviewed, not what the latest write
// touched. Reading both off one index let a later doc write drop a sensitive
// change out of scope and the whole structured-review demand with it.
func TestReviewGateKeepsSensitiveChangeInScopeAfterHarmlessWrite(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/permission/gate.go"}`), true, evidence.ToolFacts{}))

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	reg.Add(fakeTool{name: "security_review", readOnly: true})
	a := &Agent{deliveryProfile: true, task: taskRuntime{ledger: ledger}, svc: agentServices{tools: reg}}
	a.projectSensitivePaths = []string{"internal/permission/**"}

	if got := a.reviewGateFailure(); !strings.Contains(got, "high-risk") {
		t.Fatalf("review gate = %q, want high-risk demand", got)
	}

	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"README.md"}`), true, evidence.ToolFacts{}))
	got := a.reviewGateFailure()
	if !strings.Contains(got, "high-risk") {
		t.Fatalf("gate after a doc write = %q, want the sensitive change still in scope", got)
	}
	if !strings.Contains(got, "internal/permission/gate.go") {
		t.Fatalf("coverage hint = %q, want the sensitive path named", got)
	}
}

// A verdict the parent must act on is honored wherever it happened: the role
// setting decides how much review is owed, never whether a refusal counts.
func TestBlockingReviewStopsDeliveryAtEveryRoleSetting(t *testing.T) {
	block := json.RawMessage(`{
		"kind":"review",
		"verdict":"block",
		"reviewed_paths":["internal/agent/agent.go"],
		"findings":[{"severity":"block","summary":"nil deref on the error path"}]
	}`)
	for _, delivery := range []bool{false, true} {
		ledger := evidence.NewLedger()
		ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/agent/agent.go"}`), true, evidence.ToolFacts{}))
		ledger.Record(evidence.Receipt{ToolName: "review_report", Success: true, Args: block})

		reg := tool.NewRegistry()
		reg.Add(fakeTool{name: "review", readOnly: true})
		a := &Agent{deliveryProfile: delivery, task: taskRuntime{ledger: ledger}, svc: agentServices{tools: reg}}

		if got := a.reviewGateFailure(); !strings.Contains(got, "blocking findings") {
			t.Fatalf("delivery=%v gate = %q, want the block honored", delivery, got)
		}
	}
}

// The block holds until the turn changes something — a fix is a mutation, and
// that is what moves the freshness window past the refusal.
func TestBlockingReviewClearsOnlyAfterTheFixMutation(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/agent/agent.go"}`), true, evidence.ToolFacts{}))
	ledger.Record(evidence.Receipt{ToolName: "review_report", Success: true, Args: json.RawMessage(`{
		"kind":"review",
		"verdict":"block",
		"reviewed_paths":["internal/agent/agent.go"],
		"findings":[{"severity":"block","summary":"nil deref on the error path"}]
	}`)})

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	a := &Agent{task: taskRuntime{ledger: ledger}, svc: agentServices{tools: reg}}

	// Re-reviewing without changing anything cannot argue the block away.
	ledger.Record(evidence.Receipt{ToolName: "review_report", Success: true, Args: json.RawMessage(`{
		"kind":"review",
		"verdict":"pass",
		"reviewed_paths":["internal/agent/agent.go"],
		"findings":[]
	}`)})
	if got := a.reviewGateFailure(); !strings.Contains(got, "blocking findings") {
		t.Fatalf("gate after a pass with no fix = %q, want the block to hold", got)
	}

	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/agent/agent.go"}`), true, evidence.ToolFacts{}))
	if got := a.reviewGateFailure(); got != "" {
		t.Fatalf("gate after the fix = %q, want the refusal retired", got)
	}
}

// A warn verdict is the reviewer saying it could not establish the change was
// clean. Collected and never read, it made a conditional pass indistinguishable
// from a pass; the turn that ships on one has to say so.
func TestWarnVerdictReachesTheUserWhenTheTurnShips(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/agent/agent.go"}`), true, evidence.ToolFacts{}))
	ledger.Record(evidence.Receipt{ToolName: "review_report", Success: true, Args: json.RawMessage(`{
		"kind":"review",
		"verdict":"warn",
		"reviewed_paths":["internal/agent/agent.go"],
		"findings":[{"severity":"warn","summary":"error path has no test","path":"internal/agent/agent.go"}]
	}`)})

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	sink := &collectSink{}
	a := &Agent{deliveryProfile: true, task: taskRuntime{ledger: ledger}, svc: agentServices{tools: reg, sink: sink}}

	if got := a.reviewGateFailure(); got != "" {
		t.Fatalf("warn must not block: %q", got)
	}
	if len(a.ReviewWarnings()) == 0 {
		t.Fatal("warn findings were not collected")
	}

	a.reportReviewWarnings()
	if len(sink.notices) != 1 || !strings.Contains(sink.notices[0], "unresolved warnings") {
		t.Fatalf("notices = %v, want the shipped-with-warnings notice", sink.notices)
	}
	// Reported once: the readiness check runs more than once per turn.
	a.reportReviewWarnings()
	if len(sink.notices) != 1 {
		t.Fatalf("notices = %v, want the notice emitted exactly once", sink.notices)
	}
}
