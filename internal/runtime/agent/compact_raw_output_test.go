package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

const rawOnlySentinel = "RAW-ONLY-SENTINEL"

// A tool result whose Content was bounded when the model first saw it, with the
// full output kept beside it for local display and recall.
func boundedToolResult(rawCopies int) []provider.Message {
	return []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "read_file", Arguments: `{"path":"big.log"}`}}},
		{
			Role: provider.RoleTool, ToolCallID: "1", Name: "read_file",
			Content:    "bounded head\n[output truncated]\n",
			RawContent: "bounded head\n" + strings.Repeat(rawOnlySentinel+" filler line\n", rawCopies),
		},
	}
}

// RawContent is display-only state: ModelMessages and ProjectionMessages both
// clear it, so no provider ever receives it. A summarizer request is a provider
// request, and compaction is the one path that could put it in one.
func TestSummaryNeverPromotesRawToolOutput(t *testing.T) {
	prov := &countingProvider{reply: "digest"}
	a := newFoldAgent(t, 200000, prov)

	if _, err := a.window().foldToSummary(context.Background(), boundedToolResult(200), ""); err != nil {
		t.Fatalf("foldToSummary: %v", err)
	}
	if len(prov.got) != 1 {
		t.Fatalf("requests = %d, want 1", len(prov.got))
	}
	body := prov.got[0].Messages[1].Content
	if strings.Contains(body, rawOnlySentinel) {
		t.Error("the summarizer request carries output the model never saw")
	}
	if !strings.Contains(body, "bounded head") {
		t.Errorf("the bounded result the model did see is missing:\n%s", body)
	}
}

// The cost half of the same fact: a fold is sized by what rides in the request.
// While the raw copy rode, a bounded conversation was billed at the size of the
// output it had been bounded away from.
func TestFoldIsSizedByWhatTheModelSees(t *testing.T) {
	prov := &countingProvider{reply: "digest"}
	a := newFoldAgent(t, 200000, prov)
	small, large := boundedToolResult(10), boundedToolResult(20000)

	if got, want := a.window().summaryInputTokens(large), a.window().summaryInputTokens(small); got != want {
		t.Errorf("a longer local-only copy moved the fold's size: %d vs %d", got, want)
	}
	if _, err := a.window().foldToSummary(context.Background(), large, ""); err != nil {
		t.Fatalf("foldToSummary: %v", err)
	}
	if len(prov.got) != 1 {
		t.Fatalf("requests = %d, want 1", len(prov.got))
	}
	if body := prov.got[0].Messages[1].Content; strings.Contains(body, snippedMarker) {
		t.Error("a bounded fold was shortened for a budget only its local copy exceeded")
	}
}

// Recall is the deliberate exception and stays one: it answers with what was
// actually run, as a tool result the recall budget bounds.
func TestRecallStillAnswersFromTheFullOutput(t *testing.T) {
	rendered := renderTranscriptVerbatim(boundedToolResult(3))
	if !strings.Contains(rendered, rawOnlySentinel) {
		t.Error("recall lost the full output it exists to return")
	}
}

// The same rule on the path that does shorten. A sketch is cut from the body
// the conversation carried, so no route into the request reads the local copy —
// a head-and-tail of the full output would hand the digest a tail the model was
// never given, and the digest is what the session remembers afterwards.
func TestShortenedFoldSketchesOnlyWhatTheModelSaw(t *testing.T) {
	prov := &countingProvider{reply: "digest"}
	// Above summaryOutputReserve so a budget exists, low enough that the fold
	// has to be shortened to fit it.
	a := newFoldAgent(t, 24000, prov)
	body := strings.Repeat("visible filler line\n", 4000)
	fold := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "read_file", Arguments: `{"path":"big.log"}`}}},
		{
			Role: provider.RoleTool, ToolCallID: "1", Name: "read_file",
			Content:    body,
			RawContent: body + strings.Repeat(rawOnlySentinel+" tail the model never saw\n", 4000),
		},
	}

	if _, err := a.window().foldToSummary(context.Background(), fold, ""); err != nil {
		t.Fatalf("foldToSummary: %v", err)
	}
	if len(prov.got) != 1 {
		t.Fatalf("requests = %d, want 1", len(prov.got))
	}
	sent := prov.got[0].Messages[1].Content
	if !strings.Contains(sent, snippedMarker) {
		t.Fatal("this fold was meant to exercise the shortening path")
	}
	if strings.Contains(sent, rawOnlySentinel) {
		t.Error("the sketch was cut from the local full copy")
	}
}
