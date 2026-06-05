package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// stubHooks blocks PreToolUse for named tools and records what it saw.
type stubHooks struct {
	blockPre      map[string]bool
	preSeen       []string
	postSeen      []string
	preCompactOut string   // returned from PreCompact (extra summary guidance)
	subagentSeen  []string // last-answer text passed to each SubagentStop
	hasPostLLM    bool     // whether HasPostLLMCall reports a PostLLMCall hook
	postLLMOut    string   // replacement returned from PostLLMCall (when hasPostLLM)
	postLLMSeen   []string // reasoning text each PostLLMCall received
	postLLMTurns  []int    // turn number each PostLLMCall received
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

func (h *stubHooks) SubagentStop(_ context.Context, last string) {
	h.subagentSeen = append(h.subagentSeen, last)
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
	a := New(nil, reg, NewSession(""), Options{Hooks: h}, event.Discard)

	a.executeBatch(context.Background(), []provider.ToolCall{{Name: "task", Arguments: `{"prompt":"x"}`}})
	if len(h.subagentSeen) != 1 || h.subagentSeen[0] != "ok" {
		t.Fatalf("foreground task should fire SubagentStop with the answer, saw %v", h.subagentSeen)
	}

	a.executeBatch(context.Background(), []provider.ToolCall{{Name: "task", Arguments: `{"run_in_background":true}`}})
	if len(h.subagentSeen) != 1 {
		t.Errorf("backgrounded task must not fire SubagentStop, saw %v", h.subagentSeen)
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
	a := New(nil, reg, NewSession(""), Options{Hooks: h}, event.Discard)

	blocked := a.executeOne(context.Background(), provider.ToolCall{Name: "bash", Arguments: `{"command":"x"}`})
	if !blocked.blocked || !strings.HasPrefix(blocked.output, "blocked:") {
		t.Errorf("PreToolUse block should yield a blocked result, got %+v", blocked)
	}
	if !strings.Contains(blocked.output, "blocked by test hook") {
		t.Errorf("block reason should be surfaced to the model, got %q", blocked.output)
	}

	ok := a.executeOne(context.Background(), provider.ToolCall{Name: "read_file", Arguments: `{"path":"/a"}`})
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
