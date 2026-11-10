package agent

import (
	"context"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

func refusalOf(t *testing.T, a *Agent, id string) string {
	t.Helper()
	for _, msg := range a.sess.conversation.Snapshot() {
		if msg.ToolCallID == id && msg.Role == provider.RoleTool {
			if msg.ToolFailure == nil {
				t.Fatalf("call %s carries no failure", id)
			}
			return msg.ToolFailure.RefusalCode
		}
	}
	t.Fatalf("no result for call %s", id)
	return ""
}

func runBash(t *testing.T, command string) *Agent {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("m1", "bash", `{"command":`+quoteJSON(command)+`}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "ok"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestTheBarrierLevelsNameTheirIdentity(t *testing.T) {
	if got := barrierFull.refusalCode(); got != CodeBatchSkippedAfterFailedChange {
		t.Fatalf("full barrier = %q, want %q", got, CodeBatchSkippedAfterFailedChange)
	}
	if got := barrierVerificationOnly.refusalCode(); got != CodeBatchCheckAfterStoppedCall {
		t.Fatalf("verification-only barrier = %q, want %q", got, CodeBatchCheckAfterStoppedCall)
	}
}
