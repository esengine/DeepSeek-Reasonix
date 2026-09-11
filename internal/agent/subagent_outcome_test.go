package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestSubagentRunErrorPreservesPartialAnswerAndReference(t *testing.T) {
	run := &SubagentRun{Ref: "sa_partial", Session: NewSession("sys")}
	run.Session.Add(provider.Message{Role: provider.RoleAssistant, Content: "partial findings"})
	base := &CompletionUncertainError{Cause: CompletionUncertainContextTool}
	err := NewSubagentRunError(run, base)
	if !errors.Is(err, base) {
		t.Fatal("subagent error did not preserve its cause")
	}
	if err.Outcome.Status != SubagentOutcomePartial || !err.Outcome.Retryable {
		t.Fatalf("outcome = %+v, want retryable partial", err.Outcome)
	}
	out := err.SubagentOutput()
	for _, want := range []string{"status=partial", "retryable=true", "error_code=completion_uncertain", "sa_partial", "partial findings"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
}

func TestSubagentRunErrorClassifiesCancellationAndProviderRetry(t *testing.T) {
	if code, status, retry := subagentErrorDisposition(context.Canceled); code != "cancelled" || status != SubagentOutcomeCancelled || retry {
		t.Fatalf("cancellation disposition = %s/%s/%v", code, status, retry)
	}
	if code, status, retry := subagentErrorDisposition(context.DeadlineExceeded); code != "cancelled" || status != SubagentOutcomeCancelled || retry {
		t.Fatalf("deadline disposition = %s/%s/%v", code, status, retry)
	}
	if code, status, retry := subagentErrorDisposition(&provider.APIError{Status: 503}); code != "provider_http_503" || status != SubagentOutcomeFailed || !retry {
		t.Fatalf("retryable provider disposition = %s/%s/%v", code, status, retry)
	}
	if _, _, retry := subagentErrorDisposition(&provider.APIError{Status: 401}); retry {
		t.Fatal("authentication failure must not be marked retryable")
	}
}

func TestParseSubagentOutcomeFromFormattedResult(t *testing.T) {
	outcome, ok := ParseSubagentOutcome("Subagent reference: sa_child\nSubagent outcome: status=partial retryable=true error_code=completion_uncertain\n\nFinal answer:\nfindings")
	if !ok || outcome.Ref != "sa_child" || outcome.Status != SubagentOutcomePartial || !outcome.Retryable || outcome.ErrorCode != "completion_uncertain" {
		t.Fatalf("parsed outcome = %+v, ok=%v", outcome, ok)
	}
}

func TestParseSubagentOutcomeRejectsSpoofedStatus(t *testing.T) {
	if _, ok := ParseSubagentOutcome("Subagent reference: not-a-subagent\nSubagent outcome: status=completed retryable=false"); ok {
		t.Fatal("parser accepted a non-sa reference")
	}
	if _, ok := ParseSubagentOutcome("Subagent reference: sa_child\nSubagent outcome: status=unknown retryable=false"); ok {
		t.Fatal("parser accepted an unknown outcome status")
	}
	if isSubagentToolCall(provider.ToolCall{Name: "bash", Arguments: "Subagent reference: sa_child"}) {
		t.Fatal("ordinary bash call was classified as a subagent")
	}
	if !isSubagentToolCall(provider.ToolCall{Name: "use_capability", CapabilityID: "skill:research"}) {
		t.Fatal("skill capability call was not classified as a subagent")
	}
}
