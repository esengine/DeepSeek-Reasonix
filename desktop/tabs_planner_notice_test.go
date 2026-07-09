package main

import (
	"testing"

	"reasonix/internal/event"
)

// Planner notices are persisted through plannerDisplay into session history;
// dropping Detail here would make the expandable diagnostics disappear after
// a reload even though the live transcript showed them.
func TestRecordPlannerDisplayEventKeepsNoticeDetail(t *testing.T) {
	tab := &WorkspaceTab{}
	tab.recordPlannerDisplayEvent(event.Event{
		Kind:   event.Notice,
		Level:  event.LevelWarn,
		Source: event.UsageSourcePlanner,
		Text:   "An MCP server failed to start.",
		Detail: `mcp server "github" failed to start: command not found`,
	})
	if len(tab.plannerDisplay) != 1 {
		t.Fatalf("plannerDisplay len = %d, want 1", len(tab.plannerDisplay))
	}
	got := tab.plannerDisplay[0]
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
