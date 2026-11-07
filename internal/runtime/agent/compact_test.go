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

// prepareForObservedUsage preserves the old synthetic-usage test ergonomics
// while production has only one mutating entry point: ContextManager.Prepare.
func prepareForObservedUsage(a *Agent, ctx context.Context, usage *provider.Usage) {
	if a == nil || usage == nil || usage.LatestPromptTokens() <= 0 {
		return
	}
	view := a.window().modelVisibleMessages()
	a.window().setPromptTokenCalibration(usage.LatestPromptTokens(), a.window().requestCalibrationShape(provider.Request{Messages: view}))
	_, _ = a.window().contextManager().Prepare(ctx, ContextPreparePolicy{
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
	verbatim     bool  // when true, a summary request is answered with reply exactly as written
}

func (f *fakeProvider) Name() string { return "fake" }

// answer speaks the summary contract unless the test asks for the reply as
// written.
func (f *fakeProvider) answer(req provider.Request) string {
	if f.verbatim {
		return f.reply
	}
	return f.reply
}

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
	ch <- provider.Chunk{Type: provider.ChunkText, Text: f.answer(req)}
	if f.promptTokens > 0 {
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: f.promptTokens, TotalTokens: f.promptTokens}}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// visibleContext returns the model-visible projection when present, else the
// canonical transcript. Compaction tests assert against this view.

func TestPinnedPrefixLen(t *testing.T) {
	sys := provider.Message{Role: provider.RoleSystem}
	small := provider.Message{Role: provider.RoleUser, Content: "do X with token T"}
	big := provider.Message{Role: provider.RoleUser, Content: strings.Repeat("x", 100000)}
	sum := provider.Message{Role: provider.RoleUser, Content: SummaryTagOpen + "\ndigest\n" + SummaryTagClose}
	as := provider.Message{Role: provider.RoleAssistant, Content: "a"}

	newA := func(win int) *Agent {
		return New(&fakeProvider{}, tool.NewRegistry(), &sessionstore.Session{}, Options{ContextWindow: win}, event.Discard)
	}
	cases := []struct {
		name string
		win  int
		msgs []provider.Message
		want int
	}{
		{"pins-only-the-system-prompt", 0, []provider.Message{sys, small, as, as}, 1},
		{"summaries-are-not-pinned-A1-merge", 0, []provider.Message{sys, small, sum, sum, as}, 1},
		{"large-first-turn-stays-foldable", 0, []provider.Message{sys, big, as, as}, 1},
		{"tiny-window-pins-the-same", 10, []provider.Message{sys, small, as, as}, 1},
		{"no-system-prompt-pins-nothing", 0, []provider.Message{small, as}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := newA(tc.win).window().pinnedPrefixLen(tc.msgs); got != tc.want {
				t.Errorf("pinnedPrefixLen = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestMaybeCompactFoldsSingleLargeMessageAtThreshold(t *testing.T) {
	const window = 10_000
	// Large assistant work (not the first user turn) so it is foldable, not pinned.
	sess := &sessionstore.Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "task"},
		{Role: provider.RoleAssistant, Content: strings.Repeat("large prompt chunk ", 500)},
		{Role: provider.RoleUser, Content: "next"},
		{Role: provider.RoleAssistant, Content: "ok"},
	}}
	a := New(&fakeProvider{reply: "archived, not summarized"}, tool.NewRegistry(), sess, Options{
		ContextWindow: window, CompactRatio: 0.8}, event.Discard)

	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 8500})
	if got := len(sess.Messages); got != 5 {
		t.Fatalf("canonical len = %d, want 5: %+v", got, sess.Messages)
	}
	proj := visibleContext(a)
	if !hasCompactionSummary(proj) || !strings.Contains(joinContents(proj), "archived, not summarized") {
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
// prompt at, say, 70% of the window left the session latched as "window too
// small" — silently disabling auto-compaction for the rest of the run.
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
			sess := sessionstore.NewSession("sys")
			sess.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
			a := New(&fakeProvider{reply: "- summary"}, tool.NewRegistry(), sess, Options{ContextWindow: 20000}, event.Discard)
			a.sess.win.compaction.stuck = true

			prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: tc.prompt})

			if a.sess.win.compaction.stuck {
				t.Fatalf("prompt %d sits under the trigger; want the latch cleared", tc.prompt)
			}
		})
	}
}

// TestMaybeCompactDefersWhenOnlyActiveTurnRemains proves current-turn
// protection wins over a synthetic pressure observation.
func TestMaybeCompactDefersWhenOnlyActiveTurnRemains(t *testing.T) {
	sess := sessionstore.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	a := New(&fakeProvider{reply: "- summary"}, tool.NewRegistry(), sess, Options{ContextWindow: 20000}, event.Discard)

	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 17000})
	if a.sess.win.compaction.stuck {
		t.Fatal("active turn should be deferred, not durably blocked")
	}
	version := a.window().currentProjectionVersion()
	prepareForObservedUsage(a, context.Background(), &provider.Usage{PromptTokens: 17000})
	if got := a.window().currentProjectionVersion(); got != version {
		t.Fatalf("blocked fingerprint retried: projection version %d -> %d", version, got)
	}
}

func TestCompactTriggerIgnoresConfiguredOutputBudget(t *testing.T) {
	a := &Agent{agentConfig: agentConfig{contextWindow: 100_000, maxOutputTokens: 20_000, compactRatio: 0.85}}
	if got := a.window().compactTrigger(); got != 85_000 {
		t.Fatalf("trigger = %d, want 85000 (output budget must not change it)", got)
	}
	if got := a.window().hardInputCeiling(); got != 100_000-protocolReserveTokens {
		t.Fatalf("hard ceiling = %d, want window minus protocol reserve only", got)
	}
	if got := a.window().checkpointCeiling(); got != 50_000 {
		t.Fatalf("checkpoint ceiling = %d, want 50000", got)
	}
}
