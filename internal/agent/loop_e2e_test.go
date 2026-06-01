package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func echoRegistry() *tool.Registry {
	reg := tool.NewRegistry()
	reg.Add(echoTool{})
	return reg
}

// TestRunMultiToolRoundEmptyIDsSurvivePairing drives the real loop through a turn
// that fans out two tool calls carrying no id (a gateway that streams by index),
// then asserts both results still pair back after SanitizeToolPairing — the repair
// that runs on every send. Keying on tool_call_id alone collapsed them into one,
// dropping a result from the model's context on the very next turn.
func TestRunMultiToolRoundEmptyIDsSurvivePairing(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{
			{ID: "", Name: "echo", Arguments: `{"text":"alpha"}`},
			{ID: "", Name: "echo", Arguments: `{"text":"beta"}`},
		}},
		testutil.Turn{Text: "done"},
	)
	a := New(mp, echoRegistry(), NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	repaired := provider.SanitizeToolPairing(a.Session().Messages)
	var results []string
	for _, m := range repaired {
		if m.Role == provider.RoleTool {
			results = append(results, m.Content)
		}
	}
	if len(results) != 2 {
		t.Fatalf("want 2 tool results after pairing, got %d: %v", len(results), results)
	}
	if results[0] == results[1] {
		t.Fatalf("both results collapsed to %q — one was lost from the model's context", results[0])
	}
	if !strings.Contains(results[0], "alpha") || !strings.Contains(results[1], "beta") {
		t.Errorf("results lost their identity: %v", results)
	}
}

// TestRunCancelledMidStreamLeavesResumableSession proves a turn cancelled before
// the model answered leaves the session well-formed: the user message stands,
// nothing dangling, and the repaired history is sendable as-is on resume.
func TestRunCancelledMidStreamLeavesResumableSession(t *testing.T) {
	mp := testutil.NewMock("m", testutil.ErrorTurn(context.Canceled))
	a := New(mp, echoRegistry(), NewSession("sys"), Options{}, event.Discard)

	err := a.Run(context.Background(), "do the thing")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run should surface the cancellation, got %v", err)
	}

	repaired := provider.SanitizeToolPairing(a.Session().Messages)
	for i, m := range repaired {
		if m.Role == provider.RoleTool {
			t.Fatalf("a cancelled turn left a dangling tool message at %d: %+v", i, m)
		}
	}
	last := repaired[len(repaired)-1]
	if last.Role != provider.RoleUser || last.Content != "do the thing" {
		t.Errorf("the pending user message should survive a cancel, got %+v", last)
	}
}

// TestRunWellFormedToolLoopRoundTrips is the happy-path baseline: a tool round
// then a final answer. The session must end with the assistant answer and pair
// cleanly (the repair is a no-op on well-formed histories).
func TestRunWellFormedToolLoopRoundTrips(t *testing.T) {
	mp := testutil.NewMock("m",
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"text":"hi"}`}}},
		testutil.Turn{Text: "all set"},
	)
	a := New(mp, echoRegistry(), NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	msgs := a.Session().Messages
	last := msgs[len(msgs)-1]
	if last.Role != provider.RoleAssistant || last.Content != "all set" {
		t.Fatalf("final message should be the assistant answer, got %+v", last)
	}
	before := len(msgs)
	if after := len(provider.SanitizeToolPairing(msgs)); after != before {
		t.Errorf("repair mutated a well-formed session: %d -> %d", before, after)
	}
}
