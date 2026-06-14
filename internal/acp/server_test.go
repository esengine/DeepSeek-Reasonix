package acp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

// --- fakes: a Factory wrapping a behavior-driven runner in a real Controller ---

// fakeRunner stands in for an agent.Runner; it emits to the session's sink and
// honors ctx cancellation, but runs no model.
type fakeRunner struct {
	sink     event.Sink
	behavior func(ctx context.Context, sink event.Sink, input string) error
}

func (r *fakeRunner) Run(ctx context.Context, input string) error {
	return r.behavior(ctx, r.sink, input)
}

// fakeFactory builds a real control.Controller around the fake runner, so the
// service exercises the actual controller surface (Run/Cancel/Close) it uses.
type fakeFactory struct {
	behavior func(ctx context.Context, sink event.Sink, input string) error
}

func (f *fakeFactory) NewSession(_ context.Context, p SessionParams) (*control.Controller, error) {
	runner := &fakeRunner{sink: p.Sink, behavior: f.behavior}
	return control.New(control.Options{Runner: runner, Sink: p.Sink}), nil
}

// --- a minimal JSON-RPC client over the wire, for integration tests ---

type frame struct {
	ID     *json.RawMessage `json:"id"`
	Method string           `json:"method"`
	Params json.RawMessage  `json:"params"`
	Result json.RawMessage  `json:"result"`
	Error  *rpcError        `json:"error"`
}

type rpcClient struct {
	enc *json.Encoder
	wmu sync.Mutex

	mu     sync.Mutex
	nextID int64
	waits  map[int64]chan frame

	notifs chan frame
	reqs   chan frame
}

func newRPCClient(in io.Writer, out io.Reader) *rpcClient {
	c := &rpcClient{
		enc:    json.NewEncoder(in),
		waits:  make(map[int64]chan frame),
		notifs: make(chan frame, 64),
		reqs:   make(chan frame, 16),
	}
	dec := json.NewDecoder(out)
	go func() {
		for {
			var f frame
			if err := dec.Decode(&f); err != nil {
				return
			}
			switch {
			case f.Method != "" && f.ID != nil:
				c.reqs <- f
			case f.Method != "" && f.ID == nil:
				c.notifs <- f
			case f.Method == "" && f.ID != nil:
				var id int64
				if json.Unmarshal(*f.ID, &id) != nil {
					continue
				}
				c.mu.Lock()
				ch := c.waits[id]
				delete(c.waits, id)
				c.mu.Unlock()
				if ch != nil {
					ch <- f
				}
			}
		}
	}()
	return c
}

func (c *rpcClient) send(v any) {
	c.wmu.Lock()
	_ = c.enc.Encode(v)
	c.wmu.Unlock()
}

func (c *rpcClient) callAsync(method string, params any) chan frame {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan frame, 1)
	c.waits[id] = ch
	c.mu.Unlock()
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	return ch
}

func (c *rpcClient) call(t *testing.T, method string, params any) frame {
	t.Helper()
	select {
	case f := <-c.callAsync(method, params):
		return f
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: timed out", method)
		return frame{}
	}
}

func (c *rpcClient) notify(method string, params any) {
	c.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *rpcClient) reply(id *json.RawMessage, result any) {
	c.send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func startServer(t *testing.T, factory Factory) (*rpcClient, func()) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan struct{})
	go func() {
		_ = Serve(context.Background(), inR, outW, factory, AgentInfo{Name: "reasonix-test", Version: "0"})
		close(done)
	}()
	client := newRPCClient(inW, outR)
	return client, func() {
		_ = inW.Close()
		<-done
		_ = outW.Close()
	}
}

// drainPrompt collects session/update notifications until the prompt's response
// arrives, then sweeps any notifications still buffered.
func drainPrompt(t *testing.T, c *rpcClient, promptCh chan frame) ([]frame, frame) {
	t.Helper()
	var notifs []frame
	var resp frame
	for {
		select {
		case f := <-c.notifs:
			notifs = append(notifs, f)
		case resp = <-promptCh:
			for {
				select {
				case f := <-c.notifs:
					notifs = append(notifs, f)
				default:
					return notifs, resp
				}
			}
		case <-time.After(2 * time.Second):
			t.Fatal("session/prompt: timed out")
		}
	}
}

func updateKind(t *testing.T, f frame) string {
	t.Helper()
	var p struct {
		Update struct {
			SessionUpdate string `json:"sessionUpdate"`
		} `json:"update"`
	}
	if err := json.Unmarshal(f.Params, &p); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	return p.Update.SessionUpdate
}

// --- tests ---

func TestServeLifecycle(t *testing.T) {
	factory := &fakeFactory{behavior: func(_ context.Context, sink event.Sink, input string) error {
		sink.Emit(event.Event{Kind: event.Text, Text: "hi " + input})
		sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "c1", Name: "ls", Args: `{}`}})
		sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "c1", Name: "ls", Output: "file.go"}})
		return nil
	}}
	client, stop := startServer(t, factory)
	defer stop()

	initResp := client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	var ir InitializeResult
	if err := json.Unmarshal(initResp.Result, &ir); err != nil {
		t.Fatalf("initialize result: %v", err)
	}
	if ir.ProtocolVersion != ProtocolVersion {
		t.Errorf("protocolVersion = %d, want %d", ir.ProtocolVersion, ProtocolVersion)
	}
	if !ir.AgentCapabilities.PromptCapabilities.EmbeddedContext {
		t.Errorf("embeddedContext should be advertised")
	}
	if ir.AgentCapabilities.SessionCapabilities.List == nil ||
		ir.AgentCapabilities.SessionCapabilities.Resume == nil ||
		ir.AgentCapabilities.SessionCapabilities.Close == nil ||
		ir.AgentCapabilities.SessionCapabilities.Delete == nil {
		t.Errorf("sessionCapabilities = %+v, want list/resume/close/delete", ir.AgentCapabilities.SessionCapabilities)
	}
	if ir.AgentCapabilities.PromptCapabilities.Image {
		t.Errorf("image must not be advertised")
	}

	newResp := client.call(t, "session/new", SessionNewParams{Cwd: t.TempDir()})
	var nr SessionNewResult
	if err := json.Unmarshal(newResp.Result, &nr); err != nil || nr.SessionID == "" {
		t.Fatalf("session/new result: %v (%q)", err, nr.SessionID)
	}

	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "there"}},
	})
	notifs, resp := drainPrompt(t, client, promptCh)

	kinds := map[string]bool{}
	for _, n := range notifs {
		kinds[updateKind(t, n)] = true
	}
	for _, want := range []string{"agent_message_chunk", "tool_call", "tool_call_update"} {
		if !kinds[want] {
			t.Errorf("missing %s update; saw %v", want, kinds)
		}
	}
	var pr SessionPromptResult
	if err := json.Unmarshal(resp.Result, &pr); err != nil {
		t.Fatalf("prompt result: %v", err)
	}
	if pr.StopReason != StopEndTurn {
		t.Errorf("stopReason = %q, want %q", pr.StopReason, StopEndTurn)
	}
}

func TestServeCancel(t *testing.T) {
	started := make(chan struct{})
	factory := &fakeFactory{behavior: func(ctx context.Context, _ event.Sink, _ string) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{})
	var nr SessionNewResult
	json.Unmarshal(newResp.Result, &nr)

	promptCh := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "loop"}},
	})

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("prompt never started")
	}
	client.notify("session/cancel", SessionCancelParams(nr))

	select {
	case resp := <-promptCh:
		var pr SessionPromptResult
		json.Unmarshal(resp.Result, &pr)
		if pr.StopReason != StopCancelled {
			t.Errorf("stopReason = %q, want cancelled", pr.StopReason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not end the prompt")
	}
}

func TestServeRejectsConcurrentPromptForSameSession(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	factory := &fakeFactory{behavior: func(_ context.Context, _ event.Sink, _ string) error {
		once.Do(func() { close(started) })
		<-release
		return nil
	}}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{})
	var nr SessionNewResult
	json.Unmarshal(newResp.Result, &nr)

	first := client.callAsync("session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "first"}},
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first prompt never started")
	}

	second := client.call(t, "session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "second"}},
	})
	if second.Error == nil {
		t.Fatal("second concurrent prompt should return an error")
	}
	if second.Error.Code != ErrInvalidRequest || !strings.Contains(second.Error.Message, "active prompt") {
		t.Fatalf("second prompt error = %+v, want active-prompt invalid request", second.Error)
	}

	close(release)
	select {
	case resp := <-first:
		if resp.Error != nil {
			t.Fatalf("first prompt errored: %+v", resp.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first prompt did not finish")
	}
}

func TestServeSessionClose(t *testing.T) {
	factory := &fakeFactory{behavior: func(context.Context, event.Sink, string) error { return nil }}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	newResp := client.call(t, "session/new", SessionNewParams{})
	var nr SessionNewResult
	json.Unmarshal(newResp.Result, &nr)

	closeResp := client.call(t, "session/close", SessionCloseParams(nr))
	if closeResp.Error != nil {
		t.Fatalf("session/close errored: %+v", closeResp.Error)
	}

	promptResp := client.call(t, "session/prompt", SessionPromptParams{
		SessionID: nr.SessionID,
		Prompt:    []ContentBlock{{Type: "text", Text: "after close"}},
	})
	if promptResp.Error == nil || !strings.Contains(promptResp.Error.Message, "unknown session") {
		t.Fatalf("prompt after close error = %+v, want unknown session", promptResp.Error)
	}
}

func TestServeRejectsPathLikeSessionID(t *testing.T) {
	factory := &fakeFactory{behavior: func(context.Context, event.Sink, string) error { return nil }}
	client, stop := startServer(t, factory)
	defer stop()

	resp := client.call(t, "session/delete", SessionDeleteParams{SessionID: "../outside"})
	if resp.Error == nil {
		t.Fatal("session/delete with path-like sessionId should fail")
	}
	if resp.Error.Code != ErrInvalidParams || !strings.Contains(resp.Error.Message, "invalid sessionId") {
		t.Fatalf("session/delete error = %+v, want invalid sessionId", resp.Error)
	}
}

func TestDeleteSessionFilesDeletesOwnedSubagents(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte(`{"role":"user","content":"hi"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ref := "sa_20260102_030405_000000000_aabbccddeeff"
	writeACPSubagentArtifact(t, dir, ref, agent.BranchID(sessionPath))

	if err := deleteSessionFiles(sessionPath); err != nil {
		t.Fatalf("deleteSessionFiles: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "subagents", ref+".jsonl")); !os.IsNotExist(err) {
		t.Fatalf("subagent jsonl should be deleted, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "subagents", ref+".meta.json")); !os.IsNotExist(err) {
		t.Fatalf("subagent meta should be deleted, stat err = %v", err)
	}
}

func writeACPSubagentArtifact(t *testing.T, dir, ref, parentSession string) {
	t.Helper()
	subagentDir := filepath.Join(dir, "subagents")
	if err := os.MkdirAll(subagentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subagentDir, ref+".jsonl"), []byte(`{"role":"user","content":"sub"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(agent.SubagentMeta{
		Ref:           ref,
		Status:        agent.SubagentCompleted,
		Kind:          "task",
		Name:          "task",
		ParentSession: parentSession,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subagentDir, ref+".meta.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestServeUnknownMethod(t *testing.T) {
	factory := &fakeFactory{behavior: func(context.Context, event.Sink, string) error { return nil }}
	client, stop := startServer(t, factory)
	defer stop()

	resp := client.call(t, "does/not/exist", nil)
	if resp.Error == nil {
		t.Fatal("expected an error response")
	}
	if resp.Error.Code != ErrMethodNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, ErrMethodNotFound)
	}
}
