package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reasonix/internal/event"
	"strings"
	"testing"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// fakeProvider returns a fixed reply and records the messages it was asked to
// complete, so tests can drive summarization without a network call.
type fakeProvider struct {
	reply        string
	promptTokens int
	got          []provider.Message
	streamErr    error // when set, Stream emits a ChunkError instead of the reply
	hang         bool  // when true, Stream returns a channel that never sends or closes
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	f.got = req.Messages
	if f.hang {
		return make(chan provider.Chunk), nil
	}
	ch := make(chan provider.Chunk, 3)
	if f.streamErr != nil {
		ch <- provider.Chunk{Type: provider.ChunkError, Err: f.streamErr}
		close(ch)
		return ch, nil
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: f.reply}
	if f.promptTokens > 0 {
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: f.promptTokens, TotalTokens: f.promptTokens}}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// visibleContext returns the model-visible projection when present, else the
// canonical transcript. Compaction tests assert against this view.

func TestTailStart(t *testing.T) {
	// 10-char content → with tokPerChar 1.0, each non-empty message costs 10
	// "tokens"; tool-call messages carry name+args instead.
	msg := func(role provider.Role, n int) provider.Message {
		return provider.Message{Role: role, Content: strings.Repeat("x", n)}
	}
	u := func(n int) provider.Message { return msg(provider.RoleUser, n) }
	as := func(n int) provider.Message { return msg(provider.RoleAssistant, n) }
	ac := provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "f", Arguments: "{}"}}}
	to := func(n int) provider.Message {
		return provider.Message{Role: provider.RoleTool, ToolCallID: "1", Name: "f", Content: strings.Repeat("x", n)}
	}

	sys := provider.Message{Role: provider.RoleSystem}
	cases := []struct {
		name    string
		msgs    []provider.Message
		head    int
		budget  int
		minKeep int
		wantStr int
	}{
		// Budget 25 fits the two newest 10-char messages (20) but not a third (30);
		// the tail stops at the third-from-last.
		{"budget-bounds-tail", []provider.Message{u(10), as(10), u(10), as(10), u(10)}, 0, 25, 2, 3},
		// A single huge recent message can't blow the budget below minKeep: the last
		// two are kept regardless.
		{"min-keep-floor", []provider.Message{u(10), as(10), u(10), as(10), to(9999)}, 0, 25, 2, 3},
		// The boundary lands on an orphan tool result and must move back onto its
		// assistant so the tail begins with the tool_calls.
		{"align-off-tool", []provider.Message{sys, u(10), ac, to(10), ac, to(10)}, 1, 0, 1, 4},
		// A generous budget keeps everything down to the first compactable message
		// after the head.
		{"budget-keeps-all", []provider.Message{sys, u(10), as(10), u(10)}, 1, 100000, 2, 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := tailStart(tc.msgs, tc.head, tc.budget, 1.0, tc.minKeep)
			if start != tc.wantStr {
				t.Errorf("start = %d, want %d", start, tc.wantStr)
			}
			if tc.msgs[start].Role == provider.RoleTool {
				t.Errorf("recent tail begins with orphan tool message at %d", start)
			}
		})
	}
}

func TestTailStartSmallSession(t *testing.T) {
	sys := provider.Message{Role: provider.RoleSystem}
	usr := provider.Message{Role: provider.RoleUser, Content: "hi"}
	for i, msgs := range [][]provider.Message{
		{sys, usr}, // system + one message: nothing fits the tail; must not index msgs[len]
		{sys},
		{usr},
		{},
	} {
		head := 0
		if len(msgs) > 0 && msgs[0].Role == provider.RoleSystem {
			head = 1
		}
		start := tailStart(msgs, head, 16384, 0.25, 2)
		if start < head || start > len(msgs) {
			t.Errorf("case %d: start=%d out of bounds [%d,%d]", i, start, head, len(msgs))
		}
	}
}

func TestPinnedPrefixLen(t *testing.T) {
	sys := provider.Message{Role: provider.RoleSystem}
	small := provider.Message{Role: provider.RoleUser, Content: "do X with token T"}
	big := provider.Message{Role: provider.RoleUser, Content: strings.Repeat("x", 100000)}
	sum := provider.Message{Role: provider.RoleUser, Content: summaryTagOpen + "\ndigest\n" + summaryTagClose}
	as := provider.Message{Role: provider.RoleAssistant, Content: "a"}

	newA := func(win int) *Agent {
		return New(&fakeProvider{}, tool.NewRegistry(), &Session{}, Options{ContextWindow: win}, event.Discard)
	}
	cases := []struct {
		name string
		win  int
		msgs []provider.Message
		want int
	}{
		{"pins-system-and-small-task", 0, []provider.Message{sys, small, as, as}, 2},
		{"summaries-are-not-pinned-A1-merge", 0, []provider.Message{sys, small, sum, sum, as}, 2},
		{"large-first-turn-stays-foldable", 0, []provider.Message{sys, big, as, as}, 1},
		{"tiny-window-wont-pin", 10, []provider.Message{sys, small, as, as}, 1},
		{"summary-is-not-the-task-turn", 0, []provider.Message{sys, sum, as}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := newA(tc.win).pinnedPrefixLen(tc.msgs); got != tc.want {
				t.Errorf("pinnedPrefixLen = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestKeepIndexesKeepsSiblingToolResultsForKeptError(t *testing.T) {
	region := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{
			{ID: "err", Name: "bash", Arguments: `{"cmd":"bad"}`},
			{ID: "ok", Name: "read_file", Arguments: `{"path":"main.go"}`},
		}},
		{Role: provider.RoleTool, ToolCallID: "err", Name: "bash", Content: "error: command failed"},
		{Role: provider.RoleTool, ToolCallID: "ok", Name: "read_file", Content: "package main"},
	}

	keep := keepIndexes(region, KeepErrors)
	for i, kept := range keep {
		if !kept {
			t.Fatalf("keep[%d] = false, want all sibling tool-call messages kept: %v", i, keep)
		}
	}
}

func TestKeepIndexesScopesPolicyAfterLatestSummary(t *testing.T) {
	priorSummary := provider.Message{Role: provider.RoleUser, Content: summaryTagOpen + "\nprior digest\n" + summaryTagClose}
	region := []provider.Message{
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "old", Name: "bash", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "old", Name: "bash", Content: "error: old failure"},
		priorSummary,
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "new", Name: "bash", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "new", Name: "bash", Content: "error: new failure"},
	}

	keep := keepIndexes(region, KeepErrors)
	want := []bool{false, false, false, true, true}
	for i := range want {
		if keep[i] != want[i] {
			t.Fatalf("keep = %v, want %v", keep, want)
		}
	}
}

func TestKeepUserMarkedRequiresUserPrefixMarker(t *testing.T) {
	region := []provider.Message{
		{Role: provider.RoleAssistant, Content: "[keep] assistant output"},
		{Role: provider.RoleUser, Content: "ordinary prose mentioning [keep] later"},
		{Role: provider.RoleUser, Content: "  <keep> exact requirement"},
	}

	keep := keepIndexes(region, KeepUserMarked)
	want := []bool{false, false, true}
	for i := range want {
		if keep[i] != want[i] {
			t.Fatalf("keep = %v, want %v", keep, want)
		}
	}
}

// TestCompactFallsBackToMechanicalFoldWhenSummaryFails: when the summarizer is
// unreachable, /compact must still free context (fold mechanically) and surface a
// card, not hang or abort leaving a full window.
// TestSummarizeRespectsContextCancel: a stalled stream (open but never closing)
// must unblock on context cancellation instead of pinning compaction forever.
func TestSummarizeRespectsContextCancel(t *testing.T) {
	a := New(&fakeProvider{hang: true}, tool.NewRegistry(), &Session{}, Options{}, event.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := a.summarize(ctx, []provider.Message{{Role: provider.RoleUser, Content: "x"}}, ""); err == nil {
		t.Fatal("summarize must return when ctx is cancelled, not hang")
	}
}

// TestCompactEmitsEvents covers the card-driving signals: a CompactionStarted
// (before the summarizer runs) then a CompactionDone carrying the trigger,
// message count, and summary — in that order.
func TestCompactEmitsEvents(t *testing.T) {
	prov := &fakeProvider{reply: "- goal: do X"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: "step one"},
		{Role: provider.RoleUser, Content: "more"},
		{Role: provider.RoleAssistant, Content: "step two"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	var got []event.Event
	sink := event.FuncSink(func(e event.Event) { got = append(got, e) })
	a := New(prov, tool.NewRegistry(), sess, Options{RecentKeep: 2}, sink)

	if err := a.compact(context.Background(), "auto", "", true); err != nil {
		t.Fatalf("compact: %v", err)
	}

	startedAt, doneAt := -1, -1
	for i, e := range got {
		switch e.Kind {
		case event.CompactionStarted:
			startedAt = i
			if e.Compaction.Trigger != "auto" {
				t.Errorf("started trigger = %q, want auto", e.Compaction.Trigger)
			}
		case event.CompactionDone:
			doneAt = i
			c := e.Compaction
			if c.Trigger != "auto" || c.Messages == 0 || !strings.Contains(c.Summary, "do X") {
				t.Errorf("done event = %+v", c)
			}
		}
	}
	if startedAt < 0 {
		t.Fatal("no CompactionStarted event emitted")
	}
	if doneAt < 0 {
		t.Fatal("no CompactionDone event emitted")
	}
	if startedAt > doneAt {
		t.Errorf("CompactionStarted (%d) must precede CompactionDone (%d)", startedAt, doneAt)
	}
}

// TestCompactInjectsFocusAndPreCompactHook checks that /compact <focus> text and
// a PreCompact hook's output both reach the summarizer's system prompt.
func TestCompactInjectsFocusAndPreCompactHook(t *testing.T) {
	prov := &fakeProvider{reply: "- ok"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: "step one"},
		{Role: provider.RoleUser, Content: "more"},
		{Role: provider.RoleAssistant, Content: "step two"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{RecentKeep: 2, Hooks: &stubHooks{preCompactOut: "KEEP-THE-MIGRATION-PLAN"}}, event.Discard)

	if err := a.compact(context.Background(), "manual", "focus on the auth refactor", true); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if len(prov.got) == 0 || prov.got[0].Role != provider.RoleSystem {
		t.Fatalf("summarizer wasn't asked with a system prompt: %+v", prov.got)
	}
	sys := prov.got[0].Content
	if !strings.Contains(sys, "focus on the auth refactor") {
		t.Errorf("summary system prompt missing the /compact focus text: %q", sys)
	}
	if !strings.Contains(sys, "KEEP-THE-MIGRATION-PLAN") {
		t.Errorf("summary system prompt missing the PreCompact hook output: %q", sys)
	}
}

func TestCompactSkipsSingleSmallMessage(t *testing.T) {
	prov := &fakeProvider{reply: "- should not be called"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "tiny"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	if err := a.compact(context.Background(), "auto", "", false); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if got := len(sess.Messages); got != 4 {
		t.Fatalf("small single message should not compact, len = %d", got)
	}
	if len(prov.got) != 0 {
		t.Fatalf("summarizer was called for tiny region: %+v", prov.got)
	}
}

func TestMaybeCompactThreshold(t *testing.T) {
	// A large early user message gives the fold real value; with a 100-token window
	// the soft (50%), trigger (80%), and force (90%) thresholds are easy to hit.
	newSess := func() *Session {
		return &Session{Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "sys"},
			{Role: provider.RoleUser, Content: strings.Repeat("a ", 500)},
			{Role: provider.RoleAssistant, Content: "b"},
			{Role: provider.RoleUser, Content: "c"},
			{Role: provider.RoleAssistant, Content: "d"},
			{Role: provider.RoleUser, Content: "e"},
			{Role: provider.RoleAssistant, Content: "f"},
		}}
	}

	// Below 50% of the window: untouched.
	sess := newSess()
	a := New(&fakeProvider{reply: "s"}, tool.NewRegistry(), sess, Options{ContextWindow: 100, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)
	a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: 49})
	if len(sess.Messages) != 7 {
		t.Errorf("below threshold should not compact, len = %d", len(sess.Messages))
	}

	// At/above 50% only emits a soft notice; it does not rewrite the cache prefix.
	sess = newSess()
	prov := &fakeProvider{reply: "s"}
	var notices []event.Event
	a = New(prov, tool.NewRegistry(), sess, Options{ContextWindow: 100, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e)
		}
	}))
	a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: 50})
	if len(sess.Messages) != 7 {
		t.Errorf("soft threshold should not compact, len = %d", len(sess.Messages))
	}
	if len(prov.got) != 0 {
		t.Fatalf("soft threshold called summarizer: %+v", prov.got)
	}
	if len(notices) != 1 || notices[0].Text != "Context is getting large; preserving cache until cleanup is needed." || !strings.Contains(notices[0].Detail, "context reached 50%") {
		t.Fatalf("soft threshold notice = %+v", notices)
	}
	a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: 60})
	if len(notices) != 1 {
		t.Fatalf("soft threshold notice should only emit once, got %d", len(notices))
	}

	// At/above 80%: compacts when the fold is economically worthwhile. The
	// token-budgeted tail keeps the small recent messages, so the large early
	// message is the only foldable region — folding it installs a summary at
	// index 1 (the count is unchanged because one message becomes one summary).
	sess = newSess()
	a = New(&fakeProvider{reply: "s"}, tool.NewRegistry(), sess, Options{ContextWindow: 100, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)
	a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: 80})
	if !hasCompactionSummary(visibleContext(a)) {
		t.Errorf("compact threshold should fold the large early message into projection, got: %+v", visibleContext(a))
	}
	// Canonical transcript remains full.
	if len(sess.Messages) != 7 {
		t.Errorf("canonical should stay full after projection compact, len=%d", len(sess.Messages))
	}

	// No context window: compaction disabled.
	sess = newSess()
	a = New(&fakeProvider{reply: "s"}, tool.NewRegistry(), sess, Options{RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)
	a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: 1 << 30})
	if len(sess.Messages) != 7 {
		t.Errorf("no window should disable compaction, len = %d", len(sess.Messages))
	}
}

func TestMaybeCompactForceCeilingBypassesEconomics(t *testing.T) {
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "small old request"},
		{Role: provider.RoleAssistant, Content: "small old answer"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	prov := &fakeProvider{reply: "forced summary"}
	a := New(prov, tool.NewRegistry(), sess, Options{ContextWindow: 100, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: 90})
	// Force bypasses economics and installs a projection summary; canonical stays.
	if got := len(sess.Messages); got != 5 {
		t.Fatalf("canonical len = %d, want 5: %+v", got, sess.Messages)
	}
	if sess.Messages[1].Content != "small old request" {
		t.Fatalf("first user turn not pinned verbatim in canonical: %+v", sess.Messages[1])
	}
	proj := visibleContext(a)
	if !hasCompactionSummary(proj) || !strings.Contains(joinContents(proj), "forced summary") {
		t.Fatalf("forced compact did not install summary projection: %+v", proj)
	}
	if len(prov.got) == 0 {
		t.Fatalf("summarizer was not called at force ceiling")
	}
}

func TestMaybeCompactSkipsLowValueRegionBeforeForceCeiling(t *testing.T) {
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "small old request"},
		{Role: provider.RoleAssistant, Content: "small old answer"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	prov := &fakeProvider{reply: "should not summarize"}
	a := New(prov, tool.NewRegistry(), sess, Options{ContextWindow: 100, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: 80})
	if got := len(sess.Messages); got != 5 {
		t.Fatalf("low-value region should not compact before force ceiling, len = %d", got)
	}
	if len(prov.got) != 0 {
		t.Fatalf("summarizer was called for low-value non-forced region: %+v", prov.got)
	}
}

func TestMaybeCompactFoldsSingleLargeMessageAtThreshold(t *testing.T) {
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: strings.Repeat("large prompt chunk ", 500)},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(&fakeProvider{reply: "single large summary"}, tool.NewRegistry(), sess, Options{ContextWindow: 100, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: 80})
	if got := len(sess.Messages); got != 4 {
		t.Fatalf("canonical len = %d, want 4: %+v", got, sess.Messages)
	}
	proj := visibleContext(a)
	if !hasCompactionSummary(proj) || !strings.Contains(joinContents(proj), "single large summary") {
		t.Fatalf("single large message was not compacted into projection: %+v", proj)
	}
}

func TestRenderTranscriptRedactsToolCallArgs(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "Find me popular GitHub MCP projects"},
		{
			Role:    provider.RoleAssistant,
			Content: "I'll research that.",
			ToolCalls: []provider.ToolCall{
				{Name: "research", Arguments: `{"task":"Search for recently popular GitHub projects that let AI use/control any software through MCP..."}`},
			},
		},
		{Role: provider.RoleTool, Name: "research", Content: "Found 5 projects."},
	}

	out := renderTranscript(msgs)

	if strings.Contains(out, "Search for recently popular") {
		t.Fatalf("renderTranscript leaked tool-call arguments into transcript:\n%s", out)
	}
	if !strings.Contains(out, "[assistant calls research]") {
		t.Fatalf("renderTranscript missing tool-call label:\n%s", out)
	}
	if !strings.Contains(out, "task") {
		t.Fatalf("renderTranscript missing key names:\n%s", out)
	}
}

func TestInterruptedDisplayStaysVerbatimAndOutOfCompactionPrompt(t *testing.T) {
	local := provider.Message{
		Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName,
		LocalOnly: true, Content: "partial visible answer", ReasoningContent: "private partial reasoning",
		InterruptedTurn: &provider.InterruptedTurnRecovery{Pending: true},
	}
	a := &Agent{}
	kept, fold := a.partitionFold([]provider.Message{local})
	if len(kept) != 1 || !kept[0].LocalOnly || len(fold) != 0 {
		t.Fatalf("compaction partition kept=%+v fold=%+v, want local display kept verbatim", kept, fold)
	}
	if transcript := renderTranscript([]provider.Message{local}); transcript != "" {
		t.Fatalf("local interrupted output leaked into compaction prompt: %q", transcript)
	}
}

func TestCompactKeepsActiveTurnVerbatim(t *testing.T) {
	const currentCreatedAt int64 = 123456
	call := provider.Message{
		Role: provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{{
			ID: "write-1", Name: "write_file", Arguments: `{"path":"a.txt","content":"ok"}`,
		}},
	}
	result := provider.Message{Role: provider.RoleTool, ToolCallID: "write-1", Name: "write_file", Content: "wrote a.txt"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: strings.Repeat("old request ", 200)},
		{Role: provider.RoleAssistant, Content: strings.Repeat("old answer ", 200)},
		{Role: provider.RoleUser, Content: "update a.txt", CreatedAt: currentCreatedAt},
		call,
		result,
	}}
	a := New(&fakeProvider{reply: "old work summary"}, tool.NewRegistry(), sess, Options{
		ContextWindow: 100, RecentKeep: 1, ArchiveDir: t.TempDir(),
	}, event.Discard)
	a.activeTurnCreatedAt.Store(currentCreatedAt)

	if err := a.compact(context.Background(), "auto", "", true); err != nil {
		t.Fatalf("compact: %v", err)
	}
	start := a.activeTurnStart(sess.Messages)
	if start < 0 || len(sess.Messages)-start != 3 {
		t.Fatalf("active turn boundary = %d in %+v, want three-message verbatim tail", start, sess.Messages)
	}
	if sess.Messages[start].Content != "update a.txt" || sess.Messages[start+1].ToolCalls[0].Arguments != call.ToolCalls[0].Arguments || sess.Messages[start+2].Content != result.Content {
		t.Fatalf("active turn changed during compaction: %+v", sess.Messages[start:])
	}
}

func TestSummarizeFromPreservesLocalOnlyOutsideModelAndArchive(t *testing.T) {
	archiveDir := t.TempDir()
	local := provider.Message{
		Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName,
		LocalOnly: true, Content: "visible interrupted output", ReasoningContent: "private interrupted reasoning",
		InterruptedTurn: &provider.InterruptedTurnRecovery{Pending: true, InterruptedTools: []string{"bash"}},
	}
	prov := &fakeProvider{reply: "later summary"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		local,
		{Role: provider.RoleAssistant, Content: "safe answer"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{ArchiveDir: archiveDir}, event.Discard)

	if err := a.SummarizeFrom(context.Background(), 1); err != nil {
		t.Fatalf("SummarizeFrom: %v", err)
	}
	if len(sess.Messages) != 3 || !sess.Messages[2].LocalOnly || sess.Messages[2].Content != local.Content || sess.Messages[2].ReasoningContent != local.ReasoningContent || sess.Messages[2].InterruptedTurn == nil || !sess.Messages[2].InterruptedTurn.Pending {
		t.Fatalf("local-only message was not preserved verbatim: %+v", sess.Messages)
	}
	assertLocalOnlyAbsentFromSummaryAndArchive(t, prov, archiveDir, local)
}

func TestSummarizeUpToPreservesLocalOnlyOutsideModelAndArchive(t *testing.T) {
	archiveDir := t.TempDir()
	local := provider.Message{
		Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName,
		LocalOnly: true, Content: "visible earlier interruption", ReasoningContent: "private earlier reasoning",
		InterruptedTurn: &provider.InterruptedTurnRecovery{Pending: true, InterruptedTools: []string{"read_file"}},
	}
	prov := &fakeProvider{reply: "earlier summary"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "old task"},
		local,
		{Role: provider.RoleAssistant, Content: "old answer"},
		{Role: provider.RoleUser, Content: "new task"},
		{Role: provider.RoleAssistant, Content: "new answer"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{ArchiveDir: archiveDir}, event.Discard)

	if err := a.SummarizeUpTo(context.Background(), 4); err != nil {
		t.Fatalf("SummarizeUpTo: %v", err)
	}
	if len(sess.Messages) != 5 || !sess.Messages[2].LocalOnly || sess.Messages[2].Content != local.Content || sess.Messages[2].ReasoningContent != local.ReasoningContent || sess.Messages[3].Content != "new task" {
		t.Fatalf("local-only message/tail ordering was not preserved: %+v", sess.Messages)
	}
	assertLocalOnlyAbsentFromSummaryAndArchive(t, prov, archiveDir, local)
}

func assertLocalOnlyAbsentFromSummaryAndArchive(t *testing.T, prov *fakeProvider, archiveDir string, local provider.Message) {
	t.Helper()
	if len(prov.got) < 2 || strings.Contains(prov.got[1].Content, local.Content) || strings.Contains(prov.got[1].Content, local.ReasoningContent) {
		t.Fatalf("local-only output leaked into summarizer prompt: %+v", prov.got)
	}
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("ReadDir archive: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("archive entries = %d, want 1", len(entries))
	}
	b, err := os.ReadFile(filepath.Join(archiveDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile archive: %v", err)
	}
	if strings.Contains(string(b), local.Content) || strings.Contains(string(b), local.ReasoningContent) {
		t.Fatalf("local-only output leaked into archive: %s", b)
	}
}

func TestSummarizeToolArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    string
		want    string
		wantNot string
	}{
		{
			name: "redacts long task prompt",
			args: `{"task":"Search for recently popular GitHub projects that let AI use/control any software through MCP..."}`,
			want: "task",
		},
		{
			name: "empty args",
			args: "",
			want: "no arguments",
		},
		{
			name: "invalid json",
			args: "not json",
			want: "bytes",
		},
		{
			name: "multiple keys sorted",
			args: `{"prompt":"do something","model":"gpt-4"}`,
			want: "model, prompt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeToolArgs(tt.args)
			if !strings.Contains(got, tt.want) {
				t.Errorf("summarizeToolArgs(%q) = %q, want contains %q", tt.args, got, tt.want)
			}
			if tt.wantNot != "" && strings.Contains(got, tt.wantNot) {
				t.Errorf("summarizeToolArgs(%q) = %q, should NOT contain %q", tt.args, got, tt.wantNot)
			}
		})
	}
}

// TestMaybeCompactClearsStuckLatchAnywhereBelowTrigger pins the documented
// contract that any turn under the compact trigger is "breathing room" that
// clears the stuck latch. The snip band ([snip, high)) is the regression: it
// returned before the reset ran, so a compaction that healthily settled the
// prompt at, say, 70% of the window left a stale consecutive-run count behind
// and the next compaction latched the session as "window too small" — silently
// disabling auto-compaction for the rest of the run.
func TestMaybeCompactClearsStuckLatchAnywhereBelowTrigger(t *testing.T) {
	// contextWindow 20000 => soft 10000, snip 12000, high (trigger) 16000.
	for _, tc := range []struct {
		name   string
		prompt int
	}{
		{"below soft", 8000},
		{"soft band", 11000},
		{"snip band", 14000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sess := NewSession("sys")
			sess.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
			a := New(&fakeProvider{reply: "- summary"}, tool.NewRegistry(), sess, Options{ContextWindow: 20000}, event.Discard)
			a.consecutiveCompacts = 1
			a.compactStuck = true

			a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: tc.prompt})

			if a.consecutiveCompacts != 0 || a.compactStuck {
				t.Fatalf("prompt %d sits under the trigger; want the latch cleared, got consecutiveCompacts=%d compactStuck=%v",
					tc.prompt, a.consecutiveCompacts, a.compactStuck)
			}
		})
	}
}

// TestMaybeCompactStillLatchesWhenPromptStaysAboveTrigger proves the safety
// valve survives the fix above: a genuinely too-small window (the prompt never
// drops under the trigger between compactions) must still pause auto-compaction.
func TestMaybeCompactStillLatchesWhenPromptStaysAboveTrigger(t *testing.T) {
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	a := New(&fakeProvider{reply: "- summary"}, tool.NewRegistry(), sess, Options{ContextWindow: 20000}, event.Discard)

	a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: 17000})
	if a.compactStuck {
		t.Fatalf("a single over-trigger compaction must not latch: consecutiveCompacts=%d", a.consecutiveCompacts)
	}
	a.maybeCompact(context.Background(), &provider.Usage{PromptTokens: 17000})
	if !a.compactStuck {
		t.Fatalf("two consecutive over-trigger compactions must still latch: consecutiveCompacts=%d", a.consecutiveCompacts)
	}
}

func TestPartitionFoldSmallTurnWindowIsPositionFixed(t *testing.T) {
	// 25 small user turns in the region: the first 20 must be kept verbatim,
	// the last 5 must fold. The window is position-fixed (first N), never
	// "the most recent N" — a dynamic tail would rewrite the kept prefix on
	// every compaction and crater the server-side prefix cache.
	a := &Agent{}
	var region []provider.Message
	for i := range 25 {
		region = append(region, provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("small turn %d", i)})
	}
	kept, fold := a.partitionFold(region)
	if len(kept) != maxKeepSmallUserTurns {
		t.Fatalf("kept %d small user turns, want %d (position-fixed window)", len(kept), maxKeepSmallUserTurns)
	}
	if len(fold) != 5 {
		t.Fatalf("folded %d turns, want 5 (turns beyond the fixed window)", len(fold))
	}
	// The kept turns must be the FIRST ones in order (positions 0..19).
	for i := range maxKeepSmallUserTurns {
		want := fmt.Sprintf("small turn %d", i)
		if got := UserMessageText(kept[i]); got != want {
			t.Fatalf("kept[%d]=%q, want %q — keep window must be the leading turns", i, got, want)
		}
	}
	// Folded turns are the oldest beyond the window (positions 20..24).
	for i, m := range fold {
		want := fmt.Sprintf("small turn %d", 20+i)
		if got := UserMessageText(m); got != want {
			t.Fatalf("fold[%d]=%q, want %q", i, got, want)
		}
	}
}

func TestPartitionFoldLargeTurnsStillFold(t *testing.T) {
	// Large user turns are not pinnable regardless of window position.
	a := &Agent{}
	region := []provider.Message{
		{Role: provider.RoleUser, Content: strings.Repeat("big", 4000)}, // 12000 chars ×0.25 = 3000 > 1500 → not pinnable
		{Role: provider.RoleUser, Content: "small"},
	}
	kept, fold := a.partitionFold(region)
	if len(kept) != 1 || UserMessageText(kept[0]) != "small" {
		t.Fatalf("kept=%+v, want only the small turn", kept)
	}
	if len(fold) != 1 {
		t.Fatalf("fold=%d, want the large turn folded", len(fold))
	}
}

func TestCompactRollsOldDigestsIntoNew(t *testing.T) {
	// A1 rolling merge: prior digests enter the fold region and are merged into
	// one new provider-visible summary. The canonical transcript stays intact.
	oldDigest := summaryTagOpen + "\n" + strings.Repeat("old standing fact ", 60) + "\n" + summaryTagClose // >1500 chars → not pinnable
	newestDigest := summaryTagOpen + "\nnewest digest\n" + summaryTagClose
	big := strings.Repeat("work output ", 200)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleUser, Content: oldDigest}, // old digest, large → folds
		{Role: provider.RoleUser, Content: newestDigest},
		{Role: provider.RoleAssistant, Content: big},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(&fakeProvider{reply: "merged digest"}, tool.NewRegistry(), sess,
		Options{RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	if err := a.compact(context.Background(), "manual", "", true); err != nil {
		t.Fatalf("compact: %v", err)
	}
	canonical := sess.Snapshot()
	var oldDigestRetained bool
	for _, m := range canonical {
		if m.Content == oldDigest {
			oldDigestRetained = true
		}
	}
	if !oldDigestRetained {
		t.Fatalf("canonical transcript lost old digest: %+v", canonical)
	}

	projection := visibleContext(a)
	var summaryCount int
	var generatedSummaryPresent bool
	for _, m := range projection {
		if isCompactionSummary(m) {
			summaryCount++
			if strings.Contains(m.Content, "merged digest") {
				generatedSummaryPresent = true
			}
		}
		if m.Content == oldDigest {
			t.Fatalf("old digest survived verbatim in projection: %+v", projection)
		}
	}
	if summaryCount != 1 {
		t.Fatalf("projection summaries = %d, want exactly 1: %+v", summaryCount, projection)
	}
	if !generatedSummaryPresent {
		t.Fatalf("generated rolling summary missing from projection: %+v", projection)
	}
}
