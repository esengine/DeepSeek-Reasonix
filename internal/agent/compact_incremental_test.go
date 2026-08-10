package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/ablation"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// foldAgent builds an agent whose session grows past the verbatim tail every
// generation, so each CompactNow has something to fold.
func foldAgent(t *testing.T, prov provider.Provider, incremental bool) (*Agent, *Session) {
	t.Helper()
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "task"})
	// Sized so one generation of work outgrows the verbatim tail without a large
	// fixture: these tests assert what a fold reads, not how much, and
	// internal/agent already runs closest to the Windows CI timeout.
	a := New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow: 11_000,
		RecentKeep:    2,
		SessionPath:   t.TempDir() + "/session.jsonl",
		ArchiveDir:    t.TempDir(),
		Ablation:      foldArmSet(incremental),
	}, event.Discard)
	return a, sess
}

func foldArmSet(incremental bool) ablation.Set {
	if incremental {
		return ablation.New(ablation.FullFold)
	}
	return ablation.Set{}
}

func addWork(sess *Session, gen int) {
	for i := range 6 {
		id := fmt.Sprintf("g%dc%d", gen, i)
		sess.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: id, Name: "read_file", Arguments: "{}"}}})
		sess.Add(provider.Message{Role: provider.RoleTool, ToolCallID: id, Name: "read_file",
			Content: strings.Repeat(fmt.Sprintf("gen %d line %d\n", gen, i), 400)})
	}
}

// foldInput runs one generation and returns everything that fold sent the
// summarizer, joined — a fold may take several calls, and the question is what
// it read in total.
func foldInput(t *testing.T, a *Agent, sess *Session, prov *countingProvider, gen int) string {
	t.Helper()
	addWork(sess, gen)
	prov.got = nil
	if err := a.CompactNow(context.Background(), ""); err != nil {
		t.Fatalf("gen %d: %v", gen, err)
	}
	if len(prov.got) == 0 {
		t.Fatalf("gen %d folded nothing", gen)
	}
	var b strings.Builder
	for _, req := range prov.got {
		for _, m := range req.Messages {
			b.WriteString(m.Content)
		}
	}
	return b.String()
}

func TestFullFoldRereadsTheOldestHistoryEveryTime(t *testing.T) {
	// The default arm is what keeps digests from chaining: a later fold still
	// re-reads the oldest raw work rather than a summary of it.
	prov := &countingProvider{reply: "digest"}
	a, sess := foldAgent(t, prov, false)
	var last string
	for gen := range 3 {
		last = foldInput(t, a, sess, prov, gen)
	}
	if !strings.Contains(last, "gen 0 line 0") {
		t.Fatal("a full fold must re-read the oldest raw history")
	}
	if strings.Contains(last, summaryTagOpen) {
		t.Fatal("a full fold must not feed its own prior digest back in")
	}
}

func TestIncrementalFoldSummarizesOnlyTheNewWork(t *testing.T) {
	// Re-summarizing a digest is the one step that can drop a fact for good, so
	// an incremental fold sends the summarizer neither the old raw history nor
	// the digest that replaced it — only what is new.
	prov := &countingProvider{reply: "digest"}
	a, sess := foldAgent(t, prov, true)
	var last string
	for gen := range 3 {
		last = foldInput(t, a, sess, prov, gen)
	}
	if strings.Contains(last, summaryTagOpen) {
		t.Fatalf("a carried digest must not be re-summarized:\n%.200q", last)
	}
	if strings.Contains(last, "gen 0 line 0") {
		t.Fatal("an incremental fold must not re-read history a prior digest already covers")
	}
	if !strings.Contains(last, "gen 2 line 0") {
		t.Fatal("the fold must still summarize the newest work")
	}
}

func TestCarriedDigestsSurviveVerbatimAndInOrder(t *testing.T) {
	prov := &countingProvider{reply: "digest"}
	a, sess := foldAgent(t, prov, true)
	var prev []string
	for gen := range 3 {
		foldInput(t, a, sess, prov, gen)
		got := projectionDigests(a)
		if len(got) != gen+1 {
			t.Fatalf("after fold %d the projection carries %d digests, want %d", gen+1, len(got), gen+1)
		}
		for i, was := range prev {
			if got[i] != was {
				t.Fatalf("fold %d rewrote carried digest %d:\n was %.80q\n now %.80q", gen+1, i+1, was, got[i])
			}
		}
		prev = got
	}
}

func projectionDigests(a *Agent) []string {
	var out []string
	for _, m := range a.compactionState.Projection.Messages {
		if isCompactionSummary(m) {
			out = append(out, m.Content)
		}
	}
	return out
}

// The reason to carry digests instead of merging them is not only fidelity: the
// bytes ahead of the newest digest stop changing, so a fold appends to the
// prefix instead of rewriting it.
func TestCarriedDigestsKeepTheProjectionPrefixStable(t *testing.T) {
	prov := &countingProvider{reply: "digest"}
	a, sess := foldAgent(t, prov, true)
	foldInput(t, a, sess, prov, 0)
	before := renderTranscript(a.compactionState.Projection.Messages[:digestEnd(a)])
	foldInput(t, a, sess, prov, 1)
	after := renderTranscript(a.compactionState.Projection.Messages[:digestEnd(a)-1])
	if !strings.HasPrefix(after, before) {
		t.Fatalf("a fold rewrote the projection prefix instead of appending to it:\nbefore %.120q\nafter  %.120q", before, after)
	}
}

// digestEnd is one past the last carried digest in the current projection.
func digestEnd(a *Agent) int {
	end := 0
	for i, m := range a.compactionState.Projection.Messages {
		if isCompactionSummary(m) {
			end = i + 1
		}
	}
	return end
}

func TestCarriedDigestsConsolidateWhenTheyOutgrowTheirBudget(t *testing.T) {
	prov := &countingProvider{reply: strings.Repeat("carried digest body ", 400)} // ~8k tokens est. each
	a, sess := foldAgent(t, prov, true)
	var last string
	for gen := range 3 {
		last = foldInput(t, a, sess, prov, gen)
	}
	if !strings.Contains(last, summaryTagOpen) {
		t.Fatal("digests past the carry budget must be merged by one consolidating fold")
	}
	if n := len(projectionDigests(a)); n != 1 {
		t.Fatalf("projection carries %d digests after consolidation, want 1", n)
	}
}

func TestIncrementalFoldKeepsTheProjectionValidAgainstCanonical(t *testing.T) {
	// The fold reads the projection, but its coverage bookkeeping still indexes
	// the canonical transcript — otherwise the next turn rebuilds from scratch.
	prov := &countingProvider{reply: "digest"}
	a, sess := foldAgent(t, prov, true)
	for gen := range 2 {
		foldInput(t, a, sess, prov, gen)
	}
	canonical, version := a.session.snapshotMessagesVersion()
	st := a.compactionState
	if st.Projection.CoveredCount != len(canonical) {
		t.Fatalf("CoveredCount = %d, want %d (the canonical length)", st.Projection.CoveredCount, len(canonical))
	}
	if !projectionValid(st, canonical, version, a.currentPromptCacheKey()) {
		t.Fatal("incremental fold left the projection invalid against the canonical transcript")
	}
	if visible := a.modelVisibleMessages(); len(visible) >= len(canonical) {
		t.Fatalf("model-visible = %d messages vs canonical %d; the fold saved nothing", len(visible), len(canonical))
	}
}

func TestIncrementalFoldFallsBackToCanonicalWithoutAProjection(t *testing.T) {
	prov := &countingProvider{reply: "digest"}
	a, sess := foldAgent(t, prov, true)
	if first := foldInput(t, a, sess, prov, 0); !strings.Contains(first, "gen 0 line 0") {
		t.Fatal("the first fold has no projection to read and must fold the canonical transcript")
	}
}
