package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func recoverySessionWithCall(id string, r *provider.ToolCallRecord) *Session {
	s := NewSession("")
	s.Messages = []provider.Message{{Role: provider.RoleAssistant, ID: "turn-1", ToolCalls: []provider.ToolCall{{ID: id, Name: "write_file", Arguments: `{"path":"x"}`, Recovery: r}}}}
	return s
}

func TestPendingToolRecoverySurvivesUserTailAndIsProviderExcluded(t *testing.T) {
	r := &provider.ToolCallRecord{Identity: provider.ActionIdentity{AttemptID: "attempt-1", CallID: "call-1"}, State: provider.ToolRunUnknown, Arguments: json.RawMessage(`{"path":"secret"}`), ReadOnly: false}
	s := recoverySessionWithCall("call-1", r)
	a := New(nil, tool.NewRegistry(), s, Options{}, event.Discard)
	// A subsequent user message must not clear the durable unresolved record.
	s.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	pending := a.PendingToolRecovery()
	if len(pending) != 1 || pending[0].Identity.AttemptID != "attempt-1" {
		t.Fatalf("pending recovery after user tail = %+v", pending)
	}
	model := provider.ModelMessages(s.Snapshot())
	raw, _ := json.Marshal(model)
	if strings.Contains(string(raw), "tool_recovery") {
		t.Fatal("local recovery leaked to provider")
	}
	for _, m := range model {
		if m.ToolCalls != nil && m.ToolCalls[0].Recovery != nil {
			t.Fatal("Recovery metadata leaked into ModelMessages")
		}
	}
}

func TestSetToolRecoveryRecordRejectsStaleAttemptAndDetachesArguments(t *testing.T) {
	original := provider.ToolCallRecord{Identity: provider.ActionIdentity{AttemptID: "new"}, State: provider.ToolRunStarted, Arguments: json.RawMessage(`{"path":"safe"}`)}
	s := recoverySessionWithCall("call-1", &original)
	stale := original
	stale.Identity.AttemptID = "old"
	if s.setToolRecoveryRecord("call-1", stale) {
		t.Fatal("stale attempt replaced current recovery record")
	}
	updated := original
	updated.State = provider.ToolRunCompleted
	updated.Arguments[0] = 'X'
	if !s.setToolRecoveryRecord("call-1", updated) {
		t.Fatal("current attempt update rejected")
	}
	updated.Arguments[0] = 'Y'
	got := s.toolRecoveryRecord("call-1")
	if got == nil || got.State != provider.ToolRunCompleted || got.Arguments[0] != 'X' {
		t.Fatalf("record was not detached: %+v", got)
	}
}

func TestFinishToolRecoveryFailedWriterIsNotCompleted(t *testing.T) {
	r := provider.ToolCallRecord{Identity: provider.ActionIdentity{AttemptID: "attempt-1"}, State: provider.ToolRunStarted, ReadOnly: false}
	s := recoverySessionWithCall("call-1", &r)
	a := New(nil, tool.NewRegistry(), s, Options{}, event.Discard)
	a.finishToolRecovery(provider.ToolCall{ID: "call-1"}, toolOutcome{executed: true, output: "partial", errMsg: "write failed"})
	got := s.toolRecoveryRecord("call-1")
	if got == nil || got.State != provider.ToolRunFailed {
		t.Fatalf("failed writer recovery = %+v", got)
	}
}
