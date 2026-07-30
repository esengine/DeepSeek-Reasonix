package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
)

// scriptedRequester answers agent → client requests from a per-method script
// and records the call order.
type scriptedRequester struct {
	mu      sync.Mutex
	calls   []string
	params  map[string]json.RawMessage
	results map[string]any
	errs    map[string]error
}

func newScriptedRequester() *scriptedRequester {
	return &scriptedRequester{
		params:  map[string]json.RawMessage{},
		results: map[string]any{},
		errs:    map[string]error{},
	}
}

func (r *scriptedRequester) Request(_ context.Context, method string, params any) (json.RawMessage, error) {
	raw, _ := json.Marshal(params)
	r.mu.Lock()
	r.calls = append(r.calls, method)
	r.params[method] = raw
	res, err := r.results[method], r.errs[method]
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	out, _ := json.Marshal(res)
	return out, nil
}

func (r *scriptedRequester) callOrder() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func TestClientIOReadWriteTextFile(t *testing.T) {
	req := newScriptedRequester()
	req.results["fs/read_text_file"] = FSReadTextFileResult{Content: "buffer content"}
	req.results["fs/write_text_file"] = struct{}{}
	io := newClientIO(req, "sess-1", ClientCapabilities{FS: FSCapabilities{ReadTextFile: true, WriteTextFile: true}})

	content, ok := io.ReadTextFile(context.Background(), "/proj/a.go")
	if !ok || content != "buffer content" {
		t.Fatalf("ReadTextFile = %q, %v; want buffer content, true", content, ok)
	}
	var readParams FSReadTextFileParams
	json.Unmarshal(req.params["fs/read_text_file"], &readParams)
	if readParams.SessionID != "sess-1" || readParams.Path != "/proj/a.go" {
		t.Fatalf("fs/read_text_file params = %+v", readParams)
	}

	handled, err := io.WriteTextFile(context.Background(), "/proj/a.go", "new content")
	if !handled || err != nil {
		t.Fatalf("WriteTextFile = %v, %v; want true, nil", handled, err)
	}

	// Without the capability, both degrade to unhandled so tools use the disk.
	none := newClientIO(req, "sess-1", ClientCapabilities{})
	if _, ok := none.ReadTextFile(context.Background(), "/proj/a.go"); ok {
		t.Fatal("ReadTextFile without capability must report ok=false")
	}
	if handled, _ := none.WriteTextFile(context.Background(), "/proj/a.go", "x"); handled {
		t.Fatal("WriteTextFile without capability must report handled=false")
	}

	// A client read error falls back (ok=false); a client write error surfaces
	// (falling back could double-apply).
	req.errs["fs/read_text_file"] = fmt.Errorf("not open")
	if _, ok := io.ReadTextFile(context.Background(), "/proj/a.go"); ok {
		t.Fatal("ReadTextFile client error must report ok=false")
	}
	req.errs["fs/write_text_file"] = fmt.Errorf("readonly buffer")
	handled, err = io.WriteTextFile(context.Background(), "/proj/a.go", "x")
	if !handled || err == nil {
		t.Fatalf("WriteTextFile client error = %v, %v; want handled=true with error", handled, err)
	}
}

func TestClientIORunCommandLifecycle(t *testing.T) {
	req := newScriptedRequester()
	req.results["terminal/create"] = TerminalCreateResult{TerminalID: "term-1"}
	req.results["terminal/wait_for_exit"] = TerminalWaitResult{}
	exitZero := 0
	req.results["terminal/output"] = TerminalOutputResult{Output: "hello from client", ExitStatus: &TerminalExitStatus{ExitCode: &exitZero}}
	req.results["terminal/release"] = struct{}{}
	io := newClientIO(req, "sess-1", ClientCapabilities{Terminal: true})

	out, ok, err := io.RunCommand(context.Background(), "echo hello", "/proj", time.Minute)
	if !ok || err != nil || out != "hello from client" {
		t.Fatalf("RunCommand = %q, %v, %v", out, ok, err)
	}
	order := io2str(req.callOrder())
	if order != "terminal/create,terminal/wait_for_exit,terminal/output,terminal/release" {
		t.Fatalf("call order = %s", order)
	}

	// A nonzero exit surfaces as an error alongside the captured output.
	exitOne := 1
	req.results["terminal/output"] = TerminalOutputResult{Output: "boom", ExitStatus: &TerminalExitStatus{ExitCode: &exitOne}}
	out, ok, err = io.RunCommand(context.Background(), "false", "/proj", time.Minute)
	if !ok || err == nil || !strings.Contains(err.Error(), "exit status 1") || out != "boom" {
		t.Fatalf("RunCommand nonzero exit = %q, %v, %v", out, ok, err)
	}

	// No terminal capability → unhandled, local bash runs instead.
	none := newClientIO(req, "sess-1", ClientCapabilities{})
	if _, ok, _ := none.RunCommand(context.Background(), "echo", "/proj", time.Minute); ok {
		t.Fatal("RunCommand without capability must report ok=false")
	}

	// terminal/create failure degrades to unhandled rather than failing the call.
	req.errs["terminal/create"] = fmt.Errorf("client rejected")
	if _, ok, _ := io.RunCommand(context.Background(), "echo", "/proj", time.Minute); ok {
		t.Fatal("RunCommand with failed create must report ok=false")
	}
}

func io2str(calls []string) string { return strings.Join(calls, ",") }

func TestUpdateSinkToolLocations(t *testing.T) {
	s := newUpdateSink(&fakeNotifier{}, "sess")
	cwd := t.TempDir()
	s.bindCwd(cwd)

	locs := s.toolLocations("read_file", `{"path":"pkg/a.go","offset":41}`)
	if len(locs) != 1 || locs[0].Path != filepath.Join(cwd, "pkg", "a.go") {
		t.Fatalf("read_file locations = %+v", locs)
	}
	if locs[0].Line == nil || *locs[0].Line != 42 {
		t.Fatalf("read_file line = %v, want 42 (offset is 0-based)", locs[0].Line)
	}
	// A platform-absolute path passes through untouched.
	abs := filepath.Join(t.TempDir(), "b.go")
	absArgs, _ := json.Marshal(map[string]string{"path": abs})
	if locs := s.toolLocations("edit_file", string(absArgs)); len(locs) != 1 || locs[0].Path != abs || locs[0].Line != nil {
		t.Fatalf("edit_file locations = %+v, want %s", locs, abs)
	}
	if locs := s.toolLocations("bash", `{"command":"ls"}`); locs != nil {
		t.Fatalf("bash should have no locations, got %+v", locs)
	}
	if locs := s.toolLocations("grep", `{"path":"pkg","pattern":"x"}`); locs != nil {
		t.Fatalf("grep (directory scope) should have no locations, got %+v", locs)
	}
	if locs := s.toolLocations("read_file", `{"offset":1}`); locs != nil {
		t.Fatalf("path-less args should have no locations, got %+v", locs)
	}
}

func TestPlanEntriesFromTodoArgs(t *testing.T) {
	entries, ok := planEntriesFromTodoArgs(`{"todos":[
		{"content":"Phase one","status":"in_progress","level":0},
		{"content":"Sub step","status":"pending","level":1},
		{"content":"Done step","status":"completed"},
		{"content":"","status":"pending"},
		{"content":"Weird","status":"???"}
	]}`)
	if !ok || len(entries) != 4 {
		t.Fatalf("entries = %+v, ok=%v; want 4 entries", entries, ok)
	}
	if entries[0].Priority != "high" || entries[0].Status != "in_progress" {
		t.Fatalf("phase entry = %+v", entries[0])
	}
	if entries[1].Priority != "medium" {
		t.Fatalf("sub-step entry = %+v", entries[1])
	}
	if entries[3].Status != "pending" {
		t.Fatalf("unknown status must degrade to pending, got %+v", entries[3])
	}
	if _, ok := planEntriesFromTodoArgs(`{"todos":[]}`); ok {
		t.Fatal("empty todos must not produce a plan update")
	}
	if _, ok := planEntriesFromTodoArgs(`not json`); ok {
		t.Fatal("malformed args must not produce a plan update")
	}
}

func TestUpdateSinkEmitsPlanForTodoWrite(t *testing.T) {
	n := &fakeNotifier{}
	s := newUpdateSink(n, "sess-1")
	s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
		ID: "t1", Name: "todo_write", Args: `{"todos":[{"content":"Do it","status":"pending"}]}`,
	}})
	// The plan update precedes the tool_call for the same dispatch.
	plan := n.updateMap(t, 0)
	if plan["sessionUpdate"] != "plan" {
		t.Fatalf("first update = %v, want plan", plan["sessionUpdate"])
	}
	raw, _ := json.Marshal(plan)
	if !strings.Contains(string(raw), `"content":"Do it"`) {
		t.Fatalf("plan update missing entry: %s", raw)
	}
	call := n.updateMap(t, 1)
	if call["sessionUpdate"] != "tool_call" {
		t.Fatalf("second update = %v, want tool_call", call["sessionUpdate"])
	}
}

// recordingFactory wraps e2eFactory and captures the SessionParams the service
// hands to NewSession, so tests can assert the client-capability wiring.
type recordingFactory struct {
	inner  *e2eFactory
	mu     sync.Mutex
	params []SessionParams
}

func (f *recordingFactory) SessionDir() string { return f.inner.SessionDir() }

func (f *recordingFactory) NewSession(ctx context.Context, p SessionParams) (*control.Controller, error) {
	f.mu.Lock()
	f.params = append(f.params, p)
	f.mu.Unlock()
	return f.inner.NewSession(ctx, p)
}

func (f *recordingFactory) last() SessionParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.params[len(f.params)-1]
}

func TestSessionParamsCarryClientIOFromInitializeCaps(t *testing.T) {
	dir := t.TempDir()
	prov := &scriptedProvider{name: "fake", responses: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	factory := &recordingFactory{inner: &e2eFactory{
		prov:       prov,
		tool:       fakeTool{name: "peek", ro: true, out: "ok"},
		policy:     permission.New("ask", nil, nil, nil),
		sessionDir: dir,
	}}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{
		ProtocolVersion: 1,
		ClientCapabilities: ClientCapabilities{
			FS:       FSCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal: true,
		},
	})
	client.call(t, "session/new", SessionNewParams{Cwd: t.TempDir()})
	p := factory.last()
	if p.FileOverlay == nil {
		t.Fatal("SessionParams.FileOverlay should be bound when the client declares fs capabilities")
	}
	if p.Terminal == nil {
		t.Fatal("SessionParams.Terminal should be bound when the client declares the terminal capability")
	}
}

func TestSessionParamsNilClientIOWithoutCaps(t *testing.T) {
	dir := t.TempDir()
	prov := &scriptedProvider{name: "fake", responses: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	factory := &recordingFactory{inner: &e2eFactory{
		prov:       prov,
		tool:       fakeTool{name: "peek", ro: true, out: "ok"},
		policy:     permission.New("ask", nil, nil, nil),
		sessionDir: dir,
	}}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	client.call(t, "session/new", SessionNewParams{Cwd: t.TempDir()})
	p := factory.last()
	if p.FileOverlay != nil || p.Terminal != nil {
		t.Fatalf("SessionParams overlay/terminal must stay nil without client capabilities; got %v / %v", p.FileOverlay, p.Terminal)
	}
}

func TestE2ESessionModes(t *testing.T) {
	dir := t.TempDir()
	prov := &scriptedProvider{name: "fake", responses: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	factory := &e2eFactory{
		prov:       prov,
		tool:       fakeTool{name: "peek", ro: true, out: "ok"},
		policy:     permission.New("ask", nil, nil, nil),
		sessionDir: dir,
	}
	client, stop := startServer(t, factory)
	defer stop()

	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	cwd := t.TempDir()
	resp := client.call(t, "session/new", SessionNewParams{Cwd: cwd})
	var nr SessionNewResult
	if err := json.Unmarshal(resp.Result, &nr); err != nil {
		t.Fatalf("session/new result: %v", err)
	}
	if nr.Modes == nil || nr.Modes.CurrentModeID != sessionModeNormal || len(nr.Modes.AvailableModes) != 3 {
		t.Fatalf("session/new modes = %+v, want normal current with 3 available", nr.Modes)
	}

	setResp := client.call(t, "session/set_mode", SessionSetModeParams{SessionID: nr.SessionID, ModeID: sessionModePlan})
	if setResp.Error != nil {
		t.Fatalf("session/set_mode: %+v", setResp.Error)
	}
	// The switch is confirmed with a current_mode_update notification.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case n := <-client.notifs:
			var p struct {
				Update struct {
					SessionUpdate string `json:"sessionUpdate"`
					CurrentModeID string `json:"currentModeId"`
				} `json:"update"`
			}
			if json.Unmarshal(n.Params, &p) == nil && p.Update.SessionUpdate == "current_mode_update" {
				if p.Update.CurrentModeID != sessionModePlan {
					t.Fatalf("current_mode_update = %q, want plan", p.Update.CurrentModeID)
				}
				goto unknownMode
			}
		case <-deadline:
			t.Fatal("no current_mode_update notification after session/set_mode")
		}
	}
unknownMode:
	bad := client.call(t, "session/set_mode", SessionSetModeParams{SessionID: nr.SessionID, ModeID: "yolo"})
	if bad.Error == nil {
		t.Fatal("unknown modeId must be rejected")
	}

	// Reconnecting to the live session must report its actual mode, not a
	// hardcoded default — the mode picker would otherwise go stale.
	loadResp := client.call(t, "session/load", SessionLoadParams{SessionID: nr.SessionID, Cwd: cwd})
	if loadResp.Error != nil {
		t.Fatalf("session/load: %+v", loadResp.Error)
	}
	var lr SessionLoadResult
	if err := json.Unmarshal(loadResp.Result, &lr); err != nil {
		t.Fatalf("session/load result: %v", err)
	}
	if lr.Modes == nil || lr.Modes.CurrentModeID != sessionModePlan {
		t.Fatalf("session/load modes = %+v, want current plan for the live session", lr.Modes)
	}
}

// TestRebuildSessionKeepsClientIOAndMode pins two rebuild invariants: a
// model/effort switch must rebuild the controller with the same client
// capability wiring (fs overlay, host terminal) the original had, and must
// re-apply the session's ACP mode — a fresh controller boots with normal
// switches, which would silently drop a user-selected plan mode.
func TestRebuildSessionKeepsClientIOAndMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess-rebuild.jsonl")
	base := agent.NewSession("sys prompt")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	if err := base.Save(path); err != nil {
		t.Fatalf("save session: %v", err)
	}

	sink := newUpdateSink(&fakeNotifier{}, "sess-rebuild")
	sess := &acpSession{
		id:         "sess-rebuild",
		sink:       sink,
		cwd:        dir,
		model:      "fast",
		transcript: path,
		modeID:     sessionModePlan,
	}
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("acquire session lease: %v", err)
	}
	sess.lease = lease
	t.Cleanup(sess.releaseSessionLease)

	factory := &configurableFactory{dir: dir}
	svc := &service{
		factory:  factory,
		sessions: map[string]*acpSession{sess.id: sess},
		clientCaps: ClientCapabilities{
			FS:       FSCapabilities{ReadTextFile: true, WriteTextFile: true},
			Terminal: true,
		},
	}
	oldCtrl := control.New(control.Options{
		Executor:    agent.New(nil, nil, base, agent.Options{}, event.Discard),
		SessionDir:  dir,
		SessionPath: path,
		Label:       "fast",
	})
	sess.ctrl = oldCtrl

	if err := svc.rebuildSession(context.Background(), sess, SessionConfigState{Model: "pro"}, []sessionConfigDelta{{axis: "model", model: "pro"}}); err != nil {
		t.Fatalf("rebuildSession: %v", err)
	}
	if sess.ctrl == oldCtrl {
		t.Fatal("session controller was not replaced")
	}

	factory.mu.Lock()
	last := factory.builds[len(factory.builds)-1]
	factory.mu.Unlock()
	if last.FileOverlay == nil {
		t.Fatal("rebuild must keep the fs overlay wiring")
	}
	if last.Terminal == nil {
		t.Fatal("rebuild must keep the host terminal wiring")
	}
	if !sess.ctrl.PlanMode() {
		t.Fatal("rebuild must re-apply the session's plan mode to the new controller")
	}
}
