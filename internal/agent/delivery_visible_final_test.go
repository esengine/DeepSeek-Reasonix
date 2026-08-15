package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestRunSubAgentSalvagesCurrentAnswerBeforePairedGoalError(t *testing.T) {
	// Providers can co-stream a real final answer with an accidental Goal-only
	// update_goal call. Delivery salvage must recover that current answer after
	// the host pairs the invalid call with its tool error.
	goalTool, ok := tool.LookupBuiltin("update_goal")
	if !ok {
		t.Fatal("update_goal builtin not registered")
	}
	reg := evidenceRegistry()
	reg.Add(goalTool)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Add explanations","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
		{toolCallChunk("write", "write_file", `{"path":"qa/bank.md"}`), {Type: provider.ChunkDone}},
		{
			{Type: provider.ChunkText, Text: "done, explanations added"},
			toolCallChunk("goal", "update_goal", `{"status":"complete"}`),
			{Type: provider.ChunkDone},
		},
	}}
	sess := NewSession("sys")
	answer, err := RunSubAgentWithSession(context.Background(), prov, reg, sess,
		"add explanations to the question bank", Options{SubagentDepth: 1}, event.Discard)
	if err != nil {
		t.Fatalf("paired Goal error hid the current salvage answer: %v", err)
	}
	for _, want := range []string{"[unverified]", "done, explanations added", "already on disk"} {
		if !strings.Contains(answer, want) {
			t.Fatalf("salvaged answer %q missing %q", answer, want)
		}
	}
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want no hidden repair round", prov.call)
	}
	if got := lastToolResult(sess, "update_goal"); !strings.Contains(got, "only available while an active goal turn") {
		t.Fatalf("paired update_goal result = %q", got)
	}
}

func TestCurrentFinalAssistantAnswerRejectsNonCurrentToolTails(t *testing.T) {
	call := func(id, name string) provider.ToolCall {
		return provider.ToolCall{ID: id, Name: name, Arguments: `{}`}
	}
	assistant := func(content string, calls ...provider.ToolCall) provider.Message {
		return provider.Message{Role: provider.RoleAssistant, Content: content, ToolCalls: calls}
	}
	result := func(id, name string) provider.Message {
		return provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: name, Content: "result"}
	}
	tests := []struct {
		name string
		msgs []provider.Message
		want string
	}{
		{
			name: "ordinary current final",
			msgs: []provider.Message{{Role: provider.RoleUser, Content: "work"}, assistant("current answer")},
			want: "current answer",
		},
		{
			name: "paired update_goal error",
			msgs: []provider.Message{
				{Role: provider.RoleUser, Content: "work"},
				assistant("current answer", call("goal", "update_goal")),
				result("goal", "update_goal"),
			},
			want: "current answer",
		},
		{
			name: "ordinary paired tool rejected",
			msgs: []provider.Message{assistant("current answer", call("write", "write_file")), result("write", "write_file")},
		},
		{
			name: "mixed tool batch rejected",
			msgs: []provider.Message{
				assistant("current answer", call("goal", "update_goal"), call("write", "write_file")),
				result("goal", "update_goal"), result("write", "write_file"),
			},
		},
		{
			name: "duplicate call identity rejected",
			msgs: []provider.Message{
				assistant("current answer", call("goal", "update_goal"), call("goal", "update_goal")),
				result("goal", "update_goal"), result("goal", "update_goal"),
			},
		},
		{
			name: "empty call identity rejected",
			msgs: []provider.Message{assistant("current answer", call("", "update_goal")), result("", "update_goal")},
		},
		{
			name: "local only result rejected",
			msgs: []provider.Message{
				assistant("current answer", call("goal", "update_goal")),
				{Role: provider.RoleTool, ToolCallID: "goal", Name: "update_goal", Content: "result", LocalOnly: true},
			},
		},
		{
			name: "never crosses user boundary",
			msgs: []provider.Message{
				assistant("old answer", call("old", "update_goal")), result("old", "update_goal"),
				{Role: provider.RoleUser, Content: "new task"}, result("orphan", "update_goal"),
			},
		},
		{
			name: "tool-only current turn does not reuse stale preamble",
			msgs: []provider.Message{
				assistant("old preamble", call("old", "write_file")), result("old", "write_file"),
				{Role: provider.RoleUser, Content: "continue"},
				assistant("", call("goal", "update_goal")), result("goal", "update_goal"),
			},
		},
		{
			name: "incomplete batch",
			msgs: []provider.Message{assistant("current answer", call("a", "update_goal"), call("b", "update_goal")), result("a", "update_goal")},
		},
		{
			name: "mismatched result identity",
			msgs: []provider.Message{assistant("current answer", call("a", "update_goal")), result("other", "update_goal")},
		},
		{
			name: "mismatched result name",
			msgs: []provider.Message{assistant("current answer", call("a", "update_goal")), result("a", "other")},
		},
		{
			name: "extra orphan result",
			msgs: []provider.Message{assistant("current answer", call("a", "update_goal")), result("a", "update_goal"), result("b", "update_goal")},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sess := NewSession("")
			for _, msg := range tc.msgs {
				sess.Add(msg)
			}
			if got := currentFinalAssistantAnswer(sess); got != tc.want {
				t.Fatalf("currentFinalAssistantAnswer() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunSubAgentReadinessSalvageDoesNotReuseStaleToolText(t *testing.T) {
	// A reasoning-only final still fails the delivery sign-off gate after a real
	// write. Salvage must not turn an earlier tool preamble into an unverified
	// success result.
	reg := evidenceRegistry()
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkReasoning, Text: "I should define the acceptance criteria."},
			toolCallChunk("criteria", "todo_write", `{"todos":[{"content":"Add explanations","status":"in_progress"}]}`),
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkReasoning, Text: "I should make the requested edit."},
			{Type: provider.ChunkText, Text: "I'll edit first."},
			toolCallChunk("write", "write_file", `{"path":"qa/bank.md"}`),
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkReasoning, Text: "The change is complete."},
			{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop"}},
			{Type: provider.ChunkDone},
		},
	}}
	sess := NewSession("sys")
	answer, err := RunSubAgentWithSession(
		context.Background(), deepseekThinkingProvider{prov}, reg, sess,
		"add explanations to the question bank",
		Options{SubagentDepth: 1}, event.Discard,
	)
	var readinessErr *FinalReadinessError
	if !errors.As(err, &readinessErr) {
		t.Fatalf("RunSubAgentWithSession error = %v, want FinalReadinessError", err)
	}
	if answer != "" {
		t.Fatalf("reasoning-only readiness failure salvaged stale text: %q", answer)
	}
	if prov.call != 3 {
		t.Fatalf("provider calls = %d, want 3 (readiness remains host-owned)", prov.call)
	}
	if sessionHasUserMessageContaining(sess, "visible answer") {
		t.Fatal("readiness failure must not start a hidden visible-answer retry")
	}
}
