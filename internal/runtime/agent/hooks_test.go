package agent

import (
	"context"
	"encoding/json"
	"errors"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/hook"
)

// okTool always succeeds; failTool always fails the same way. Together they
// stand in for a hook's happy and error paths without a real tool.
type okTool struct{ name string }

func (o okTool) Name() string                                             { return o.name }
func (o okTool) Description() string                                      { return "always succeeds" }
func (o okTool) Schema() json.RawMessage                                  { return json.RawMessage(`{"type":"object"}`) }
func (o okTool) ReadOnly() bool                                           { return true }
func (o okTool) Execute(context.Context, json.RawMessage) (string, error) { return "ok", nil }

type failTool struct{ name string }

func (f failTool) Name() string            { return f.name }
func (f failTool) Description() string     { return "always fails" }
func (f failTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f failTool) ReadOnly() bool          { return true }
func (f failTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", errors.New("unexpected end of JSON input")
}

func TestToolHooksMayMutateWorkspaceUsesRunnerCapabilities(t *testing.T) {
	if toolHooksMayMutateWorkspace(hook.NewRunner(nil, "/tmp", nil, nil)) {
		t.Fatal("empty hook runner must not create a checkpoint coverage gap")
	}
	sessionOnly := hook.NewRunner([]hook.ResolvedHook{{Event: hook.SessionStart}}, "/tmp", nil, nil)
	if toolHooksMayMutateWorkspace(sessionOnly) {
		t.Fatal("non-tool hooks must not create a tool mutation coverage gap")
	}
	preTool := hook.NewRunner([]hook.ResolvedHook{{Event: hook.PreToolUse}}, "/tmp", nil, nil)
	if !toolHooksMayMutateWorkspace(preTool) {
		t.Fatal("PreToolUse shell hook must preserve the conservative coverage gap")
	}
	if !toolHooksMayMutateWorkspace(&stubHooks{}) {
		t.Fatal("custom legacy ToolHooks without a capability report must remain conservative")
	}
}

// stubHooks blocks PreToolUse for named tools and records what it saw.
type stubHooks struct {
	blockPre        map[string]bool
	preSeen         []string
	postSeen        []string
	postFailureSeen []string
	preCompactOut   string   // returned from PreCompact (extra summary guidance)
	subagentSeen    []string // last-answer text passed to each SubagentStop
	subagentStarts  []string // task arguments passed to each SubagentStart
	subagentStops   []subagentStop
	hasPostLLM      bool     // whether HasPostLLMCall reports a PostLLMCall hook
	postLLMOut      string   // replacement returned from PostLLMCall (when hasPostLLM)
	postLLMSeen     []string // reasoning text each PostLLMCall received
	postLLMTurns    []int    // turn number each PostLLMCall received
}

func (h *stubHooks) PreToolUse(_ context.Context, name string, _ json.RawMessage) (bool, string) {
	h.preSeen = append(h.preSeen, name)
	if h.blockPre[name] {
		return true, "blocked by test hook"
	}
	return false, ""
}

func (h *stubHooks) PostToolUse(_ context.Context, name string, _ json.RawMessage, _ string) {
	h.postSeen = append(h.postSeen, name)
}

func (h *stubHooks) PostToolUseFailure(_ context.Context, name string, _ json.RawMessage, _ string, _ error) {
	h.postFailureSeen = append(h.postFailureSeen, name)
}

type subagentStop struct {
	startID, stopID string
	err             error
	ctxErr          error
}

func (h *stubHooks) SubagentStart(_ context.Context, callID string, args json.RawMessage) {
	h.subagentStarts = append(h.subagentStarts, string(args))
	h.subagentStops = append(h.subagentStops, subagentStop{startID: callID})
}

func (h *stubHooks) SubagentStop(ctx context.Context, callID, last string, err error) {
	h.subagentSeen = append(h.subagentSeen, last)
	if n := len(h.subagentStops); n > 0 {
		s := &h.subagentStops[n-1]
		s.stopID, s.err, s.ctxErr = callID, err, ctx.Err()
	}
}
func (h *stubHooks) PreCompact(context.Context, string) string { return h.preCompactOut }

func (h *stubHooks) PostLLMCall(_ context.Context, reasoning string, turn int) string {
	h.postLLMSeen = append(h.postLLMSeen, reasoning)
	h.postLLMTurns = append(h.postLLMTurns, turn)
	if h.hasPostLLM && h.postLLMOut != "" {
		return h.postLLMOut
	}
	return reasoning
}

func (h *stubHooks) HasPostLLMCall() bool { return h.hasPostLLM }

// TestSubagentStopFiresForForegroundTask checks SubagentStop fires (with the
// sub-agent's answer) when a foreground `task` call completes, but not for a
// backgrounded one (which only returns a "started" handle and stops later).
func TestSubagentStopFiresForForegroundTask(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(okTool{name: "task"}) // stands in for the real task tool; returns "ok"
	h := &stubHooks{}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: h}, event.Discard)

	a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{{Name: "task", Arguments: `{"prompt":"x"}`}})
	if len(h.subagentSeen) != 1 || h.subagentSeen[0] != "ok" {
		t.Fatalf("foreground task should fire SubagentStop with the answer, saw %v", h.subagentSeen)
	}

	a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{{Name: "task", Arguments: `{"run_in_background":true}`}})
	if len(h.subagentSeen) != 1 {
		t.Errorf("backgrounded task must not fire SubagentStop, saw %v", h.subagentSeen)
	}
}

// scriptedTask stands in for the task tool with a chosen outcome.
type scriptedTask struct {
	run func(context.Context) (string, error)
}

func (scriptedTask) Name() string            { return "task" }
func (scriptedTask) Description() string     { return "scripted sub-agent" }
func (scriptedTask) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (scriptedTask) ReadOnly() bool          { return true }
func (s scriptedTask) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	return s.run(ctx)
}

// TestSubagentStartPairsWithSubagentStop checks every foreground `task` call
// that announces a start also announces a stop, whether the sub-agent answers,
// fails, is cancelled, or refuses its own call; background tasks fire neither.
func TestSubagentStartPairsWithSubagentStop(t *testing.T) {
	cases := []struct {
		name    string
		run     func(context.Context, context.CancelFunc) (string, error)
		wantErr bool
	}{
		{"answers", func(context.Context, context.CancelFunc) (string, error) { return "ok", nil }, false},
		{"fails", func(context.Context, context.CancelFunc) (string, error) { return "", errors.New("sub-agent crashed") }, true},
		{"cancelled", func(ctx context.Context, cancel context.CancelFunc) (string, error) {
			cancel()
			<-ctx.Done()
			return "", ctx.Err()
		}, true},
		{"refuses", func(context.Context, context.CancelFunc) (string, error) {
			return "", tool.Blocked("no sub-agent here")
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reg := tool.NewRegistry()
			reg.Add(scriptedTask{run: func(c context.Context) (string, error) { return tc.run(c, cancel) }})
			h := &stubHooks{}
			a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: h}, event.Discard)

			a.executeBatch(ctx, &a.turn, []provider.ToolCall{{ID: "call-1", Name: "task", Arguments: `{"prompt":"x"}`}})
			if len(h.subagentStarts) != 1 || h.subagentStarts[0] != `{"prompt":"x"}` {
				t.Fatalf("foreground task should fire SubagentStart with its arguments, saw %v", h.subagentStarts)
			}
			if len(h.subagentSeen) != 1 {
				t.Fatalf("SubagentStart must be followed by exactly one SubagentStop, saw %d", len(h.subagentSeen))
			}
			stop := h.subagentStops[0]
			if stop.startID != "call-1" || stop.stopID != "call-1" {
				t.Errorf("call ids = start %q stop %q, want both call-1", stop.startID, stop.stopID)
			}
			if (stop.err != nil) != tc.wantErr {
				t.Errorf("SubagentStop err = %v, want error=%v", stop.err, tc.wantErr)
			}
			if stop.ctxErr != nil {
				t.Errorf("SubagentStop ran under a done context (%v); its hook would be killed", stop.ctxErr)
			}
		})
	}

	reg := tool.NewRegistry()
	reg.Add(okTool{name: "task"})
	h := &stubHooks{}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: h}, event.Discard)
	a.executeBatch(context.Background(), &a.turn, []provider.ToolCall{{Name: "task", Arguments: `{"run_in_background":true}`}})
	if len(h.subagentStarts) != 0 || len(h.subagentSeen) != 0 {
		t.Errorf("backgrounded task must fire neither event, saw starts=%v stops=%v", h.subagentStarts, h.subagentSeen)
	}
}

// TestPreToolUseHookBlocks proves a gating PreToolUse hook refuses a tool call
// (returning a blocked result, never running the tool or its PostToolUse), while
// an unblocked call runs and fires PostToolUse.
func TestPreToolUseHookBlocks(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	reg.Add(fakeTool{name: "read_file", readOnly: true})

	h := &stubHooks{blockPre: map[string]bool{"bash": true}}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: h}, event.Discard)

	blocked := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "bash", Arguments: `{"command":"x"}`})
	if !blocked.blocked || !strings.HasPrefix(blocked.output, "blocked:") {
		t.Errorf("PreToolUse block should yield a blocked result, got %+v", blocked)
	}
	if !strings.Contains(blocked.output, "blocked by test hook") {
		t.Errorf("block reason should be surfaced to the model, got %q", blocked.output)
	}

	ok := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "read_file", Arguments: `{"path":"/a"}`})
	if ok.blocked || !strings.Contains(ok.output, "done") {
		t.Errorf("unblocked call should run, got %+v", ok)
	}

	if got := strings.Join(h.preSeen, ","); got != "bash,read_file" {
		t.Errorf("PreToolUse should fire for both calls, saw %q", got)
	}
	// PostToolUse fires only for the call that actually ran.
	if got := strings.Join(h.postSeen, ","); got != "read_file" {
		t.Errorf("PostToolUse should fire only for the run tool, saw %q", got)
	}
}

func TestPostToolUseFailureUsesFailureHook(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(failTool{name: "broken"})
	h := &stubHooks{}
	a := New(nil, reg, sessionstore.NewSession(""), Options{Hooks: h}, event.Discard)
	a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "broken", Arguments: `{}`})
	if got := strings.Join(h.postFailureSeen, ","); got != "broken" {
		t.Fatalf("failure hooks = %q", got)
	}
	if len(h.postSeen) != 0 {
		t.Fatalf("success hook fired for failure: %v", h.postSeen)
	}
}
