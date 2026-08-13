package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// prepareForObservedUsage preserves the old synthetic-usage test ergonomics
// while production has only one mutating entry point: ContextManager.Prepare.
func prepareForObservedUsage(a *Agent, ctx context.Context, usage *provider.Usage) {
	if a == nil || usage == nil || usage.LatestPromptTokens() <= 0 {
		return
	}
	view := a.modelVisibleMessages()
	a.setPromptTokenCalibration(usage.LatestPromptTokens(), a.requestCalibrationShape(provider.Request{Messages: view}))
	_, _ = a.contextManager().Prepare(ctx, ContextPreparePolicy{
		Trigger: CompactionTriggerPressure, ObservedInputTokens: usage.LatestPromptTokens(),
	})
}

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

	keep, _ := (&Agent{keepPolicy: KeepErrors}).keepIndexes(region)
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

	keep, _ := (&Agent{keepPolicy: KeepErrors}).keepIndexes(region)
	want := []bool{false, false, false, true, true}
	for i := range want {
		if keep[i] != want[i] {
			t.Fatalf("keep = %v, want %v", keep, want)
		}
	}
}

// The marker is what lets a user turn exceed the size budget keepUserTurns
// applies, so it is asserted directly: at keepIndexes level every small user
// turn is kept regardless, which would hide a broken marker match.
func TestKeepUserMarkedRequiresUserPrefixMarker(t *testing.T) {
	cases := []struct {
		name string
		msg  provider.Message
		want bool
	}{
		{"assistant marker ignored", provider.Message{Role: provider.RoleAssistant, Content: "[keep] assistant output"}, false},
		{"marker must lead", provider.Message{Role: provider.RoleUser, Content: "ordinary prose mentioning [keep] later"}, false},
		{"leading marker after space", provider.Message{Role: provider.RoleUser, Content: "  <keep> exact requirement"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isUserMarked(tc.msg); got != tc.want {
				t.Fatalf("isUserMarked = %v, want %v", got, tc.want)
			}
		})
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
		{Role: provider.RoleAssistant, Content: strings.Repeat("step one work ", 200)},
		{Role: provider.RoleUser, Content: "more"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("step two work ", 200)},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	var got []event.Event
	sink := event.FuncSink(func(e event.Event) { got = append(got, e) })
	a := New(prov, tool.NewRegistry(), sess, Options{ContextWindow: 50_000, RecentKeep: 2}, sink)

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
	big := strings.Repeat("step work detail ", 200)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: big},
		{Role: provider.RoleUser, Content: "more"},
		{Role: provider.RoleAssistant, Content: big},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{
		ContextWindow: 50_000, RecentKeep: 2,
		Hooks: &stubHooks{preCompactOut: "KEEP-THE-MIGRATION-PLAN"},
	}, event.Discard)

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
	// compact_ratio is the sole trigger. Use a realistic window so hardInputCeiling
	// (window−protocolReserve) stays above the fold trigger; tiny synthetic
	// windows collapse hard to 1 and force every observation.
	const window = 10_000
	const ratio = 0.8 // fold trigger = 8000
	newSess := func() *Session {
		return &Session{Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: "sys"},
			{Role: provider.RoleUser, Content: "task"},
			{Role: provider.RoleAssistant, Content: strings.Repeat("a ", 5000)},
			{Role: provider.RoleUser, Content: "c"},
			{Role: provider.RoleAssistant, Content: strings.Repeat("b ", 5000)},
			{Role: provider.RoleUser, Content: "e"},
			{Role: provider.RoleAssistant, Content: "f"},
		}}
	}
	opts := Options{ContextWindow: window, CompactRatio: ratio, RecentKeep: 2}

	// Below compact_ratio: untouched, no summarizer call.
	sess := newSess()
	prov := &fakeProvider{reply: "s"}
	a := New(prov, tool.NewRegistry(), sess, opts, event.Discard)
	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 7000})
	if len(sess.Messages) != 7 {
		t.Errorf("below threshold should not compact, len = %d", len(sess.Messages))
	}
	if len(prov.got) != 0 {
		t.Fatalf("below threshold called summarizer: %+v", prov.got)
	}

	// 60% is below the sole 80% trigger: still no maintenance.
	sess = newSess()
	prov = &fakeProvider{reply: "s"}
	a = New(prov, tool.NewRegistry(), sess, opts, event.Discard)
	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 6000})
	if a.currentProjectionVersion() != 0 || len(prov.got) != 0 {
		t.Fatalf("60%% should not maintain: version=%d calls=%d", a.currentProjectionVersion(), len(prov.got))
	}

	// At/above compact_ratio: one summary projection; canonical stays full.
	sess = newSess()
	a = New(&fakeProvider{reply: "s"}, tool.NewRegistry(), sess, opts, event.Discard)
	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 8500})
	if !hasCompactionSummary(visibleContext(a)) {
		t.Errorf("compact threshold should install a summary projection, got: %+v", visibleContext(a))
	}
	if len(sess.Messages) != 7 {
		t.Errorf("canonical should stay full after projection compact, len=%d", len(sess.Messages))
	}

	// No context window: compaction disabled.
	sess = newSess()
	a = New(&fakeProvider{reply: "s"}, tool.NewRegistry(), sess, Options{RecentKeep: 2}, event.Discard)
	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 1 << 30})
	if len(sess.Messages) != 7 {
		t.Errorf("no window should disable compaction, len = %d", len(sess.Messages))
	}
}

func TestMaybeCompactForceCeilingBypassesEconomics(t *testing.T) {
	// Physical hard ceiling (window−reserve) forces a summary even when the fold
	// is below the minFoldTokens economics floor — but the fold must still be
	// large enough that the candidate lands under the compact_ratio trigger.
	const window = 10_000
	big := strings.Repeat("old analysis detail ", 400)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: big},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	prov := &fakeProvider{reply: "forced summary"}
	a := New(prov, tool.NewRegistry(), sess, Options{ContextWindow: window, CompactRatio: 0.85, RecentKeep: 2}, event.Discard)

	// hard = 10000-256 = 9744; observe just above it.
	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 9800})
	if got := len(sess.Messages); got != 5 {
		t.Fatalf("canonical len = %d, want 5: %+v", got, sess.Messages)
	}
	if sess.Messages[1].Content != "task" {
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
	// Above compact_ratio but below the physical hard ceiling: low-value folds
	// are rejected by foldEconomics without calling the summarizer.
	const window = 10_000
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "small old request"},
		{Role: provider.RoleAssistant, Content: "small old answer"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	prov := &fakeProvider{reply: "should not summarize"}
	a := New(prov, tool.NewRegistry(), sess, Options{ContextWindow: window, CompactRatio: 0.85, RecentKeep: 2}, event.Discard)

	// fold trigger = 8500; hard = 9744. Observe between them.
	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 8600})
	if got := len(sess.Messages); got != 5 {
		t.Fatalf("low-value region should not compact before force ceiling, len = %d", got)
	}
	if len(prov.got) != 0 {
		t.Fatalf("summarizer was called for low-value non-forced region: %+v", prov.got)
	}
	if a.currentProjectionVersion() != 0 {
		t.Fatalf("low-value region installed projection version %d", a.currentProjectionVersion())
	}
}

func TestMaybeCompactFoldsSingleLargeMessageAtThreshold(t *testing.T) {
	const window = 10_000
	// Large assistant work (not the first user turn) so it is foldable, not pinned.
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("large prompt chunk ", 500)},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(&fakeProvider{reply: "single large summary"}, tool.NewRegistry(), sess, Options{
		ContextWindow: window, CompactRatio: 0.8, RecentKeep: 2,
	}, event.Discard)

	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 8500})
	if got := len(sess.Messages); got != 5 {
		t.Fatalf("canonical len = %d, want 5: %+v", got, sess.Messages)
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

// Display-only output stays verbatim in the canonical transcript by construction
// (compaction only writes a projection); this pins the other half: it must never
// reach the summarizer or the model-visible projection.
func TestInterruptedDisplayStaysOutOfCompactionPromptAndProjection(t *testing.T) {
	local := provider.Message{
		Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName,
		LocalOnly: true, Content: "partial visible answer", ReasoningContent: "private partial reasoning",
		InterruptedTurn: &provider.InterruptedTurnRecovery{Pending: true},
	}
	a := &Agent{}
	kept, fold, retention := a.partitionFoldForProjection([]provider.Message{local})
	if len(kept) != 0 || len(fold) != 0 {
		t.Fatalf("compaction partition kept=%+v fold=%+v, want display-only output in neither", kept, fold)
	}
	if retention.Kept != 0 || retention.Dropped != 0 {
		t.Fatalf("retention = %+v, want display-only output counted as neither kept nor dropped", retention)
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
		ContextWindow: 50_000, CompactRatio: 0.85, RecentKeep: 1,
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
			a.sess.compaction.consecutive = 1
			a.sess.compaction.stuck = true

			prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: tc.prompt})

			if a.sess.compaction.consecutive != 0 || a.sess.compaction.stuck {
				t.Fatalf("prompt %d sits under the trigger; want the latch cleared, got consecutiveCompacts=%d compactStuck=%v",
					tc.prompt, a.sess.compaction.consecutive, a.sess.compaction.stuck)
			}
		})
	}
}

// TestMaybeCompactDefersWhenOnlyActiveTurnRemains proves current-turn
// protection wins over a synthetic pressure observation.
func TestMaybeCompactDefersWhenOnlyActiveTurnRemains(t *testing.T) {
	sess := NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	a := New(&fakeProvider{reply: "- summary"}, tool.NewRegistry(), sess, Options{ContextWindow: 20000}, event.Discard)

	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 17000})
	if a.sess.compaction.stuck {
		t.Fatalf("active turn should be deferred, not durably blocked: consecutiveCompacts=%d", a.sess.compaction.consecutive)
	}
	version := a.currentProjectionVersion()
	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 17000})
	if got := a.currentProjectionVersion(); got != version {
		t.Fatalf("blocked fingerprint retried: projection version %d -> %d", version, got)
	}
}

func TestCompactTriggerIgnoresConfiguredOutputBudget(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 100_000, maxOutputTokens: 20_000, compactRatio: 0.85}}
	if got := a.compactTrigger(); got != 85_000 {
		t.Fatalf("trigger = %d, want 85000 (output budget must not change it)", got)
	}
	if got := a.hardInputCeiling(); got != 100_000-protocolReserveTokens {
		t.Fatalf("hard ceiling = %d, want window minus protocol reserve only", got)
	}
	if got := a.checkpointCeiling(); got != 50_000 {
		t.Fatalf("checkpoint ceiling = %d, want 50000", got)
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
