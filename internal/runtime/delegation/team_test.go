package delegation

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reasonix/internal/contract/agentgraph"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/execgraph"
	"reasonix/internal/state/execjournal"
	"strings"
	"testing"
)

func TestTeamMemberReusesTranscriptAfterStoreReopenAndRetires(t *testing.T) {
	prov := &mockProvider{name: "sub", chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}}}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	store := NewSubagentStore(filepath.Join(t.TempDir(), "subagents"))
	task := newTestTaskTool(t, prov, reg, "sys", "", "", nil).WithTranscripts(store, t.TempDir(), "m", "")
	ctx := agent.WithCallContext(agent.WithParentSession(context.Background(), "parent"), "create", nil, nil, false)
	team := NewTeamTool(task)
	if _, err := team.Execute(ctx, json.RawMessage(`{"action":"create","name":"parser","prompt":"inspect parser","tools":["read_file"]}`)); err != nil {
		t.Fatal(err)
	}
	runs, err := store.Runs("parent", task.workspaceRoot)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs %v: %v", runs, err)
	}
	ref := runs[0].Ref
	if _, err := team.Execute(ctx, json.RawMessage(`{"action":"create","name":"parser","prompt":"duplicate"}`)); err == nil {
		t.Fatal("duplicate member created")
	}
	foreign := agent.WithParentSession(ctx, "another-parent")
	if _, err := team.Execute(foreign, json.RawMessage(`{"action":"send","name":"parser","prompt":"foreign"}`)); err == nil {
		t.Fatal("another conversation reached the member")
	}
	if _, err := team.Execute(ctx, json.RawMessage(`{"action":"send","name":"parser","prompt":"expand","tools":["write_file"]}`)); err == nil {
		t.Fatal("member tool ceiling widened")
	}
	task.transcripts = NewSubagentStore(store.dir)
	ctx = agent.WithCallContext(ctx, "followup", nil, nil, false)
	if _, err := team.Execute(ctx, json.RawMessage(`{"action":"send","name":"parser","prompt":"check edge cases"}`)); err != nil {
		t.Fatal(err)
	}
	runs, err = task.transcripts.Runs("parent", task.workspaceRoot)
	if err != nil || len(runs) != 1 || runs[0].Ref != ref {
		t.Fatalf("member lost transcript %v: %v", runs, err)
	}
	history := execjournal.History(task.executionSessionPath(ctx))
	graph := execgraph.Rebuild(history, nil, nil).Graph
	for _, id := range []string{"create/sub-1", "followup/sub-1"} {
		node, ok := graph.Node(id)
		if !ok || node.State != agentgraph.StateCompleted || node.Ref != ref {
			t.Fatalf("continuation erased execution %s: %+v", id, node)
		}
	}
	if _, err := team.Execute(ctx, json.RawMessage(`{"action":"retire","name":"parser"}`)); err != nil {
		t.Fatal(err)
	}
	if out, err := team.Execute(ctx, json.RawMessage(`{"action":"list"}`)); err != nil || out != "[]" {
		t.Fatalf("retired member listed: %s %v", out, err)
	}
	if _, err := team.Execute(ctx, json.RawMessage(`{"action":"send","name":"parser","prompt":"again"}`)); err == nil {
		t.Fatal("retired member resumed")
	}
}

func TestTeamRefusesNestedManagementAndUnknownAuthority(t *testing.T) {
	team := NewTeamTool(&TaskTool{transcripts: mustSubagentStore(t)})
	ctx := agent.WithSubagentDepth(agent.WithParentSession(context.Background(), "p"), 1)
	if _, err := team.Execute(ctx, json.RawMessage(`{"action":"list"}`)); err == nil {
		t.Fatal("child browsed team")
	}
	if _, err := team.Execute(context.Background(), json.RawMessage(`{"action":"list","authorization":"all"}`)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("authority accepted: %v", err)
	}
}
