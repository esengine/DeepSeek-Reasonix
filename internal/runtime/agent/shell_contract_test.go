package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/builtin"
)

func TestBatchDependencyBarrierSkipsVerificationAfterFailedMutation(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	for _, tl := range (builtin.Workspace{Dir: dir}).Tools("edit_file") {
		reg.Add(tl)
	}
	// Verification would return "bash done" if it ran — the barrier must prevent that.
	reg.Add(fakeTool{name: "bash", readOnly: false})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("e1", "edit_file", `{"path":"x.txt","old_string":"missing","new_string":"b"}`),
			toolCallChunk("v1", "bash", `{"command":"go test ./..."}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "edit then verify"); err != nil {
		t.Fatal(err)
	}
	if got := toolResultByID(a.sess.conversation, "v1"); !strings.Contains(got, "earlier modification") {
		t.Fatalf("verify result = %q, want dependency skip", got)
	}
	if strings.Contains(toolResultByID(a.sess.conversation, "v1"), "bash done") {
		t.Fatal("verification process should not have started")
	}
	for _, msg := range a.sess.conversation.Snapshot() {
		if msg.ToolCallID != "v1" {
			continue
		}
		if msg.ToolExecution == nil {
			t.Fatal("missing execution metadata on skipped verify")
		}
		if msg.ToolExecution.State != tool.ShellStateNotRun || msg.ToolExecution.FailurePhase != tool.ShellPhaseDependency {
			t.Fatalf("execution = %+v", msg.ToolExecution)
		}
		if msg.ToolExecution.Verification != tool.ShellVerificationNotRun {
			t.Fatalf("verification = %q, want not_run (not failed)", msg.ToolExecution.Verification)
		}
		return
	}
	t.Fatal("verify tool result missing")
}

// TestBatchDependencyBarrierIgnoresFailedNonMutationMetaTool keeps bookkeeping
// writers out of the barrier. todo_write, complete_step, ask, bash_output and
// wait all report ReadOnly()==false, but evidence.ToolCallMutates deliberately
// exempts them: they never touch workspace state. A failed todo update must not
// block the real edits queued behind it in the same batch.
func TestBatchDependencyBarrierIgnoresFailedNonMutationMetaTool(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	for _, tl := range (builtin.Workspace{Dir: dir}).Tools("edit_file") {
		reg.Add(tl)
	}
	reg.Add(fakeTool{name: "todo_write", readOnly: false, err: fmt.Errorf("todo store unavailable")})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("t1", "todo_write", `{"todos":[]}`),
			toolCallChunk("e1", "edit_file", `{"path":"x.txt","old_string":"a","new_string":"b"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{CheckTargetAccess: allowTestFileAccess}, event.Discard)
	if err := a.Run(context.Background(), "track then edit"); err != nil {
		t.Fatal(err)
	}
	if got := toolResultByID(a.sess.conversation, "e1"); strings.Contains(got, "earlier modification") {
		t.Fatalf("edit was blocked by a failed todo_write: %s", got)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "b\n" {
		t.Fatalf("file = %q, want the edit to have been applied", string(got))
	}
}

// TestBatchDependencyBarrierStopsAfterFailedWorkspaceWrite is the other half of
// the same boundary: a genuine workspace mutation failing still stops the batch.
func TestBatchDependencyBarrierStopsAfterFailedWorkspaceWrite(t *testing.T) {
	dir := testenv.TempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	for _, tl := range (builtin.Workspace{Dir: dir}).Tools("edit_file") {
		reg.Add(tl)
	}
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("e1", "edit_file", `{"path":"x.txt","old_string":"missing","new_string":"b"}`),
			toolCallChunk("e2", "edit_file", `{"path":"x.txt","old_string":"a","new_string":"c"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "two edits"); err != nil {
		t.Fatal(err)
	}
	if got := toolResultByID(a.sess.conversation, "e2"); !strings.Contains(got, "earlier modification") {
		t.Fatalf("second edit result = %q, want dependency skip", got)
	}
	got, err := os.ReadFile(filepath.Join(dir, "x.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "a\n" {
		t.Fatalf("file = %q, want it untouched after the barrier", string(got))
	}
}

// writerProxy is a use_capability-shaped CallResolver: schema ReadOnly is true,
// but ResolveCall points at a real writer. The batch barrier must not let this
// run after an earlier mutation failed.
type writerProxy struct {
	target   tool.Tool
	resolves *int
}

func (writerProxy) Name() string        { return "use_capability" }
func (writerProxy) Description() string { return "proxy" }
func (writerProxy) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"action":{"type":"string"}}}`)
}
func (writerProxy) ReadOnly() bool { return true }
func (p writerProxy) Execute(context.Context, json.RawMessage) (string, error) {
	return "", fmt.Errorf("proxy Execute must not run; ResolveCall provides the target")
}
func (p writerProxy) ResolveCall(_ context.Context, args json.RawMessage) (tool.ResolvedCall, error) {
	if p.resolves != nil {
		(*p.resolves)++
	}
	return tool.ResolvedCall{
		DisplayName:  "use_capability",
		TargetName:   p.target.Name(),
		Args:         args,
		Target:       p.target,
		ReadOnly:     false,
		ProxyAction:  "call",
		CapabilityID: "mcp-tool:test/write",
	}, nil
}

type capturingWriter struct {
	name  string
	path  string
	calls *int
}

func (c *capturingWriter) Name() string            { return c.name }
func (c *capturingWriter) Description() string     { return "" }
func (c *capturingWriter) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (c *capturingWriter) ReadOnly() bool          { return false }
func (c *capturingWriter) Execute(context.Context, json.RawMessage) (string, error) {
	if c.calls != nil {
		*c.calls++
	}
	if c.path != "" {
		_ = os.WriteFile(c.path, []byte("proxy-wrote\n"), 0o600)
	}
	return "wrote", nil
}

func TestBatchDependencyBarrierBlocksResolvedMCPWriterAfterFailedMutation(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	proxyWrote := filepath.Join(dir, "proxy-out.txt")
	var writerCalls int
	var resolves int
	writer := &capturingWriter{name: "mcp__test__write", path: proxyWrote, calls: &writerCalls}
	reg := tool.NewRegistry()
	for _, tl := range (builtin.Workspace{Dir: dir}).Tools("edit_file") {
		reg.Add(tl)
	}
	reg.Add(writerProxy{target: writer, resolves: &resolves})
	reg.Add(writer) // real target available for ResolveCall

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("e1", "edit_file", `{"path":"x.txt","old_string":"missing","new_string":"b"}`),
			toolCallChunk("m1", "use_capability", `{"action":"call","capability_id":"mcp-tool:test/write"}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "fail then mcp write"); err != nil {
		t.Fatal(err)
	}
	if writerCalls != 0 {
		t.Fatalf("MCP writer Execute ran %d times; dependency barrier must block after failed edit", writerCalls)
	}
	if resolves != 1 {
		t.Fatalf("proxy ResolveCall ran %d times, want exactly once before the dependency barrier", resolves)
	}
	if _, err := os.Stat(proxyWrote); err == nil {
		t.Fatal("proxy writer mutated disk after failed edit")
	}
	got := toolResultByID(a.sess.conversation, "m1")
	if !strings.Contains(got, "earlier modification") {
		t.Fatalf("proxy result = %q, want dependency skip", got)
	}
}

func TestBatchDependencyBarrierAllowsReadOnlyDiagnosisAfterFailedMutation(t *testing.T) {
	// After a mutating failure, host-proven read-only diagnosis must still run.
	// Only subsequent mutations and verification commands are skipped.
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(path, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	for _, name := range []string{"edit_file", "read_file"} {
		for _, tl := range (builtin.Workspace{Dir: dir}).Tools(name) {
			reg.Add(tl)
		}
	}
	reg.Add(fakeTool{name: "bash", readOnly: false})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("e1", "edit_file", `{"path":"x.txt","old_string":"missing","new_string":"b"}`),
			toolCallChunk("r1", "read_file", `{"path":"x.txt"}`),
			toolCallChunk("v1", "bash", `{"command":"go test ./..."}`),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "fail then diagnose"); err != nil {
		t.Fatal(err)
	}
	readOut := toolResultByID(a.sess.conversation, "r1")
	if strings.Contains(readOut, "earlier modification") {
		t.Fatalf("read_file was incorrectly dependency-skipped: %q", readOut)
	}
	trimmed := strings.TrimSpace(readOut)
	if strings.HasPrefix(trimmed, "error:") || strings.HasPrefix(trimmed, "blocked:") {
		t.Fatalf("read_file should have executed successfully, got %q", readOut)
	}
	if !strings.Contains(readOut, "a") {
		t.Fatalf("read_file body missing original file content: %q", readOut)
	}
	if got := toolResultByID(a.sess.conversation, "v1"); !strings.Contains(got, "earlier modification") {
		t.Fatalf("verification should be dependency-skipped, got %q", got)
	}
	if strings.Contains(toolResultByID(a.sess.conversation, "v1"), "bash done") {
		t.Fatal("verification process must not start after failed mutation")
	}
}

func TestModelMessagesStripsToolExecution(t *testing.T) {
	code := 1
	in := []provider.Message{
		{Role: provider.RoleUser, Content: "hi"},
		{Role: provider.RoleAssistant, Content: "", ToolCalls: []provider.ToolCall{{ID: "c1", Name: "bash", Arguments: `{"command":"false"}`}}},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "bash", Content: "error", ToolExecution: &provider.ToolExecution{
			Kind: "shell", Shell: "bash", State: "failed", ExitCode: &code, FailurePhase: "execution",
		}},
	}
	out := provider.ModelMessages(in)
	if len(out) != 3 {
		t.Fatalf("len = %d", len(out))
	}
	if out[2].ToolExecution != nil {
		t.Fatalf("ToolExecution leaked into model messages: %+v", out[2].ToolExecution)
	}
	if in[2].ToolExecution == nil {
		t.Fatal("session copy was mutated")
	}
}
