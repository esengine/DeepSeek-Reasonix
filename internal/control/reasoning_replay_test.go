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
