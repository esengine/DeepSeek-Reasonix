package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/tool"
)

func TestDeliveryReviewGateExplainsOpaqueMutationRecovery(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "bash",
		Success:  true,
		Mutation: true,
		Command:  "opaque-writer",
	})

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	reg.Add(fakeTool{name: "security_review", readOnly: true})
	a := &Agent{deliveryProfile: true, evidence: ledger, tools: reg}

	got := a.deliveryReviewGateFailure()
	for _, want := range []string{"high-risk", "git status --short", "git diff", "mutation did not report file paths"} {
		if !strings.Contains(got, want) {
			t.Fatalf("review gate = %q, want %q", got, want)
		}
	}
	if strings.HasSuffix(got, "covering: ") {
		t.Fatalf("review gate must not end with empty coverage: %q", got)
	}

	ledger.Record(evidence.Receipt{ToolName: "review_report", Success: true, Args: json.RawMessage(`{
		"kind":"review",
		"verdict":"pass",
		"reviewed_paths":["internal/agent/agent.go"],
		"findings":[]
	}`)})
	got = a.deliveryReviewGateFailure()
	if !strings.Contains(got, "security_review") || !strings.Contains(got, "mutation did not report file paths") {
		t.Fatalf("security review gate = %q, want opaque-mutation recovery guidance", got)
	}

	ledger.Record(evidence.Receipt{ToolName: "review_report", Success: true, Args: json.RawMessage(`{
		"kind":"security",
		"verdict":"pass",
		"reviewed_paths":["internal/agent/agent.go"],
		"findings":[]
	}`)})
	if got := a.deliveryReviewGateFailure(); got != "" {
		t.Fatalf("review gate = %q after both reports, want ready", got)
	}
}

func TestNonDeliveryProfileNeverRequiresStructuredReview(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/permission/gate.go"}`), true, false))

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	reg.Add(fakeTool{name: "security_review", readOnly: true})
	a := &Agent{deliveryProfile: false, evidence: ledger, tools: reg}

	if got := a.deliveryReviewGateFailure(); got != "" {
		t.Fatalf("non-Delivery review gate = %q, want disabled", got)
	}
}

func TestDeliveryReviewGateHighRiskStillRequiresSecurityReview(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/permission/gate.go"}`), true, false))

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	reg.Add(fakeTool{name: "security_review", readOnly: true})
	a := &Agent{deliveryProfile: true, evidence: ledger, tools: reg}

	if got := a.deliveryReviewGateFailure(); !strings.Contains(got, "high-risk") {
		t.Fatalf("review gate = %q, want high-risk review demand", got)
	}

	ledger.Record(evidence.Receipt{ToolName: "review_report", Success: true, Args: json.RawMessage(`{
		"kind":"review",
		"verdict":"pass",
		"reviewed_paths":["internal/permission/gate.go"],
		"findings":[]
	}`)})
	if got := a.deliveryReviewGateFailure(); !strings.Contains(got, "security_review") {
		t.Fatalf("security review gate = %q, want security_review demand", got)
	}

	ledger.Record(evidence.Receipt{ToolName: "review_report", Success: true, Args: json.RawMessage(`{
		"kind":"security",
		"verdict":"pass",
		"reviewed_paths":["internal/permission/gate.go"],
		"findings":[]
	}`)})
	if got := a.deliveryReviewGateFailure(); got != "" {
		t.Fatalf("review gate = %q after both reports, want ready", got)
	}
}

func TestDeliveryReviewGateMediumAcceptsHostProvenVerificationAndCoverage(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/agent/parser.go"}`), true, false))
	ledger.Record(evidence.ReceiptFromToolCall("bash", json.RawMessage(`{"command":"go test ./..."}`), true, true))
	ledger.Record(evidence.ReceiptFromToolCall("read_file", json.RawMessage(`{"path":"internal/agent/parser.go"}`), true, true))
	ledger.Record(evidence.Receipt{ToolName: "complete_step", Success: true, Args: json.RawMessage(`{
		"step":"fix parser",
		"evidence":[{"kind":"verification","command":"go test ./..."}]
	}`)})

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	a := &Agent{deliveryProfile: true, evidence: ledger, tools: reg}
	if got := a.deliveryReviewGateFailure(); got != "" {
		t.Fatalf("medium-risk host proof was rejected: %q", got)
	}

	missingVerification := evidence.NewLedger()
	missingVerification.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/agent/parser.go"}`), true, false))
	missingVerification.Record(evidence.ReceiptFromToolCall("read_file", json.RawMessage(`{"path":"internal/agent/parser.go"}`), true, true))
	a.evidence = missingVerification
	if got := a.deliveryReviewGateFailure(); !strings.Contains(got, "host-proven verification") {
		t.Fatalf("medium-risk review without verification = %q, want host-proof guidance", got)
	}
}

func TestDeliveryReviewGateDefersToParentInSubagents(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/permission/gate.go"}`), true, false))

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	reg.Add(fakeTool{name: "security_review", readOnly: true})
	a := &Agent{deliveryProfile: true, evidence: ledger, tools: reg, subagentDepth: 1}

	// Inside a sub-agent the structured-review contract belongs to the parent,
	// which receives the child's mutation receipts via mergeChildEvidence. The
	// child must not wedge against a review_report demand it may be unable to
	// satisfy.
	if got := a.deliveryReviewGateFailure(); got != "" {
		t.Fatalf("subagent review gate = %q, want deferred to parent", got)
	}
}
