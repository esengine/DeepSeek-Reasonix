package delegation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
)

func TestUpstreamPreviewPreservesRetrievalReference(t *testing.T) {
	ref := "sa_1234567890abcdef"
	note := renderUpstreamNote([]UpstreamResult{{ID: "research", Ref: ref, Answer: strings.Repeat("long answer ", 2000)}}, 128)
	if !strings.Contains(note, ref) || !strings.Contains(note, "read_task_context") || !strings.Contains(note, "truncated") {
		t.Fatalf("no recovery route for truncated context: %s", note)
	}
}

func TestSelectedContextAndCompleteUpstreamReachChildWithoutPrefixChanges(t *testing.T) {
	prov := &mockProvider{name: "sub", streams: [][]provider.Chunk{
		{{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "read-context", Name: "read_task_context", Arguments: `{"id":"research","offset_bytes":10000}`}}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0, "", "STABLE", nil, 0, "", "", nil).WithTranscripts(mustSubagentStore(t), testenv.TempDir(t), "m", "")
	spec, err := task.buildTaskSpec(context.Background(), "inspect", "", "", nil, nil, 0, "", "", "", "", false, false)
	if err != nil {
		t.Fatal(err)
	}
	spec.Context.Summary = "SELECTED DECISION: preserve zero"
	spec.Context.Upstream = []UpstreamResult{{ID: "research", Answer: strings.Repeat("x", 10000) + "MIDDLE EVIDENCE" + strings.Repeat("x", 10000)}}
	if _, err := task.RunProfileSpec(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if len(prov.requests) != 2 {
		t.Fatalf("requests=%d", len(prov.requests))
	}
	first, second := prov.requests[0], prov.requests[1]
	if !strings.Contains(lastUser(first), "SELECTED DECISION") || !strings.Contains(lastUser(first), "read_task_context") {
		t.Fatal("opening context missing")
	}
	if strings.Contains(lastUser(first), "MIDDLE EVIDENCE") {
		t.Fatal("preview was not bounded")
	}
	found := false
	for _, m := range second.Messages {
		if m.Role == provider.RoleTool && strings.Contains(m.Content, "MIDDLE EVIDENCE") {
			found = true
		}
	}
	if !found {
		t.Fatal("truncated evidence was not retrievable")
	}
	a, _ := json.Marshal(first.Tools)
	b, _ := json.Marshal(second.Tools)
	if string(a) != string(b) || strings.Contains(string(a), "MIDDLE EVIDENCE") {
		t.Fatal("context changed tool prefix")
	}
	if first.Messages[0].Content != second.Messages[0].Content || strings.Contains(first.Messages[0].Content, "SELECTED DECISION") {
		t.Fatal("context changed system prefix")
	}
}

func TestContextReferencesEnforceOwnerWorkspaceAndNestedScope(t *testing.T) {
	root := testenv.TempDir(t)
	store := mustSubagentStore(t)
	source := testSubagentSpec(t, "research")
	source.WorkspaceRoot = root
	run := startedRun(t, store, source, provider.Message{Role: provider.RoleAssistant, Content: "private result"})
	if err := store.SaveCompleted(run); err != nil {
		t.Fatal(err)
	}
	run.Release()
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, "", "sys", nil, 0, "", "", nil).WithTranscripts(store, root, "m", "")
	ctx := agent.WithParentSession(context.Background(), source.ParentSession)
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		root    string
		allowed bool
	}{
		{"owner", ctx, root, true},
		{"foreign parent", agent.WithParentSession(ctx, "someone-else"), root, false},
		{"foreign workspace", ctx, testenv.TempDir(t), false},
		{"nested without grant", agent.WithSubagentDepth(ctx, 1), root, false},
		{"nested with grant", agent.WithSubagentDepth(withTaskContext(ctx, ContextRequest{Upstream: []UpstreamResult{{Ref: run.Ref}}}), 1), root, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task.workspaceRoot = tc.root
			spec := ProfileExecSpec{Context: ContextRequest{ResultRefs: []string{run.Ref}}}
			err := task.resolveTaskContext(tc.ctx, &spec)
			if (err == nil) != tc.allowed {
				t.Fatalf("resolve=%v allowed=%v", err, tc.allowed)
			}
		})
	}
}

func TestContextReaderCannotReadUndeclaredResults(t *testing.T) {
	reg := tool.NewRegistry()
	attachTaskContext(reg, []UpstreamResult{{ID: "research", Ref: "sa_allowed", Answer: "begin " + strings.Repeat("中", 10000) + " END"}})
	reader, ok := reg.Get("read_task_context")
	if !ok {
		t.Fatal("missing scoped context reader")
	}
	if _, err := reader.Execute(context.Background(), json.RawMessage(`{"id":"sa_other"}`)); err == nil {
		t.Fatal("reader admitted a result outside its snapshot")
	}
	first, err := reader.Execute(context.Background(), json.RawMessage(`{"id":"research","offset_bytes":0,"limit_bytes":1024}`))
	if err != nil || !strings.Contains(first, "begin") || !strings.Contains(first, "next_offset_bytes") {
		t.Fatalf("first page: %q, %v", first, err)
	}
	last, err := reader.Execute(context.Background(), json.RawMessage(`{"id":"research","offset_bytes":30006,"limit_bytes":1024}`))
	if err != nil || !strings.Contains(last, "END") {
		t.Fatalf("last page: %q, %v", last, err)
	}
	if _, err := reader.Execute(context.Background(), json.RawMessage(`{"id":"research","offset_bytes":7}`)); err == nil {
		t.Fatal("reader accepted an offset inside a UTF-8 character")
	}
}

func TestContextReferencesSurviveACompatibleContinuation(t *testing.T) {
	root := testenv.TempDir(t)
	store := mustSubagentStore(t)
	source := testSubagentSpec(t, "research")
	source.WorkspaceRoot = root
	run := startedRun(t, store, source, provider.Message{Role: provider.RoleAssistant, Content: "retained background"})
	if err := store.SaveCompleted(run); err != nil {
		t.Fatal(err)
	}
	run.Release()
	prov := &mockProvider{name: "sub", chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}}}
	task := NewTaskTool(prov, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, "", "sys", nil, 0, "", "", nil).WithTranscripts(store, root, "m", "")
	ctx := agent.WithParentSession(context.Background(), source.ParentSession)
	raw, _ := json.Marshal(map[string]any{"prompt": "inspect", "context_refs": []string{run.Ref}})
	out, err := task.Execute(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	_, child := splitSubagentRunResult(out)
	meta, err := store.LoadMeta(child)
	if err != nil || len(meta.Capsule.Inherited.ResultRefs) != 1 || meta.Capsule.Inherited.ResultRefs[0].Ref != run.Ref {
		t.Fatalf("persisted context = %+v, %v", meta.Capsule.Inherited, err)
	}
	raw, _ = json.Marshal(map[string]any{"prompt": "continue inspecting", "continue_from": child})
	if _, err := task.Execute(ctx, raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lastUser(prov.requests[len(prov.requests)-1]), "retained background") {
		t.Fatal("continuation lost the supplied context")
	}
}

func TestContextPagesRemainLosslessAfterJSONEscaping(t *testing.T) {
	answer := strings.Repeat("<\x00中", 9000)
	id := strings.Repeat("<", 10000)
	reader := &taskContextTool{results: []UpstreamResult{{ID: id, Answer: answer}}}
	var joined strings.Builder
	offset := 0
	for {
		raw, _ := json.Marshal(map[string]any{"id": id, "offset_bytes": offset, "limit_bytes": subagentResultMaxBytes})
		out, err := reader.Execute(context.Background(), raw)
		if err != nil || len(out) > agent.MaxToolOutputBytes {
			t.Fatalf("page bytes=%d err=%v", len(out), err)
		}
		var page struct {
			Text string `json:"text"`
			Next int    `json:"next_offset_bytes"`
			More bool   `json:"has_more"`
		}
		if err := json.Unmarshal([]byte(out), &page); err != nil {
			t.Fatal(err)
		}
		joined.WriteString(page.Text)
		if !page.More {
			break
		}
		if page.Next <= offset {
			t.Fatal("page failed to advance")
		}
		offset = page.Next
	}
	if joined.String() != answer {
		t.Fatal("escaped context was lost between pages")
	}
}

func TestContextPreviewUsesBoundedKeysAndPreservesEmptyPositions(t *testing.T) {
	results := []UpstreamResult{{ID: "blank"}, {ID: strings.Repeat("<", 10000), Answer: "kept evidence"}}
	note := renderUpstreamNote(results, upstreamBudgetBytes)
	if len(note) > 1024 || !strings.Contains(note, `read_task_context(id="2")`) {
		t.Fatalf("unbounded or wrong lookup key: %.300s", note)
	}
	reader := &taskContextTool{results: results}
	out, err := reader.Execute(context.Background(), json.RawMessage(`{"id":"2"}`))
	if err != nil || !strings.Contains(out, "kept evidence") {
		t.Fatalf("lookup=%q err=%v", out, err)
	}
}
