package control

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestToolRecoverySnapshotStripsArgumentsAndIsStable(t *testing.T) {
	const secret = "RECOVERY-ARGUMENT-MUST-STAY-LOCAL"
	a := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	a.Session().Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call", Name: "write_file", Arguments: `{"path":"x"}`, Recovery: &provider.ToolCallRecord{Identity: provider.ActionIdentity{AttemptID: "attempt", CallID: "call"}, Arguments: json.RawMessage(`{"content":"` + secret + `"}`), State: provider.ToolRunUnknown}}}})
	c := New(Options{Executor: a, Sink: event.Discard})
	s := c.ToolRecoverySnapshot()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) {
		t.Fatal("snapshot leaked raw recovery arguments")
	}
	if len(s.Calls) != 1 || s.Calls[0].Identity.AttemptID != "attempt" {
		t.Fatalf("snapshot=%+v", s)
	}
	if len(s.Calls[0].Arguments) != 0 {
		t.Fatal("snapshot retained arguments")
	}
	if s.Revision == "" {
		t.Fatal("snapshot revision missing")
	}
	if s.Revision != c.ToolRecoverySnapshot().Revision {
		t.Fatal("unchanged snapshot revision moved")
	}
}

func TestToolRecoveryRejectsStaleSnapshot(t *testing.T) {
	c := New(Options{Sink: event.Discard})
	v := c.ToolRecoverySnapshot()
	v.Revision = "stale"
	_, err := c.ResolveToolRecovery(context.TODO(), ToolRecoveryRequest{SessionPath: v.SessionPath, RuntimeEpoch: v.RuntimeEpoch, Revision: v.Revision, Action: "confirm"})
	if err == nil || !strings.Contains(err.Error(), "snapshot changed") {
		t.Fatalf("err=%v", err)
	}
}
