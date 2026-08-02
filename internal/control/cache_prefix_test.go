package control

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// TestHistoryPrefixByteStableAcrossTurns is the cache-hit-rate bedrock: DeepSeek's
// context cache matches full prefix units, so any byte drift in the persisted
// history between adjacent requests misses the whole cached prefix. This test
// asserts that turn N+1's request messages are byte-identical to turn N's
// request messages for every message except the newly appended ones (the new
// user turn, its assistant reply, and any tool messages it produced).
func TestHistoryPrefixByteStableAcrossTurns(t *testing.T) {
	prov := testutil.NewMock("deepseek-v4-flash",
		testutil.Turn{Text: "answer one"},
		testutil.Turn{Text: "answer two"},
	)
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession("system head"), agent.Options{MaxSteps: 1}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag, Sink: event.Discard, SessionDir: t.TempDir()})
	c.SetReasoningLanguage("zh")

	// First turn: a Chinese prompt (auto reasoning-language injects the zh block).
	one := c.Compose("第一个问题")
	if err := c.RunTurn(context.Background(), one); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	hist1 := jsonMessages(c.History())
	if len(hist1) < 2 {
		t.Fatalf("turn 1 history too short: %d", len(hist1))
	}

	// Second turn appends; every pre-existing message must stay byte-identical.
	two := c.Compose("第二个问题")
	if err := c.RunTurn(context.Background(), two); err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	hist2 := jsonMessages(c.History())

	// The first turn's messages must appear unchanged as a prefix of the second.
	for i := range hist1 {
		if i >= len(hist2) {
			t.Fatalf("history shrank from %d to %d messages", len(hist1), len(hist2))
		}
		if hist1[i] != hist2[i] {
			t.Fatalf("message %d drifted between turns — this invalidates the DeepSeek prefix cache.\nbefore: %s\nafter:  %s", i, hist1[i], hist2[i])
		}
	}
}

// TestHistoryPrefixByteStableAcrossToolTurns is the same bedrock for tool-heavy
// sessions: assistant tool_calls turns and their reasoning replay sit in the
// persisted history, so they too must stay byte-identical between adjacent
// requests for the prefix cache to keep hitting through them.
func TestHistoryPrefixByteStableAcrossToolTurns(t *testing.T) {
	prov := testutil.NewMock("deepseek-v4-flash",
		testutil.Turn{ToolCalls: []provider.ToolCall{{
			ID: "call_1", Name: "read_file", Arguments: `{"path":"config.toml"}`,
		}}},
		testutil.Turn{Text: "checked the config"},
		testutil.Turn{Text: "final answer"},
	)
	reg := tool.NewRegistry()
	reg.Add(mustBuiltinControlTool(t, "read_file"))
	ag := agent.New(prov, reg, agent.NewSession("system head"), agent.Options{MaxSteps: 1}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag, Sink: event.Discard, SessionDir: t.TempDir()})
	c.SetReasoningLanguage("zh")

	one := c.Compose("看下配置文件")
	if err := c.RunTurn(context.Background(), one); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	hist1 := jsonMessages(c.History())

	two := c.Compose("继续")
	if err := c.RunTurn(context.Background(), two); err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	hist2 := jsonMessages(c.History())

	// The assistant tool_calls turn (with reasoning replay) and its tool result
	// must be unchanged when they become part of turn 2's prefix.
	for i := range hist1 {
		if i >= len(hist2) {
			t.Fatalf("history shrank from %d to %d messages", len(hist1), len(hist2))
		}
		if hist1[i] != hist2[i] {
			t.Fatalf("message %d drifted across tool turns — prefix cache broken.\nbefore: %s\nafter:  %s", i, hist1[i], hist2[i])
		}
	}
}

func mustBuiltinControlTool(t *testing.T, name string) tool.Tool {
	t.Helper()
	builtin, ok := tool.LookupBuiltin(name)
	if !ok {
		t.Fatalf("builtin %q is not registered", name)
	}
	return builtin
}

func jsonMessages(msgs []provider.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		b, _ := json.Marshal(provider.ModelMessages([]provider.Message{m})[0])
		out = append(out, string(b))
	}
	return out
}
