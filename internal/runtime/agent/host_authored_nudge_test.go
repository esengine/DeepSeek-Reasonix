package agent

import (
	"context"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

// A reopened transcript draws every user-role message not marked HostAuthored
// as something the person typed, so each instruction the loop composes for the
// model must carry the mark, not only the one the user typed stays unmarked.
func TestLoopComposedUserMessagesAreHostAuthored(t *testing.T) {
	cases := []struct {
		name  string
		input string
		agent func() *Agent
	}{
		{
			name:  "task budget finalization",
			input: "read everything",
			agent: func() *Agent {
				reg := tool.NewRegistry()
				reg.Add(readProbe{})
				return New(&spendingProvider{max: 500}, reg, sessionstore.NewSession("sys"), Options{
					Pricing:    &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2},
					TaskBudget: TaskBudget{Cost: 1e-3},
				}, event.Discard)
			},
		},
		{
			name:  "empty final retry",
			input: "answer me",
			agent: func() *Agent {
				prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
					{{Type: provider.ChunkReasoning, Text: "I should answer."}, {Type: provider.ChunkDone}},
					{{Type: provider.ChunkText, Text: "visible reply"}, {Type: provider.ChunkDone}},
				}}
				return New(prov, tool.NewRegistry(), sessionstore.NewSession(""), Options{}, event.Discard)
			},
		},
		{
			name:  "truncation fact",
			input: "edit then verify",
			agent: func() *Agent {
				reg := tool.NewRegistry()
				reg.Add(schemaTool{name: "bash", runs: new(atomic.Int32)})
				prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
					{toolCallChunk("c1", "bash", `{"command":"go test ./`), terminalChunk("length"), {Type: provider.ChunkDone}},
					{{Type: provider.ChunkText, Text: "done"}, terminalChunk("stop"), {Type: provider.ChunkDone}},
				}}
				return New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := tc.agent()
			_ = a.Run(context.Background(), tc.input)
			var typed, composed int
			for _, m := range a.sess.conversation.Messages {
				if m.Role != provider.RoleUser {
					continue
				}
				if m.HostAuthored {
					composed++
					continue
				}
				typed++
				if got := sessionstore.UserMessageText(m); got != tc.input {
					t.Fatalf("a line the loop composed is recorded as the user's: %q", got)
				}
			}
			if typed != 1 {
				t.Fatalf("unmarked user lines = %d, want only the one typed", typed)
			}
			if composed == 0 {
				t.Fatal("the loop composed nothing; this case no longer reaches its injection site")
			}
		})
	}
}
