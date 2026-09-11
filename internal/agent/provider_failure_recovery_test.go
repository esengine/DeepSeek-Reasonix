package agent

import (
	"context"
	"errors"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestProviderFailurePersistsStructuredFailedTerminal(t *testing.T) {
	apiErr := &provider.APIError{Provider: "deepseek-anthropic", ProviderDisplayName: "Deepseek2", Protocol: "openai", Status: 404}
	mp := testutil.NewMock("m", testutil.ErrorTurn(apiErr))
	a := New(mp, echoRegistry(), NewSession(""), Options{}, event.Discard)
	if err := a.Run(withNoClosedLoop(context.Background()), "check bug"); !errors.Is(err, apiErr) {
		t.Fatalf("Run error = %v", err)
	}
	last := a.Session().Messages[len(a.Session().Messages)-1]
	recovery := last.InterruptedTurn
	if !last.LocalOnly || recovery == nil || recovery.TerminalStatus != "failed" {
		t.Fatalf("failed recovery = %+v", last)
	}
	if d := recovery.FailureDiagnostic; d == nil || d.ProviderID != "deepseek-anthropic" || d.ProviderDisplayName != "Deepseek2" || d.Protocol != "openai" || d.Status != 404 {
		t.Fatalf("failure diagnostic = %+v", d)
	}
}

func TestInterruptedRecoveryDisplayMetadataDoesNotChangeProviderBlock(t *testing.T) {
	base := &provider.InterruptedTurnRecovery{Pending: true, InterruptedTools: []string{"bash"}, DroppedPartialText: true}
	enriched := *base
	enriched.TerminalStatus = "failed"
	enriched.FailureDiagnostic = &provider.FailureDiagnostic{Kind: "request", Status: 404, ProviderID: "deepseek-anthropic", ProviderDisplayName: "Deepseek2", Protocol: "openai"}
	if before, after := interruptedRecoveryBlock(base), interruptedRecoveryBlock(&enriched); before != after {
		t.Fatalf("display metadata changed provider-visible recovery\nbefore=%q\nafter=%q", before, after)
	}
}

func TestCancelledTurnPersistsInterruptedTerminal(t *testing.T) {
	a := New(testutil.NewMock("m"), echoRegistry(), NewSession(""), Options{}, event.Discard)
	a.recordInterruptedDisplay("partial", "", nil, true, context.Canceled, 0)
	last := a.Session().Messages[len(a.Session().Messages)-1]
	if last.InterruptedTurn == nil || last.InterruptedTurn.TerminalStatus != "interrupted" || last.InterruptedTurn.FailureDiagnostic != nil {
		t.Fatalf("cancelled recovery = %+v", last.InterruptedTurn)
	}
}
