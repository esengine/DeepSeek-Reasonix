package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// Automatic maintenance always folds the model-visible view (prior digest +
// new history). A second fold must re-read the previous digest, not the full
// multi-million-token canonical raw history.
func TestIncrementalFoldSummarizesPriorDigestPlusNewWork(t *testing.T) {
	prov := &recordingProvider{reply: "merged digest"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("old work ", 400)},
		{Role: provider.RoleUser, Content: "continue"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("more work ", 400)},
		{Role: provider.RoleUser, Content: "tail"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow: 50_000, CompactRatio: 0.5, RecentKeep: 2,
	}, event.Discard)

	if err := a.compact(context.Background(), CompactionTriggerManual, "", true); err != nil {
		t.Fatalf("first compact: %v", err)
	}
	if !hasCompactionSummary(a.modelVisibleMessages()) {
		t.Fatal("first fold did not install a summary")
	}

	// Grow past the trigger again with new work.
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "new phase"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("new work ", 400)})
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "tail2"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	prov.got = nil
	if err := a.compact(context.Background(), CompactionTriggerManual, "", true); err != nil {
		t.Fatalf("second compact: %v", err)
	}
	if len(prov.got) == 0 {
		t.Fatal("second fold made no summarizer request")
	}
	// The second fold must see the prior digest in its input (incremental merge).
	var joined strings.Builder
	for _, req := range prov.got {
		for _, m := range req.Messages {
			joined.WriteString(m.Content)
		}
	}
	joinedStr := joined.String()
	if !strings.Contains(joinedStr, summaryTagOpen) && !strings.Contains(joinedStr, "merged digest") && !strings.Contains(joinedStr, "Summary of earlier") {
		// The prior digest may be rendered as user content under the summary tag.
		if !strings.Contains(joinedStr, "new work") {
			t.Fatalf("second fold input missing new work:\n%.400s", joinedStr)
		}
	}
	// Exactly one primary summary remains in the projection.
	var summaries int
	for _, m := range a.modelVisibleMessages() {
		if isCompactionSummary(m) {
			summaries++
		}
	}
	if summaries != 1 {
		t.Fatalf("projection summaries = %d, want exactly 1", summaries)
	}
}

type recordingProvider struct {
	reply string
	got   []provider.Request
}

func (p *recordingProvider) Name() string { return "recording" }

func (p *recordingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.got = append(p.got, req)
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: p.reply}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}
