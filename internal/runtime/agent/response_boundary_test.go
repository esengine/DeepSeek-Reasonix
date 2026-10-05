package agent

import (
	"context"
	"encoding/json"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/state/sessionstore"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// schemaTool stands in for a tool whose schema actually declares a required
// field, which is what makes the ordinary "you omitted it" contract fire. The
// fakeTool used elsewhere declares no properties, so no contract applies to it.
type schemaTool struct {
	name  string
	runs  *atomic.Int32
	fails bool
}

func (s schemaTool) Name() string        { return s.name }
func (s schemaTool) Description() string { return "" }
func (s schemaTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`)
}
func (s schemaTool) ReadOnly() bool { return !s.fails }
func (s schemaTool) Execute(context.Context, json.RawMessage) (string, error) {
	s.runs.Add(1)
	return s.name + " ran", nil
}

func terminalChunk(reason string) provider.Chunk {
	return provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: reason, CompletionTokens: 64}}
}

func boundaryCalls(names ...string) []provider.ToolCall {
	out := make([]provider.ToolCall, 0, len(names))
	for i, n := range names {
		args := `{"command":"ok"}`
		if trimmed, cut := strings.CutPrefix(n, "!"); cut {
			n, args = trimmed, `{"command":"git st`
		}
		out = append(out, provider.ToolCall{ID: string(rune('a' + i)), Name: n, Arguments: args})
	}
	return out
}

func TestClassifyResponseBoundaryDropsOnlyTheTruncatedTail(t *testing.T) {
	in := boundaryCalls("edit_file", "read_file", "!bash")
	got := classifyResponseBoundary(&provider.Usage{FinishReason: "length"}, in)

	if len(got.committed) != 2 {
		t.Fatalf("committed %d calls, want the 2 that were emitted whole", len(got.committed))
	}
	if got.dropped != "bash" {
		t.Fatalf("dropped = %q, want the incomplete trailing call", got.dropped)
	}
	if !strings.Contains(got.fact, "output-token limit") || !strings.Contains(got.fact, `"bash"`) {
		t.Fatalf("fact = %q, want it to name the limit and the dropped call", got.fact)
	}
}

func TestClassifyResponseBoundaryKeepsCompleteCallsUnderTruncation(t *testing.T) {
	in := boundaryCalls("edit_file", "read_file")
	got := classifyResponseBoundary(&provider.Usage{FinishReason: "length"}, in)

	// A cut that landed after the last call closed makes none of them suspect.
	if len(got.committed) != 2 || got.dropped != "" {
		t.Fatalf("committed %d dropped %q, want all 2 kept and nothing dropped", len(got.committed), got.dropped)
	}
	if got.fact == "" {
		t.Fatal("a truncated response owes the model an attribution even when every call survived")
	}
}

// A malformed call the model finished writing is its own to fix, whatever the
// terminal says. The fixture is a syntax error rather than a cut: arguments
// that stop mid-value are evidence of truncation on their own, and the case
// for that is TestBoundaryDropsACutCallOnACleanTerminal.
func TestClassifyResponseBoundaryLeavesNormalTerminalsAlone(t *testing.T) {
	for _, reason := range []string{"stop", "tool_calls", "content_filter"} {
		calls := []provider.ToolCall{
			{ID: "a", Name: "edit_file", Arguments: `{"command":"ok"}`},
			{ID: "b", Name: "bash", Arguments: `{"command":"ok",}`},
		}
		got := classifyResponseBoundary(&provider.Usage{FinishReason: reason}, calls)
		if len(got.committed) != 2 || got.dropped != "" || got.fact != "" {
			t.Fatalf("finish=%q: committed %d dropped %q fact %q, want the model's own malformed call left intact",
				reason, len(got.committed), got.dropped, got.fact)
		}
	}
}

// TestTruncatedTailNeverRunsAndNeverEntersHistory is the effect regression at
// the final boundary: what executed, what the session persisted, and what the
// next provider request carried.
func TestTruncatedTailNeverRunsAndNeverEntersHistory(t *testing.T) {
	var edits, bashes atomic.Int32
	reg := tool.NewRegistry()
	reg.Add(schemaTool{name: "edit_file", runs: &edits})
	reg.Add(schemaTool{name: "bash", runs: &bashes})

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{
			toolCallChunk("c1", "edit_file", `{"command":"apply the patch"}`),
			toolCallChunk("c2", "bash", `{"command":"go test ./`), // cut mid-string
			terminalChunk("length"),
			{Type: provider.ChunkDone},
		},
		{{Type: provider.ChunkText, Text: "done"}, terminalChunk("stop"), {Type: provider.ChunkDone}},
	}}

	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "edit then verify"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := edits.Load(); got != 1 {
		t.Fatalf("edit_file ran %d times, want exactly 1 — a complete call must not be lost to a truncated sibling", got)
	}
	if got := bashes.Load(); got != 0 {
		t.Fatalf("bash ran %d times, want 0 — its arguments were never finished", got)
	}

	for _, m := range a.sess.conversation.Messages {
		for _, c := range m.ToolCalls {
			if c.Name == "bash" {
				t.Fatalf("the truncated call was persisted as %q; it must not replay on every later request", c.Arguments)
			}
		}
	}

	if len(prov.requests) < 2 {
		t.Fatalf("provider saw %d requests, want a second one carrying the attribution", len(prov.requests))
	}
	var carried bool
	for _, m := range prov.requests[1].Messages {
		if strings.Contains(m.Content, "Host fact about your previous response") {
			carried = true
		}
	}
	if !carried {
		t.Fatal("the next request did not carry the truncation attribution; the model is left to invent one")
	}
}

// TestNormalTerminalStillReportsTheModelsOwnMissingArgument is the other half:
// truncation attribution must not become the answer to every schema failure.
func TestNormalTerminalStillReportsTheModelsOwnMissingArgument(t *testing.T) {
	var runs atomic.Int32
	reg := tool.NewRegistry()
	reg.Add(schemaTool{name: "bash", runs: &runs})

	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "bash", `{}`), terminalChunk("stop"), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, terminalChunk("stop"), {Type: provider.ChunkDone}},
	}}

	a := New(prov, reg, sessionstore.NewSession(""), Options{}, event.Discard)
	if err := a.Run(context.Background(), "run the tests"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := toolResult(a.sess.conversation, "bash")
	if !strings.Contains(got, `requires "command"`) {
		t.Fatalf("bash result = %q, want the ordinary missing-argument contract", got)
	}
	if strings.Contains(got, "Host fact about your previous response") {
		t.Fatalf("bash result = %q, want no truncation attribution on a clean terminal", got)
	}
	for _, m := range a.sess.conversation.Messages {
		if strings.Contains(m.Content, "Host fact about your previous response") {
			t.Fatal("a clean terminal must not be attributed to truncation anywhere in the turn")
		}
	}
}

func TestArgumentsExcerptPointsAtTheFailingByte(t *testing.T) {
	detail := malformedArgumentsDetail(`{"summary">"A"}`)
	// The excerpt must show the payload and put the caret under the byte the
	// parser stopped on, so the reader is not left guessing from an offset.
	lines := strings.Split(detail, "\n")
	var body, caret string
	for i, l := range lines {
		if strings.Contains(l, `{"summary"`) && i+1 < len(lines) {
			body, caret = l, lines[i+1]
		}
	}
	if body == "" {
		t.Fatalf("detail = %q, want it to quote the offending arguments", detail)
	}
	// visibleWidth spans the caret glyph too, so the column it sits in is one less.
	if got := visibleWidth(caret) - 1; got != strings.Index(body, ">") {
		t.Fatalf("caret column %d does not land on '>' at %d\n%s\n%s",
			got, strings.Index(body, ">"), body, caret)
	}
}

func TestAngleBracketInsideAStringIsValidJSON(t *testing.T) {
	// The superstition this diagnostic exists to prevent: '>' is only ever a
	// problem in the syntax around the strings, never inside one.
	if !json.Valid([]byte(`{"summary":"A > B"}`)) {
		t.Fatal("an angle bracket inside a JSON string must stay valid")
	}
	contract, ok := usecap.ReadArgumentContract(
		json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"]}`),
		json.RawMessage(`{"summary":"A > B"}`))
	if !ok || contract.Broken() {
		t.Fatalf("contract ok=%v broken=%v, want an angle bracket in a string to pass cleanly", ok, contract.Broken())
	}
}

func TestCutOffArgumentsAreReportedAsCutOff(t *testing.T) {
	detail := malformedArgumentsDetail(`{"command":"git status`)
	if !strings.Contains(detail, "cut off") {
		t.Fatalf("detail = %q, want a truncation diagnosis rather than a syntax one", detail)
	}
}

// A provider that reports a clean terminal can still deliver a call its own
// output limit cut in half. The half-written arguments are the host's evidence:
// executing them spends a round and comes back as the model's invalid JSON,
// when the cause was the response size and the host could say so.
func TestBoundaryDropsACutCallOnACleanTerminal(t *testing.T) {
	calls := []provider.ToolCall{
		{ID: "1", Name: "read_file", Arguments: `{"path":"a.go"}`},
		{ID: "2", Name: "complete_subtask", Arguments: `{"status":"complete","summary":"the sub-task is do`},
	}
	b := classifyResponseBoundary(&provider.Usage{FinishReason: "tool_calls"}, calls)

	if len(b.committed) != 1 || b.committed[0].Name != "read_file" {
		t.Fatalf("committed = %+v, want only the call that was complete", b.committed)
	}
	if b.dropped != "complete_subtask" {
		t.Fatalf("dropped = %q, want the call that was cut", b.dropped)
	}
	if b.fact == "" {
		t.Fatal("no host fact owed to the model for a dropped call")
	}
}
