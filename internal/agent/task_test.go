package agent

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func testTaskContext() context.Context {
	return WithParentSession(context.Background(), "parent-session")
}

// TestTaskToolReturnsSubAgentFinalAnswer runs a task against a mock provider
// that emits a single text turn, and verifies the tool returns that text with a
// transcript reference — sub-agent intermediate state isn't supposed to leak.
func TestTaskToolReturnsSubAgentFinalAnswer(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "found 3 callers of Foo"},
		{Type: provider.ChunkDone},
	}}
	parentReg := tool.NewRegistry()
	task := newTestTaskTool(t, sub, parentReg, "test-sys-prompt", "", "", nil)

	out, err := task.Execute(testTaskContext(), []byte(`{"prompt":"find callers of Foo"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_ = subagentRefFromOutput(t, out)
	if !strings.Contains(out, "found 3 callers of Foo") {
		t.Errorf("got %q, want sub-agent final answer", out)
	}
	if !strings.Contains(out, "To continue this same subagent transcript in a later call, pass this ref as `continue_from`. Start a fresh subagent when the next task is independent.") {
		t.Errorf("got %q, want continuation guidance", out)
	}

	// The sub-agent must have received the prompt as its user message and
	// the configured system prompt at the top — proving the session was
	// fresh, not the parent's.
	if sys := sub.lastReq.Messages[0]; sys.Role != provider.RoleSystem || sys.Content != "test-sys-prompt" {
		t.Errorf("first message = %+v, want system 'test-sys-prompt'", sys)
	}
	if got := lastUser(sub.lastReq); !strings.Contains(got, `<subagent-context event="SubagentStart">`) || !strings.HasSuffix(got, "find callers of Foo") {
		t.Errorf("sub-agent user = %q, want SubagentStart context plus prompt", got)
	}
}

func TestTaskToolCancelDuringStuckProviderReturnsPromptly(t *testing.T) {
	task := newTestTaskTool(t, stuckStreamProvider{}, tool.NewRegistry(), "sys", "", "", nil)

	ctx, cancel := context.WithCancel(testTaskContext())
	done := make(chan error, 1)
	go func() {
		_, err := task.Execute(ctx, []byte(`{"prompt":"wait on stuck provider"}`))
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Execute returned nil after context cancellation")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Execute error = %v, want context cancellation", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("TaskTool.Execute did not return promptly after cancellation")
	}
}

func TestTaskToolSchemaExposesOnlyContinueFromForPersistence(t *testing.T) {
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil)
	schema := string(task.Schema())
	if !strings.Contains(schema, `"continue_from"`) {
		t.Fatalf("task schema = %s, want continue_from", schema)
	}
	if strings.Contains(schema, "fork_from") {
		t.Fatalf("task schema = %s, want no fork_from", schema)
	}
}

func TestParallelTasksSchemaDoesNotExposePersistentContinuation(t *testing.T) {
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil)
	parallel := NewParallelTasksTool(task, tool.NewRegistry())
	schema := string(parallel.Schema())
	if strings.Contains(schema, "continue_from") || strings.Contains(schema, "fork_from") {
		t.Fatalf("parallel_tasks schema = %s, want no persistent continuation fields", schema)
	}
}

func TestTaskToolInheritsReasoningLanguageFromContext(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "done"},
		{Type: provider.ChunkDone},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil)

	ctx := WithReasoningLanguagePreference(testTaskContext(), "zh")
	if _, err := task.Execute(ctx, []byte(`{"prompt":"inspect auth"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := lastUser(sub.lastReq)
	if !strings.HasPrefix(got, "<reasoning-language>") || !strings.Contains(got, "Simplified Chinese") || !strings.HasSuffix(got, "inspect auth") {
		t.Fatalf("sub-agent user = %q, want reasoning-language-prefixed prompt", got)
	}
}

// TestTaskToolFiltersTools verifies the whitelist behaviour: when the caller
// names a subset of tools, the sub-agent's registry contains exactly that set
// with recursive delegation tools available while max_subagent_depth leaves one
// more layer.
func TestTaskToolFiltersTools(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkDone},
	}}
	parentReg := tool.NewRegistry()
	parentReg.Add(fakeTool{name: "read_file", readOnly: true})
	parentReg.Add(fakeTool{name: "write_file", readOnly: false})
	parentReg.Add(fakeTool{name: "bash", readOnly: false})
	task := newTestTaskTool(t, sub, parentReg, "sys", "", "", nil)
	parentReg.Add(task) // simulate the wiring in cli.setup
	parentReg.Add(fakeTool{name: "run_skill", readOnly: false})
	parentReg.Add(fakeTool{name: "read_only_skill", readOnly: true})
	parentReg.Add(fakeTool{name: "research", readOnly: false})

	args := []byte(`{"prompt":"x","tools":["read_file","task","write_file","run_skill","read_only_skill","research"]}`)
	if _, err := task.Execute(testTaskContext(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// The sub-agent's tool schemas should reflect the whitelist minus always
	// unavailable background/install tools. Recursive tools stay visible at depth 1.
	got := map[string]bool{}
	for _, s := range sub.lastReq.Tools {
		got[s.Name] = true
	}
	for _, want := range []string{"read_file", "write_file", "task", "run_skill", "read_only_skill", "research"} {
		if !got[want] {
			t.Errorf("sub-agent tools = %v, want %q exposed at depth 1", got, want)
		}
	}
	if got["bash"] {
		t.Errorf("sub-agent tools = %v, want bash omitted when not requested", got)
	}
}

// TestTaskToolDefaultsToParentToolsWithDepthRemaining covers the no-whitelist
// path: the first-layer sub-agent inherits parent tools except always-hidden
// background/install tools because it still has one delegation layer available.
func TestTaskToolDefaultsToParentToolsWithoutMetaTools(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkDone},
	}}
	parentReg := tool.NewRegistry()
	parentReg.Add(fakeTool{name: "read_file", readOnly: true})
	parentReg.Add(fakeTool{name: "grep", readOnly: true})
	task := newTestTaskTool(t, sub, parentReg, "sys", "", "", nil)
	parentReg.Add(task)
	parentReg.Add(fakeTool{name: "run_skill", readOnly: false})
	parentReg.Add(fakeTool{name: "read_only_skill", readOnly: true})
	parentReg.Add(fakeTool{name: "explore", readOnly: false})
	parentReg.Add(fakeTool{name: "research", readOnly: false})
	parentReg.Add(fakeTool{name: "review", readOnly: false})
	parentReg.Add(fakeTool{name: "security_review", readOnly: false})
	parentReg.Add(fakeTool{name: "remember", readOnly: false})

	if _, err := task.Execute(testTaskContext(), []byte(`{"prompt":"x"}`)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := map[string]bool{}
	for _, s := range sub.lastReq.Tools {
		got[s.Name] = true
	}
	for _, want := range []string{"read_file", "grep", "remember", "task", "run_skill", "read_only_skill", "explore", "research", "review", "security_review"} {
		if !got[want] {
			t.Errorf("default sub-agent tools = %v, want %q inherited at depth 1", got, want)
		}
	}
}

func TestTaskToolAllowsSecondLayerAndStopsThere(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "depth two answer"},
		{Type: provider.ChunkDone},
	}}
	parentReg := tool.NewRegistry()
	parentReg.Add(fakeTool{name: "read_file", readOnly: true})
	task := newTestTaskTool(t, sub, parentReg, "sys", "", "", nil).WithMaxSubagentDepth(2)
	parentReg.Add(task)
	parentReg.Add(fakeTool{name: "run_skill", readOnly: false})

	depthOneCtx := WithSubagentDepth(testTaskContext(), 1)
	if _, err := task.Execute(depthOneCtx, []byte(`{"prompt":"spawn second layer"}`)); err != nil {
		t.Fatalf("depth-1 task should be able to spawn depth 2: %v", err)
	}
	got := map[string]bool{}
	for _, s := range sub.lastReq.Tools {
		got[s.Name] = true
	}
	if got["task"] || got["run_skill"] {
		t.Fatalf("depth-2 child should not receive recursive tools; tools=%v", toolSchemaNames(sub.lastReq.Tools))
	}

	depthTwoCtx := WithSubagentDepth(testTaskContext(), 2)
	if _, err := task.Execute(depthTwoCtx, []byte(`{"prompt":"spawn third layer"}`)); err == nil || !strings.Contains(err.Error(), "subagent delegation depth limit reached") {
		t.Fatalf("depth-2 task error = %v, want depth limit", err)
	}
}

func TestTaskToolMaxSubagentDepthOneRestoresSingleLayerBoundary(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "single layer answer"},
		{Type: provider.ChunkDone},
	}}
	parentReg := tool.NewRegistry()
	parentReg.Add(fakeTool{name: "read_file", readOnly: true})
	task := newTestTaskTool(t, sub, parentReg, "sys", "", "", nil).WithMaxSubagentDepth(1)
	parentReg.Add(task)
	parentReg.Add(fakeTool{name: "run_skill", readOnly: false})
	parentReg.Add(fakeTool{name: "read_only_task", readOnly: true})

	if _, err := task.Execute(testTaskContext(), []byte(`{"prompt":"single layer"}`)); err != nil {
		t.Fatalf("root task should still spawn first-layer subagent: %v", err)
	}
	got := map[string]bool{}
	for _, s := range sub.lastReq.Tools {
		got[s.Name] = true
	}
	for _, hidden := range []string{"task", "run_skill", "read_only_task"} {
		if got[hidden] {
			t.Fatalf("max_subagent_depth=1 should hide recursive tool %q; tools=%v", hidden, toolSchemaNames(sub.lastReq.Tools))
		}
	}

	depthOneCtx := WithSubagentDepth(testTaskContext(), 1)
	if _, err := task.Execute(depthOneCtx, []byte(`{"prompt":"too deep"}`)); err == nil || !strings.Contains(err.Error(), "max_subagent_depth=1") {
		t.Fatalf("depth-1 task error = %v, want max depth rejection", err)
	}
}

func TestTaskToolUsesConfiguredProfileForExecution(t *testing.T) {
	parent := &mockProvider{name: "parent", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "parent answer"},
		{Type: provider.ChunkDone},
	}}
	resolved := &mockProvider{name: "resolved", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "resolved answer"},
		{Type: provider.ChunkDone},
	}}
	var gotModel, gotEffort string
	resolve := func(model, effort string) (provider.Provider, *provider.Pricing, int, error) {
		gotModel, gotEffort = model, effort
		return resolved, nil, 0, nil
	}
	task := newTestTaskTool(t, parent, tool.NewRegistry(), "sys", "deepseek-pro", "max", resolve)

	out, err := task.Execute(testTaskContext(), []byte(`{"prompt":"x"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "resolved answer") {
		t.Fatalf("sub-agent did not use resolved provider, got %q", out)
	}
	if gotModel != "deepseek-pro" || gotEffort != "max" {
		t.Fatalf("resolved profile = %q/%q, want deepseek-pro/max", gotModel, gotEffort)
	}
}

func TestTaskToolReturnsProfileResolutionErrors(t *testing.T) {
	parent := &mockProvider{name: "parent", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "parent answer"},
		{Type: provider.ChunkDone},
	}}
	resolve := func(string, string) (provider.Provider, *provider.Pricing, int, error) {
		return nil, nil, 0, errors.New("bad effort")
	}
	task := newTestTaskTool(t, parent, tool.NewRegistry(), "sys", "", "", resolve)

	_, err := task.Execute(testTaskContext(), []byte(`{"prompt":"x","effort":"turbo"}`))
	if err == nil || !strings.Contains(err.Error(), "bad effort") {
		t.Fatalf("Execute error = %v, want profile resolution error", err)
	}
}

func TestTaskToolRequiresTranscriptStore(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "answer"},
		{Type: provider.ChunkDone},
	}}
	task := NewTaskTool(sub, nil, tool.NewRegistry(), 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil)

	_, err := task.Execute(testTaskContext(), []byte(`{"prompt":"x"}`))
	if err == nil || !strings.Contains(err.Error(), "transcript store is required") {
		t.Fatalf("Execute error = %v, want transcript store requirement", err)
	}
}

// TestTaskToolRunsEphemerallyWithoutParentSession mirrors headless `reasonix run`:
// the store is wired but the context carries no parent session, so the sub-agent
// must run without persistence and return its plain answer (no transcript ref).
func TestTaskToolRunsEphemerallyWithoutParentSession(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "headless answer"},
		{Type: provider.ChunkDone},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil)

	out, err := task.Execute(context.Background(), []byte(`{"prompt":"x"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "headless answer") {
		t.Fatalf("got %q, want sub-agent final answer", out)
	}
	if strings.Contains(out, "Subagent reference") {
		t.Fatalf("ephemeral run should not emit a transcript reference: %q", out)
	}
}

func TestReadOnlyTaskToolRunsEphemerallyWithReadOnlyRegistry(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "read-only findings"},
		{Type: provider.ChunkDone},
	}}
	parentReg := tool.NewRegistry()
	parentReg.Add(fakeTool{name: "read_file", readOnly: true})
	parentReg.Add(fakeTool{name: "write_file", readOnly: false})
	parentReg.Add(fakeTool{name: "todo_write", readOnly: true})
	parentReg.Add(fakeTool{name: "complete_step", readOnly: true})
	parentReg.Add(fakeTool{name: "connect_tool_source", readOnly: true})
	parentReg.Add(fakeTool{name: "read_only_skill", readOnly: true})
	parentReg.Add(fakeTool{name: "bash", readOnly: false})
	task := newTestTaskTool(t, sub, parentReg, "writer sys", "", "", nil)
	readonly := NewReadOnlyTaskTool(task)
	parentReg.Add(task)
	parentReg.Add(readonly)

	out, err := readonly.Execute(testTaskContext(), []byte(`{"prompt":"inspect callers"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "read-only findings") {
		t.Fatalf("output = %q, want final answer", out)
	}
	if strings.Contains(out, "Subagent reference") {
		t.Fatalf("read_only_task should not persist transcript refs: %q", out)
	}
	if sys := sub.lastReq.Messages[0]; sys.Role != provider.RoleSystem || sys.Content != DefaultReadOnlyTaskSystemPrompt {
		t.Fatalf("read_only_task system prompt = %+v, want read-only prompt", sys)
	}

	got := map[string]bool{}
	for _, s := range sub.lastReq.Tools {
		got[s.Name] = true
	}
	for _, want := range []string{"read_file", "bash"} {
		if !got[want] {
			t.Fatalf("read_only_task sub-agent missing %q; tools=%v", want, toolSchemaNames(sub.lastReq.Tools))
		}
	}
	for _, hidden := range []string{"write_file", "todo_write", "complete_step", "connect_tool_source", "task"} {
		if got[hidden] {
			t.Fatalf("read_only_task sub-agent should hide %q; tools=%v", hidden, toolSchemaNames(sub.lastReq.Tools))
		}
	}
	for _, want := range []string{"read_only_task", "read_only_skill"} {
		if !got[want] {
			t.Fatalf("read_only_task depth-1 sub-agent should expose %q; tools=%v", want, toolSchemaNames(sub.lastReq.Tools))
		}
	}
}

func TestTaskToolRejectsContinuationWithoutParentSession(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "answer"},
		{Type: provider.ChunkDone},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil)

	_, err := task.Execute(context.Background(), []byte(`{"prompt":"x","continue_from":"sa_whatever"}`))
	if err == nil || !strings.Contains(err.Error(), "persisted session") {
		t.Fatalf("Execute error = %v, want persisted-session requirement", err)
	}
}

func TestTaskToolPersistsAndContinuesTranscript(t *testing.T) {
	sub := &mockProvider{name: "sub", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "first answer"},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "second answer"},
			{Type: provider.ChunkDone},
		},
	}}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	store := NewSubagentStore(t.TempDir())
	task := newTestTaskTool(t, sub, reg, "sys", "", "", nil).
		WithTranscripts(store, t.TempDir(), "base-model", "base-effort")

	first, err := task.Execute(testTaskContext(), []byte(`{"prompt":"first task"}`))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	ref := subagentRefFromOutput(t, first)
	meta, err := store.LoadMeta(ref)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.ParentSession != "parent-session" {
		t.Fatalf("parent session = %q, want parent-session", meta.ParentSession)
	}
	if !strings.Contains(first, "first answer") {
		t.Fatalf("first output = %q, want answer", first)
	}

	second, err := task.Execute(testTaskContext(), []byte(`{"prompt":"second task","continue_from":"`+ref+`"}`))
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if !strings.Contains(second, "second answer") {
		t.Fatalf("second output = %q, want answer", second)
	}
	if len(sub.requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(sub.requests))
	}
	msgs := sub.requests[1].Messages
	if len(msgs) < 4 {
		t.Fatalf("continued request messages = %+v, want prior transcript plus new task", msgs)
	}
	if !strings.HasSuffix(msgs[1].Content, "first task") || msgs[2].Content != "first answer" || lastUser(sub.requests[1]) != "second task" {
		t.Fatalf("continued request messages = %+v, want first task/answer then second task", msgs)
	}
}

func TestTaskToolContinueFromAncestorReturnsCopiedReferenceGuidance(t *testing.T) {
	sub := &mockProvider{name: "sub", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "root answer"},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "child answer"},
			{Type: provider.ChunkDone},
		},
	}}
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	task := NewTaskTool(sub, nil, reg, 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(store, t.TempDir(), "base-model", "base-effort")

	rootCtx := WithParentSession(context.Background(), "root")
	first, err := task.Execute(rootCtx, []byte(`{"prompt":"root task"}`))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	rootRef := subagentRefFromOutput(t, first)

	if err := SaveBranchMeta(filepath.Join(sessionDir, "root.jsonl"), BranchMeta{}); err != nil {
		t.Fatalf("SaveBranchMeta root: %v", err)
	}
	if err := SaveBranchMeta(filepath.Join(sessionDir, "child.jsonl"), BranchMeta{ParentID: "root"}); err != nil {
		t.Fatalf("SaveBranchMeta child: %v", err)
	}

	childCtx := WithParentSession(context.Background(), "child")
	second, err := task.Execute(childCtx, []byte(`{"prompt":"child task","continue_from":"`+rootRef+`"}`))
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	childRef := subagentRefFromOutput(t, second)
	if childRef == rootRef {
		t.Fatalf("child ref = source ref %q, want copied ref", childRef)
	}
	if !strings.Contains(second, "Forked from: "+rootRef) {
		t.Fatalf("second output = %q, want Forked from source ref", second)
	}
	if !strings.Contains(second, "The requested ref resolves to an ancestor conversation transcript") {
		t.Fatalf("second output = %q, want ancestor-copy guidance", second)
	}
	if !strings.Contains(second, "Final answer:\nchild answer") {
		t.Fatalf("second output = %q, want final answer", second)
	}
}

func TestTaskToolLegacyForkFromAncestorConvertsToCopiedReference(t *testing.T) {
	sub := &mockProvider{name: "sub", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "root answer"},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "child answer"},
			{Type: provider.ChunkDone},
		},
	}}
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	task := NewTaskTool(sub, nil, reg, 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(store, t.TempDir(), "base-model", "base-effort")

	rootCtx := WithParentSession(context.Background(), "root")
	first, err := task.Execute(rootCtx, []byte(`{"prompt":"root task"}`))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	rootRef := subagentRefFromOutput(t, first)

	if err := SaveBranchMeta(filepath.Join(sessionDir, "root.jsonl"), BranchMeta{}); err != nil {
		t.Fatalf("SaveBranchMeta root: %v", err)
	}
	if err := SaveBranchMeta(filepath.Join(sessionDir, "child.jsonl"), BranchMeta{ParentID: "root"}); err != nil {
		t.Fatalf("SaveBranchMeta child: %v", err)
	}

	childCtx := WithParentSession(context.Background(), "child")
	second, err := task.Execute(childCtx, []byte(`{"prompt":"child task","fork_from":"`+rootRef+`"}`))
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	childRef := subagentRefFromOutput(t, second)
	if childRef == rootRef {
		t.Fatalf("child ref = source ref %q, want copied ref", childRef)
	}
	if !strings.Contains(second, "Forked from: "+rootRef) ||
		!strings.Contains(second, "Final answer:\nchild answer") {
		t.Fatalf("second output = %q, want copied reference guidance and final answer", second)
	}
}

func TestTaskToolRejectsLegacyForkFromCurrentSession(t *testing.T) {
	sub := &mockProvider{name: "sub", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "first answer"},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "should not run"},
			{Type: provider.ChunkDone},
		},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil).
		WithTranscripts(NewSubagentStore(t.TempDir()), t.TempDir(), "base-model", "base-effort")

	first, err := task.Execute(testTaskContext(), []byte(`{"prompt":"first task"}`))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	ref := subagentRefFromOutput(t, first)
	_, err = task.Execute(testTaskContext(), []byte(`{"prompt":"second task","fork_from":"`+ref+`"}`))
	if err == nil || !strings.Contains(err.Error(), "cannot be safely converted") {
		t.Fatalf("legacy fork error = %v, want unsafe conversion rejection", err)
	}
	if len(sub.requests) != 1 {
		t.Fatalf("provider requests = %d, want only first run", len(sub.requests))
	}
}

func TestTaskToolFailedForegroundContinuationPersistsAndRejectsReuse(t *testing.T) {
	sub := &mockProvider{name: "sub", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "first answer"},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkError, Err: errors.New("provider failed")},
		},
	}}
	store := NewSubagentStore(t.TempDir())
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	task := NewTaskTool(sub, nil, reg, 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(store, t.TempDir(), "base-model", "base-effort")

	first, err := task.Execute(testTaskContext(), []byte(`{"prompt":"first task"}`))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	ref := subagentRefFromOutput(t, first)

	_, err = task.Execute(testTaskContext(), []byte(`{"prompt":"second task","continue_from":"`+ref+`"}`))
	if err == nil || !strings.Contains(err.Error(), "provider failed") {
		t.Fatalf("second Execute error = %v, want provider failure", err)
	}
	meta, err := store.LoadMeta(ref)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.Status != SubagentFailed {
		t.Fatalf("status = %q, want failed", meta.Status)
	}
	loaded, err := LoadSession(store.sessionPath(ref))
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	msgs := loaded.Snapshot()
	if len(msgs) != 4 || !strings.HasSuffix(msgs[1].Content, "first task") || msgs[2].Content != "first answer" || msgs[3].Content != "second task" {
		t.Fatalf("failed continuation transcript = %+v, want first task/answer plus second task", msgs)
	}
	if _, err := task.Execute(testTaskContext(), []byte(`{"prompt":"third task","continue_from":"`+ref+`"}`)); err == nil || !strings.Contains(err.Error(), "failed and cannot be continued") {
		t.Fatalf("reuse error = %v, want failed ref rejection", err)
	}
}

func TestTaskToolBackgroundPanicPersistsFailedMetadata(t *testing.T) {
	sub := panicProvider{name: "panic-sub"}
	store := NewSubagentStore(t.TempDir())
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	task := NewTaskTool(sub, nil, reg, 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(store, t.TempDir(), "base-model", "base-effort")

	jm := jobs.NewManager(event.Discard)
	defer jm.Close()
	ctx := testTaskContext()
	ctx = jobs.WithSession(ctx, "parent-session")
	ctx = jobs.WithManager(ctx, jm)
	out, err := task.Execute(ctx, []byte(`{"prompt":"panic task","run_in_background":true}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	ref := subagentRefFromOutput(t, out)
	jobID := extractJobID(out)
	if jobID == "" {
		t.Fatalf("no background job id in output:\n%s", out)
	}
	res := jm.WaitForSession(context.Background(), "parent-session", []string{jobID}, 5)
	if len(res) != 1 || res[0].Status != jobs.Failed {
		t.Fatalf("background job result = %+v, want failed", res)
	}
	if !strings.Contains(res[0].Output, "Subagent reference (failed): "+ref) {
		t.Fatalf("job output = %q, want failed subagent ref %s", res[0].Output, ref)
	}
	meta, err := store.LoadMeta(ref)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.Status != SubagentFailed {
		t.Fatalf("status = %q, want failed", meta.Status)
	}
	if _, err := task.Execute(testTaskContext(), []byte(`{"prompt":"again","continue_from":"`+ref+`"}`)); err == nil || !strings.Contains(err.Error(), "failed and cannot be continued") {
		t.Fatalf("reuse error = %v, want failed continuation rejection", err)
	}
}

func TestTaskToolBackgroundResultIncludesReferenceGuidance(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "background answer"},
		{Type: provider.ChunkDone},
	}}
	store := NewSubagentStore(t.TempDir())
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	task := NewTaskTool(sub, nil, reg, 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(store, t.TempDir(), "base-model", "base-effort")

	jm := jobs.NewManager(event.Discard)
	defer jm.Close()
	ctx := testTaskContext()
	ctx = jobs.WithSession(ctx, "parent-session")
	ctx = jobs.WithManager(ctx, jm)
	out, err := task.Execute(ctx, []byte(`{"prompt":"background task","run_in_background":true}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	ref := subagentRefFromOutput(t, out)
	if !strings.Contains(out, "To continue this same subagent transcript in a later call") {
		t.Fatalf("start output = %q, want reference guidance", out)
	}
	jobID := extractJobID(out)
	if jobID == "" {
		t.Fatalf("no background job id in output:\n%s", out)
	}
	res := jm.WaitForSession(context.Background(), "parent-session", []string{jobID}, 5)
	if len(res) != 1 || res[0].Status != jobs.Done {
		t.Fatalf("background job result = %+v, want succeeded", res)
	}
	if !strings.Contains(res[0].Output, "Subagent reference: "+ref) ||
		!strings.Contains(res[0].Output, "To continue this same subagent transcript in a later call") ||
		!strings.Contains(res[0].Output, "Final answer:\nbackground answer") {
		t.Fatalf("job output = %q, want reference guidance and final answer", res[0].Output)
	}
}

func TestTaskToolBackgroundAncestorContinuationIncludesForkGuidance(t *testing.T) {
	sub := &mockProvider{name: "sub", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkText, Text: "root answer"},
			{Type: provider.ChunkDone},
		},
		{
			{Type: provider.ChunkText, Text: "child background answer"},
			{Type: provider.ChunkDone},
		},
	}}
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	task := NewTaskTool(sub, nil, reg, 20, 0, 0, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(store, t.TempDir(), "base-model", "base-effort")

	rootCtx := WithParentSession(context.Background(), "root")
	rootOut, err := task.Execute(rootCtx, []byte(`{"prompt":"root task"}`))
	if err != nil {
		t.Fatalf("root Execute: %v", err)
	}
	rootRef := subagentRefFromOutput(t, rootOut)
	if err := SaveBranchMeta(filepath.Join(sessionDir, "root.jsonl"), BranchMeta{}); err != nil {
		t.Fatalf("SaveBranchMeta root: %v", err)
	}
	if err := SaveBranchMeta(filepath.Join(sessionDir, "child.jsonl"), BranchMeta{ParentID: "root"}); err != nil {
		t.Fatalf("SaveBranchMeta child: %v", err)
	}

	jm := jobs.NewManager(event.Discard)
	defer jm.Close()
	childCtx := WithParentSession(context.Background(), "child")
	childCtx = jobs.WithSession(childCtx, "child")
	childCtx = jobs.WithManager(childCtx, jm)
	startOut, err := task.Execute(childCtx, []byte(`{"prompt":"child task","continue_from":"`+rootRef+`","run_in_background":true}`))
	if err != nil {
		t.Fatalf("child Execute: %v", err)
	}
	childRef := subagentRefFromOutput(t, startOut)
	if childRef == rootRef {
		t.Fatalf("child ref = source ref %q, want copied ref", childRef)
	}
	if !strings.Contains(startOut, "Forked from: "+rootRef) ||
		!strings.Contains(startOut, "The requested ref resolves to an ancestor conversation transcript") ||
		strings.Contains(startOut, "Final answer:") {
		t.Fatalf("start output = %q, want fork guidance without final answer", startOut)
	}
	jobID := extractJobID(startOut)
	if jobID == "" {
		t.Fatalf("no background job id in output:\n%s", startOut)
	}
	res := jm.WaitForSession(context.Background(), "child", []string{jobID}, 5)
	if len(res) != 1 || res[0].Status != jobs.Done {
		t.Fatalf("background job result = %+v, want succeeded", res)
	}
	if !strings.Contains(res[0].Output, "Subagent reference: "+childRef) ||
		!strings.Contains(res[0].Output, "Forked from: "+rootRef) ||
		!strings.Contains(res[0].Output, "The requested ref resolves to an ancestor conversation transcript") ||
		!strings.Contains(res[0].Output, "Final answer:\nchild background answer") {
		t.Fatalf("job output = %q, want copied ref guidance and final answer", res[0].Output)
	}
}

func TestTaskToolRejectsMismatchedContinuationProfile(t *testing.T) {
	sub := &mockProvider{name: "sub", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "answer"},
		{Type: provider.ChunkDone},
	}}
	task := newTestTaskTool(t, sub, tool.NewRegistry(), "sys", "", "", nil).
		WithTranscripts(NewSubagentStore(t.TempDir()), t.TempDir(), "base-model", "")

	out, err := task.Execute(testTaskContext(), []byte(`{"prompt":"first task"}`))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	ref := subagentRefFromOutput(t, out)
	_, err = task.Execute(testTaskContext(), []byte(`{"prompt":"second task","continue_from":"`+ref+`","model":"other-model"}`))
	if err == nil || !strings.Contains(err.Error(), "model/effort") {
		t.Fatalf("mismatched model error = %v, want compatibility failure", err)
	}
}

func extractJobID(msg string) string {
	quote := strings.Index(msg, `"`)
	if quote < 0 {
		return ""
	}
	end := strings.Index(msg[quote+1:], `"`)
	if end < 0 {
		return ""
	}
	return msg[quote+1 : quote+1+end]
}

func subagentRefFromOutput(t *testing.T, out string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Subagent reference: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Subagent reference: "))
		}
	}
	t.Fatalf("no subagent reference in output:\n%s", out)
	return ""
}

func TestSubSinkForwardsUsageToParent(t *testing.T) {
	var got []event.Event
	parent := event.FuncSink(func(e event.Event) {
		got = append(got, e)
	})
	subSinkFor("task_1", parent).Emit(event.Event{
		Kind:        event.Usage,
		Usage:       &provider.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
		UsageSource: event.UsageSourceSubagent,
	})
	if len(got) != 1 || got[0].Usage == nil || got[0].UsageSource != event.UsageSourceSubagent {
		t.Fatalf("forwarded events = %+v, want subagent usage", got)
	}
}

func TestTaskToolCarriesRecentKeepIntoSubsessions(t *testing.T) {
	task := NewTaskTool(&mockProvider{name: "sub"}, nil, tool.NewRegistry(), 20, 0, 7, 0, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil)
	if task.recentKeep != 7 {
		t.Fatalf("recentKeep = %d, want 7", task.recentKeep)
	}
}

func newTestTaskTool(t *testing.T, prov provider.Provider, reg *tool.Registry, sysPrompt, subagentModel, subagentEffort string, resolve func(string, string) (provider.Provider, *provider.Pricing, int, error)) *TaskTool {
	t.Helper()
	return NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0, 0, 0, 0.0, "", sysPrompt, nil, 0, subagentModel, subagentEffort, resolve).
		WithTranscripts(NewSubagentStore(t.TempDir()), t.TempDir(), "base-model", "base-effort")
}

type panicProvider struct{ name string }

func (p panicProvider) Name() string { return p.name }

func (p panicProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	panic("subagent boom")
}
