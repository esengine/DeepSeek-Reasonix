package delegation

import (
	"context"
	"path/filepath"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/writeclaim"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// Converging read_only_task onto the unified runner must not quietly give it
// durable side effects: its contract is that the call leaves nothing behind.
func TestReadOnlyTaskStaysEphemeralOnTheUnifiedRunner(t *testing.T) {
	root := testenv.TempDir(t)
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "research done"}, {Type: provider.ChunkDone}},
	}}
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(writeclaim.NewSubagentScheduler(4, 4))

	ctx := agent.WithCallContext(context.Background(), "call-1", event.Discard, nil, false)
	ctx = agent.WithParentSession(ctx, filepath.Join(root, "parent.jsonl"))
	out, err := NewReadOnlyTaskTool(task).Execute(ctx, []byte(`{"prompt":"inspect the parser"}`))
	if err != nil {
		t.Fatalf("read_only_task: %v", err)
	}
	if !strings.Contains(out, "research done") {
		t.Fatalf("answer = %q", out)
	}
	if strings.Contains(out, "Subagent reference") {
		t.Fatalf("read_only_task must not persist a transcript even under a parent session:\n%s", out)
	}
}
