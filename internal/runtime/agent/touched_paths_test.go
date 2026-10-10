package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/dispatch"
	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/state/sessionstore"
)

type recordedPaths struct {
	mu    sync.Mutex
	paths []string
	reset int
}

func (r *recordedPaths) Observe(p string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, p)
	return nil
}

func (r *recordedPaths) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths, r.reset = nil, r.reset+1
}

func (r *recordedPaths) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]string(nil), r.paths...)
	slices.Sort(out)
	return out
}

type pathTool struct {
	name     string
	readOnly bool
	read     string
	writes   []string
	writeErr error
	failWith error
}

func (p pathTool) Name() string            { return p.name }
func (p pathTool) Description() string     { return p.name }
func (p pathTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (p pathTool) ReadOnly() bool          { return p.readOnly }
func (p pathTool) Execute(context.Context, json.RawMessage) (string, error) {
	if p.failWith != nil {
		return "", p.failWith
	}
	return "done", nil
}

type readingTool struct{ pathTool }

func (r readingTool) ReadTarget(json.RawMessage) string { return r.read }

type writingTool struct{ pathTool }

func (w writingTool) WritePaths(json.RawMessage) ([]string, error) { return w.writes, w.writeErr }

func touchedAgent(t *testing.T, obs PathObserver, opts Options, tools ...tool.Tool) *Agent {
	t.Helper()
	reg := tool.NewRegistry()
	for _, tl := range tools {
		reg.Add(tl)
	}
	a := New(nil, reg, sessionstore.NewSession("sys"), opts, event.Discard)
	a.SetPathObserver(obs)
	return a
}

func callOnce(a *Agent, name, args string) {
	a.executeOne(context.Background(), &a.turn, provider.ToolCall{ID: "c-" + name, Name: name, Arguments: args})
}

func TestCompletedReadAndWriteCallsFeedTheObserver(t *testing.T) {
	obs := &recordedPaths{}
	a := touchedAgent(t, obs, Options{},
		readingTool{pathTool{name: "read_file", readOnly: true, read: "/ws/a.go"}},
		writingTool{pathTool{name: "edit_file", writes: []string{"/ws/b.go", "/ws/c.go"}}},
	)
	callOnce(a, "read_file", `{"path":"a.go"}`)
	callOnce(a, "edit_file", `{"path":"b.go"}`)
	if got, want := obs.seen(), []string{"/ws/a.go", "/ws/b.go", "/ws/c.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("observed %q, want %q", got, want)
	}
}

func TestFailedCallDoesNotFeedTheObserver(t *testing.T) {
	obs := &recordedPaths{}
	a := touchedAgent(t, obs, Options{},
		readingTool{pathTool{name: "read_file", readOnly: true, read: "/ws/a.go", failWith: errors.New("no such file")}},
		writingTool{pathTool{name: "edit_file", writes: []string{"/ws/b.go"}, failWith: errors.New("no match")}},
	)
	callOnce(a, "read_file", `{"path":"a.go"}`)
	callOnce(a, "edit_file", `{"path":"b.go"}`)
	if got := obs.seen(); len(got) != 0 {
		t.Fatalf("failed calls fed %q", got)
	}
}

func TestHookBlockedCallDoesNotFeedTheObserver(t *testing.T) {
	obs := &recordedPaths{}
	h := &stubHooks{blockPre: map[string]bool{"read_file": true}}
	a := touchedAgent(t, obs, Options{Hooks: h},
		readingTool{pathTool{name: "read_file", readOnly: true, read: "/ws/a.go"}},
	)
	callOnce(a, "read_file", `{"path":"a.go"}`)
	if got := obs.seen(); len(got) != 0 {
		t.Fatalf("a call a PreToolUse hook blocked fed %q", got)
	}
}

func TestRefusedByTheGateDoesNotFeedTheObserver(t *testing.T) {
	obs := &recordedPaths{}
	a := touchedAgent(t, obs, Options{Gate: denyAllGate{}},
		writingTool{pathTool{name: "edit_file", writes: []string{"/ws/b.go"}}},
	)
	callOnce(a, "edit_file", `{"path":"b.go"}`)
	if got := obs.seen(); len(got) != 0 {
		t.Fatalf("a call the gate refused fed %q", got)
	}
}

func TestToolWithoutADeclarationFeedsNothingWhateverItsArguments(t *testing.T) {
	obs := &recordedPaths{}
	a := touchedAgent(t, obs, Options{}, pathTool{name: "bash"}, pathTool{name: "custom", readOnly: true})
	callOnce(a, "bash", `{"command":"cat /ws/a.go","path":"/ws/a.go"}`)
	callOnce(a, "custom", `{"path":"/ws/a.go","file_path":"/ws/b.go"}`)
	if got := obs.seen(); len(got) != 0 {
		t.Fatalf("undeclared paths fed %q", got)
	}
}

func TestWriterThatCannotResolveItsPathsFeedsNothing(t *testing.T) {
	obs := &recordedPaths{}
	a := touchedAgent(t, obs, Options{},
		writingTool{pathTool{name: "edit_file", writes: []string{"/ws/b.go"}, writeErr: errors.New("ambiguous")}},
	)
	callOnce(a, "edit_file", `{"path":"b.go"}`)
	if got := obs.seen(); len(got) != 0 {
		t.Fatalf("unresolvable write fed %q", got)
	}
}

func TestParallelToolCallsAllFeedTheObserver(t *testing.T) {
	obs := &recordedPaths{}
	reg := tool.NewRegistry()
	reg.Add(readingTool{pathTool{name: "read_file", readOnly: true, read: "/ws/a.go"}})
	reg.Add(readingTool{pathTool{name: "grep", readOnly: true, read: "/ws/b.go"}})
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("1", "read_file", `{}`), toolCallChunk("2", "grep", `{}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession("sys"), Options{}, event.Discard)
	a.SetPathObserver(obs)
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatal(err)
	}
	if got, want := obs.seen(), []string{"/ws/a.go", "/ws/b.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("observed %q, want %q", got, want)
	}
}

func TestAgentWithoutAnObserverStaysQuiet(t *testing.T) {
	a := touchedAgent(t, nil, Options{}, readingTool{pathTool{name: "read_file", readOnly: true, read: "/ws/a.go"}})
	callOnce(a, "read_file", `{}`)
}

func sessionWith(msgs ...provider.Message) *sessionstore.Session {
	s := sessionstore.NewSession("sys")
	for _, m := range msgs {
		s.Add(m)
	}
	return s
}

func callMsg(id, name string) provider.Message {
	return provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: name, Arguments: `{}`}}}
}

func resultMsg(id, name string, failure *provider.ToolFailure) provider.Message {
	return provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: name, Content: "x", ToolFailure: failure}
}

func TestSwappingTheSessionRebuildsTheObservedSetFromTheTranscript(t *testing.T) {
	obs := &recordedPaths{}
	a := touchedAgent(t, obs, Options{},
		readingTool{pathTool{name: "read_file", readOnly: true, read: "/ws/read.go"}},
		writingTool{pathTool{name: "edit_file", writes: []string{"/ws/edited.go"}}},
		readingTool{pathTool{name: "grep", readOnly: true, read: "/ws/refused.go"}},
	)
	callOnce(a, "read_file", `{}`)
	a.SetSession(sessionWith(
		callMsg("1", "read_file"), resultMsg("1", "read_file", nil),
		callMsg("2", "edit_file"), resultMsg("2", "edit_file", nil),
		callMsg("3", "grep"), resultMsg("3", "grep", &provider.ToolFailure{Blocked: true}),
		callMsg("4", "read_file"),
		callMsg("5", "removed_tool"), resultMsg("5", "removed_tool", nil),
	))
	if got, want := obs.seen(), []string{"/ws/edited.go", "/ws/read.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after resume observed %q, want only completed calls %q", got, want)
	}
	a.SetSession(sessionstore.NewSession("sys"))
	if got := obs.seen(); len(got) != 0 {
		t.Fatalf("a fresh session kept %q", got)
	}
}

func TestInstallingTheObserverSeedsItFromTheCurrentConversation(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(readingTool{pathTool{name: "read_file", readOnly: true, read: "/ws/a.go"}})
	a := New(nil, reg, sessionWith(callMsg("1", "read_file"), resultMsg("1", "read_file", nil)), Options{}, event.Discard)
	obs := &recordedPaths{}
	a.SetPathObserver(obs)
	if got, want := obs.seen(), []string{"/ws/a.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("seeded %q, want %q", got, want)
	}
}

type argPathTool struct{ pathTool }

func (a argPathTool) ReadTarget(args json.RawMessage) string {
	var p struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(args, &p)
	return p.Path
}

// A call an extension rewrote touched what it executed with; the transcript
// only keeps what the model wrote, so a replay can only name that.
func TestRewrittenCallFeedsExecutedArgumentsLiveAndOriginalOnesOnReplay(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventToolBefore {
			return replaceWith(t, dispatch.ToolBeforePayload{Name: "read_file", Arguments: `{"path":"/substituted.go"}`}), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointToolBefore)
	obs := &recordedPaths{}
	a := touchedAgent(t, obs, Options{Extensions: d}, argPathTool{pathTool{name: "read_file", readOnly: true}})

	callOnce(a, "read_file", `{"path":"/original.go"}`)
	if got, want := obs.seen(), []string{"/substituted.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("live observed %q, want the executed %q", got, want)
	}

	a.SetSession(sessionWith(
		provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "read_file", Arguments: `{"path":"/original.go"}`}}},
		resultMsg("1", "read_file", nil),
	))
	if got, want := obs.seen(), []string{"/original.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("replay observed %q, want the recorded %q", got, want)
	}
}

// Compaction folds what the model sees and leaves the canonical transcript
// whole, so a conversation reopened after one still replays every call.
func TestCompactionDoesNotCostTheReplayAnyTouchedPath(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(readingTool{pathTool{name: "read_file", readOnly: true, read: "/ws/early.go"}})
	sess := &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		callMsg("1", "read_file"), resultMsg("1", "read_file", nil),
		{Role: provider.RoleAssistant, Content: strings.Repeat("a ", 5000)},
		{Role: provider.RoleUser, Content: "c"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("b ", 5000)},
		{Role: provider.RoleUser, Content: "e"},
		{Role: provider.RoleAssistant, Content: "f"},
	}}
	opts := Options{ContextWindow: 10_000, CompactRatio: 0.8, RecentKeep: 2}
	a := New(&fakeProvider{reply: "s"}, reg, sess, opts, event.Discard)
	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 8500})
	if !hasCompactionSummary(visibleContext(a)) {
		t.Fatal("the fixture did not compact")
	}
	obs := &recordedPaths{}
	a.SetPathObserver(obs)
	if got, want := obs.seen(), []string{"/ws/early.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after compaction the replay saw %q, want %q", got, want)
	}
}

type proxyTool struct {
	pathTool
	target tool.Tool
}

func (p proxyTool) ResolveCall(_ context.Context, args json.RawMessage) (tool.ResolvedCall, error) {
	return tool.ResolvedCall{DisplayName: p.name, TargetName: p.target.Name(), Args: p.TargetArgs(args), Target: p.target, ReadOnly: true, ProxyAction: "call"}, nil
}

func (proxyTool) TargetArgs(args json.RawMessage) json.RawMessage {
	var p struct {
		Action    string          `json:"action"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(args, &p) != nil || p.Action != "call" {
		return nil
	}
	return p.Arguments
}

func TestCallThroughAProxyFeedsLiveAndReplaysAsItsTarget(t *testing.T) {
	obs := &recordedPaths{}
	target := argPathTool{pathTool{name: "grep", readOnly: true}}
	proxy := proxyTool{pathTool: pathTool{name: "use_capability", readOnly: true}, target: target}
	a := touchedAgent(t, obs, Options{}, proxy, target)
	wrapped := `{"action":"call","capability_id":"tool:grep","arguments":{"path":"/ws/a.go"}}`

	callOnce(a, "use_capability", wrapped)
	if got, want := obs.seen(), []string{"/ws/a.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("live observed %q, want %q", got, want)
	}

	proxied := func(id, resolved, args string) []provider.Message {
		return []provider.Message{
			{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "use_capability", Arguments: args, ResolvedName: resolved}}},
			resultMsg(id, "use_capability", nil),
		}
	}
	var msgs []provider.Message
	msgs = append(msgs, proxied("1", "grep", wrapped)...)
	msgs = append(msgs, proxied("2", "", `{"action":"search","arguments":{"path":"/ws/searched.go"}}`)...)
	msgs = append(msgs, proxied("3", "removed_target", wrapped)...)
	a.SetSession(sessionWith(msgs...))
	if got, want := obs.seen(), []string{"/ws/a.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("replay observed %q, want only the call whose recorded target resolves: %q", got, want)
	}
}
