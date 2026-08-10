package main

import (
	"path/filepath"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// Planner notices are persisted through plannerDisplay into session history;
// dropping Detail here would make the expandable diagnostics disappear after
// a reload even though the live transcript showed them.
func TestRecordPlannerDisplayEventKeepsNoticeDetail(t *testing.T) {
	tab := &WorkspaceTab{}
	tab.recordDisplayEvent(event.Event{
		Kind:   event.Notice,
		Level:  event.LevelWarn,
		Source: event.UsageSourcePlanner,
		Text:   "An MCP server failed to start.",
		Detail: `mcp server "github" failed to start: command not found`,
	})
	messages := tab.takeDisplayTurn(false)
	if len(messages) != 1 {
		t.Fatalf("planner display len = %d, want 1", len(messages))
	}
	got := messages[0]
	if got.Role != "notice" || got.Level != "warn" {
		t.Fatalf("unexpected notice message: %+v", got)
	}
	if got.Content != "An MCP server failed to start." {
		t.Fatalf("Content = %q", got.Content)
	}
	if got.Detail != `mcp server "github" failed to start: command not found` {
		t.Fatalf("Detail = %q, want diagnostic detail preserved", got.Detail)
	}
}

func TestCancelledDisplaySidecarReplaysStructuredDecisionReceipt(t *testing.T) {
	const userContent = "run the build"
	receipt := &provider.DecisionReceipt{
		ID:      "approval-1",
		Kind:    "tool",
		Tool:    "bash",
		Subject: "npm run build",
		Outcome: "allow_once",
	}
	tab := &WorkspaceTab{}
	tab.recordDisplayEvent(event.Event{
		Kind:            event.Notice,
		Level:           event.LevelInfo,
		Source:          event.UsageSourceExecutor,
		Text:            "Decision recorded: allow_once",
		Code:            event.NoticeCodeDecisionReceipt,
		DecisionReceipt: receipt,
	})
	display := tab.takeDisplayTurn(true)
	if len(display) != 2 || display[0].DecisionReceipt == nil || display[1].Code != event.NoticeCodeCancelledTurn {
		t.Fatalf("cancelled display = %+v, want structured receipt plus cancellation notice", display)
	}

	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	if err := recordSessionPlannerDisplay(dir, sessionPath, userContent, display); err != nil {
		t.Fatalf("record cancelled display: %v", err)
	}
	plannerTurns := sessionPlannerDisplayTurns(dir, sessionPath)
	if len(plannerTurns) != 1 || len(plannerTurns[0].Messages) != 2 || plannerTurns[0].Messages[0].DecisionReceipt == nil {
		t.Fatalf("persisted display lost decision receipt: %+v", plannerTurns)
	}

	visible := historyMessagesWithPlannerDisplays([]provider.Message{
		{Role: provider.RoleUser, Content: userContent},
		{Role: provider.RoleAssistant, DecisionReceipts: []*provider.DecisionReceipt{receipt}},
	}, func(content string) string { return content }, plannerTurns, nil)
	if len(visible) != 3 {
		t.Fatalf("visible history = %+v, want user, sidecar receipt, and cancellation notice", visible)
	}
	got := visible[1]
	if got.Code != event.NoticeCodeDecisionReceipt || got.DecisionReceipt == nil {
		t.Fatalf("structured decision receipt missing after reload: %+v", got)
	}
	if got.DecisionReceipt.ID != receipt.ID || got.DecisionReceipt.Tool != receipt.Tool || got.DecisionReceipt.Subject != receipt.Subject || got.DecisionReceipt.Outcome != receipt.Outcome {
		t.Fatalf("replayed receipt = %+v, want %+v", got.DecisionReceipt, receipt)
	}
	for i, message := range visible {
		if i != 1 && message.DecisionReceipt != nil {
			t.Fatalf("decision receipt replayed more than once: %+v", visible)
		}
	}
}
