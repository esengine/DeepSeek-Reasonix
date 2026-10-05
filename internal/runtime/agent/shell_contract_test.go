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

func TestOrdinaryModeBlocksMixedMutationAndVerification(t *testing.T) {
	// Preflight runs before Execute, so a fake bash is enough — the process
	// must never start for a mixed mutation+verification command. `;` is the
	// shape that matters: the verifier's exit status replaces go generate's.
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("m1", "bash", `{"command":"go generate ./... ; go test ./..."}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "ok"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	got := toolResultByID(a.sess.conversation, "m1")
	if strings.Contains(got, "bash done") {
		t.Fatal("mixed command was executed")
	}
	if !strings.Contains(got, "cannot prove leaves the workspace alone") {
		t.Fatalf("result = %q, want ordinary-mode mixed block", got)
	}
	// The block names the segment it tripped on; without it the model rewrites
	// whichever part it guesses is at fault.
	if !strings.Contains(got, "go generate ./...") {
		t.Fatalf("result = %q, want the offending segment named", got)
	}
	for _, msg := range a.sess.conversation.Snapshot() {
		if msg.ToolCallID != "m1" {
			continue
		}
		if msg.ToolExecution == nil || msg.ToolExecution.State != tool.ShellStateNotRun {
			t.Fatalf("execution = %+v, want not_run", msg.ToolExecution)
		}
		if msg.ToolExecution.FailurePhase != tool.ShellPhasePreflight {
			t.Fatalf("phase = %q", msg.ToolExecution.FailurePhase)
		}
		return
	}
	t.Fatal("tool result missing")
}

// TestOrdinaryModeRunsShortCircuitBuildAndVerify guards the everyday shape the
// preflight must not touch. `go build ./... && go test ./...` cannot report a
// false success: bash stops at the failing build and returns its status. Only
// Delivery blocks it, because there a mutation invalidates the verification
// receipt regardless of exit status.
func TestOrdinaryModeRunsShortCircuitBuildAndVerify(t *testing.T) {
	commands := []string{
		"go build ./... && go test ./...",
		"npm install && npm test",
		"mkdir -p out && go test ./...",
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			reg := tool.NewRegistry()
			reg.Add(fakeTool{name: "bash", readOnly: false})
			args, err := json.Marshal(map[string]string{"command": command})
			if err != nil {
				t.Fatal(err)
			}
			prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
				{toolCallChunk("m1", "bash", string(args)), {Type: provider.ChunkDone}},
				{{Type: provider.ChunkText, Text: "ok"}, {Type: provider.ChunkDone}},
			}}
			a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
			if err := a.Run(context.Background(), "test"); err != nil {
				t.Fatal(err)
			}
			got := toolResultByID(a.sess.conversation, "m1")
			if strings.Contains(got, "blocked:") {
				t.Fatalf("ordinary mode blocked %q: %s", command, got)
			}
			if !strings.Contains(got, "bash done") {
				t.Fatalf("command did not run: result = %q", got)
			}
		})
	}
}

// The host recovers each stage's status from PIPESTATUS, so a pipeline it reads
// no longer hides the check. `&&` still short-circuits on the build, which
// leaves nothing about this shape unreadable — it must not be blocked.
func TestOrdinaryModeRunsBuildAndVerifyThroughReadablePipe(t *testing.T) {
	command := "go build ./internal/x/ && go test ./internal/x/ -v 2>&1 | tail -10"
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	args, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("m1", "bash", string(args)), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "ok"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	got := toolResultByID(a.sess.conversation, "m1")
	if strings.Contains(got, "blocked:") {
		t.Fatalf("ordinary mode blocked %q: %s", command, got)
	}
	if !strings.Contains(got, "bash done") {
		t.Fatalf("command did not run: result = %q", got)
	}
}

// The exemption covers exactly one masking source. Anything that drops a
// mutation's status before the final pipeline stays blocked, because no
// per-stage report can bring that status back.
func TestPipeStatusExemptionStopsAtTheFinalPipeline(t *testing.T) {
	for _, tt := range []struct {
		name    string
		command string
		exempt  bool
	}{
		{"&& into a readable pipe", "go build ./x/ && go test ./x/ 2>&1 | tail -10", true},
		{"; drops the generate status", "go generate ./x/ ; go test ./x/ 2>&1 | tail -10", false},
		{"|| decides the status itself", "go generate ./x/ || go test ./x/ 2>&1 | tail -3", false},
		{"backgrounded mutation is never waited on", "go generate ./x/ & go test ./x/ 2>&1 | tail -3", false},
		{"no pipeline leaves nothing for the probe", "go generate ./x/ && go test ./x/", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{"command": tt.command})
			if err != nil {
				t.Fatal(err)
			}
			if got := hostReadsCheckThroughPipeStatus(args); got != tt.exempt {
				t.Errorf("hostReadsCheckThroughPipeStatus(%q) = %v, want %v", tt.command, got, tt.exempt)
			}
		})
	}
}

// A background call never reaches the foreground probe, so its shape cannot be
// exempted on the strength of a report nothing will write.
func TestPipeStatusExemptionSkipsBackgroundCalls(t *testing.T) {
	args := json.RawMessage(`{"command":"go build ./x/ && go test ./x/ 2>&1 | tail -10","run_in_background":true}`)
	if hostReadsCheckThroughPipeStatus(args) {
		t.Error("a background call has no foreground pipe-status report to read")
	}
}

func TestOrdinaryModeBlocksMaskedVerifierExit(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("m1", "bash", `{"command":"go test ./...; echo $?"}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "ok"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	got := toolResultByID(a.sess.conversation, "m1")
	if strings.Contains(got, "bash done") {
		t.Fatal("masked exit command was executed")
	}
	if !strings.Contains(got, "masks") && !strings.Contains(got, "exit status") {
		t.Fatalf("result = %q, want mask block", got)
	}
}

// Handing source over is no longer a refusal. The shape rules that remain are
// about a status that cannot answer for the check, which is a different fact.
func TestDeliveryNoLongerRefusesAnInlineInterpreter(t *testing.T) {
	for _, command := range []string{
		"python3 -c 'print(1)' && go test ./...",
		"go test ./... && python3 -c 'print(1)'",
		"python3 -c 'print(1)'",
	} {
		t.Run(command, func(t *testing.T) {
			reg := tool.NewRegistry()
			reg.Add(fakeTool{name: "bash", readOnly: false})
			args, err := json.Marshal(map[string]string{"command": command})
			if err != nil {
				t.Fatal(err)
			}
			prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
				{toolCallChunk("t1", "todo_write", `{"todos":[{"content":"check","status":"in_progress"}]}`), {Type: provider.ChunkDone}},
				{toolCallChunk("m1", "bash", string(args)), {Type: provider.ChunkDone}},
				{{Type: provider.ChunkText, Text: "ok"}, {Type: provider.ChunkDone}},
			}}
			a := New(prov, reg, sessionstore.NewSession(""), Options{DeliveryProfile: true}, event.Discard)
			_ = a.Run(context.Background(), "check")
			if got := toolResultByID(a.sess.conversation, "m1"); strings.Contains(got, "cannot audit inline interpreter source") {
				t.Fatalf("result = %q, want no refusal for handing over source", got)
			}
		})
	}
}

func TestOrdinaryModeBlocksNonTerminalInlineInterpreter(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("m1", "bash", `{"command":"python3 -c 'open(\"x\",\"w\").write(\"y\")' ; node verify_frontend_logic.js"}`), {Type: provider.ChunkDone}},
		// A `&&` variant of the same pair is covered by the allow-list test above.
		{{Type: provider.ChunkText, Text: "ok"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	got := toolResultByID(a.sess.conversation, "m1")
	if strings.Contains(got, "bash done") {
		t.Fatal("non-terminal inline interpreter was executed")
	}
	if !strings.Contains(got, "inline interpreter") {
		t.Fatalf("result = %q, want non-terminal inline block", got)
	}
}

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
	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
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
