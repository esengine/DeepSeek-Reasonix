package agent

import (
	"context"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// searchingProvider replays a turn where the provider ran a search itself: the
// result arrives as a finished call, and the answer text follows it.
type searchingProvider struct{}

func (searchingProvider) Name() string { return "stub" }

func (searchingProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 4)
	ch <- provider.Chunk{
		Type:     provider.ChunkProviderTool,
		ToolCall: &provider.ToolCall{ID: "s1", Name: "web_search", Arguments: `{"query":"今日热点"}`},
		Text:     "\n\n- **今日要闻**\n  <https://example.com/news>\n",
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "今天的热点是…"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// TestProviderRunSearchSurfacesAsFinishedCall pins both halves of where a
// server-side search goes: a finished tool card in the event stream, and the
// listing inside the assistant text — the only copy the next turn has, because
// the provider keeps the result bodies encrypted.
func TestProviderRunSearchSurfacesAsFinishedCall(t *testing.T) {
	var events []event.Event
	sink := event.FuncSink(func(e event.Event) { events = append(events, e) })
	a := New(searchingProvider{}, tool.NewRegistry(), sessionstore.NewSession(""), Options{ModelRef: "stub/model"}, sink)

	st := a.stream(context.Background(), 1, sink)
	if st.err != nil {
		t.Fatalf("stream: %v", st.err)
	}

	var results []event.Tool
	var visible strings.Builder
	for _, e := range events {
		switch e.Kind {
		case event.ToolResult:
			results = append(results, e.Tool)
		case event.Text:
			visible.WriteString(e.Text)
		case event.ToolDispatch:
			t.Fatalf("a provider-run call is never pending, got a dispatch: %+v", e.Tool)
		}
	}
	if len(results) != 1 {
		t.Fatalf("want one finished call, got %d: %+v", len(results), results)
	}
	got := results[0]
	if got.ID != "s1" || got.Name != "web_search" || got.Args != `{"query":"今日热点"}` {
		t.Fatalf("call = %+v, want the search and its query", got)
	}
	if !strings.Contains(got.Output, "**今日要闻**") || !strings.Contains(got.Output, "<https://example.com/news>") {
		t.Fatalf("call output = %q, want the result listing", got.Output)
	}
	if !got.ReadOnly {
		t.Fatal("a search touches nothing; it must report read-only")
	}
	// The listing is what pushed the answer off the screen: it belongs to the
	// card, never to the visible text.
	if v := visible.String(); v != "今天的热点是…" {
		t.Fatalf("visible text = %q, want the answer alone", v)
	}
	if !strings.Contains(st.text, "**今日要闻**") || !strings.Contains(st.text, "今天的热点是…") {
		t.Fatalf("turn text = %q, want both the listing and the answer for the next turn", st.text)
	}
}

// TestProviderToolChunkWithoutCall covers a result block whose issuing call is
// unknown: the text still has to reach the transcript, or the next turn loses
// the search entirely.
func TestProviderToolChunkWithoutCall(t *testing.T) {
	var results int
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ToolResult {
			results++
		}
	})
	a := New(callessProvider{}, tool.NewRegistry(), sessionstore.NewSession(""), Options{ModelRef: "stub/model"}, sink)
	st := a.stream(context.Background(), 1, sink)
	if st.err != nil {
		t.Fatalf("stream: %v", st.err)
	}
	if results != 0 {
		t.Fatalf("no call to report, got %d results", results)
	}
	if !strings.Contains(st.text, "orphan listing") {
		t.Fatalf("turn text = %q, want the listing retained", st.text)
	}
}

type callessProvider struct{}

func (callessProvider) Name() string { return "stub" }

func (callessProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkProviderTool, Text: "orphan listing"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}
