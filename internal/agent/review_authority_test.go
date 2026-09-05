package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/agentpreset"
	"reasonix/internal/evidence"
	"reasonix/internal/taskpolicy"
	"reasonix/internal/tool"
)

// reviewLedgerWithRead is a run that has read one file and nothing else, which
// is the state a reviewer is in when it submits.
func reviewLedgerWithRead(path string) *evidence.Ledger {
	l := evidence.NewLedger()
	l.Record(evidence.Receipt{ToolName: "read_file", Success: true, Read: true,
		Paths: []string{path}, OutputBytes: 4096})
	return l
}

// A run may only report the kind it was assigned. The label is an assertion
// about the host's own grant, so a mismatch is refused at the tool rather than
// recorded and unpicked by a gate downstream.
func TestReviewReportRefusesAKindTheRunWasNotAssigned(t *testing.T) {
	const target = "internal/permission/gate.go"
	for _, tc := range []struct {
		granted, submitted evidence.ReviewKind
		accepted           bool
	}{
		{evidence.ReviewKindReview, evidence.ReviewKindReview, true},
		{evidence.ReviewKindReview, evidence.ReviewKindSecurity, false},
		{evidence.ReviewKindSecurity, evidence.ReviewKindSecurity, true},
		{evidence.ReviewKindSecurity, evidence.ReviewKindReview, false},
	} {
		led := reviewLedgerWithRead(target)
		ctx := evidence.WithLedger(context.Background(), led)
		tl := NewReviewReportTool(reviewGrant(tc.granted))
		body := fmt.Sprintf(`{"kind":%q,"verdict":"pass","reviewed_paths":[%q]}`, tc.submitted, target)

		out, err := tl.Execute(ctx, json.RawMessage(body))
		if tc.accepted {
			if err != nil {
				t.Fatalf("granted=%s submitted=%s: %v", tc.granted, tc.submitted, err)
			}
			if !strings.Contains(out, "accepted") {
				t.Fatalf("granted=%s: out=%q", tc.granted, out)
			}
			continue
		}
		if err == nil {
			t.Fatalf("granted=%s submitted=%s was accepted; a run must not choose which review it counts as",
				tc.granted, tc.submitted)
		}
		if !strings.Contains(err.Error(), "assigned") {
			t.Fatalf("refusal must name the assignment, got %v", err)
		}
	}
}

// Accepted exactly once. A refused attempt leaves nothing behind, so a run that
// got the schema or the kind wrong can still correct itself; a run that already
// reported cannot follow its verdict with a second one.
func TestReviewReportAcceptsOneReportPerExecution(t *testing.T) {
	const target = "a.go"
	led := reviewLedgerWithRead(target)
	ctx := evidence.WithLedger(context.Background(), led)
	tl := NewReviewReportTool(reviewGrant(evidence.ReviewKindReview))
	body := json.RawMessage(`{"kind":"review","verdict":"pass","reviewed_paths":["a.go"]}`)

	// A refused attempt is not an accepted one: nothing is recorded for it.
	if _, err := tl.Execute(ctx, json.RawMessage(`{"kind":"security","verdict":"pass","reviewed_paths":["a.go"]}`)); err == nil {
		t.Fatal("wrong kind must be refused")
	}
	if _, err := tl.Execute(ctx, body); err != nil {
		t.Fatalf("a run must still be able to correct itself: %v", err)
	}
	led.Record(evidence.Receipt{ToolName: "review_report", Success: true, Args: body,
		ReportKind: evidence.ReviewKindReview})
	if _, err := tl.Execute(ctx, body); err == nil {
		t.Fatal("a second accepted report must be refused")
	}
}

// A worker that owes no verdict never sees the instrument. Refusing the call
// would still describe a tool the model can reach for; there is none.
func TestOrdinaryWorkerIsNeverMountedTheReportTool(t *testing.T) {
	reg := tool.NewRegistry()
	attachReviewReport(reg, ReviewReportGrant{})
	if _, ok := reg.Get("review_report"); ok {
		t.Fatal("a worker owing no verdict must not be mounted the report tool")
	}
	attachReviewReport(reg, reviewGrant(evidence.ReviewKindReview))
	if _, ok := reg.Get("review_report"); !ok {
		t.Fatal("a worker owing a verdict must be mounted the report tool")
	}
}

// The 10c-0 counterfactual, rerun. Same report body, same execution facts, and
// the only thing that moves is what the host granted the run. A host decision
// that changes here is correct: the variable is a host-owned capability.
func TestHostGrantIsWhatMovesTheReviewObligation(t *testing.T) {
	const target = "internal/permission/gate.go"
	const body = `{"kind":%q,"verdict":"pass","reviewed_paths":["internal/permission/gate.go"]}`

	gateFor := func(granted evidence.ReviewKind) string {
		ledger := evidence.NewLedger()
		ledger.Record(evidence.ReceiptFromToolCall("edit_file",
			json.RawMessage(`{"path":"`+target+`"}`), true, evidence.ToolFacts{}))
		ledger.Record(evidence.Receipt{ToolName: "read_file", Success: true, Read: true,
			Paths: []string{target}, OutputBytes: 4096})
		ledger.Record(grantedReviewReceipt(granted, fmt.Sprintf(body, granted)))

		reg := tool.NewRegistry()
		reg.Add(fakeTool{name: "review", readOnly: true})
		reg.Add(fakeTool{name: "security_review", readOnly: true})
		a := &Agent{deliveryProfile: true, task: taskRuntime{ledger: ledger}, svc: agentServices{tools: reg}}
		a.projectSensitivePaths = []string{"internal/permission/**"}
		a.turn.policy = taskpolicy.Derive(taskpolicy.Input{Preset: agentpreset.Delivery})
		a.turn.policySet = true
		return a.reviewGateFailure()
	}

	if got := gateFor(evidence.ReviewKindReview); !strings.Contains(got, "security_review") {
		t.Fatalf("a review grant must leave the security obligation open, got %q", got)
	}
	if got := gateFor(evidence.ReviewKindSecurity); !strings.Contains(got, "require review with") {
		t.Fatalf("a security grant must leave the review obligation open, got %q", got)
	}
}

// The leak 10c-0 measured, rerun end to end: a run contracted for review, its
// receipts merged into the parent exactly as production merges them. It can no
// longer close the security obligation, because it never could — it just used
// to be able to say it had.
func TestReviewContractedRunCannotCloseTheSecurityObligation(t *testing.T) {
	const target = "internal/permission/gate.go"
	child := reviewLedgerWithRead(target)
	ctx := evidence.WithLedger(context.Background(), child)
	grant := reviewGrant(evidence.ReviewKindReview)
	tl := NewReviewReportTool(grant)

	for _, kind := range evidence.ReviewKinds() {
		body := fmt.Sprintf(`{"kind":%q,"verdict":"pass","reviewed_paths":[%q]}`, kind, target)
		_, err := tl.Execute(ctx, json.RawMessage(body))
		if kind == evidence.ReviewKindSecurity && err == nil {
			t.Fatal("a review-contracted run must not be able to file a security report")
		}
		if err != nil {
			continue
		}
		authority := grant.Authority
		child.Record(evidence.Receipt{ToolName: "review_report", Success: true,
			Args: json.RawMessage(body), ReportKind: kind,
			ReviewAuthority: &authority, SourceExecutionID: grant.Execution})
	}

	parent := evidence.NewLedger()
	parent.Record(evidence.ReceiptFromToolCall("edit_file",
		json.RawMessage(`{"path":"`+target+`"}`), true, evidence.ToolFacts{}))
	parent.MergeChild(child.Summary())

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	reg.Add(fakeTool{name: "security_review", readOnly: true})
	a := &Agent{deliveryProfile: true, task: taskRuntime{ledger: parent}, svc: agentServices{tools: reg}}
	a.projectSensitivePaths = []string{"internal/permission/**"}
	a.turn.policy = taskpolicy.Derive(taskpolicy.Input{Preset: agentpreset.Delivery})
	a.turn.policySet = true

	if got := a.reviewGateFailure(); !strings.Contains(got, "security_review") {
		t.Fatalf("one review-contracted run closed the security obligation: gate = %q", got)
	}
}

// The stamp comes off the mounted instrument, so what the receipt says it may
// prove is what the host issued — not what the payload asked to be read as.
func TestReceiptStampIsReadFromTheGrantNotThePayload(t *testing.T) {
	grant := reviewGrant(evidence.ReviewKindReview)
	rec := evidence.ReceiptFromToolCall("review_report",
		json.RawMessage(`{"kind":"review","verdict":"pass","reviewed_paths":["a.go"]}`),
		true, evidence.ToolFacts{ReadOnly: true})
	stampReviewGrant(&rec, &toolCallPlan{tool: NewReviewReportTool(grant)})

	if rec.ReviewAuthority == nil || !rec.ReviewAuthority.Proves(evidence.ReviewKindReview) {
		t.Fatalf("stamp = %+v, want the issued grant", rec.ReviewAuthority)
	}
	if rec.ReviewAuthority.Proves(evidence.ReviewKindSecurity) {
		t.Fatal("a review grant must not prove the security obligation")
	}
	if rec.SourceExecutionID != grant.Execution {
		t.Fatalf("execution = %q, want %q", rec.SourceExecutionID, grant.Execution)
	}
	// The delivery fact still comes off the payload; it just decides nothing.
	if rec.ReportKind != evidence.ReviewKindReview {
		t.Fatalf("report kind = %q", rec.ReportKind)
	}
}

// A receipt the host did not stamp is not upgraded by anything downstream
// recognising the payload. If review_report is ever reached through a proxy or
// an alias, the receipt records no grant — so the resolution path has to be
// given one deliberately rather than inheriting proof from a parser.
func TestAReceiptTheHostDidNotStampProvesNothing(t *testing.T) {
	body := json.RawMessage(`{"kind":"review","verdict":"pass","reviewed_paths":["a.go"]}`)

	direct := evidence.ReceiptFromToolCall("review_report", body, true, evidence.ToolFacts{ReadOnly: true})
	stampReviewGrant(&direct, &toolCallPlan{tool: NewReviewReportTool(reviewGrant(evidence.ReviewKindReview))})
	if !evidence.ReceiptProves(direct, evidence.ReviewKindReview) {
		t.Fatal("a directly mounted report must prove its own obligation")
	}

	// The same valid payload, recorded under the same tool name, but resolved
	// through something that is not the mounted instrument.
	proxied := evidence.ReceiptFromToolCall("review_report", body, true, evidence.ToolFacts{ReadOnly: true})
	stampReviewGrant(&proxied, &toolCallPlan{tool: fakeTool{name: "use_capability", readOnly: true}})

	if proxied.ReviewAuthority != nil {
		t.Fatalf("a proxied report carried a grant: %+v", proxied.ReviewAuthority)
	}
	if proxied.SourceExecutionID != "" {
		t.Fatalf("a proxied report named an execution: %q", proxied.SourceExecutionID)
	}
	for _, kind := range evidence.ReviewKinds() {
		if evidence.ReceiptProves(proxied, kind) {
			t.Fatalf("a proxied report proved %q", kind)
		}
	}
	// It is still recognisably a report: delivery is unaffected, proof is not.
	if proxied.ReportKind != evidence.ReviewKindReview {
		t.Fatalf("report kind = %q", proxied.ReportKind)
	}
}

// A grant with no execution behind it is no grant. Held at issuance so the
// ledger never carries a permission that reads as wider than it is.
func TestAGrantWithNoExecutionCarriesNoAuthority(t *testing.T) {
	named := IssueReviewGrant(evidence.ReviewKindReview,
		evidence.GrantReviewAuthority(evidence.ReviewKindReview), "exec-1")
	if !named.Authority.Proves(evidence.ReviewKindReview) {
		t.Fatal("a named execution keeps the authority it was granted")
	}

	unnamed := IssueReviewGrant(evidence.ReviewKindReview,
		evidence.GrantReviewAuthority(evidence.ReviewKindReview), "  ")
	if unnamed.Delivery != evidence.ReviewKindReview {
		t.Fatal("the report is still owed")
	}
	if len(unnamed.Authority.Satisfies) != 0 {
		t.Fatalf("an unnamed execution was granted %v", unnamed.Authority.Satisfies)
	}
}
