package delegation

import (
	"context"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// End to end: the parent's view leads with the adjudicated status, not prose.
func TestSubAgentAnswerLeadsWithAdjudicatedStatus(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeWriteFileTool{})
	agent.AttachCompleteSubtaskTool(reg)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("1", "write_file", `{"path":"parser.go"}`), {Type: provider.ChunkDone}},
		{toolCallChunk("2", "complete_subtask", `{"status":"complete","summary":"fixed the parser","acceptance_criteria":[{"id":"AC1","status":"satisfied","evidence":[{"kind":"diff","summary":"the fix","paths":["parser.go"]}]},{"id":"AC2","status":"satisfied","evidence":[{"kind":"verification","summary":"suite","command":"go test ./..."}]}],"unresolved":["integration suite not executed"]}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "all good"}, {Type: provider.ChunkDone}},
	}}

	answer, err := agent.RunSubAgentWithSession(context.Background(), prov, reg, sessionstore.NewSession("sys"),
		"fix the parser", agent.Options{}, event.Discard)
	if err != nil {
		t.Fatalf("RunSubAgentWithSession: %v", err)
	}
	if !strings.HasPrefix(answer, "status: partial") {
		t.Fatalf("answer must lead with the host-adjudicated status:\n%s", answer)
	}
	for _, want := range []string{
		"AC1 satisfied",
		"AC2 unsatisfied",
		"host lowered AC2",
		"unresolved: integration suite not executed",
		agent.HostReceiptsHeader,
	} {
		if !strings.Contains(answer, want) {
			t.Fatalf("answer missing %q:\n%s", want, answer)
		}
	}
}
