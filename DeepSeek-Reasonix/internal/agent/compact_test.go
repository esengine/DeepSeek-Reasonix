package agent

import (
	"context"
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
	reply string
	got   []provider.Message
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	f.got = req.Messages
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: f.reply}
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

	// system + summary + last 2 verbatim.
	if got := len(sess.Messages); got != 4 {
		t.Fatalf("len = %d, want 4: %+v", got, sess.Messages)
	}
	if sess.Messages[0].Role != provider.RoleSystem {
		t.Errorf("message 0 = %s, want system", sess.Messages[0].Role)
	}
	summary := sess.Messages[1]
	if summary.Role != provider.RoleUser || !strings.Contains(summary.Content, "Summary of earlier") || !strings.Contains(summary.Content, "do X") {
		t.Errorf("summary message = %+v", summary)
	}
	if sess.Messages[2].Content != "next" || sess.Messages[3].Content != "ok" {
		t.Errorf("recent tail not preserved: %+v", sess.Messages[2:])
	}

	// The 4 dropped originals were archived, one JSON object per line.
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("archive dir: entries=%d err=%v", len(entries), err)
	}
	data, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read archive: %v", err)
	}
	if lines := strings.Count(strings.TrimSpace(string(data)), "\n") + 1; lines != 4 {
		t.Errorf("archived %d lines, want 4:\n%s", lines, data)
	}
	if !strings.HasSuffix(entries[0].Name(), ".jsonl") {
		t.Errorf("archive name = %q, want .jsonl", entries[0].Name())
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
	// The token-budgeted tail keeps "small old answer", next, ok, so only the
	// single early message folds — force bypasses the economics skip and installs
	// a summary at index 1, leaving the count at 5.
	if got := len(sess.Messages); got != 5 {
		t.Fatalf("len = %d, want 5 after forced single-message fold: %+v", got, sess.Messages)
	}
	if !strings.Contains(sess.Messages[1].Content, "forced summary") {
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
