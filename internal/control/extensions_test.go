package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// Stage 6b1 control wiring tests. The dispatcher under test is real; only its
// sidecar client is faked, so every assertion exercises the actual dispatch
// ruling logic (chain walk, strict replacement decode, slot ownership).

type recordedExtCall struct {
	event   protocol.InterceptEvent
	payload json.RawMessage
}

// fakeExtClient is a scriptable dispatch.Client recording every call.
type fakeExtClient struct {
	mu          sync.Mutex
	interceptFn func(event protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error)
	intercepts  []recordedExtCall
	notifies    []recordedExtCall
}

func (f *fakeExtClient) Intercept(_ context.Context, event protocol.InterceptEvent, payload json.RawMessage, _ time.Duration) (protocol.InterceptResult, error) {
	f.mu.Lock()
	f.intercepts = append(f.intercepts, recordedExtCall{event: event, payload: append(json.RawMessage(nil), payload...)})
	fn := f.interceptFn
	f.mu.Unlock()
	if fn == nil {
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}
	return fn(event, payload)
}

func (f *fakeExtClient) TryNotifyEvent(event protocol.InterceptEvent, payload json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notifies = append(f.notifies, recordedExtCall{event: event, payload: append(json.RawMessage(nil), payload...)})
	return nil
}

func (f *fakeExtClient) notifyEvents() []protocol.InterceptEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]protocol.InterceptEvent, len(f.notifies))
	for i, call := range f.notifies {
		out[i] = call.event
	}
	return out
}

func (f *fakeExtClient) notifyPayloadsFor(event protocol.InterceptEvent) []json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []json.RawMessage
	for _, call := range f.notifies {
		if call.event == event {
			out = append(out, call.payload)
		}
	}
	return out
}

const extensionTestPlugin = "fake"

// newExtensionTestDispatcher builds a dispatcher whose chain lists the fake
// plugin at every given point and whose slots (slot → plugin ID) are owned as
// given. The fake is optional-class unless it owns a slot.
func newExtensionTestDispatcher(client dispatch.Client, points []extension.InterceptorPoint, slots map[extension.Slot]string) *dispatch.Dispatcher {
	chain := map[extension.InterceptorPoint][]extension.Contribution{}
	for _, point := range points {
		chain[point] = []extension.Contribution{{
			Kind:   extension.KindInterceptor,
			ID:     string(point),
			Source: extension.ContributionSource{Scope: extension.ScopePlugin, PluginID: extensionTestPlugin},
		}}
	}
	replacements := map[extension.Slot]extension.ContributionSource{}
	for slot, plugin := range slots {
		replacements[slot] = extension.ContributionSource{Scope: extension.ScopePlugin, PluginID: plugin}
	}
	return dispatch.New(chain, replacements, func(string) dispatch.Client { return client }, nil, dispatch.Options{})
}

var sessionPoints = []extension.InterceptorPoint{
	extension.PointSessionStart, extension.PointSessionEnd, extension.PointSessionLoad,
	extension.PointSessionSave, extension.PointSessionRotate,
}

// recordingSink captures emitted events.
type recordingSink struct {
	mu     sync.Mutex
	events []event.Event
}

func (s *recordingSink) Emit(ev event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *recordingSink) all() []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]event.Event(nil), s.events...)
}

func runTestTurn(c *Controller, input string) error {
	return newTurnOrchestrator(c).runTurnWithRawDisplay(context.Background(), input, input, "")
}

func TestInputReceiveContinue(t *testing.T) {
	client := &fakeExtClient{}
	d := newExtensionTestDispatcher(client, []extension.InterceptorPoint{extension.PointInputReceive}, nil)
	runner := &fakeTurnRunner{}
	c := New(Options{Runner: runner, Extensions: d})

	if err := runTestTurn(c, "hello world"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 || !strings.Contains(runner.inputs[0], "hello world") {
		t.Fatalf("runner inputs = %v, want the composed turn", runner.inputs)
	}
	if len(client.intercepts) != 1 || client.intercepts[0].event != protocol.EventInputReceive {
		t.Fatalf("intercepts = %+v, want exactly one input.receive", client.intercepts)
	}
	if !strings.Contains(string(client.intercepts[0].payload), "hello world") {
		t.Fatalf("intercept payload = %s, want the composed text", client.intercepts[0].payload)
	}
}

func TestInputReceiveReplace(t *testing.T) {
	client := &fakeExtClient{
		interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{Decision: protocol.DecisionReplace, Replacement: json.RawMessage(`{"text":"rewritten input"}`)}, nil
		},
	}
	d := newExtensionTestDispatcher(client, []extension.InterceptorPoint{extension.PointInputReceive}, nil)
	runner := &fakeTurnRunner{}
	c := New(Options{Runner: runner, Extensions: d})

	if err := runTestTurn(c, "original"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 || runner.inputs[0] != "rewritten input" {
		t.Fatalf("runner inputs = %v, want the replaced text only", runner.inputs)
	}
	if !strings.Contains(string(client.intercepts[0].payload), "original") {
		t.Fatalf("intercept payload = %s, want the pre-replacement text", client.intercepts[0].payload)
	}
}

func TestInputReceiveBlock(t *testing.T) {
	client := &fakeExtClient{
		interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{Decision: protocol.DecisionBlock, Reason: "api_key=sk-SECRET refused"}, nil
		},
	}
	d := newExtensionTestDispatcher(client, []extension.InterceptorPoint{extension.PointInputReceive}, nil)
	runner := &fakeTurnRunner{}
	sink := &recordingSink{}
	c := New(Options{Runner: runner, Sink: sink, Extensions: d})

	if err := runTestTurn(c, "do something"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 0 {
		t.Fatalf("blocked turn reached the runner: %v", runner.inputs)
	}
	var notice *event.Event
	for i, ev := range sink.all() {
		if ev.Kind == event.Notice {
			notice = &sink.all()[i]
		}
	}
	if notice == nil {
		t.Fatal("blocked turn surfaced no notice")
	}
	if strings.Contains(notice.Detail, "sk-SECRET") {
		t.Fatalf("block reason was not credential-redacted: %q", notice.Detail)
	}
	if !strings.Contains(notice.Detail, "refused") {
		t.Fatalf("block reason detail = %q, want the extension's reason", notice.Detail)
	}
}

func TestInputReceiveNilDispatcherUntouched(t *testing.T) {
	runner := &fakeTurnRunner{}
	c := New(Options{Runner: runner})
	if sinkHasFrontendWrapper(c.sink) {
		t.Fatal("sink wrapped without a dispatcher — the nil fast path must stay unwrapped")
	}
	if err := runTestTurn(c, "plain"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 {
		t.Fatalf("runner inputs = %v, want 1", runner.inputs)
	}
}

// TestInputReceiveInterceptedOnHeadlessRun pins the shared seam: the
// synchronous headless Run path composes input outside the turn orchestrator
// and must cross the same input.receive chain.
func TestInputReceiveInterceptedOnHeadlessRun(t *testing.T) {
	client := &fakeExtClient{
		interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{Decision: protocol.DecisionReplace, Replacement: json.RawMessage(`{"text":"headless rewritten"}`)}, nil
		},
	}
	d := newExtensionTestDispatcher(client, []extension.InterceptorPoint{extension.PointInputReceive}, nil)
	runner := &fakeTurnRunner{}
	c := New(Options{Runner: runner, Extensions: d})

	if err := c.Run(context.Background(), "original"); err != nil {
		t.Fatal(err)
	}
	if len(runner.inputs) != 1 || runner.inputs[0] != "headless rewritten" {
		t.Fatalf("runner inputs = %v, want the replaced headless input", runner.inputs)
	}
}

func TestSetExtensionsInstallsDispatcher(t *testing.T) {
	client := &fakeExtClient{}
	d := newExtensionTestDispatcher(client, []extension.InterceptorPoint{extension.PointInputReceive}, nil)
	runner := &fakeTurnRunner{}
	c := New(Options{Runner: runner})

	c.SetExtensions(nil) // no-op
	if _, wrapped := c.sink.(*frontendEventSink); wrapped {
		t.Fatal("SetExtensions(nil) wrapped the sink")
	}
	c.SetExtensions(d)
	// Durable inbox observation sits outside the frontend wrapper.
	if !sinkHasFrontendWrapper(c.sink) {
		t.Fatal("SetExtensions did not wrap the sink")
	}
	// The first install wins; a later SetExtensions is ignored.
	c.SetExtensions(newExtensionTestDispatcher(&fakeExtClient{}, nil, nil))
	if c.extensions != d {
		t.Fatal("SetExtensions swapped an installed dispatcher")
	}
	// ReplaceExtensions is the generation-safe rebuild path.
	client2 := &fakeExtClient{}
	d2 := newExtensionTestDispatcher(client2, []extension.InterceptorPoint{extension.PointInputReceive}, nil)
	c.ReplaceExtensions(d2)
	if c.extensions != d2 {
		t.Fatal("ReplaceExtensions did not swap dispatcher")
	}
	if err := runTestTurn(c, "hello"); err != nil {
		t.Fatal(err)
	}
	if len(client.intercepts) != 0 {
		t.Fatalf("old dispatcher still fired: %d", len(client.intercepts))
	}
	if len(client2.intercepts) != 1 {
		t.Fatalf("intercepts = %d, want the replaced dispatcher to fire once", len(client2.intercepts))
	}
}

// newSessionController builds a controller with a real executor session and
// session file so lifecycle points have something to save/load/rotate.
func newSessionController(t *testing.T, d *dispatch.Dispatcher, sink event.Sink) (*Controller, string) {
	t.Helper()
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	exec := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	path := filepath.Join(dir, "s.jsonl")
	opts := Options{Runner: &fakeTurnRunner{}, Executor: exec, SessionDir: dir, SessionPath: path, Extensions: d}
	if sink != nil {
		opts.Sink = sink
	}
	return New(opts), path
}

func TestSessionEventsFireAtLifecyclePoints(t *testing.T) {
	client := &fakeExtClient{}
	d := newExtensionTestDispatcher(client, sessionPoints, nil)
	c, path := newSessionController(t, d, nil)

	if err := runTestTurn(c, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := c.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	loaded := agent.NewSession("sys2")
	c.Resume(loaded, filepath.Join(filepath.Dir(path), "other.jsonl"))
	if err := c.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	c.Close()

	want := []protocol.InterceptEvent{
		protocol.EventSessionStart,  // first turn
		protocol.EventSessionSave,   // Snapshot
		protocol.EventSessionLoad,   // Resume
		protocol.EventSessionRotate, // NewSession
		protocol.EventSessionEnd,    // NewSession retiring the old session
		protocol.EventSessionStart,  // NewSession's fresh session
		protocol.EventSessionEnd,    // Close
	}
	got := client.notifyEvents()
	if len(got) != len(want) {
		t.Fatalf("session notify events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("session notify events = %v, want %v", got, want)
		}
	}
	// The save event carries the phase payload: the session file and phase.
	// Compare typed fields — a Windows path contains backslashes, which JSON
	// escapes, so a raw-substring match on the payload would miss it.
	payloads := client.notifyPayloadsFor(protocol.EventSessionSave)
	if len(payloads) != 1 {
		t.Fatalf("session.save payloads = %v, want exactly one", payloads)
	}
	var savePayload dispatch.SessionPayload
	if err := json.Unmarshal(payloads[0], &savePayload); err != nil {
		t.Fatalf("session.save payload does not decode: %v (%s)", err, payloads[0])
	}
	if savePayload.Phase != "save" || savePayload.SessionPath != path {
		t.Fatalf("session.save payload = %+v, want phase=save path=%q", savePayload, path)
	}
}

func TestSessionSaveStrategyVeto(t *testing.T) {
	client := &fakeExtClient{
		interceptFn: func(event protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
			if event == protocol.EventSessionSave {
				return protocol.InterceptResult{Decision: protocol.DecisionBlock, Reason: "no saves today"}, nil
			}
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		},
	}
	d := newExtensionTestDispatcher(client, sessionPoints, map[extension.Slot]string{extension.SlotSessionPolicy: extensionTestPlugin})
	c, path := newSessionController(t, d, nil)

	err := c.Snapshot()
	if err == nil {
		t.Fatal("Snapshot succeeded with a blocking session_policy owner")
	}
	var blockErr *dispatch.BlockError
	if !errors.As(err, &blockErr) {
		t.Fatalf("Snapshot error = %v, want a dispatch.BlockError", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("vetoed save still wrote %s", path)
	}
	if n := len(client.notifyPayloadsFor(protocol.EventSessionSave)); n != 0 {
		t.Fatalf("vetoed save broadcast %d events, want none", n)
	}
}

func TestSessionStrategyAdjustsObservedPayload(t *testing.T) {
	client := &fakeExtClient{
		interceptFn: func(event protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
			if event == protocol.EventSessionSave {
				return protocol.InterceptResult{Decision: protocol.DecisionReplace,
					Replacement: json.RawMessage(`{"sessionPath":"/adjusted.jsonl","phase":"save"}`)}, nil
			}
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		},
	}
	d := newExtensionTestDispatcher(client, sessionPoints, map[extension.Slot]string{extension.SlotSessionPolicy: extensionTestPlugin})
	c, path := newSessionController(t, d, nil)

	if err := c.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// Host-side decision unchanged: the transcript lands on the original path.
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("save did not write the original path: %v", statErr)
	}
	// Observers receive the owner-adjusted payload.
	payloads := client.notifyPayloadsFor(protocol.EventSessionSave)
	if len(payloads) != 1 || !strings.Contains(string(payloads[0]), "/adjusted.jsonl") {
		t.Fatalf("session.save observed payload = %v, want the adjusted path", payloads)
	}
}

func TestFrontendEventObserved(t *testing.T) {
	client := &fakeExtClient{}
	d := newExtensionTestDispatcher(client, []extension.InterceptorPoint{extension.PointFrontendEvent}, nil)
	c := New(Options{Runner: &fakeTurnRunner{}, Extensions: d})

	c.notice("hello frontend")
	payloads := client.notifyPayloadsFor(protocol.EventFrontendEvent)
	if len(payloads) != 1 {
		t.Fatalf("frontend.event observations = %d, want 1", len(payloads))
	}
	var payload struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(payloads[0], &payload); err != nil {
		t.Fatalf("payload decode: %v", err)
	}
	if payload.Kind != "notice" || payload.Text != "hello frontend" {
		t.Fatalf("observed payload = %+v, want notice/hello frontend", payload)
	}
}

func TestFrontendEventStrategyRewrite(t *testing.T) {
	client := &fakeExtClient{
		interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{Decision: protocol.DecisionReplace,
				Replacement: json.RawMessage(`{"kind":"notice","text":"rewritten","detail":"adjusted detail"}`)}, nil
		},
	}
	d := newExtensionTestDispatcher(client,
		[]extension.InterceptorPoint{extension.PointFrontendEvent},
		map[extension.Slot]string{extension.SlotFrontendEvents: extensionTestPlugin})
	sink := &recordingSink{}
	c := New(Options{Runner: &fakeTurnRunner{}, Sink: sink, Extensions: d})

	c.noticeDetail("original", "original detail")
	events := sink.all()
	if len(events) != 1 {
		t.Fatalf("inner sink events = %d, want 1", len(events))
	}
	if events[0].Kind != event.Notice || events[0].Text != "rewritten" || events[0].Detail != "adjusted detail" {
		t.Fatalf("emitted event = %+v, want rewritten text/detail with the kind intact", events[0])
	}
	// Observers see exactly what the frontend received.
	payloads := client.notifyPayloadsFor(protocol.EventFrontendEvent)
	if len(payloads) != 1 || !strings.Contains(string(payloads[0]), "rewritten") {
		t.Fatalf("observed payloads = %v, want the rewritten event", payloads)
	}
}

func TestFrontendEventStrategyKindChangeRejected(t *testing.T) {
	client := &fakeExtClient{
		interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{Decision: protocol.DecisionReplace,
				Replacement: json.RawMessage(`{"kind":"text","text":"hijacked"}`)}, nil
		},
	}
	d := newExtensionTestDispatcher(client,
		[]extension.InterceptorPoint{extension.PointFrontendEvent},
		map[extension.Slot]string{extension.SlotFrontendEvents: extensionTestPlugin})
	sink := &recordingSink{}
	c := New(Options{Runner: &fakeTurnRunner{}, Sink: sink, Extensions: d})

	c.notice("original")
	events := sink.all()
	if len(events) != 1 || events[0].Text != "original" || events[0].Kind != event.Notice {
		t.Fatalf("emitted events = %+v, want the original event when the owner tries to change the kind", events)
	}
}

func TestFrontendEventStrategyBlockSuppresses(t *testing.T) {
	client := &fakeExtClient{
		interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{Decision: protocol.DecisionBlock, Reason: "suppress"}, nil
		},
	}
	d := newExtensionTestDispatcher(client,
		[]extension.InterceptorPoint{extension.PointFrontendEvent},
		map[extension.Slot]string{extension.SlotFrontendEvents: extensionTestPlugin})
	sink := &recordingSink{}
	c := New(Options{Runner: &fakeTurnRunner{}, Sink: sink, Extensions: d})

	c.notice("suppressed")
	if events := sink.all(); len(events) != 0 {
		t.Fatalf("blocked event reached the frontend: %+v", events)
	}
}

// Stage 6b2: the dispatcher installed on the controller must reach the
// executor agent, and a strategy-replaced system prompt must land in the
// executor's live session (and survive session rotations).

func TestSetExtensionsPropagatesToExecutor(t *testing.T) {
	client := &fakeExtClient{}
	d := newExtensionTestDispatcher(client, []extension.InterceptorPoint{extension.PointAgentBeforeStart}, nil)
	mp := testutil.NewMock("p", testutil.Turn{Text: "hi"})
	exec := agent.New(mp, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Runner: &fakeTurnRunner{}, Executor: exec})

	c.SetExtensions(d)
	if err := c.Executor().Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	found := false
	for _, call := range client.intercepts {
		if call.event == protocol.EventAgentBeforeStart {
			found = true
		}
	}
	if !found {
		t.Fatal("executor run did not consult the dispatcher installed by SetExtensions")
	}
	if mp.CallCount() != 1 {
		t.Fatalf("provider calls = %d, want 1", mp.CallCount())
	}
}

func TestApplyExtensionSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	exec := agent.New(nil, tool.NewRegistry(), agent.NewSession("HOST PROMPT"), agent.Options{}, event.Discard)
	c := New(Options{
		Runner:       &fakeTurnRunner{},
		Executor:     exec,
		SessionDir:   dir,
		SessionPath:  filepath.Join(dir, "s.jsonl"),
		SystemPrompt: "HOST PROMPT",
	})

	c.ApplyExtensionSystemPrompt("EXTENSION PROMPT")
	if got := controlSystemMessage(c.History()); got != "EXTENSION PROMPT" {
		t.Fatalf("system message = %q, want the extension prompt", got)
	}
	// A session rotation must keep the strategy prompt, not revert to the
	// host-composed one.
	if err := c.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if got := controlSystemMessage(c.History()); got != "EXTENSION PROMPT" {
		t.Fatalf("system message after rotation = %q, want the extension prompt", got)
	}
}

func controlSystemMessage(msgs []provider.Message) string {
	for _, m := range msgs {
		if m.Role == provider.RoleSystem {
			return m.Content
		}
	}
	return ""
}

func sinkHasFrontendWrapper(s event.Sink) bool {
	switch t := s.(type) {
	case *frontendEventSink:
		return true
	case *inboxEventSink:
		if _, ok := t.inner.(*frontendEventSink); ok {
			return true
		}
		if lifecycle, ok := t.inner.(*turnEventSink); ok {
			_, wrapped := lifecycle.inner.(*frontendEventSink)
			return wrapped
		}
		return false
	default:
		return false
	}
}
