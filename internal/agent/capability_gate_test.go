package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/evidence"
	"reasonix/internal/taskpolicy"
	"reasonix/internal/tool"
)

func closedLoopTurnPolicy(review taskpolicy.Review) taskpolicy.TaskPolicy {
	return taskpolicy.TaskPolicy{
		Evidence: taskpolicy.EvidenceClosedLoop,
		Review:   review,
	}
}

func withClosedLoopContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return taskpolicy.WithContext(ctx, closedLoopTurnPolicy(taskpolicy.ReviewForced))
}

func noClosedLoopPolicy() taskpolicy.TaskPolicy {
	return taskpolicy.TaskPolicy{
		// Pin risk at the ceiling so escalatePolicyFromEvidence cannot
		// RaiseRisk and reevaluate floors into a closed loop.
		Risk:         taskpolicy.RiskHigh,
		Evidence:     taskpolicy.EvidenceNone,
		Review:       taskpolicy.ReviewNone,
		Verification: taskpolicy.VerifyNone,
	}
}

func withNoClosedLoop(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return taskpolicy.WithContext(ctx, noClosedLoopPolicy())
}

func TestClosedLoopReviewGateExplainsOpaqueMutationRecovery(t *testing.T) {
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
	a := &Agent{
		task: taskRuntime{ledger: ledger},
		svc:  agentServices{tools: reg},
		turn: turnRuntime{policySet: true, policy: closedLoopTurnPolicy(taskpolicy.ReviewForcedSecurity)},
	}

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

func TestUnsetTaskPolicyNeverRequiresStructuredReview(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/permission/gate.go"}`), true, false))

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	reg.Add(fakeTool{name: "security_review", readOnly: true})
	a := &Agent{task: taskRuntime{ledger: ledger}, svc: agentServices{tools: reg}}

	if got := a.deliveryReviewGateFailure(); got != "" {
		t.Fatalf("unset TaskPolicy review gate = %q, want disabled", got)
	}
}

func TestClosedLoopReviewGateHighRiskStillRequiresSecurityReview(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/permission/gate.go"}`), true, false))

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	reg.Add(fakeTool{name: "security_review", readOnly: true})
	a := &Agent{
		task: taskRuntime{ledger: ledger},
		svc:  agentServices{tools: reg},
		turn: turnRuntime{policySet: true, policy: closedLoopTurnPolicy(taskpolicy.ReviewForcedSecurity)},
	}

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

func TestClosedLoopReviewGateMediumAcceptsHostProvenVerificationAndCoverage(t *testing.T) {
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
	a := &Agent{
		task: taskRuntime{ledger: ledger},
		svc:  agentServices{tools: reg},
		turn: turnRuntime{policySet: true, policy: closedLoopTurnPolicy(taskpolicy.ReviewForced)},
	}
	if got := a.deliveryReviewGateFailure(); got != "" {
		t.Fatalf("medium-risk host proof was rejected: %q", got)
	}

	missingVerification := evidence.NewLedger()
	missingVerification.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/agent/parser.go"}`), true, false))
	missingVerification.Record(evidence.ReceiptFromToolCall("read_file", json.RawMessage(`{"path":"internal/agent/parser.go"}`), true, true))
	a.task.ledger = missingVerification
	if got := a.deliveryReviewGateFailure(); !strings.Contains(got, "host-proven verification") {
		t.Fatalf("medium-risk review without verification = %q, want host-proof guidance", got)
	}
}

func TestClosedLoopReviewGateDefersToParentInSubagents(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"internal/permission/gate.go"}`), true, false))

	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "review", readOnly: true})
	reg.Add(fakeTool{name: "security_review", readOnly: true})
	a := &Agent{
		agentConfig: agentConfig{subagentDepth: 1},
		task:        taskRuntime{ledger: ledger},
		svc:         agentServices{tools: reg},
		turn:        turnRuntime{policySet: true, policy: closedLoopTurnPolicy(taskpolicy.ReviewForcedSecurity)},
	}

	// Inside a sub-agent the structured-review contract belongs to the parent,
	// which receives the child's mutation receipts via mergeChildEvidence. The
	// child must not wedge against a review_report demand it may be unable to
	// satisfy.
	if got := a.deliveryReviewGateFailure(); got != "" {
		t.Fatalf("subagent review gate = %q, want deferred to parent", got)
	}
}
