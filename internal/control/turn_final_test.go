package control

import (
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func latestAssistantTextForTest(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleAssistant && strings.TrimSpace(msgs[i].Content) != "" {
			return msgs[i].Content
		}
	}
	return ""
}

func newTurnFinalTestController(sess *agent.Session) (*Controller, *agent.Agent) {
	exec := agent.New(&scriptedTurns{}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	return New(Options{Runner: exec, Executor: exec, Sink: event.Discard}), exec
}

func TestTurnFinalBoundaryReadsOnlyCurrentTurn(t *testing.T) {
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "old question"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "OLD answer"})
	c, _ := newTurnFinalTestController(sess)
	boundary := c.captureTurnFinalBoundary()

	if got := boundary.currentVisibleFinal(c); got != "" {
		t.Fatalf("final before current turn = %q, want empty", got)
	}
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "current question"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "CURRENT answer"})
	if got := boundary.currentVisibleFinal(c); got != "CURRENT answer" {
		t.Fatalf("current final = %q, want CURRENT answer", got)
	}
}

func TestTurnFinalBoundaryFailsClosedOnNonTerminalAssistant(t *testing.T) {
	tests := []struct {
		name string
		tail []provider.Message
	}{
		{name: "tool result follows assistant", tail: []provider.Message{
			{Role: provider.RoleAssistant, Content: "not terminal", ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read_file"}}},
			{Role: provider.RoleTool, Content: "result", ToolCallID: "call-1", Name: "read_file"},
		}},
		{name: "local-only assistant", tail: []provider.Message{{Role: provider.RoleAssistant, Content: "partial", LocalOnly: true}}},
		{name: "reasoning-only assistant", tail: []provider.Message{{Role: provider.RoleAssistant, ReasoningContent: "private reasoning"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := agent.NewSession("")
			c, _ := newTurnFinalTestController(sess)
			boundary := c.captureTurnFinalBoundary()
			sess.Add(provider.Message{Role: provider.RoleUser, Content: "current"})
			for _, msg := range tt.tail {
				sess.Add(msg)
			}
			if got := boundary.currentVisibleFinal(c); got != "" {
				t.Fatalf("current final = %q, want fail-closed empty", got)
			}
		})
	}
}

func TestTurnFinalBoundaryKeepsCurrentPartialTextForHooks(t *testing.T) {
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "OLD answer"})
	c, _ := newTurnFinalTestController(sess)
	boundary := c.captureTurnFinalBoundary()
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "current"})
	sess.Add(provider.Message{
		Role: provider.RoleAssistant, Content: "CURRENT tool preamble", LocalOnly: true,
		ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read_file"}},
	})
	sess.Add(provider.Message{Role: provider.RoleTool, Content: "result", ToolCallID: "call-1", Name: "read_file"})

	if got := boundary.currentVisibleFinal(c); got != "" {
		t.Fatalf("semantic final = %q, want strict empty", got)
	}
	if got := boundary.currentAssistantText(c); got != "CURRENT tool preamble" {
		t.Fatalf("hook assistant text = %q, want current partial", got)
	}
}

func TestTurnFinalBoundaryRejectsRewriteWithCurrentUser(t *testing.T) {
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "old question", CreatedAt: 1})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "OLD answer"})
	c, _ := newTurnFinalTestController(sess)
	boundary := c.captureTurnFinalBoundary()

	sess.Rewrite([]provider.Message{
		{Role: provider.RoleAssistant, Content: "summary"},
		{Role: provider.RoleUser, Content: "current question"},
		{Role: provider.RoleAssistant, Content: "CURRENT after compaction"},
	}, "test_compaction")
	if got := boundary.currentVisibleFinal(c); got != "" {
		t.Fatalf("rewritten current final = %q, want fail-closed empty", got)
	}
}

func TestTurnFinalBoundaryRejectsReplace(t *testing.T) {
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "old question", CreatedAt: 1})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "OLD answer"})
	c, _ := newTurnFinalTestController(sess)
	boundary := c.captureTurnFinalBoundary()

	sess.Replace([]provider.Message{
		{Role: provider.RoleUser, Content: "old question"},
		{Role: provider.RoleAssistant, Content: "OLD replacement answer"},
	})
	if got := boundary.currentVisibleFinal(c); got != "" {
		t.Fatalf("replacement stale final = %q, want empty", got)
	}
}

func TestTurnFinalBoundaryAllowsLocalMetadataUpdates(t *testing.T) {
	tests := []struct {
		name   string
		update func(*testing.T, *agent.Session)
	}{
		{name: "tool resolution", update: func(t *testing.T, sess *agent.Session) {
			readOnly := true
			if !sess.UpdateToolCallResolution(provider.ToolCall{
				ID: "call-1", ResolvedName: "read_file", ResolvedReadOnly: &readOnly,
			}) {
				t.Fatal("UpdateToolCallResolution returned false")
			}
		}},
		{name: "tool preview", update: func(t *testing.T, sess *agent.Session) {
			if !sess.UpdateToolCallPreview(provider.ToolCall{ID: "call-1", Diff: "preview", Added: 1}) {
				t.Fatal("UpdateToolCallPreview returned false")
			}
		}},
		{name: "decision receipt", update: func(_ *testing.T, sess *agent.Session) {
			sess.AddDecisionReceipt(&provider.DecisionReceipt{ID: "decision-1", Kind: "approval", Outcome: "allow"})
		}},
		{name: "local metadata replacement", update: func(_ *testing.T, sess *agent.Session) {
			msgs := sess.Snapshot()
			msgs[0].Edited = true
			sess.ReplaceLocalMetadata(msgs)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := agent.NewSession("")
			c, _ := newTurnFinalTestController(sess)
			boundary := c.captureTurnFinalBoundary()
			sess.Add(provider.Message{Role: provider.RoleUser, Content: "current"})
			sess.Add(provider.Message{
				Role: provider.RoleAssistant,
				ToolCalls: []provider.ToolCall{{
					ID:   "call-1",
					Name: "mcp__proxy",
				}},
			})
			tt.update(t, sess)
			sess.Add(provider.Message{Role: provider.RoleTool, Content: "result", ToolCallID: "call-1", Name: "mcp__proxy"})
			sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "CURRENT answer"})

			if got := boundary.currentVisibleFinal(c); got != "CURRENT answer" {
				t.Fatalf("current final after local metadata update = %q, want CURRENT answer", got)
			}
		})
	}
}

func TestTurnFinalBoundaryRejectsSessionReplacement(t *testing.T) {
	sess := agent.NewSession("")
	c, exec := newTurnFinalTestController(sess)
	boundary := c.captureTurnFinalBoundary()
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "current"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "CURRENT old-session answer"})

	replacement := agent.NewSession("")
	replacement.Add(provider.Message{Role: provider.RoleUser, Content: "replacement"})
	replacement.Add(provider.Message{Role: provider.RoleAssistant, Content: "replacement answer"})
	exec.SetSession(replacement)
	if got := boundary.currentVisibleFinal(c); got != "" {
		t.Fatalf("replaced-session final = %q, want empty", got)
	}
}
