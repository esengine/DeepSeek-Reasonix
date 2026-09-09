package main

import (
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestHistoryRestoresProviderFailureInsteadOfInterruptedNotice(t *testing.T) {
	message := provider.Message{
		Role: provider.RoleTool, LocalOnly: true,
		InterruptedTurn: &provider.InterruptedTurnRecovery{
			Pending: true, TerminalStatus: "failed",
			FailureDiagnostic: &provider.FailureDiagnostic{Kind: "request", Status: 404, ProviderID: "deepseek-anthropic", ProviderDisplayName: "Deepseek2", Protocol: "openai", RequestPath: "/anthropic/v1/chat/completions"},
		},
	}
	history := historyMessages([]provider.Message{message}, func(value string) string { return value })
	if len(history) != 1 {
		t.Fatalf("history = %+v", history)
	}
	got := history[0]
	if got.Code != event.NoticeCodeProviderRequestFailed || got.Level != "warn" || !strings.Contains(got.Content, "Deepseek2 · Chat Completions") || !strings.Contains(got.Content, "HTTP 404") {
		t.Fatalf("failure history = %+v", got)
	}
	if got.Detail != "Connection ID: deepseek-anthropic\nRequest path: /anthropic/v1/chat/completions" || got.Diagnostic == nil || got.Diagnostic.ProviderID != "deepseek-anthropic" {
		t.Fatalf("failure detail = %+v", got)
	}
}

func TestHistoryKeepsLegacyAndExplicitInterruptionsCompatible(t *testing.T) {
	for _, recovery := range []*provider.InterruptedTurnRecovery{{Pending: true}, {Pending: true, TerminalStatus: "interrupted"}} {
		history := historyMessages([]provider.Message{{Role: provider.RoleTool, LocalOnly: true, InterruptedTurn: recovery}}, func(value string) string { return value })
		if len(history) != 1 || history[0].Code != event.NoticeCodeCancelledTurn {
			t.Fatalf("interrupted history = %+v", history)
		}
	}
}
