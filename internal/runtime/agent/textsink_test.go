package agent

import (
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// TestTextSinkReproducesInlineOutput pins the byte-exact rendering of a turn's
// event stream with no markdown renderer (the piped / chat-bridge path).
func TestTextSinkReproducesInlineOutput(t *testing.T) {
	var b strings.Builder
	s := NewTextSink(&b, nil, 80)

	s.Emit(event.Event{Kind: event.TurnStarted})
	s.Emit(event.Event{Kind: event.Reasoning, Text: "let me think"})
	s.Emit(event.Event{Kind: event.Text, Text: "Hello"})
	s.Emit(event.Event{Kind: event.Message, Text: "Hello", Reasoning: "let me think"})
	s.Emit(event.Event{Kind: event.Usage, Usage: &provider.Usage{
		PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200,
		CacheHitTokens: 900, CacheMissTokens: 100,
	}})
	s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{Name: "read_file", Args: `{"path":"a"}`}})
	s.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "read_file", Output: "contents"}})
	s.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "bash", Err: "blocked by permission policy"}})
	s.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "tool output truncated: 5 of 100 bytes elided"})
	s.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "response truncated: hit max output tokens"})

	want := "\x1b[2m  ▎ thinking\x1b[0m\n" + // reasoning header
		"Hello" + // answer delta
		"\n" + // Message close (no renderer)
		"  · 1200 tok · in 1000 (900 cached / 100 new) · out 200\n" + // usage
		"  -> read_file {\"path\":\"a\"}\n" + // tool dispatch
		// successful read_file result is silent
		"  ⊘ bash blocked by permission policy\n" + // blocked result
		"  · tool output truncated: 5 of 100 bytes elided\n" + // info notice
		"  ! response truncated: hit max output tokens\n" // warn notice

	if got := b.String(); got != want {
		t.Errorf("TextSink output mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestTextSinkCanShowReasoningInVerboseMode(t *testing.T) {
	var b strings.Builder
	s := NewTextSink(&b, nil, 80)
	s.SetShowReasoning(true)

	s.Emit(event.Event{Kind: event.TurnStarted})
	s.Emit(event.Event{Kind: event.Reasoning, Text: "let me think"})
	s.Emit(event.Event{Kind: event.Text, Text: "Hello"})
	s.Emit(event.Event{Kind: event.Message, Text: "Hello", Reasoning: "let me think"})

	want := "\x1b[2m  ▎ thinking\x1b[0m\n" +
		"\x1b[2mlet me think\x1b[0m" +
		"\n" +
		"Hello\n"
	if got := b.String(); got != want {
		t.Errorf("verbose TextSink output mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestTextSinkFormatsCapabilityProxyForHeadlessCLI(t *testing.T) {
	var b strings.Builder
	s := NewTextSink(&b, nil, 80)
	args := `{"action":"call","capability_id":"mcp-tool:github/search_issues","arguments":{"query":"bug"}}`

	s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{Name: "use_capability", Args: args}})
	s.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		Name: "use_capability", Args: args, Err: "blocked by permission policy",
	}})
	s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
		Name: "use_capability", Args: `{"action":"list"}`,
	}})

	want := "  -> MCP(mcp-tool:github/search_issues)\n" +
		"  ⊘ MCP(mcp-tool:github/search_issues) blocked by permission policy\n" +
		"  -> MCP(list)\n"
	if got := b.String(); got != want {
		t.Fatalf("TextSink capability output mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// fakeRenderer returns a fixed string so the redraw escape sequence is testable
// without a real markdown library.
type fakeRenderer struct{}

func (fakeRenderer) Render(string) string { return "RENDERED" }

// TestTextSinkRedrawsWithRenderer pins the cursor-control redraw: with a
// renderer wired in, a completed text stream is moved over and replaced by the
// styled output. "ab\ncd" occupies one extra row (one newline), so the cursor
// moves up 1 and clears to end of screen.
func TestTextSinkRedrawsWithRenderer(t *testing.T) {
	var b strings.Builder
	s := NewTextSink(&b, fakeRenderer{}, 80)

	s.Emit(event.Event{Kind: event.TurnStarted})
	s.Emit(event.Event{Kind: event.Text, Text: "ab\ncd"})
	s.Emit(event.Event{Kind: event.Message, Text: "ab\ncd"})

	want := "ab\ncd" + "\r\033[1A\033[0J" + "RENDERED"
	if got := b.String(); got != want {
		t.Errorf("redraw output mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// TestTextSinkKeepsPlannerAndExecutorApartInOneTurn is the dual-model render
// fixture. Planner work and executor work are now one top-level turn, so the
// phase marker is the only thing left between them: it is what has to carry the
// separation a second turn boundary used to provide. Without that, the
// executor's thinking joins the planner's block and reads as one model's.
func TestTextSinkKeepsPlannerAndExecutorApartInOneTurn(t *testing.T) {
	var b strings.Builder
	s := NewTextSink(&b, nil, 80)
	s.SetShowReasoning(true)

	s.Emit(event.Event{Kind: event.TurnStarted})
	s.Emit(event.Event{Kind: event.Phase, Text: "planner · planning"})
	s.Emit(event.Event{Kind: event.Reasoning, Text: "what does this need"})
	s.Emit(event.Event{Kind: event.Text, Text: "1. do the thing"})
	s.Emit(event.Event{Kind: event.Phase, Text: "executor · executing"})
	s.Emit(event.Event{Kind: event.Reasoning, Text: "starting on step 1"})
	s.Emit(event.Event{Kind: event.Text, Text: "done"})

	want := "[planner · planning]\n" +
		"\x1b[2m  ▎ thinking\x1b[0m\n" +
		"\x1b[2mwhat does this need\x1b[0m" +
		"\n1. do the thing" + // the planner's answer, split from its reasoning
		"\n[executor · executing]\n" +
		"\x1b[2m  ▎ thinking\x1b[0m\n" + // the executor's own header, not the planner's
		"\x1b[2mstarting on step 1\x1b[0m" +
		"\ndone"

	if got := b.String(); got != want {
		t.Errorf("dual-model output mismatch:\n got: %q\nwant: %q", got, want)
	}
}
