package agent

import (
	"context"
	"errors"
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
		{"also-pins-prior-summaries", 0, []provider.Message{sys, small, sum, sum, as}, 4},
		{"large-first-turn-stays-foldable", 0, []provider.Message{sys, big, as, as}, 1},
		{"tiny-window-wont-pin", 10, []provider.Message{sys, small, as, as}, 1},
		{"summary-is-not-the-task-turn", 0, []provider.Message{sys, sum, as}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := newA(tc.win).pinnedPrefixLen(tc.msgs); got != tc.want {
				t.Errorf("pinnedPrefixLen = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestCompactKeepsMidSessionUserTurns(t *testing.T) {
	big := strings.Repeat("work output ", 100)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "first task"},
		{Role: provider.RoleAssistant, Content: big},
		{Role: provider.RoleTool, ToolCallID: "1", Name: "read_file", Content: big},
		{Role: provider.RoleUser, Content: "by the way, always use pnpm not npm"},
		{Role: provider.RoleAssistant, Content: big},
		{Role: provider.RoleTool, ToolCallID: "2", Name: "read_file", Content: big},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(&fakeProvider{reply: "digest"}, tool.NewRegistry(), sess,
		Options{RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	if err := a.compact(context.Background(), "manual", "", true); err != nil {
		t.Fatalf("compact: %v", err)
	}

	// Both the pinned first turn and the mid-session fact survive verbatim — not as
	// summary text — while the assistant/tool work between them is folded.
	var pinnedFirst, keptMid bool
	for _, m := range sess.Snapshot() {
		if isCompactionSummary(m) {
			continue
		}
		if m.Role == provider.RoleUser && m.Content == "first task" {
			pinnedFirst = true
		}
		if m.Role == provider.RoleUser && strings.Contains(m.Content, "always use pnpm not npm") {
			keptMid = true
		}
	}
	if !pinnedFirst || !keptMid {
		t.Fatalf("user turns not kept verbatim (first=%v mid=%v): %+v", pinnedFirst, keptMid, sess.Snapshot())
	}
	if strings.Contains(strings.Join(snapshotContents(sess), " "), big) {
		t.Errorf("assistant/tool work was not folded")
	}
}

func snapshotContents(s *Session) []string {
	msgs := s.Snapshot()
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Content
	}
	return out
}

func TestRunCompactsAfterFinalAnswer(t *testing.T) {
	// A turn that ends with a final answer (no trailing tool batch) must still
	// compact when the context is over the trigger; otherwise a large context
	// carries into the next turn un-folded and overflows the model window.
	big := strings.Repeat("old work ", 200)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: big},
		{Role: provider.RoleAssistant, Content: big},
	}}
	a := New(&fakeProvider{reply: "done", promptTokens: 95}, tool.NewRegistry(), sess,
		Options{ContextWindow: 100, RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	if err := a.Run(context.Background(), "what's the status?"); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := sess.RewriteVersion(); got != 1 {
		t.Fatalf("final-answer turn over the trigger did not compact: rewrite version = %d, want 1", got)
	}
}

func TestCompactKeepsPriorDigests(t *testing.T) {
	// A prior digest anywhere in the folded region is kept verbatim, not
	// re-summarized — so a fact it already captured is not lost to re-fold drift.
	priorDigest := summaryTagOpen + "\n## Standing facts\n- db is orion_prod_42\n" + summaryTagClose
	big := strings.Repeat("work output ", 200)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: big}, // breaks leading-summary contiguity
		{Role: provider.RoleUser, Content: priorDigest},
		{Role: provider.RoleAssistant, Content: big},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(&fakeProvider{reply: "new digest"}, tool.NewRegistry(), sess,
		Options{RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	if err := a.compact(context.Background(), "manual", "", true); err != nil {
		t.Fatalf("compact: %v", err)
	}

	// The fake summarizer returns "new digest" (no fact); the prior fact survives
	// only because the prior digest was kept verbatim rather than re-folded.
	var kept bool
	for _, m := range sess.Snapshot() {
		if strings.Contains(m.Content, "orion_prod_42") {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("prior digest re-summarized away: %+v", sess.Snapshot())
	}
}

func TestCompactReplacesHistory(t *testing.T) {
	prov := &fakeProvider{reply: "- goal: do X\n- changed file Y"}
	bigStep := strings.Repeat("important implementation detail ", 80)
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task " + bigStep},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "1", Name: "read_file", Arguments: "{}"}}},
		{Role: provider.RoleTool, ToolCallID: "1", Name: "read_file", Content: "file contents"},
		{Role: provider.RoleAssistant, Content: "did a step"},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	dir := t.TempDir()
	a := New(prov, tool.NewRegistry(), sess, Options{RecentKeep: 2, ArchiveDir: dir}, event.Discard)

	if err := a.compact(context.Background(), "manual", "", true); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if got := sess.RewriteVersion(); got != 1 {
		t.Fatalf("rewrite version = %d, want 1", got)
	}

	// system + pinned first user turn + summary + last 2 verbatim.
	if got := len(sess.Messages); got != 5 {
		t.Fatalf("len = %d, want 5: %+v", got, sess.Messages)
	}
	if sess.Messages[0].Role != provider.RoleSystem {
		t.Errorf("message 0 = %s, want system", sess.Messages[0].Role)
	}
	if task := sess.Messages[1]; task.Role != provider.RoleUser || !strings.HasPrefix(task.Content, "task ") {
		t.Errorf("first user turn not pinned verbatim: %+v", task)
	}
	summary := sess.Messages[2]
	if summary.Role != provider.RoleUser || !strings.Contains(summary.Content, "Summary of earlier") || !strings.Contains(summary.Content, "do X") {
		t.Errorf("summary message = %+v", summary)
	}
	if sess.Messages[3].Content != "next" || sess.Messages[4].Content != "ok" {
		t.Errorf("recent tail not preserved: %+v", sess.Messages[3:])
	}

	// The 3 dropped originals were archived, one JSON object per line (the task
	// turn is pinned, not folded, so it is not among them).
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("archive dir: entries=%d err=%v", len(entries), err)
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if lines := strings.Count(strings.TrimSpace(string(data)), "\n") + 1; lines != 3 {
		t.Errorf("archived %d lines, want 3:\n%s", lines, data)
	}
	if !strings.HasSuffix(entries[0].Name(), ".jsonl") {
		t.Errorf("archive name = %q, want .jsonl", entries[0].Name())
	}
}

// TestCompactFallsBackToMechanicalFoldWhenSummaryFails: when the summarizer is
// unreachable, /compact must still free context (fold mechanically) and surface a
// card, not hang or abort leaving a full window.
func TestCompactFallsBackToMechanicalFoldWhenSummaryFails(t *testing.T) {
	prov := &fakeProvider{streamErr: errors.New("provider down")}
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
	a := New(prov, tool.NewRegistry(), sess, Options{RecentKeep: 2, ArchiveDir: t.TempDir()}, sink)

	before := len(sess.Messages)
	if err := a.compact(context.Background(), "manual", "", true); err != nil {
		t.Fatalf("compact should fall back, not error: %v", err)
	}
	if len(sess.Messages) >= before {
		t.Fatalf("session not compacted on summarizer failure: %d -> %d", before, len(sess.Messages))
	}
	var done *event.Compaction
	for i := range got {
		if got[i].Kind == event.CompactionDone {
			done = &got[i].Compaction
		}
	}
	if done == nil || !strings.Contains(done.Summary, "summary was unavailable") {
		t.Fatalf("CompactionDone = %+v, want a mechanical-fold summary", done)
	}
}

// TestSummarizeRespectsContextCancel: a stalled stream (open but never closing)
// must unblock on context cancellation instead of pinning compaction forever.
func TestSummarizeRespectsContextCancel(t *testing.T) {
	a := New(&fakeProvider{hang: true}, tool.NewRegistry(), &Session{}, Options{}, event.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.summarize(ctx, []provider.Message{{Role: provider.RoleUser, Content: "x"}}, ""); err == nil {
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

func TestCompactRewriteVersionFeedsCacheDiagnostics(t *testing.T) {
	prov := &fakeProvider{reply: "- summary"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "a"},
		{Role: provider.RoleAssistant, Content: "b"},
		{Role: provider.RoleUser, Content: "c"},
		{Role: provider.RoleAssistant, Content: "d"},
		{Role: provider.RoleUser, Content: "e"},
		{Role: provider.RoleAssistant, Content: "f"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{RecentKeep: 2}, event.Discard)
	before := CaptureShape("sys", nil, sess.RewriteVersion())

	if err := a.compact(context.Background(), "auto", "", true); err != nil {
		t.Fatalf("compact: %v", err)
	}

	after := CaptureShape("sys", nil, sess.RewriteVersion())
	diag := CompareShape(before, after, &provider.Usage{CacheMissTokens: 10})
	if !diag.PrefixChanged {
		t.Fatalf("diagnostics should report prefix change: %+v", diag)
	}
	if len(diag.PrefixChangeReasons) != 1 || diag.PrefixChangeReasons[0] != "log_rewrite" {
		t.Fatalf("change reasons = %v, want [log_rewrite]", diag.PrefixChangeReasons)
	}
}

func TestCompactFoldsSingleLargeMessage(t *testing.T) {
	prov := &fakeProvider{reply: "- captured the large file contents"}
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleTool, ToolCallID: "1", Name: "read_file", Content: strings.Repeat("large output line\n", 500)},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(prov, tool.NewRegistry(), sess, Options{RecentKeep: 2, ArchiveDir: t.TempDir()}, event.Discard)

	if err := a.compact(context.Background(), "auto", "", false); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if got := len(sess.Messages); got != 4 {
		t.Fatalf("len = %d, want 4: %+v", got, sess.Messages)
	}
	if !strings.Contains(sess.Messages[1].Content, "large file contents") {
		t.Fatalf("single large message was not summarized: %+v", sess.Messages)
	}
	if len(prov.got) == 0 || !strings.Contains(prov.got[1].Content, "large output line") {
		t.Fatalf("summarizer did not receive the large message: %+v", prov.got)
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
	if len(notices) != 1 || !strings.Contains(notices[0].Text, "context reached 50%") {
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
	if !strings.Contains(sess.Messages[1].Content, "Summary of earlier") {
		t.Errorf("compact threshold should fold the large early message, got: %+v", sess.Messages[1])
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
	// The first user turn is pinned (index 1) and the token-budgeted tail keeps
	// next, ok, so only "small old answer" folds — force bypasses the economics
	// skip and installs a summary at index 2, leaving the count at 5.
	if got := len(sess.Messages); got != 5 {
		t.Fatalf("len = %d, want 5 after forced single-message fold: %+v", got, sess.Messages)
	}
	if sess.Messages[1].Content != "small old request" {
		t.Fatalf("first user turn not pinned verbatim: %+v", sess.Messages[1])
	}
	if !strings.Contains(sess.Messages[2].Content, "forced summary") {
		t.Fatalf("forced compact did not install summary: %+v", sess.Messages)
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
		t.Fatalf("len = %d, want 4: %+v", got, sess.Messages)
	}
	if !strings.Contains(sess.Messages[1].Content, "single large summary") {
		t.Fatalf("single large message was not compacted at threshold: %+v", sess.Messages)
	}
}
