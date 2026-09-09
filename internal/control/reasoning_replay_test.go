package control

import (
	"context"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type strictReplayControlProvider struct{}

func (strictReplayControlProvider) Name() string { return "deepseek-anthropic" }
func (strictReplayControlProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	panic("unexpected Stream call")
}
func (strictReplayControlProvider) RequiresAssistantReasoningReplay(m provider.Message) bool {
	return len(m.ToolCalls) > 0 || len(m.ServerSearch) > 0
}

func TestInterruptedRecoveryExcludesStructurallyCompleteButUnreplayableToolTurn(t *testing.T) {
	sess := agent.NewSession("system")
	user := provider.Message{Role: provider.RoleUser, Content: "update file"}
	sess.Add(user)
	sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
		ID: "c1", Name: "write_file", Arguments: `{"path":"a.txt","content":"ok"}`,
	}}})
	sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "c1", Name: "write_file", Content: "wrote a.txt"})
	exec := agent.New(strictReplayControlProvider{}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec})

	c.stripCancelledVisibleTurnMessagesAfterWithFallback(1, user)

	model := provider.ModelMessages(sess.Snapshot())
	for _, m := range model {
		if len(m.ToolCalls) > 0 || m.Role == provider.RoleTool {
			t.Fatalf("unreplayable completed pair remained provider-visible: %+v", model)
		}
	}
	msgs := sess.Snapshot()
	last := msgs[len(msgs)-1]
	if !last.LocalOnly || last.InterruptedTurn == nil || !last.InterruptedTurn.Pending {
		t.Fatalf("missing local recovery handoff: %+v", msgs)
	}
}

func TestInterruptedPartialBatchKeepsCompletedAndUnknownSeparate(t *testing.T) {
	sess := agent.NewSession("system")
	user := provider.Message{Role: provider.RoleUser, Content: "update files"}
	sess.Add(user)
	sess.Add(provider.Message{Role: provider.RoleAssistant, ReasoningContent: "original", ReasoningSignature: "original-proof", ToolCalls: []provider.ToolCall{
		{ID: "done", Name: "write_file", Arguments: `{"path":"a.txt"}`},
		{ID: "uncertain", Name: "write_file", Arguments: `{"path":"b.txt"}`},
		{ID: "pending", Name: "read_file", Arguments: `{"path":"c.txt"}`},
	}})
	sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "done", Name: "write_file", Content: "wrote a.txt", ToolRunState: provider.ToolRunCompleted})
	sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "pending", Name: "read_file", Content: "cancelled: context cancelled before execution", ToolRunState: provider.ToolRunNotStarted})
	exec := agent.New(strictReplayControlProvider{}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec})
	c.stripCancelledVisibleTurnMessagesAfterWithFallback(1, user)
	msgs := sess.Snapshot()
	r := msgs[len(msgs)-1].InterruptedTurn
	if r == nil || len(r.CompletedTools) != 1 || r.CompletedTools[0].ID != "done" || len(r.UnknownTools) != 1 || r.UnknownTools[0].ID != "uncertain" || len(r.NotStartedTools) != 1 {
		t.Fatalf("recovery=%+v", r)
	}
	if msgs[2].ReasoningSignature != "original-proof" || msgs[2].ToolCalls[0].Arguments == "" {
		t.Fatalf("canonical evidence lost: %+v", msgs[2])
	}
}
