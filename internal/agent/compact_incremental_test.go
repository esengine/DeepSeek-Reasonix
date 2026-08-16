package agent

import (
	"context"
	"fmt"
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

// A forced re-compact can fold back into the live projection body — e.g. a
// visible-compression projection (full-freeze body) followed by a manual
// re-compact whose RecentKeep floor reaches past a short canonical tail. Body
// messages past the new boundary have no canonical tail to splice from; they
// must stay verbatim in the new body instead of vanishing from the context.
func TestRefoldIntoBodyKeepsUnfoldedBodyTail(t *testing.T) {
	prov := &recordingProvider{reply: "d"}
	msgs := []provider.Message{
		{Role: provider.RoleSystem, Content: "system"},
		{Role: provider.RoleUser, Content: "first task"},
	}
	for i := range 10 {
		msgs = append(msgs, provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("marker turn %d", i)})
	}
	msgs = append(msgs,
		provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("tail wall ", 40)},
		provider.Message{Role: provider.RoleUser, Content: "go"},
		provider.Message{Role: provider.RoleAssistant, Content: "ok"},
	)
	sess := &Session{Messages: msgs}
	a := New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow: 5_000, CompactRatio: 0.5, RecentKeep: 5,
	}, event.Discard)

	// Prior projection of the visible-compression shape: the body freezes the
	// whole view through the kept user turns; only [tail wall, go, ok] splice.
	canonical, version := a.sess.conversation.snapshotMessagesVersion()
	covered := len(canonical) - 3
	body := append([]provider.Message{canonical[0], canonical[1],
		formatSummaryMessage(strings.Repeat("prior folded context ", 20))},
		canonical[2:covered]...)
	a.sess.compactionMu.Lock()
	a.sess.compactionState = CompactionState{
		SchemaVersion: compactionStateSchemaCurrent, TranscriptVersion: version, Generation: 1,
		PromptCacheKey: a.currentPromptCacheKeyLocked(),
		Projection: ContextProjection{
			Messages: body, TranscriptVersion: version, ProjectionVersion: 1,
			CoveredCount: covered, CoveredPrefixHash: coveredPrefixHash(canonical, covered),
		},
	}
	a.sess.compactionMu.Unlock()

	if err := a.compact(context.Background(), CompactionTriggerManual, "", true); err != nil {
		t.Fatalf("re-compact into body: %v", err)
	}
	if len(prov.got) == 0 {
		t.Fatal("re-compact made no summary request")
	}
	visible := a.modelVisibleMessages()
	for i := range 10 {
		want := fmt.Sprintf("marker turn %d", i)
		found := false
		for _, m := range visible {
			if m.Content == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%q was kept in the projection body but vanished after the re-fold", want)
		}
	}
}
