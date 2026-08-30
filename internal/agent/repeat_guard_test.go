package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/diff"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
	"reasonix/internal/tool/builtin"
)

type failingWriterTool struct {
	name  string
	calls *int32
}

func (f failingWriterTool) Name() string            { return f.name }
func (f failingWriterTool) Description() string     { return "always fails to write" }
func (f failingWriterTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f failingWriterTool) ReadOnly() bool          { return false }
func (f failingWriterTool) Execute(context.Context, json.RawMessage) (string, error) {
	if f.calls != nil {
		atomic.AddInt32(f.calls, 1)
	}
	return "", errors.New("old_string not found in prompt.txt")
}

type stateAwareWriterTool struct {
	name  string
	calls *int32
	valid *atomic.Bool
}

type previewSuccessFailWriterTool struct {
	name  string
	calls *int32
}

func (f previewSuccessFailWriterTool) Name() string { return f.name }
func (f previewSuccessFailWriterTool) Description() string {
	return "previews successfully but cannot write"
}
func (f previewSuccessFailWriterTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (f previewSuccessFailWriterTool) ReadOnly() bool { return false }
func (f previewSuccessFailWriterTool) Execute(context.Context, json.RawMessage) (string, error) {
	if f.calls != nil {
		atomic.AddInt32(f.calls, 1)
	}
	return "", errors.New("write prompt.txt: permission denied")
}
func (f previewSuccessFailWriterTool) Preview(context.Context, json.RawMessage) (diff.Change, error) {
	return diff.Change{Path: "prompt.txt"}, nil
}

func (f stateAwareWriterTool) Name() string            { return f.name }
func (f stateAwareWriterTool) Description() string     { return "fails until target state changes" }
func (f stateAwareWriterTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f stateAwareWriterTool) ReadOnly() bool          { return false }
func (f stateAwareWriterTool) Execute(context.Context, json.RawMessage) (string, error) {
	if f.calls != nil {
		atomic.AddInt32(f.calls, 1)
	}
	if f.valid != nil && f.valid.Load() {
		return "edited prompt.txt", nil
	}
	return "", errors.New("old_string not found in prompt.txt")
}
func (f stateAwareWriterTool) Preview(context.Context, json.RawMessage) (diff.Change, error) {
	if f.valid != nil && f.valid.Load() {
		return diff.Change{Path: "prompt.txt"}, nil
	}
	return diff.Change{}, errors.New("old_string not found in prompt.txt")
}

func TestRepeatGuardBlocksRepeatedSuccessfulBashFileWrite(t *testing.T) {
	var calls int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false, calls: &calls})
	args := `{"command":"python -c \"with open('prompt.txt', 'w') as f: f.write('hello')\""}`
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "bash", args), {Type: provider.ChunkDone}},
		{toolCallChunk("c2", "bash", args), {Type: provider.ChunkDone}},
		{toolCallChunk("c3", "bash", args), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(withNoClosedLoop(context.Background()), "update the prompt file"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("bash executed %d times, want 2 before the repeat guard blocks", got)
	}
	results := toolResults(a.sess.conversation, "bash")
	if len(results) != 3 {
		t.Fatalf("tool results = %d, want 3", len(results))
	}
	last := results[len(results)-1]
	if !strings.Contains(last, "[loop guard]") || !strings.Contains(last, "edit_file") {
		t.Fatalf("third repeated write should nudge the model to change tools, got %q", last)
	}
}

func TestRepeatGuardAllowsRepeatedNonWritingBashCommand(t *testing.T) {
	var calls int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false, calls: &calls})
	args := `{"command":"go test ./internal/agent"}`
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "bash", args), {Type: provider.ChunkDone}},
		{toolCallChunk("c2", "bash", args), {Type: provider.ChunkDone}},
		{toolCallChunk("c3", "bash", args), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(withNoClosedLoop(context.Background()), "verify repeatedly"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("bash executed %d times, want 3 for non-writing commands", got)
	}
	if last := lastToolResult(a.sess.conversation, "bash"); strings.Contains(last, "[loop guard]") {
		t.Fatalf("non-writing bash should not trip the repeat guard, got %q", last)
	}
}

func TestRepeatGuardBashWriteRedirectDetectionUsesAST(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{
			name: "stdout file redirect",
			cmd:  "printf hi > prompt.txt",
			want: true,
		},
		{
			name: "stderr file redirect",
			cmd:  "printf hi 2>err.log",
			want: true,
		},
		{
			name: "append redirect",
			cmd:  "printf hi >> prompt.txt",
			want: true,
		},
		{
			name: "combined redirect",
			cmd:  "printf hi &> prompt.txt",
			want: true,
		},
		{
			name: "read write redirect",
			cmd:  "cat <> prompt.txt",
			want: true,
		},
		{
			name: "null sink redirect",
			cmd:  "printf hi >/dev/null",
			want: false,
		},
		{
			name: "powershell null sink spelling",
			cmd:  "printf hi >$null",
			want: false,
		},
		{
			name: "windows nul sink spelling",
			cmd:  "printf hi >NUL",
			want: false,
		},
		{
			name: "fd duplication",
			cmd:  "printf hi 2>&1",
			want: false,
		},
		{
			name: "quoted redirect text",
			cmd:  `printf '%s\n' 'a > b'`,
			want: false,
		},
		{
			name: "heredoc body redirect text",
			cmd:  "cat <<'EOF'\n> prompt.txt\nEOF",
			want: false,
		},
		{
			name: "heredoc with file redirect",
			cmd:  "cat <<'EOF' > prompt.txt\nbody\nEOF",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isShellFileWriteCommand(tt.cmd); got != tt.want {
				t.Fatalf("isShellFileWriteCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestRepeatGuardNormalizesStaticBashFields(t *testing.T) {
	singleQuoted := normalizeShellCommand(`printf '%s\n' 'hello world'`)
	doubleQuoted := normalizeShellCommand(`printf "%s\n" "hello world"`)
	if singleQuoted != doubleQuoted {
		t.Fatalf("normalized quote styles differ:\n single: %q\n double: %q", singleQuoted, doubleQuoted)
	}
}

func TestRepeatGuardAllowsTwoRepeatedWriterSuccesses(t *testing.T) {
	var calls int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", readOnly: false, calls: &calls})
	args := `{"path":"prompt.txt","content":"hello"}`
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "write_file", args), {Type: provider.ChunkDone}},
		{toolCallChunk("c2", "write_file", args), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(withNoClosedLoop(context.Background()), "write twice"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("writer executed %d times, want 2 before the guard threshold", got)
	}
	if last := lastToolResult(a.sess.conversation, "write_file"); strings.Contains(last, "[loop guard]") {
		t.Fatalf("second repeated writer call should still be allowed, got %q", last)
	}
}

func TestRepeatGuardBlocksStaleEditLoopAcrossSuccessfulReads(t *testing.T) {
	var editCalls int32
	var readCalls int32
	reg := tool.NewRegistry()
	reg.Add(failingWriterTool{name: "edit_file", calls: &editCalls})
	reg.Add(fakeTool{name: "read_file", readOnly: true, calls: &readCalls})
	editArgs1 := `{"path":"prompt.txt","old_string":"stale","new_string":"ready-v1"}`
	editArgs2 := `{"path":"prompt.txt","old_string":"stale","new_string":"ready-v2"}`
	editArgs3 := `{"path":"prompt.txt","old_string":"stale","new_string":"ready-v3"}`
	readArgs := `{"path":"prompt.txt"}`
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("e1", "edit_file", editArgs1), {Type: provider.ChunkDone}},
		{toolCallChunk("r1", "read_file", readArgs), {Type: provider.ChunkDone}},
		{toolCallChunk("e2", "edit_file", editArgs2), {Type: provider.ChunkDone}},
		{toolCallChunk("r2", "read_file", readArgs), {Type: provider.ChunkDone}},
		{toolCallChunk("e3", "edit_file", editArgs3), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "blocked"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)

	if err := a.Run(withNoClosedLoop(context.Background()), "fix prompt.txt"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := atomic.LoadInt32(&editCalls); got != 2 {
		t.Fatalf("edit_file executed %d times, want 2 before the repeat guard blocks", got)
	}
	if got := atomic.LoadInt32(&readCalls); got != 2 {
		t.Fatalf("read_file executed %d times, want 2", got)
	}
	last := lastToolResult(a.sess.conversation, "edit_file")
	for _, want := range []string{"[loop guard]", "already failed 2 times", "Re-reading alone"} {
		if !strings.Contains(last, want) {
			t.Fatalf("blocked stale edit result should mention %q, got %q", want, last)
		}
	}
}

func TestRepeatGuardRetainsStaleEditFailuresAcrossGoalScope(t *testing.T) {
	var editCalls int32
	reg := tool.NewRegistry()
	reg.Add(failingWriterTool{name: "edit_file", calls: &editCalls})
	editArgs := `{"path":"prompt.txt","old_string":"stale","new_string":"ready"}`
	readArgs := `{"path":"prompt.txt"}`
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("e1", "edit_file", editArgs), {Type: provider.ChunkDone}},
		{toolCallChunk("r1", "read_file", readArgs), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "[goal:continue]"}, {Type: provider.ChunkDone}},
		{toolCallChunk("e2", "edit_file", editArgs), {Type: provider.ChunkDone}},
		{toolCallChunk("r2", "read_file", readArgs), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "[goal:continue]"}, {Type: provider.ChunkDone}},
		{toolCallChunk("e3", "edit_file", editArgs), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "[goal:blocked:stale edit]"}, {Type: provider.ChunkDone}},
	}}
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	ctx := WithDeliveryExecutionScope(context.Background(), DeliveryExecutionScope{
		ID:       "goal-scope-1",
		TaskText: "fix prompt.txt",
	})

	for i := range 3 {
		if err := a.Run(ctx, "continue goal"); err != nil {
			t.Fatalf("Run %d: %v", i+1, err)
		}
	}
	if got := atomic.LoadInt32(&editCalls); got != 2 {
		t.Fatalf("edit_file executed %d times across one goal scope, want 2", got)
	}
	if last := lastToolResult(a.sess.conversation, "edit_file"); !strings.Contains(last, "[loop guard]") {
		t.Fatalf("third goal-scope edit should be blocked, got %q", last)
	}
}

func TestRepeatGuardClearsOrdinaryWriteFailureAcrossGoalRuns(t *testing.T) {
	var editCalls int32
	reg := tool.NewRegistry()
	reg.Add(previewSuccessFailWriterTool{name: "edit_file", calls: &editCalls})
	editArgs := `{"path":"prompt.txt","old_string":"current","new_string":"ready"}`
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("e1", "edit_file", editArgs), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "[goal:continue]"}, {Type: provider.ChunkDone}},
		{toolCallChunk("e2", "edit_file", editArgs), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "[goal:continue]"}, {Type: provider.ChunkDone}},
		{toolCallChunk("e3", "edit_file", editArgs), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "[goal:blocked:permission denied]"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, NewSession(""), Options{}, event.Discard)
	ctx := WithDeliveryExecutionScope(context.Background(), DeliveryExecutionScope{
		ID:       "goal-scope-1",
		TaskText: "fix prompt.txt",
	})

	for i := range 3 {
		if err := a.Run(ctx, "continue goal"); err != nil {
			t.Fatalf("Run %d: %v", i+1, err)
		}
	}
	if got := atomic.LoadInt32(&editCalls); got != 3 {
		t.Fatalf("edit_file executed %d times across Goal Runs, want ordinary write failure retried each Run", got)
	}
}

func TestRepeatGuardDoesNotUsePreviewToClearWriteFailure(t *testing.T) {
	var editCalls int32
	reg := tool.NewRegistry()
	reg.Add(previewSuccessFailWriterTool{name: "edit_file", calls: &editCalls})
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	ctx := context.Background()
	edit := provider.ToolCall{Name: "edit_file", Arguments: `{"path":"prompt.txt","old_string":"current","new_string":"ready"}`}

	executeBatchOutputs(a, ctx, []provider.ToolCall{edit})
	executeBatchOutputs(a, ctx, []provider.ToolCall{edit})
	last := executeBatchOutputs(a, ctx, []provider.ToolCall{edit})[0]

	if !strings.Contains(last, "[loop guard]") {
		t.Fatalf("successful preview must not clear a repeated write failure, got %q", last)
	}
	for _, staleHint := range []string{"stale anchor", "Re-reading alone", "new old_string"} {
		if strings.Contains(last, staleHint) {
			t.Fatalf("ordinary write failure should not use stale-anchor guidance %q, got %q", staleHint, last)
		}
	}
	if got := atomic.LoadInt32(&editCalls); got != 2 {
		t.Fatalf("edit_file executed %d times, want write failure blocked before third execution", got)
	}
}

func TestRepeatGuardKeepsStaleFailureAfterUnrelatedMutation(t *testing.T) {
	var editCalls int32
	reg := tool.NewRegistry()
	reg.Add(failingWriterTool{name: "edit_file", calls: &editCalls})
	reg.Add(fakeTool{name: "write_file", readOnly: false})
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	ctx := context.Background()
	edit := provider.ToolCall{Name: "edit_file", Arguments: `{"path":"prompt.txt","old_string":"stale","new_string":"ready"}`}

	executeBatchOutputs(a, ctx, []provider.ToolCall{edit})
	executeBatchOutputs(a, ctx, []provider.ToolCall{edit})
	executeBatchOutputs(a, ctx, []provider.ToolCall{{
		Name: "write_file", Arguments: `{"path":"other.txt","content":"unrelated"}`,
	}})
	last := executeBatchOutputs(a, ctx, []provider.ToolCall{edit})[0]

	if !strings.Contains(last, "[loop guard]") {
		t.Fatalf("unrelated mutation should not clear stale failure history, got %q", last)
	}
	if got := atomic.LoadInt32(&editCalls); got != 2 {
		t.Fatalf("edit_file executed %d times, want unrelated mutation to preserve the guard", got)
	}
}

func TestRepeatGuardKeepsStaleFailureAfterSameFileMutation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(path, []byte("status=ready\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	for _, tl := range (builtin.Workspace{Dir: dir}).Tools("edit_file") {
		reg.Add(tl)
	}
	a := New(nil, reg, NewSession(""), Options{WriteWorkspaceRoot: dir}, event.Discard)
	ctx := context.Background()
	stale := provider.ToolCall{
		Name:      "edit_file",
		Arguments: `{"path":"prompt.txt","old_string":"status=stale","new_string":"status=fixed"}`,
	}

	executeBatchOutputs(a, ctx, []provider.ToolCall{stale})
	executeBatchOutputs(a, ctx, []provider.ToolCall{stale})
	executeBatchOutputs(a, ctx, []provider.ToolCall{{
		Name:      "edit_file",
		Arguments: `{"path":"prompt.txt","old_string":"status=ready","new_string":"status=done"}`,
	}})
	last := executeBatchOutputs(a, ctx, []provider.ToolCall{stale})[0]

	if !strings.Contains(last, "[loop guard]") {
		t.Fatalf("same-file mutation must not renew a still-stale anchor budget, got %q", last)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "status=done\n" {
		t.Fatalf("file content = %q, want successful unrelated edit preserved", got)
	}
}

func TestRepeatGuardNormalizesFailureTargetPaths(t *testing.T) {
	var editCalls int32
	dir := t.TempDir()
	reg := tool.NewRegistry()
	reg.Add(failingWriterTool{name: "edit_file", calls: &editCalls})
	a := New(nil, reg, NewSession(""), Options{WriteWorkspaceRoot: dir}, event.Discard)
	ctx := context.Background()
	args := []string{
		`{"path":"prompt.txt","old_string":"stale","new_string":"ready-v1"}`,
		`{"path":"./prompt.txt","old_string":"stale","new_string":"ready-v2"}`,
		`{"path":` + string(mustJSON(t, filepath.Join(dir, "prompt.txt"))) + `,"old_string":"stale","new_string":"ready-v3"}`,
	}

	executeBatchOutputs(a, ctx, []provider.ToolCall{{Name: "edit_file", Arguments: args[0]}})
	executeBatchOutputs(a, ctx, []provider.ToolCall{{Name: "edit_file", Arguments: args[1]}})
	last := executeBatchOutputs(a, ctx, []provider.ToolCall{{Name: "edit_file", Arguments: args[2]}})[0]

	if !strings.Contains(last, "[loop guard]") {
		t.Fatalf("path aliases should share one repeated-failure signature, got %q", last)
	}
	if got := atomic.LoadInt32(&editCalls); got != 2 {
		t.Fatalf("edit_file executed %d times, want absolute-path retry blocked", got)
	}
}

func TestRepeatGuardAllowsRetryAfterExternalTargetChange(t *testing.T) {
	var editCalls int32
	var valid atomic.Bool
	reg := tool.NewRegistry()
	reg.Add(stateAwareWriterTool{name: "edit_file", calls: &editCalls, valid: &valid})
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	ctx := context.Background()
	edit := provider.ToolCall{Name: "edit_file", Arguments: `{"path":"prompt.txt","old_string":"stale","new_string":"ready"}`}

	executeBatchOutputs(a, ctx, []provider.ToolCall{edit})
	executeBatchOutputs(a, ctx, []provider.ToolCall{edit})
	valid.Store(true)
	last := executeBatchOutputs(a, ctx, []provider.ToolCall{edit})[0]

	if strings.Contains(last, "[loop guard]") {
		t.Fatalf("changed target state should allow the retry, got %q", last)
	}
	if got := atomic.LoadInt32(&editCalls); got != 3 {
		t.Fatalf("edit_file executed %d times, want retry after external target change", got)
	}
}
