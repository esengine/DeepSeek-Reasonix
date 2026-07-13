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
	for _, want := range []string{"git status --short", "git diff", "mutation did not report file paths"} {
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
