package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// Stage 6b2 agent-loop wiring tests. The dispatcher under test is real; only
// its sidecar client is faked, so every assertion exercises the actual
// dispatch ruling logic (chain walk, strict replacement decode, error
// policy). Each intercept point is covered for: continue (no-op), block,
// replace (the substituted value is what the host uses), a required
// extension's failure (operation fails), and an optional extension's failure
// (warn + continue).

const extTestPlugin = "fake"

type extRecordedCall struct {
	event   protocol.InterceptEvent
	payload json.RawMessage
}

// fakeDispatchClient is a scriptable dispatch.Client recording every call.
// A nil interceptFn answers continue.
type fakeDispatchClient struct {
	mu          sync.Mutex
	interceptFn func(event protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error)
	intercepts  []extRecordedCall
	notifies    []extRecordedCall
}

func (f *fakeDispatchClient) Intercept(_ context.Context, event protocol.InterceptEvent, payload json.RawMessage, _ time.Duration) (protocol.InterceptResult, error) {
	f.mu.Lock()
	f.intercepts = append(f.intercepts, extRecordedCall{event: event, payload: append(json.RawMessage(nil), payload...)})
	fn := f.interceptFn
	f.mu.Unlock()
	if fn == nil {
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}
	return fn(event, payload)
}

func (f *fakeDispatchClient) TryNotifyEvent(event protocol.InterceptEvent, payload json.RawMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notifies = append(f.notifies, extRecordedCall{event: event, payload: append(json.RawMessage(nil), payload...)})
	return nil
}

func (f *fakeDispatchClient) notifyCountFor(event protocol.InterceptEvent) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, call := range f.notifies {
		if call.event == event {
			n++
		}
	}
	return n
}

// interceptPayloadFor returns the decoded payload of the first intercept call
// for event, for assertions about what the host sent.
func (f *fakeDispatchClient) interceptPayloadFor(event protocol.InterceptEvent, out any) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.intercepts {
		if call.event == event {
			return json.Unmarshal(call.payload, out) == nil
		}
	}
	return false
}

// extWarnRecorder collects dispatcher warnings (optional-extension failures).
type extWarnRecorder struct {
	mu   sync.Mutex
	msgs []string
}

func (w *extWarnRecorder) warn(msg string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.msgs = append(w.msgs, msg)
}

func (w *extWarnRecorder) contains(substr string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, msg := range w.msgs {
		if strings.Contains(msg, substr) {
			return true
		}
	}
	return false
}

// newExtDispatcher builds a dispatcher whose chain lists the fake plugin at
// every given point. required=true marks the plugin required-class (manifest
// required:true), so its failures fail the operation.
func newExtDispatcher(client dispatch.Client, required bool, warn func(string), points ...extension.InterceptorPoint) *dispatch.Dispatcher {
	return newExtSlotDispatcher(client, required, warn, points, nil)
}

// newExtSlotDispatcher builds a dispatcher with the fake plugin chained at
// the given points and owning the given replacement slots (slot → plugin ID).
// A slot owner is required-class by definition, independent of required.
func newExtSlotDispatcher(client dispatch.Client, required bool, warn func(string), points []extension.InterceptorPoint, slots map[extension.Slot]string) *dispatch.Dispatcher {
	chain := map[extension.InterceptorPoint][]extension.Contribution{}
	for _, point := range points {
		chain[point] = []extension.Contribution{{
			Kind:   extension.KindInterceptor,
			ID:     string(point),
			Source: extension.ContributionSource{Scope: extension.ScopePlugin, PluginID: extTestPlugin},
		}}
	}
	replacements := map[extension.Slot]extension.ContributionSource{}
	for slot, plugin := range slots {
		replacements[slot] = extension.ContributionSource{Scope: extension.ScopePlugin, PluginID: plugin}
	}
	requiredSet := map[string]bool{}
	if required {
		requiredSet[extTestPlugin] = true
	}
	return dispatch.New(chain, replacements, func(string) dispatch.Client { return client }, requiredSet, dispatch.Options{Warn: warn})
}

// replaceWith marshals v as the replacement payload of a replace ruling.
func replaceWith(t *testing.T, v any) protocol.InterceptResult {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal replacement: %v", err)
	}
	return protocol.InterceptResult{Decision: protocol.DecisionReplace, Replacement: raw}
}

func blockWith(reason string) protocol.InterceptResult {
	return protocol.InterceptResult{Decision: protocol.DecisionBlock, Reason: reason}
}

// recordingTool is a Tool stand-in that records the args it executed with.
type recordingTool struct {
	name     string
	readOnly bool
	execs    int
	gotArgs  string
}

func (r *recordingTool) Name() string            { return r.name }
func (r *recordingTool) Description() string     { return "" }
func (r *recordingTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (r *recordingTool) ReadOnly() bool          { return r.readOnly }
func (r *recordingTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	r.execs++
	r.gotArgs = string(args)
	return r.name + " ok", nil
}

// sessionContents flattens the session's message contents for substring
// assertions.
func sessionContents(s *Session) string {
	var b strings.Builder
	for _, m := range s.Messages {
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

func assistantMessages(s *Session) []provider.Message {
	var out []provider.Message
	for _, m := range s.Messages {
		if m.Role == provider.RoleAssistant {
			out = append(out, m)
		}
	}
	return out
}

func requestContents(req provider.Request) string {
	var b strings.Builder
	for _, m := range req.Messages {
		b.WriteString(string(m.Role))
		b.WriteByte(':')
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

// agent.before_start

func TestAgentBeforeStartContinue(t *testing.T) {
	client := &fakeDispatchClient{}
	d := newExtDispatcher(client, true, nil, extension.PointAgentBeforeStart)
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "hi"}, {Type: provider.ChunkDone},
	}}
	a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
	if err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(mp.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(mp.requests))
	}
	var payload dispatch.AgentStartPayload
	if !client.interceptPayloadFor(protocol.EventAgentBeforeStart, &payload) {
		t.Fatal("agent.before_start intercept did not fire")
	}
	if payload.Model != "p" || payload.ToolCount != 0 {
		t.Fatalf("payload = %+v, want model p and 0 tools", payload)
	}
	if n := client.notifyCountFor(protocol.EventAgentBeforeStart); n != 1 {
		t.Fatalf("before_start events = %d, want 1", n)
	}
}

func TestAgentBeforeStartBlock(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventAgentBeforeStart {
			return blockWith("no runs today"), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointAgentBeforeStart)
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "hi"}, {Type: provider.ChunkDone},
	}}
	sess := NewSession("sys")
	a := New(mp, tool.NewRegistry(), sess, Options{Extensions: d}, event.Discard)
	err := a.Run(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "no runs today") {
		t.Fatalf("Run err = %v, want the block reason", err)
	}
	if len(mp.requests) != 0 {
		t.Fatalf("blocked run still hit the provider: %d requests", len(mp.requests))
	}
	if len(sess.Messages) != 1 {
		t.Fatalf("session = %d messages, want only the system message (turn never appended)", len(sess.Messages))
	}
}

func TestAgentBeforeStartReplace(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventAgentBeforeStart {
			return replaceWith(t, dispatch.AgentStartPayload{Model: "other", ToolCount: 3, SessionID: "s1"}), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointAgentBeforeStart)
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "hi"}, {Type: provider.ChunkDone},
	}}
	a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
	// The payload is informational: a replacement validates but does not alter
	// the run.
	if err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestAgentBeforeStartFailurePolicy(t *testing.T) {
	boom := errors.New("sidecar timeout")
	t.Run("required fails the run", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{}, boom
		}}
		d := newExtDispatcher(client, true, nil, extension.PointAgentBeforeStart)
		mp := &mockProvider{name: "p", chunks: []provider.Chunk{{Type: provider.ChunkDone}}}
		a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
		err := a.Run(context.Background(), "hello")
		if err == nil || !strings.Contains(err.Error(), "extension fake failed at agent.before_start") {
			t.Fatalf("Run err = %v, want the required failure", err)
		}
		if len(mp.requests) != 0 {
			t.Fatalf("failed run still hit the provider: %d requests", len(mp.requests))
		}
	})
	t.Run("optional warns and proceeds", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{}, boom
		}}
		warns := &extWarnRecorder{}
		d := newExtDispatcher(client, false, warns.warn, extension.PointAgentBeforeStart)
		mp := &mockProvider{name: "p", chunks: []provider.Chunk{
			{Type: provider.ChunkText, Text: "hi"}, {Type: provider.ChunkDone},
		}}
		a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
		if err := a.Run(context.Background(), "hello"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if !warns.contains("skipping this optional extension") {
			t.Fatalf("warnings = %v, want an optional-extension skip warning", warns.msgs)
		}
	})
}

func TestSetExtensionsInstallsAfterConstruction(t *testing.T) {
	client := &fakeDispatchClient{}
	d := newExtDispatcher(client, true, nil, extension.PointAgentBeforeStart)
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "hi"}, {Type: provider.ChunkDone},
	}}
	a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{}, event.Discard)
	a.SetExtensions(d)
	if err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var payload dispatch.AgentStartPayload
	if !client.interceptPayloadFor(protocol.EventAgentBeforeStart, &payload) {
		t.Fatal("SetExtensions-installed dispatcher did not fire")
	}
}

// context.prepare

func TestContextPrepareReplaceIsEphemeral(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventContextPrepare {
			return replaceWith(t, dispatch.ContextPayload{Messages: []protocol.ProviderMessage{
				{Role: protocol.ProviderRoleSystem, Content: "REPLACED SYS"},
				{Role: protocol.ProviderRoleUser, Content: "REPLACED USER"},
			}}), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointContextPrepare)
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "answer"}, {Type: provider.ChunkDone},
	}}
	sess := NewSession("sys")
	a := New(mp, tool.NewRegistry(), sess, Options{Extensions: d}, event.Discard)
	if err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(mp.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(mp.requests))
	}
	got := requestContents(mp.requests[0])
	if !strings.Contains(got, "REPLACED USER") || strings.Contains(got, "hello") {
		t.Fatalf("request messages = %q, want the replacement only", got)
	}
	// Ephemerality: the session log keeps the original history untouched.
	sc := sessionContents(sess)
	if strings.Contains(sc, "REPLACED USER") || strings.Contains(sc, "REPLACED SYS") {
		t.Fatalf("session mutated by context.prepare replacement:\n%s", sc)
	}
	if !strings.Contains(sc, "hello") {
		t.Fatalf("session lost the user turn:\n%s", sc)
	}
}

func TestContextPrepareBlock(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventContextPrepare {
			return blockWith("context denied"), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointContextPrepare)
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "answer"}, {Type: provider.ChunkDone},
	}}
	a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
	err := a.Run(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "context denied") {
		t.Fatalf("Run err = %v, want the block reason", err)
	}
	if len(mp.requests) != 0 {
		t.Fatalf("blocked request still hit the provider: %d requests", len(mp.requests))
	}
}

func TestContextPrepareFailurePolicy(t *testing.T) {
	boom := errors.New("sidecar timeout")
	t.Run("required fails the turn", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{}, boom
		}}
		d := newExtDispatcher(client, true, nil, extension.PointContextPrepare)
		mp := &mockProvider{name: "p", chunks: []provider.Chunk{{Type: provider.ChunkDone}}}
		a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
		err := a.Run(context.Background(), "hello")
		if err == nil || !strings.Contains(err.Error(), "extension fake failed at context.prepare") {
			t.Fatalf("Run err = %v, want the required failure", err)
		}
	})
	t.Run("optional warns and proceeds", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{}, boom
		}}
		warns := &extWarnRecorder{}
		d := newExtDispatcher(client, false, warns.warn, extension.PointContextPrepare)
		mp := &mockProvider{name: "p", chunks: []provider.Chunk{
			{Type: provider.ChunkText, Text: "answer"}, {Type: provider.ChunkDone},
		}}
		a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
		if err := a.Run(context.Background(), "hello"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if !warns.contains("skipping this optional extension") {
			t.Fatalf("warnings = %v, want an optional-extension skip warning", warns.msgs)
		}
	})
}

// provider.request

func TestProviderRequestReplace(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventProviderRequest {
			var in dispatch.ProviderRequestPayload
			if err := json.Unmarshal(payload, &in); err != nil {
				return protocol.InterceptResult{}, err
			}
			in.Request.Messages = append(in.Request.Messages, protocol.ProviderMessage{
				Role: protocol.ProviderRoleUser, Content: "EXTENSION INJECTED",
			})
			return replaceWith(t, in), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointProviderRequest)
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "answer"}, {Type: provider.ChunkDone},
	}}
	sess := NewSession("sys")
	a := New(mp, tool.NewRegistry(), sess, Options{Extensions: d}, event.Discard)
	if err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := requestContents(mp.requests[0]); !strings.Contains(got, "EXTENSION INJECTED") {
		t.Fatalf("request = %q, want the injected message", got)
	}
	if sc := sessionContents(sess); strings.Contains(sc, "EXTENSION INJECTED") {
		t.Fatalf("session mutated by provider.request replacement:\n%s", sc)
	}
	if n := client.notifyCountFor(protocol.EventProviderRequest); n != 1 {
		t.Fatalf("provider.request events = %d, want 1", n)
	}
}

func TestProviderRequestBlock(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventProviderRequest {
			return blockWith("request denied"), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointProviderRequest)
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "answer"}, {Type: provider.ChunkDone},
	}}
	a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
	err := a.Run(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "request denied") {
		t.Fatalf("Run err = %v, want the block reason", err)
	}
	if len(mp.requests) != 0 {
		t.Fatalf("blocked request still hit the provider: %d requests", len(mp.requests))
	}
}

func TestProviderRequestFailurePolicy(t *testing.T) {
	boom := errors.New("sidecar timeout")
	t.Run("required fails the turn", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{}, boom
		}}
		d := newExtDispatcher(client, true, nil, extension.PointProviderRequest)
		mp := &mockProvider{name: "p", chunks: []provider.Chunk{{Type: provider.ChunkDone}}}
		a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
		err := a.Run(context.Background(), "hello")
		if err == nil || !strings.Contains(err.Error(), "extension fake failed at provider.request") {
			t.Fatalf("Run err = %v, want the required failure", err)
		}
	})
	t.Run("optional warns and proceeds", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{}, boom
		}}
		warns := &extWarnRecorder{}
		d := newExtDispatcher(client, false, warns.warn, extension.PointProviderRequest)
		mp := &mockProvider{name: "p", chunks: []provider.Chunk{
			{Type: provider.ChunkText, Text: "answer"}, {Type: provider.ChunkDone},
		}}
		a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
		if err := a.Run(context.Background(), "hello"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if !warns.contains("skipping this optional extension") {
			t.Fatalf("warnings = %v, want an optional-extension skip warning", warns.msgs)
		}
	})
}

// TestProviderRequestReplacementCacheEphemerality is the cache contract: a
// replacement shapes only the request it ruled on. Two identical agents — one
// with an extension that injects a message into run 1's request only, one
// without — must send byte-identical requests on run 2.
func TestProviderRequestReplacementCacheEphemerality(t *testing.T) {
	streams := func() [][]provider.Chunk {
		return [][]provider.Chunk{
			{{Type: provider.ChunkText, Text: "one"}, {Type: provider.ChunkDone}},
			{{Type: provider.ChunkText, Text: "two"}, {Type: provider.ChunkDone}},
		}
	}
	client := &fakeDispatchClient{}
	replaced := 0
	client.interceptFn = func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventProviderRequest && replaced == 0 {
			replaced++
			var in dispatch.ProviderRequestPayload
			if err := json.Unmarshal(payload, &in); err != nil {
				return protocol.InterceptResult{}, err
			}
			in.Request.Messages = append(in.Request.Messages, protocol.ProviderMessage{
				Role: protocol.ProviderRoleUser, Content: "RUN-1 ONLY",
			})
			return replaceWith(t, in), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}
	d := newExtDispatcher(client, true, nil, extension.PointProviderRequest)

	withExt := &mockProvider{name: "p", streams: streams()}
	a := New(withExt, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
	for _, input := range []string{"first", "second"} {
		if err := a.Run(context.Background(), input); err != nil {
			t.Fatalf("Run(%q): %v", input, err)
		}
	}
	if len(withExt.requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(withExt.requests))
	}
	if got := requestContents(withExt.requests[0]); !strings.Contains(got, "RUN-1 ONLY") {
		t.Fatalf("run 1 request = %q, want the injected message", got)
	}
	if got := requestContents(withExt.requests[1]); strings.Contains(got, "RUN-1 ONLY") {
		t.Fatalf("run 2 request leaked the run-1 replacement:\n%s", got)
	}

	baseline := &mockProvider{name: "p", streams: streams()}
	b := New(baseline, tool.NewRegistry(), NewSession("sys"), Options{}, event.Discard)
	for _, input := range []string{"first", "second"} {
		if err := b.Run(context.Background(), input); err != nil {
			t.Fatalf("baseline Run(%q): %v", input, err)
		}
	}
	gotRun2 := requestContents(withExt.requests[1])
	wantRun2 := requestContents(baseline.requests[1])
	if gotRun2 != wantRun2 {
		t.Fatalf("run 2 request differs from the no-extension baseline:\ngot:\n%s\nwant:\n%s", gotRun2, wantRun2)
	}
}

// provider.response

func TestProviderResponseReplaceIsTranscript(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventProviderResponse {
			return replaceWith(t, dispatch.ProviderResponsePayload{Text: "REPLACED ANSWER", Reasoning: "replaced reasoning"}), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointProviderResponse)
	mp := &mockProvider{name: "p", streams: [][]provider.Chunk{
		{
			{Type: provider.ChunkReasoning, Text: "ORIGINAL REASONING", ReasoningID: "rs_original", ReasoningStatus: "completed"},
			{Type: provider.ChunkText, Text: "ORIGINAL ANSWER"},
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "second"}, {Type: provider.ChunkDone}},
	}}
	sess := NewSession("sys")
	a := New(mp, tool.NewRegistry(), sess, Options{Extensions: d}, event.Discard)
	if err := a.Run(context.Background(), "one"); err != nil {
		t.Fatalf("Run one: %v", err)
	}
	assistants := assistantMessages(sess)
	if len(assistants) != 1 || assistants[0].Content != "REPLACED ANSWER" {
		t.Fatalf("assistant turn = %+v, want the replaced answer persisted", assistants)
	}
	if assistants[0].ReasoningContent != "replaced reasoning" {
		t.Fatalf("assistant reasoning = %q, want the replaced reasoning", assistants[0].ReasoningContent)
	}
	if assistants[0].ReasoningID != "" || assistants[0].ReasoningStatus != "" {
		t.Fatalf("replaced reasoning retained provider metadata = (%q, %q)", assistants[0].ReasoningID, assistants[0].ReasoningStatus)
	}
	// The transcript contract: the next request replays the replaced turn,
	// never the provider's original text.
	if err := a.Run(context.Background(), "two"); err != nil {
		t.Fatalf("Run two: %v", err)
	}
	got := requestContents(mp.requests[1])
	if !strings.Contains(got, "REPLACED ANSWER") {
		t.Fatalf("run 2 request = %q, want the replaced turn replayed", got)
	}
	if strings.Contains(got, "ORIGINAL ANSWER") {
		t.Fatalf("run 2 request leaked the original provider text:\n%s", got)
	}
}

func TestProviderResponseBlock(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventProviderResponse {
			return blockWith("response denied"), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointProviderResponse)
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ORIGINAL ANSWER"}, {Type: provider.ChunkDone},
	}}
	sess := NewSession("sys")
	a := New(mp, tool.NewRegistry(), sess, Options{Extensions: d}, event.Discard)
	err := a.Run(context.Background(), "one")
	if err == nil || !strings.Contains(err.Error(), "response denied") {
		t.Fatalf("Run err = %v, want the block reason", err)
	}
	if n := len(assistantMessages(sess)); n != 0 {
		t.Fatalf("blocked response persisted %d assistant turns, want 0", n)
	}
}

func TestProviderResponseFailurePolicy(t *testing.T) {
	boom := errors.New("sidecar timeout")
	t.Run("required fails the turn", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{}, boom
		}}
		d := newExtDispatcher(client, true, nil, extension.PointProviderResponse)
		mp := &mockProvider{name: "p", chunks: []provider.Chunk{
			{Type: provider.ChunkText, Text: "ORIGINAL ANSWER"}, {Type: provider.ChunkDone},
		}}
		sess := NewSession("sys")
		a := New(mp, tool.NewRegistry(), sess, Options{Extensions: d}, event.Discard)
		err := a.Run(context.Background(), "one")
		if err == nil || !strings.Contains(err.Error(), "extension fake failed at provider.response") {
			t.Fatalf("Run err = %v, want the required failure", err)
		}
		if n := len(assistantMessages(sess)); n != 0 {
			t.Fatalf("failed response persisted %d assistant turns, want 0", n)
		}
	})
	t.Run("optional warns and persists the original", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{}, boom
		}}
		warns := &extWarnRecorder{}
		d := newExtDispatcher(client, false, warns.warn, extension.PointProviderResponse)
		mp := &mockProvider{name: "p", chunks: []provider.Chunk{
			{Type: provider.ChunkText, Text: "ORIGINAL ANSWER"}, {Type: provider.ChunkDone},
		}}
		sess := NewSession("sys")
		a := New(mp, tool.NewRegistry(), sess, Options{Extensions: d}, event.Discard)
		if err := a.Run(context.Background(), "one"); err != nil {
			t.Fatalf("Run: %v", err)
		}
		assistants := assistantMessages(sess)
		if len(assistants) != 1 || assistants[0].Content != "ORIGINAL ANSWER" {
			t.Fatalf("assistant turn = %+v, want the original answer persisted", assistants)
		}
		if !warns.contains("skipping this optional extension") {
			t.Fatalf("warnings = %v, want an optional-extension skip warning", warns.msgs)
		}
	})
}

// tool.before

func TestToolBeforeContinue(t *testing.T) {
	client := &fakeDispatchClient{}
	d := newExtDispatcher(client, true, nil, extension.PointToolBefore)
	rec := &recordingTool{name: "read_file", readOnly: true}
	reg := tool.NewRegistry()
	reg.Add(rec)
	a := New(nil, reg, NewSession(""), Options{Extensions: d}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "read_file", Arguments: `{"path":"/x"}`})
	if out.errMsg != "" || !strings.Contains(out.output, "read_file ok") {
		t.Fatalf("outcome = %+v, want the tool to run", out)
	}
	if rec.gotArgs != `{"path":"/x"}` {
		t.Fatalf("tool args = %q, want the original call args", rec.gotArgs)
	}
	if n := client.notifyCountFor(protocol.EventToolBefore); n != 1 {
		t.Fatalf("tool.before events = %d, want 1", n)
	}
}

func TestToolBeforeBlock(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventToolBefore {
			return blockWith("tool denied"), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointToolBefore)
	rec := &recordingTool{name: "read_file", readOnly: true}
	reg := tool.NewRegistry()
	reg.Add(rec)
	a := New(nil, reg, NewSession(""), Options{Extensions: d}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "read_file", Arguments: `{"path":"/x"}`})
	if !out.blocked || out.output != "blocked: tool denied" {
		t.Fatalf("outcome = %+v, want a blocked tool result with the reason", out)
	}
	if rec.execs != 0 {
		t.Fatalf("blocked tool executed %d times", rec.execs)
	}
}

func TestToolBeforeReplaceArgs(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventToolBefore {
			return replaceWith(t, dispatch.ToolBeforePayload{Name: "read_file", Arguments: `{"path":"/substituted"}`}), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointToolBefore)
	rec := &recordingTool{name: "read_file", readOnly: true}
	reg := tool.NewRegistry()
	reg.Add(rec)
	a := New(nil, reg, NewSession(""), Options{Extensions: d}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "read_file", Arguments: `{"path":"/original"}`})
	if out.errMsg != "" {
		t.Fatalf("outcome = %+v, want success", out)
	}
	if rec.gotArgs != `{"path":"/substituted"}` {
		t.Fatalf("tool args = %q, want the extension-substituted args", rec.gotArgs)
	}
}

func TestToolBeforeReplaceName(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventToolBefore {
			return replaceWith(t, dispatch.ToolBeforePayload{Name: "grep", Arguments: `{"pattern":"x"}`}), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointToolBefore)
	orig := &recordingTool{name: "read_file", readOnly: true}
	substituted := &recordingTool{name: "grep", readOnly: true}
	reg := tool.NewRegistry()
	reg.Add(orig)
	reg.Add(substituted)
	a := New(nil, reg, NewSession(""), Options{Extensions: d}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "read_file", Arguments: `{"path":"/x"}`})
	if out.errMsg != "" || !strings.Contains(out.output, "grep ok") {
		t.Fatalf("outcome = %+v, want the substituted tool to run", out)
	}
	if orig.execs != 0 || substituted.execs != 1 {
		t.Fatalf("execs = %d/%d, want the substituted tool only", orig.execs, substituted.execs)
	}
}

func TestToolBeforeInvalidReplacements(t *testing.T) {
	cases := []struct {
		name        string
		replacement dispatch.ToolBeforePayload
		want        string
	}{
		{"empty arguments", dispatch.ToolBeforePayload{Name: "read_file", Arguments: ""}, "arguments must decode as a JSON object"},
		{"unresolvable name", dispatch.ToolBeforePayload{Name: "no_such_tool", Arguments: `{}`}, "does not resolve"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
				if ev == protocol.EventToolBefore {
					return replaceWith(t, tc.replacement), nil
				}
				return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
			}}
			d := newExtDispatcher(client, true, nil, extension.PointToolBefore)
			rec := &recordingTool{name: "read_file", readOnly: true}
			reg := tool.NewRegistry()
			reg.Add(rec)
			a := New(nil, reg, NewSession(""), Options{Extensions: d}, event.Discard)
			out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "read_file", Arguments: `{"path":"/x"}`})
			if out.errMsg == "" || !strings.Contains(out.output, "violated the intercept contract") || !strings.Contains(out.output, tc.want) {
				t.Fatalf("outcome = %+v, want a contract-violation error result containing %q", out, tc.want)
			}
			if rec.execs != 0 {
				t.Fatalf("invalid replacement still executed the tool")
			}
		})
	}
	// A replacement that fails the point's DTO (arguments not a JSON object)
	// is a dispatch-level violation: for a required extension the call fails.
	t.Run("DTO violation", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
			if ev == protocol.EventToolBefore {
				return protocol.InterceptResult{Decision: protocol.DecisionReplace, Replacement: json.RawMessage(`{"name":"read_file","arguments":"[1,2]"}`)}, nil
			}
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		}}
		d := newExtDispatcher(client, true, nil, extension.PointToolBefore)
		rec := &recordingTool{name: "read_file", readOnly: true}
		reg := tool.NewRegistry()
		reg.Add(rec)
		a := New(nil, reg, NewSession(""), Options{Extensions: d}, event.Discard)
		out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "read_file", Arguments: `{"path":"/x"}`})
		if out.errMsg == "" || !strings.Contains(out.output, "violated the intercept contract") {
			t.Fatalf("outcome = %+v, want a dispatch violation error result", out)
		}
		if rec.execs != 0 {
			t.Fatal("DTO-violating replacement still executed the tool")
		}
	})
}

func TestToolBeforeFailurePolicy(t *testing.T) {
	boom := errors.New("sidecar timeout")
	t.Run("required fails the call", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{}, boom
		}}
		d := newExtDispatcher(client, true, nil, extension.PointToolBefore)
		rec := &recordingTool{name: "read_file", readOnly: true}
		reg := tool.NewRegistry()
		reg.Add(rec)
		a := New(nil, reg, NewSession(""), Options{Extensions: d}, event.Discard)
		out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "read_file", Arguments: `{"path":"/x"}`})
		if out.errMsg == "" || !strings.Contains(out.output, "extension fake failed at tool.before") {
			t.Fatalf("outcome = %+v, want the required failure as the tool result", out)
		}
		if rec.execs != 0 {
			t.Fatal("failed extension still let the tool run")
		}
	})
	t.Run("optional warns and runs", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{}, boom
		}}
		warns := &extWarnRecorder{}
		d := newExtDispatcher(client, false, warns.warn, extension.PointToolBefore)
		rec := &recordingTool{name: "read_file", readOnly: true}
		reg := tool.NewRegistry()
		reg.Add(rec)
		a := New(nil, reg, NewSession(""), Options{Extensions: d}, event.Discard)
		out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "read_file", Arguments: `{"path":"/x"}`})
		if out.errMsg != "" || rec.execs != 1 {
			t.Fatalf("outcome = %+v execs = %d, want the tool to run", out, rec.execs)
		}
		if !warns.contains("skipping this optional extension") {
			t.Fatalf("warnings = %v, want an optional-extension skip warning", warns.msgs)
		}
	})
}

// permission.decision

func TestPermissionDecisionExtensionAllowOverridesHostDeny(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventPermissionDecision {
			var in dispatch.PermissionPayload
			if err := json.Unmarshal(payload, &in); err != nil {
				return protocol.InterceptResult{}, err
			}
			if in.HostDecision != "deny" {
				t.Errorf("host decision = %q, want deny (host computes first)", in.HostDecision)
			}
			return protocol.InterceptResult{Decision: protocol.DecisionAllow}, nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointPermissionDecision)
	rec := &recordingTool{name: "edit_file", readOnly: false}
	reg := tool.NewRegistry()
	reg.Add(rec)
	gate := &stubGate{deny: map[string]bool{"edit_file": true}}
	var events []event.Event
	sink := event.FuncSink(func(e event.Event) { events = append(events, e) })
	a := New(nil, reg, NewSession(""), Options{Gate: gate, Extensions: d}, sink)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "edit_file", Arguments: `{"path":"/x"}`})
	if out.errMsg != "" || rec.execs != 1 {
		t.Fatalf("outcome = %+v execs = %d, want the full-trust override to execute", out, rec.execs)
	}
	audit := false
	for _, e := range events {
		if e.Kind == event.Notice && strings.Contains(e.Text, "allowed the tool overriding the host deny") {
			audit = true
		}
	}
	if !audit {
		t.Fatal("full-trust override produced no audit notice")
	}
	if n := client.notifyCountFor(protocol.EventPermissionDecision); n != 1 {
		t.Fatalf("permission.decision events = %d, want 1", n)
	}
}

func TestPermissionDecisionExtensionDenyOverridesHostAllow(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventPermissionDecision {
			return protocol.InterceptResult{Decision: protocol.DecisionDeny}, nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointPermissionDecision)
	rec := &recordingTool{name: "edit_file", readOnly: false}
	reg := tool.NewRegistry()
	reg.Add(rec)
	a := New(nil, reg, NewSession(""), Options{Gate: &stubGate{}, Extensions: d}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "edit_file", Arguments: `{"path":"/x"}`})
	if !out.blocked || !strings.Contains(out.output, "denied by extension permission policy") {
		t.Fatalf("outcome = %+v, want an extension denial", out)
	}
	if rec.execs != 0 {
		t.Fatal("extension-denied tool executed")
	}
}

func TestPermissionDecisionContinueKeepsHostDeny(t *testing.T) {
	client := &fakeDispatchClient{}
	d := newExtDispatcher(client, true, nil, extension.PointPermissionDecision)
	rec := &recordingTool{name: "edit_file", readOnly: false}
	reg := tool.NewRegistry()
	reg.Add(rec)
	gate := &stubGate{deny: map[string]bool{"edit_file": true}}
	a := New(nil, reg, NewSession(""), Options{Gate: gate, Extensions: d}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "edit_file", Arguments: `{"path":"/x"}`})
	if !out.blocked || !strings.Contains(out.output, "denied by test policy") {
		t.Fatalf("outcome = %+v, want the host denial to stand", out)
	}
	if rec.execs != 0 {
		t.Fatal("host-denied tool executed")
	}
}

func TestPermissionDecisionBlockAndFailure(t *testing.T) {
	t.Run("block denies", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
			if ev == protocol.EventPermissionDecision {
				return blockWith("policy says no"), nil
			}
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		}}
		d := newExtDispatcher(client, true, nil, extension.PointPermissionDecision)
		rec := &recordingTool{name: "edit_file", readOnly: false}
		reg := tool.NewRegistry()
		reg.Add(rec)
		a := New(nil, reg, NewSession(""), Options{Gate: &stubGate{}, Extensions: d}, event.Discard)
		out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "edit_file", Arguments: `{"path":"/x"}`})
		if !out.blocked || !strings.Contains(out.output, "policy says no") {
			t.Fatalf("outcome = %+v, want the block reason", out)
		}
	})
	t.Run("required failure denies", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{}, errors.New("sidecar timeout")
		}}
		d := newExtDispatcher(client, true, nil, extension.PointPermissionDecision)
		rec := &recordingTool{name: "edit_file", readOnly: false}
		reg := tool.NewRegistry()
		reg.Add(rec)
		a := New(nil, reg, NewSession(""), Options{Gate: &stubGate{}, Extensions: d}, event.Discard)
		out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "edit_file", Arguments: `{"path":"/x"}`})
		if !out.blocked || !strings.Contains(out.output, "extension fake failed at permission.decision") {
			t.Fatalf("outcome = %+v, want the required failure", out)
		}
		if rec.execs != 0 {
			t.Fatal("failed extension still let the tool run")
		}
	})
	t.Run("optional failure keeps the host allow", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{}, errors.New("sidecar timeout")
		}}
		warns := &extWarnRecorder{}
		d := newExtDispatcher(client, false, warns.warn, extension.PointPermissionDecision)
		rec := &recordingTool{name: "edit_file", readOnly: false}
		reg := tool.NewRegistry()
		reg.Add(rec)
		a := New(nil, reg, NewSession(""), Options{Gate: &stubGate{}, Extensions: d}, event.Discard)
		out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "edit_file", Arguments: `{"path":"/x"}`})
		if out.errMsg != "" || rec.execs != 1 {
			t.Fatalf("outcome = %+v execs = %d, want the host allow to stand", out, rec.execs)
		}
		if !warns.contains("skipping this optional extension") {
			t.Fatalf("warnings = %v, want an optional-extension skip warning", warns.msgs)
		}
	})
}

// tool.after

func TestToolAfterReplaceResult(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventToolAfter {
			return replaceWith(t, dispatch.ToolAfterPayload{
				Name: "read_file", Arguments: `{"path":"/x"}`, Result: "EXTENSION RESULT",
			}), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointToolAfter)
	rec := &recordingTool{name: "read_file", readOnly: true}
	reg := tool.NewRegistry()
	reg.Add(rec)
	a := New(nil, reg, NewSession(""), Options{Extensions: d}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "read_file", Arguments: `{"path":"/x"}`})
	if out.errMsg != "" || !strings.Contains(out.output, "EXTENSION RESULT") {
		t.Fatalf("outcome = %+v, want the replaced result", out)
	}
	if strings.Contains(out.output, "read_file ok") {
		t.Fatalf("outcome leaked the original result: %q", out.output)
	}
}

func TestToolAfterReplaceClearsError(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventToolAfter {
			var in dispatch.ToolAfterPayload
			if err := json.Unmarshal(payload, &in); err != nil {
				return protocol.InterceptResult{}, err
			}
			if !in.IsError {
				t.Errorf("payload IsError = false, want true for a failed tool")
			}
			return replaceWith(t, dispatch.ToolAfterPayload{
				Name: "read_file", Arguments: `{"path":"/x"}`, Result: "RECOVERED BY EXTENSION",
			}), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointToolAfter)
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true, err: errors.New("boom")})
	a := New(nil, reg, NewSession(""), Options{Extensions: d}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "read_file", Arguments: `{"path":"/x"}`})
	if out.errMsg != "" || !strings.Contains(out.output, "RECOVERED BY EXTENSION") {
		t.Fatalf("outcome = %+v, want the failure converted to the replaced success", out)
	}
}

func TestToolAfterBlock(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventToolAfter {
			return blockWith("result withheld"), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointToolAfter)
	rec := &recordingTool{name: "read_file", readOnly: true}
	reg := tool.NewRegistry()
	reg.Add(rec)
	a := New(nil, reg, NewSession(""), Options{Extensions: d}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "read_file", Arguments: `{"path":"/x"}`})
	if out.errMsg == "" || !strings.Contains(out.output, "result withheld") {
		t.Fatalf("outcome = %+v, want an error tool result with the reason", out)
	}
	if rec.execs != 1 {
		t.Fatalf("the tool itself must still have run (block only withholds the result), execs = %d", rec.execs)
	}
}

func TestToolAfterFailurePolicy(t *testing.T) {
	boom := errors.New("sidecar timeout")
	t.Run("required converts the result to the failure", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{}, boom
		}}
		d := newExtDispatcher(client, true, nil, extension.PointToolAfter)
		rec := &recordingTool{name: "read_file", readOnly: true}
		reg := tool.NewRegistry()
		reg.Add(rec)
		a := New(nil, reg, NewSession(""), Options{Extensions: d}, event.Discard)
		out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "read_file", Arguments: `{"path":"/x"}`})
		if out.errMsg == "" || !strings.Contains(out.output, "extension fake failed at tool.after") {
			t.Fatalf("outcome = %+v, want the required failure as the tool result", out)
		}
	})
	t.Run("optional warns and keeps the result", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{}, boom
		}}
		warns := &extWarnRecorder{}
		d := newExtDispatcher(client, false, warns.warn, extension.PointToolAfter)
		rec := &recordingTool{name: "read_file", readOnly: true}
		reg := tool.NewRegistry()
		reg.Add(rec)
		a := New(nil, reg, NewSession(""), Options{Extensions: d}, event.Discard)
		out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "read_file", Arguments: `{"path":"/x"}`})
		if out.errMsg != "" || !strings.Contains(out.output, "read_file ok") {
			t.Fatalf("outcome = %+v, want the original result", out)
		}
		if !warns.contains("skipping this optional extension") {
			t.Fatalf("warnings = %v, want an optional-extension skip warning", warns.msgs)
		}
	})
}

// compaction.prepare / compaction.complete

// newCompactionAgent builds an agent whose session has a foldable middle
// (large assistant turns) so CompactNow always finds a region, with the
// summarizer scripted to answer "SUMMARY TEXT". The recent tail stays small so
// the content-driven candidate lands under compact_ratio.
func newCompactionAgent(t *testing.T, d *dispatch.Dispatcher) (*mockProvider, *Agent) {
	t.Helper()
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "SUMMARY TEXT"}, {Type: provider.ChunkDone},
	}}
	sess := NewSession("sys")
	big := strings.Repeat("a", 8000)
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "task"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: big})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "more"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: big})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "next"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	return mp, New(mp, tool.NewRegistry(), sess, Options{
		ContextWindow: 50_000, CompactRatio: 0.85, RecentKeep: 2, Extensions: d,
	}, event.Discard)
}

func TestCompactionPrepareBlock(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventCompactionPrepare {
			return blockWith("compaction denied"), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointCompactionPrepare)
	mp, a := newCompactionAgent(t, d)
	before := len(a.Session().Messages)
	err := a.CompactNow(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "compaction denied") {
		t.Fatalf("CompactNow err = %v, want the block reason", err)
	}
	if len(mp.requests) != 0 {
		t.Fatalf("blocked compaction still called the summarizer: %d requests", len(mp.requests))
	}
	if len(a.Session().Messages) != before {
		t.Fatal("blocked compaction rewrote the session")
	}
}

func TestCompactionPrepareFailurePolicy(t *testing.T) {
	boom := errors.New("sidecar timeout")
	t.Run("required skips the pass with the failure", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{}, boom
		}}
		d := newExtDispatcher(client, true, nil, extension.PointCompactionPrepare)
		mp, a := newCompactionAgent(t, d)
		before := len(a.Session().Messages)
		err := a.CompactNow(context.Background(), "")
		if err == nil || !strings.Contains(err.Error(), "extension fake failed at compaction.prepare") {
			t.Fatalf("CompactNow err = %v, want the required failure", err)
		}
		if len(mp.requests) != 0 || len(a.Session().Messages) != before {
			t.Fatal("failed compaction still ran the summarizer or rewrote the session")
		}
	})
	t.Run("optional warns and folds", func(t *testing.T) {
		client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
			return protocol.InterceptResult{}, boom
		}}
		warns := &extWarnRecorder{}
		d := newExtDispatcher(client, false, warns.warn, extension.PointCompactionPrepare)
		_, a := newCompactionAgent(t, d)
		if err := a.CompactNow(context.Background(), ""); err != nil {
			t.Fatalf("CompactNow: %v", err)
		}
		if sc := joinContents(visibleContext(a)); !strings.Contains(sc, "SUMMARY TEXT") {
			t.Fatalf("projection missing the summary:\n%.200q", sc)
		}
		if !warns.contains("skipping this optional extension") {
			t.Fatalf("warnings = %v, want an optional-extension skip warning", warns.msgs)
		}
	})
}

func TestCompactionCompleteReplace(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventCompactionComplete {
			var in dispatch.CompactionCompletePayload
			if err := json.Unmarshal(payload, &in); err != nil {
				return protocol.InterceptResult{}, err
			}
			if in.Summary != "SUMMARY TEXT" {
				t.Errorf("complete payload summary = %q, want the produced summary", in.Summary)
			}
			return replaceWith(t, dispatch.CompactionCompletePayload{Summary: "EXTENSION SUMMARY"}), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointCompactionComplete)
	_, a := newCompactionAgent(t, d)
	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	sc := joinContents(visibleContext(a))
	if !strings.Contains(sc, "EXTENSION SUMMARY") {
		t.Fatalf("projection missing the replaced summary:\n%.200q", sc)
	}
	if strings.Contains(sc, "SUMMARY TEXT") {
		t.Fatalf("session leaked the original summary:\n%.200q", sc)
	}
}

func TestCompactionCompleteBlock(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventCompactionComplete {
			return blockWith("summary denied"), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointCompactionComplete)
	_, a := newCompactionAgent(t, d)
	before := len(a.Session().Messages)
	err := a.CompactNow(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "summary denied") {
		t.Fatalf("CompactNow err = %v, want the block reason", err)
	}
	if len(a.Session().Messages) != before {
		t.Fatal("blocked compaction rewrote the session")
	}
}

func TestCompactionCompleteRequiredFailure(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{}, errors.New("sidecar timeout")
	}}
	d := newExtDispatcher(client, true, nil, extension.PointCompactionComplete)
	_, a := newCompactionAgent(t, d)
	before := len(a.Session().Messages)
	err := a.CompactNow(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "extension fake failed at compaction.complete") {
		t.Fatalf("CompactNow err = %v, want the required failure", err)
	}
	if len(a.Session().Messages) != before {
		t.Fatal("failed compaction rewrote the session")
	}
}

// --- slot-owner strategy phase (two-phase ruling: chain walk, then the slot
// owner's RunStrategy as the final replacement phase) ---

func TestContextPrepareSlotOwnerConsulted(t *testing.T) {
	// The owner declared ONLY replaces (no intercepts): the chain is empty,
	// yet its strategy ruling must drive the request.
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventContextPrepare {
			return replaceWith(t, dispatch.ContextPayload{Messages: []protocol.ProviderMessage{
				{Role: protocol.ProviderRoleSystem, Content: "OWNER SYS"},
				{Role: protocol.ProviderRoleUser, Content: "OWNER USER"},
			}}), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtSlotDispatcher(client, false, nil, nil,
		map[extension.Slot]string{extension.SlotContext: extTestPlugin})
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "answer"}, {Type: provider.ChunkDone},
	}}
	sess := NewSession("sys")
	a := New(mp, tool.NewRegistry(), sess, Options{Extensions: d}, event.Discard)
	if err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := requestContents(mp.requests[0]); !strings.Contains(got, "OWNER USER") || strings.Contains(got, "hello") {
		t.Fatalf("request = %q, want the slot owner's replacement", got)
	}
	if sc := sessionContents(sess); strings.Contains(sc, "OWNER USER") {
		t.Fatalf("session mutated by the owner's replacement:\n%s", sc)
	}
}

func TestContextPrepareSlotOwnerFinalSayAfterChain(t *testing.T) {
	// The owner declared BOTH intercepts and replaces: it participates as a
	// chain interceptor first, then as the slot strategy — and the strategy
	// sees the chain's output.
	calls := 0
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev != protocol.EventContextPrepare {
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		}
		calls++
		var in dispatch.ContextPayload
		if err := json.Unmarshal(payload, &in); err != nil {
			return protocol.InterceptResult{}, err
		}
		if calls == 1 {
			return replaceWith(t, dispatch.ContextPayload{Messages: []protocol.ProviderMessage{
				{Role: protocol.ProviderRoleUser, Content: "CHAIN VALUE"},
			}}), nil
		}
		if got := in.Messages[0].Content; got != "CHAIN VALUE" {
			t.Errorf("strategy phase received %q, want the chain's output", got)
		}
		return replaceWith(t, dispatch.ContextPayload{Messages: []protocol.ProviderMessage{
			{Role: protocol.ProviderRoleUser, Content: "OWNER VALUE"},
		}}), nil
	}}
	d := newExtSlotDispatcher(client, false, nil,
		[]extension.InterceptorPoint{extension.PointContextPrepare},
		map[extension.Slot]string{extension.SlotContext: extTestPlugin})
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "answer"}, {Type: provider.ChunkDone},
	}}
	a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
	if err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 2 {
		t.Fatalf("owner consulted %d times, want 2 (chain, then strategy)", calls)
	}
	if got := requestContents(mp.requests[0]); !strings.Contains(got, "OWNER VALUE") || strings.Contains(got, "CHAIN VALUE") {
		t.Fatalf("request = %q, want the strategy ruling to win", got)
	}
}

func TestContextPrepareSlotOwnerFailureIsFatal(t *testing.T) {
	// Slot ownership alone makes the extension required-class (required=false
	// here): its timeout fails the operation.
	client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{}, errors.New("sidecar timeout")
	}}
	d := newExtSlotDispatcher(client, false, nil, nil,
		map[extension.Slot]string{extension.SlotContext: extTestPlugin})
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{{Type: provider.ChunkDone}}}
	a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
	err := a.Run(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "extension fake failed at context.prepare") {
		t.Fatalf("Run err = %v, want the owner failure", err)
	}
}

func TestProviderRequestSlotOwnerConsulted(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventProviderRequest {
			var in dispatch.ProviderRequestPayload
			if err := json.Unmarshal(payload, &in); err != nil {
				return protocol.InterceptResult{}, err
			}
			in.Request.Messages = append(in.Request.Messages, protocol.ProviderMessage{
				Role: protocol.ProviderRoleUser, Content: "OWNER INJECTED",
			})
			return replaceWith(t, in), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtSlotDispatcher(client, false, nil, nil,
		map[extension.Slot]string{extension.SlotProviderRequest: extTestPlugin})
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "answer"}, {Type: provider.ChunkDone},
	}}
	sess := NewSession("sys")
	a := New(mp, tool.NewRegistry(), sess, Options{Extensions: d}, event.Discard)
	if err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := requestContents(mp.requests[0]); !strings.Contains(got, "OWNER INJECTED") {
		t.Fatalf("request = %q, want the slot owner's replacement", got)
	}
	if sc := sessionContents(sess); strings.Contains(sc, "OWNER INJECTED") {
		t.Fatalf("session mutated by the owner's replacement:\n%s", sc)
	}
}

func TestProviderRequestSlotOwnerFinalSayAfterChain(t *testing.T) {
	calls := 0
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev != protocol.EventProviderRequest {
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		}
		calls++
		var in dispatch.ProviderRequestPayload
		if err := json.Unmarshal(payload, &in); err != nil {
			return protocol.InterceptResult{}, err
		}
		marker := "CHAIN MARKER"
		if calls == 2 {
			var last string
			if n := len(in.Request.Messages); n > 0 {
				last = in.Request.Messages[n-1].Content
			}
			if last != "CHAIN MARKER" {
				t.Errorf("strategy phase last message = %q, want the chain's output", last)
			}
			marker = "OWNER MARKER"
		}
		in.Request.Messages = append(in.Request.Messages, protocol.ProviderMessage{
			Role: protocol.ProviderRoleUser, Content: marker,
		})
		return replaceWith(t, in), nil
	}}
	d := newExtSlotDispatcher(client, false, nil,
		[]extension.InterceptorPoint{extension.PointProviderRequest},
		map[extension.Slot]string{extension.SlotProviderRequest: extTestPlugin})
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "answer"}, {Type: provider.ChunkDone},
	}}
	a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
	if err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 2 {
		t.Fatalf("owner consulted %d times, want 2 (chain, then strategy)", calls)
	}
	got := requestContents(mp.requests[0])
	if !strings.Contains(got, "OWNER MARKER") || !strings.Contains(got, "CHAIN MARKER") {
		t.Fatalf("request = %q, want both chain and owner replacements", got)
	}
}

func TestProviderRequestSlotOwnerFailureIsFatal(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{}, errors.New("sidecar timeout")
	}}
	d := newExtSlotDispatcher(client, false, nil, nil,
		map[extension.Slot]string{extension.SlotProviderRequest: extTestPlugin})
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{{Type: provider.ChunkDone}}}
	a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
	err := a.Run(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "extension fake failed at provider.request") {
		t.Fatalf("Run err = %v, want the owner failure", err)
	}
}

func TestProviderResponseSlotOwnerConsulted(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventProviderResponse {
			return replaceWith(t, dispatch.ProviderResponsePayload{Text: "OWNER ANSWER"}), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtSlotDispatcher(client, false, nil, nil,
		map[extension.Slot]string{extension.SlotProviderResponse: extTestPlugin})
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ORIGINAL"}, {Type: provider.ChunkDone},
	}}
	sess := NewSession("sys")
	a := New(mp, tool.NewRegistry(), sess, Options{Extensions: d}, event.Discard)
	if err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	assistants := assistantMessages(sess)
	if len(assistants) != 1 || assistants[0].Content != "OWNER ANSWER" {
		t.Fatalf("assistant turn = %+v, want the slot owner's replacement persisted", assistants)
	}
}

func TestProviderResponseSlotOwnerFinalSayAfterChain(t *testing.T) {
	calls := 0
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev != protocol.EventProviderResponse {
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		}
		calls++
		var in dispatch.ProviderResponsePayload
		if err := json.Unmarshal(payload, &in); err != nil {
			return protocol.InterceptResult{}, err
		}
		if calls == 1 {
			return replaceWith(t, dispatch.ProviderResponsePayload{Text: "CHAIN TEXT"}), nil
		}
		if in.Text != "CHAIN TEXT" {
			t.Errorf("strategy phase received %q, want the chain's output", in.Text)
		}
		return replaceWith(t, dispatch.ProviderResponsePayload{Text: "OWNER TEXT"}), nil
	}}
	d := newExtSlotDispatcher(client, false, nil,
		[]extension.InterceptorPoint{extension.PointProviderResponse},
		map[extension.Slot]string{extension.SlotProviderResponse: extTestPlugin})
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ORIGINAL"}, {Type: provider.ChunkDone},
	}}
	sess := NewSession("sys")
	a := New(mp, tool.NewRegistry(), sess, Options{Extensions: d}, event.Discard)
	if err := a.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls != 2 {
		t.Fatalf("owner consulted %d times, want 2 (chain, then strategy)", calls)
	}
	assistants := assistantMessages(sess)
	if len(assistants) != 1 || assistants[0].Content != "OWNER TEXT" {
		t.Fatalf("assistant turn = %+v, want the strategy ruling persisted", assistants)
	}
}

func TestProviderResponseSlotOwnerFailureIsFatal(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{}, errors.New("sidecar timeout")
	}}
	d := newExtSlotDispatcher(client, false, nil, nil,
		map[extension.Slot]string{extension.SlotProviderResponse: extTestPlugin})
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "ORIGINAL"}, {Type: provider.ChunkDone},
	}}
	sess := NewSession("sys")
	a := New(mp, tool.NewRegistry(), sess, Options{Extensions: d}, event.Discard)
	err := a.Run(context.Background(), "hello")
	if err == nil || !strings.Contains(err.Error(), "extension fake failed at provider.response") {
		t.Fatalf("Run err = %v, want the owner failure", err)
	}
	if n := len(assistantMessages(sess)); n != 0 {
		t.Fatalf("failed owner ruling persisted %d assistant turns, want 0", n)
	}
}

func TestPermissionDecisionSlotOwnerVeto(t *testing.T) {
	// The owner declared ONLY replaces. Its block vetoes even a chain allow —
	// and here even the host allow.
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventPermissionDecision {
			return blockWith("owner policy says no"), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtSlotDispatcher(client, false, nil, nil,
		map[extension.Slot]string{extension.SlotPermission: extTestPlugin})
	rec := &recordingTool{name: "edit_file", readOnly: false}
	reg := tool.NewRegistry()
	reg.Add(rec)
	a := New(nil, reg, NewSession(""), Options{Gate: &stubGate{}, Extensions: d}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "edit_file", Arguments: `{"path":"/x"}`})
	if !out.blocked || !strings.Contains(out.output, "owner policy says no") {
		t.Fatalf("outcome = %+v, want the owner's veto", out)
	}
	if rec.execs != 0 {
		t.Fatal("owner-vetoed tool executed")
	}
}

func TestPermissionDecisionSlotOwnerFinalAfterChainAllow(t *testing.T) {
	// Both phases: the chain's allow overrides the host deny first, then the
	// owner's strategy block vetoes the call — strategy is the final phase.
	calls := 0
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev != protocol.EventPermissionDecision {
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		}
		calls++
		if calls == 1 {
			return protocol.InterceptResult{Decision: protocol.DecisionAllow}, nil
		}
		return blockWith("owner vetoes the chain allow"), nil
	}}
	d := newExtSlotDispatcher(client, false, nil,
		[]extension.InterceptorPoint{extension.PointPermissionDecision},
		map[extension.Slot]string{extension.SlotPermission: extTestPlugin})
	rec := &recordingTool{name: "edit_file", readOnly: false}
	reg := tool.NewRegistry()
	reg.Add(rec)
	gate := &stubGate{deny: map[string]bool{"edit_file": true}}
	a := New(nil, reg, NewSession(""), Options{Gate: gate, Extensions: d}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "edit_file", Arguments: `{"path":"/x"}`})
	if calls != 2 {
		t.Fatalf("owner consulted %d times, want 2 (chain, then strategy)", calls)
	}
	if !out.blocked || !strings.Contains(out.output, "owner vetoes the chain allow") {
		t.Fatalf("outcome = %+v, want the owner's final veto", out)
	}
	if rec.execs != 0 {
		t.Fatal("owner-vetoed tool executed")
	}
}

func TestPermissionDecisionSlotOwnerFailureIsFatal(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{}, errors.New("sidecar timeout")
	}}
	d := newExtSlotDispatcher(client, false, nil, nil,
		map[extension.Slot]string{extension.SlotPermission: extTestPlugin})
	rec := &recordingTool{name: "edit_file", readOnly: false}
	reg := tool.NewRegistry()
	reg.Add(rec)
	a := New(nil, reg, NewSession(""), Options{Gate: &stubGate{}, Extensions: d}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "edit_file", Arguments: `{"path":"/x"}`})
	if !out.blocked || !strings.Contains(out.output, "extension fake failed at permission.decision") {
		t.Fatalf("outcome = %+v, want the owner failure", out)
	}
	if rec.execs != 0 {
		t.Fatal("failed owner still let the tool run")
	}
}

func TestCompactionPrepareSlotOwnerConsulted(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventCompactionPrepare {
			var in dispatch.CompactionPreparePayload
			if err := json.Unmarshal(payload, &in); err != nil {
				return protocol.InterceptResult{}, err
			}
			in.Guidance = "OWNER GUIDANCE"
			return replaceWith(t, in), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtSlotDispatcher(client, false, nil, nil,
		map[extension.Slot]string{extension.SlotCompaction: extTestPlugin})
	mp, a := newCompactionAgent(t, d)
	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if instruction := mp.requests[0].Messages[len(mp.requests[0].Messages)-1].Content; !strings.Contains(instruction, "OWNER GUIDANCE") {
		t.Fatalf("final summary instruction missing the owner's guidance:\n%.200q", instruction)
	}
}

func TestCompactionPrepareSlotOwnerFinalSayAfterChain(t *testing.T) {
	calls := 0
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev != protocol.EventCompactionPrepare {
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		}
		calls++
		var in dispatch.CompactionPreparePayload
		if err := json.Unmarshal(payload, &in); err != nil {
			return protocol.InterceptResult{}, err
		}
		if calls == 1 {
			in.Guidance = "CHAIN GUIDANCE"
			return replaceWith(t, in), nil
		}
		if in.Guidance != "CHAIN GUIDANCE" {
			t.Errorf("strategy phase guidance = %q, want the chain's output", in.Guidance)
		}
		in.Guidance = "OWNER GUIDANCE"
		return replaceWith(t, in), nil
	}}
	d := newExtSlotDispatcher(client, false, nil,
		[]extension.InterceptorPoint{extension.PointCompactionPrepare},
		map[extension.Slot]string{extension.SlotCompaction: extTestPlugin})
	mp, a := newCompactionAgent(t, d)
	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if calls != 2 {
		t.Fatalf("owner consulted %d times, want 2 (chain, then strategy)", calls)
	}
	instruction := mp.requests[0].Messages[len(mp.requests[0].Messages)-1].Content
	if !strings.Contains(instruction, "OWNER GUIDANCE") || strings.Contains(instruction, "CHAIN GUIDANCE") {
		t.Fatalf("final summary instruction = %.200q, want the strategy ruling to win", instruction)
	}
}

func TestCompactionPrepareSlotOwnerFailureIsFatal(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(protocol.InterceptEvent, json.RawMessage) (protocol.InterceptResult, error) {
		return protocol.InterceptResult{}, errors.New("sidecar timeout")
	}}
	d := newExtSlotDispatcher(client, false, nil, nil,
		map[extension.Slot]string{extension.SlotCompaction: extTestPlugin})
	mp, a := newCompactionAgent(t, d)
	before := len(a.Session().Messages)
	err := a.CompactNow(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "extension fake failed at compaction.prepare") {
		t.Fatalf("CompactNow err = %v, want the owner failure", err)
	}
	if len(mp.requests) != 0 || len(a.Session().Messages) != before {
		t.Fatal("failed owner still ran the summarizer or rewrote the session")
	}
}

func TestCompactionCompleteSlotOwnerConsulted(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventCompactionComplete {
			return replaceWith(t, dispatch.CompactionCompletePayload{Summary: "OWNER SUMMARY"}), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtSlotDispatcher(client, false, nil, nil,
		map[extension.Slot]string{extension.SlotCompaction: extTestPlugin})
	_, a := newCompactionAgent(t, d)
	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if sc := joinContents(visibleContext(a)); !strings.Contains(sc, "OWNER SUMMARY") {
		t.Fatalf("projection missing the owner's summary:\n%.200q", sc)
	}
}

func TestCompactionCompleteSlotOwnerFinalSayAfterChain(t *testing.T) {
	calls := 0
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev != protocol.EventCompactionComplete {
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		}
		calls++
		var in dispatch.CompactionCompletePayload
		if err := json.Unmarshal(payload, &in); err != nil {
			return protocol.InterceptResult{}, err
		}
		if calls == 1 {
			return replaceWith(t, dispatch.CompactionCompletePayload{Summary: "CHAIN SUMMARY"}), nil
		}
		if in.Summary != "CHAIN SUMMARY" {
			t.Errorf("strategy phase summary = %q, want the chain's output", in.Summary)
		}
		return replaceWith(t, dispatch.CompactionCompletePayload{Summary: "OWNER SUMMARY"}), nil
	}}
	d := newExtSlotDispatcher(client, false, nil,
		[]extension.InterceptorPoint{extension.PointCompactionComplete},
		map[extension.Slot]string{extension.SlotCompaction: extTestPlugin})
	_, a := newCompactionAgent(t, d)
	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("CompactNow: %v", err)
	}
	if calls != 2 {
		t.Fatalf("owner consulted %d times, want 2 (chain, then strategy)", calls)
	}
	sc := joinContents(visibleContext(a))
	if !strings.Contains(sc, "OWNER SUMMARY") || strings.Contains(sc, "CHAIN SUMMARY") {
		t.Fatalf("projection = %.200q, want the strategy ruling persisted", sc)
	}
}

func TestCompactionCompleteSlotOwnerFailureIsFatal(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventCompactionComplete {
			return protocol.InterceptResult{}, errors.New("sidecar timeout")
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtSlotDispatcher(client, false, nil, nil,
		map[extension.Slot]string{extension.SlotCompaction: extTestPlugin})
	_, a := newCompactionAgent(t, d)
	before := len(a.Session().Messages)
	err := a.CompactNow(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "extension fake failed at compaction.complete") {
		t.Fatalf("CompactNow err = %v, want the owner failure", err)
	}
	if len(a.Session().Messages) != before {
		t.Fatal("failed owner still rewrote the session")
	}
}

// TestSlotUnownedKeepsFastPath pins the no-owner case: a chain-only plugin
// (intercepts but no replaces) leaves the slot unowned, and the original
// values reach the provider byte-identically.
func TestSlotUnownedKeepsFastPath(t *testing.T) {
	client := &fakeDispatchClient{}
	d := newExtSlotDispatcher(client, false, nil,
		[]extension.InterceptorPoint{extension.PointContextPrepare, extension.PointProviderRequest}, nil)
	streams := [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "one"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "two"}, {Type: provider.ChunkDone}},
	}
	withExt := &mockProvider{name: "p", streams: streams}
	a := New(withExt, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
	baseline := &mockProvider{name: "p", streams: streams}
	b := New(baseline, tool.NewRegistry(), NewSession("sys"), Options{}, event.Discard)
	for _, input := range []string{"first", "second"} {
		if err := a.Run(context.Background(), input); err != nil {
			t.Fatalf("Run(%q): %v", input, err)
		}
		if err := b.Run(context.Background(), input); err != nil {
			t.Fatalf("baseline Run(%q): %v", input, err)
		}
	}
	for i := range baseline.requests {
		if got, want := requestContents(withExt.requests[i]), requestContents(baseline.requests[i]); got != want {
			t.Fatalf("request %d differs from the no-extension baseline:\ngot:\n%s\nwant:\n%s", i+1, got, want)
		}
	}
}
