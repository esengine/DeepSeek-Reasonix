package delegation

import (
	"context"
	"encoding/json"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/hostaudit"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/writeclaim"
	_ "reasonix/internal/tools/builtin"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// evidenceRegistry wires the real complete_step + todo_write builtins (the
// enforcement surface under test) alongside bash/write stubs that emit real
// receipts without touching the host — so the whole turn loop, ledger, gate,
// and host-advance run end to end.
func evidenceRegistry() *tool.Registry {
	reg := tool.NewRegistry()
	for _, bt := range tool.Builtins() {
		if bt.Name() == "complete_step" || bt.Name() == "todo_write" {
			reg.Add(bt)
		}
	}
	reg.Add(stubBash{})
	reg.Add(stubWrite{})
	return reg
}

// fakeReadFileTool is a minimal read-only tool whose successful calls produce
// Read receipts with an extractable path, like the real read_file.
type fakeReadFileTool struct{}

// fakeTool is a minimal Tool stand-in for dispatch tests; ReadOnly is
// configurable and Execute sleeps a fixed duration so we can measure
// serial vs parallel behaviour by wall-clock.
type fakeTool struct {
	name     string
	readOnly bool
	// writesPaths mirrors the PathWriter contract of the tool being stood in
	// for: a double named write_file counts as one only if it says so, the
	// same way the shipped tool has to.
	writesPaths bool
	delay       time.Duration
	err         error
	calls       *int32 // shared counter to assert all dispatched
}

func (f fakeTool) Name() string { return f.name }

func (f fakeTool) Description() string { return "" }

func (f fakeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (f fakeTool) ReadOnly() bool { return f.readOnly }

func (f fakeTool) WritesNamedPaths() bool { return f.writesPaths }

func (f fakeTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	if f.calls != nil {
		atomic.AddInt32(f.calls, 1)
	}
	select {
	case <-time.After(f.delay):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if f.err != nil {
		return "", f.err
	}
	return f.name + " done", nil
}

func lastUser(req provider.Request) string {
	for _, v := range slices.Backward(req.Messages) {
		if v.Role == provider.RoleUser {
			return v.Content
		}
	}
	return ""
}

// mockProvider replays preset chunks and records the last request it received.
type mockProvider struct {
	name     string
	chunks   []provider.Chunk
	streams  [][]provider.Chunk
	lastReq  provider.Request
	requests []provider.Request
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	m.lastReq = req
	call := len(m.requests)
	m.requests = append(m.requests, req)
	chunks := m.chunks
	if len(m.streams) > 0 {
		if call >= len(m.streams) {
			call = len(m.streams) - 1
		}
		chunks = m.streams[call]
	}
	ch := make(chan provider.Chunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func newConsumingStreamProvider() *consumingStreamProvider {
	return &consumingStreamProvider{consumed: make(chan struct{})}
}

type readOnlyBoundaryProxy struct {
	resolved tool.ResolvedCall
}

func (p readOnlyBoundaryProxy) ResolveCall(context.Context, json.RawMessage) (tool.ResolvedCall, error) {
	return p.resolved, nil
}

type recordSink struct {
	mu       sync.Mutex
	evs      []event.Event
	recovery []event.ProtocolRecoveryAudit
}

func (s *recordSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evs = append(s.evs, e)
}

func (s *recordSink) kinds(k event.Kind) []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []event.Event
	for _, e := range s.evs {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return out
}

func (s *recordSink) RecordProtocolRecovery(a event.ProtocolRecoveryAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recovery = append(s.recovery, a)
}

type recordingWriter struct {
	name        string
	readOnly    bool
	calls       int
	writesPaths bool
}

func (r *recordingWriter) Name() string { return r.name }

func (r *recordingWriter) Description() string { return r.name }

func (r *recordingWriter) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
}

func (r *recordingWriter) ReadOnly() bool { return r.readOnly }

func (r *recordingWriter) WritesNamedPaths() bool { return r.writesPaths }

func (r *recordingWriter) Execute(context.Context, json.RawMessage) (string, error) {
	r.calls++
	return "ok", nil
}

// scriptedProvider replays a distinct chunk set per Stream call, so a multi-turn
// Run() sees tool calls on turn 1 and a plain final answer on turn 2.
type scriptedProvider struct {
	name     string
	turns    [][]provider.Chunk
	call     int
	requests []provider.Request
}

func (s *scriptedProvider) Name() string { return s.name }

func (s *scriptedProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	s.requests = append(s.requests, req)
	i := s.call
	if i >= len(s.turns) {
		i = len(s.turns) - 1
	}
	s.call++
	ch := make(chan provider.Chunk, len(s.turns[i]))
	for _, c := range s.turns[i] {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// stubGate denies any call whose tool name is in deny; everything else allows.
type stubGate struct {
	deny    map[string]bool
	checked []string
}

func (g *stubGate) Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (bool, string, error) {
	g.checked = append(g.checked, toolName)
	if g.deny[toolName] {
		return false, "denied by test policy", nil
	}
	return true, "", nil
}

func toolCallChunk(id, name, args string) provider.Chunk {
	return provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: args}}
}

func toolSchemaNames(schemas []provider.ToolSchema) []string {
	out := make([]string, 0, len(schemas))
	for _, s := range schemas {
		out = append(out, s.Name)
	}
	return out
}

// waitForQueuedAcquires blocks until exactly want acquires are parked in the
// queue, so a test can prove which one a freed slot goes to instead of racing
// the goroutines that ask for it.
func waitForQueuedAcquires(t *testing.T, s *writeclaim.SubagentScheduler, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s.Queued() == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("waited for %d queued acquires and never saw them", want)
}

// consumingStreamProvider hands over one harmless chunk and then nothing, ever.
// The channel is unbuffered, so the send returning is proof the run took that
// chunk: a waiter on `consumed` knows the run is inside the stream loop. A
// sleep only guesses, and on a loaded runner guesses short — cancelling before
// the loop is entered, which is not the path this test names.
type consumingStreamProvider struct{ consumed chan struct{} }

func (p *consumingStreamProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk)
	go func() {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "working"}
		close(p.consumed)
		// The channel is never closed and never written again: from here the
		// run is blocked on a read only cancellation can end.
	}()
	return ch, nil
}

type stubBash struct{}

type stubWrite struct{}

func (fakeReadFileTool) Name() string { return "read_file" }

func (fakeReadFileTool) Description() string { return "fake read" }

func (fakeReadFileTool) ReadOnly() bool { return true }

func (fakeReadFileTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (fakeReadFileTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "contents", nil
}

func (stubWrite) Name() string { return "write_file" }

func (stubWrite) Description() string { return "stub write" }

func (stubWrite) ReadOnly() bool { return false }

func (stubWrite) WritesNamedPaths() bool { return true }

func (stubWrite) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}

func (stubWrite) Execute(context.Context, json.RawMessage) (string, error) { return "wrote", nil }

func (stubBash) Name() string { return "bash" }

func (stubBash) Description() string { return "stub bash" }

func (stubBash) ReadOnly() bool { return false }

func (stubBash) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`)
}

func (stubBash) Execute(context.Context, json.RawMessage) (string, error) { return "ok", nil }

func (readOnlyBoundaryProxy) Name() string { return "use_capability" }

func (readOnlyBoundaryProxy) Description() string { return "" }

func (readOnlyBoundaryProxy) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (readOnlyBoundaryProxy) ReadOnly() bool { return true }

func (readOnlyBoundaryProxy) Execute(context.Context, json.RawMessage) (string, error) {
	return "proxy executed", nil
}

func (*consumingStreamProvider) Name() string { return "consuming-stream" }

type auditProbe2 struct {
	event.Sink
	got []hostaudit.DelegationAudit
}

func (p *auditProbe2) RecordDelegationAudit(a hostaudit.DelegationAudit) { p.got = append(p.got, a) }

// deepseekThinkingProvider marks a scripted provider as DeepSeek thinking mode
// (provider.ToolCallReasoningPolicy) — the scope within which a reasoning-only
// finish_reason="stop" turn is accepted as a final answer.
type deepseekThinkingProvider struct{ *scriptedProvider }

func (deepseekThinkingProvider) RequiresToolCallReasoning() bool { return true }

// echoTool is a trivial read-only tool used to drive a multi-step tool loop:
// each call appends an assistant(tool_call) + tool(result) pair to the history,
// growing the request prefix the way a real multi-turn session does.
type echoTool struct{}

func (echoTool) Name() string { return "echo" }

func (echoTool) Description() string { return "echo back the given text" }

func (echoTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)
}

func (echoTool) ReadOnly() bool { return true }

func (echoTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(args, &a)
	return "echoed: " + a.Text, nil
}

func newWorkspaceSignalSink() *workspaceSignalSink {
	return &workspaceSignalSink{mutations: make(chan event.WorkspaceMutation, 8)}
}

type recordingAsker struct {
	questions []event.AskQuestion
}

func (r *recordingAsker) Ask(_ context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error) {
	r.questions = questions
	return []event.AskAnswer{{QuestionID: "q1", Selected: []string{"Keep going"}}}, nil
}

type recoverySink struct {
	audits []event.ProtocolRecoveryAudit
}

func (s *recoverySink) Emit(event.Event) {}

func (s *recoverySink) RecordProtocolRecovery(a event.ProtocolRecoveryAudit) {
	s.audits = append(s.audits, a)
}

type workspaceSignalSink struct {
	mu        sync.Mutex
	events    []event.Event
	mutations chan event.WorkspaceMutation
}

func (s *workspaceSignalSink) Emit(e event.Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
}

func (s *workspaceSignalSink) RecordWorkspaceMutation(m event.WorkspaceMutation) {
	s.mutations <- m
}
