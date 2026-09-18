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

func feed(t *testing.T, g *perseverationGuard, delta string) bool {
	t.Helper()
	return g.observe(delta)
}

func TestPerseverationRetryMessageIsParameterized(t *testing.T) {
	if got, want := perseverationRetryMessage(1), "\n[retrying (1) avoiding perseveration]\n"; got != want {
		t.Fatalf("perseverationRetryMessage(1) = %q, want %q", got, want)
	}
	if got, want := perseverationRetryMessage(3), "\n[retrying (3) avoiding perseveration]\n"; got != want {
		t.Fatalf("perseverationRetryMessage(3) = %q, want %q", got, want)
	}
}

// nonRepeatingText builds non-periodic filler of at least n bytes. Tests that
// need long streamed text or reasoning must not use strings.Repeat: a literally
// repeated block now trips the perseveration guard and would abort or retry the
// stream instead of exercising the size path under test.
func nonRepeatingText(n int) string {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, "deliberation step %d weighs a distinct option. ", i)
	}
	return b.String()
}

func TestPerseverationGuardFiresOnRepeatedPhraseLoop(t *testing.T) {
	g := newPerseverationGuard()
	block := "Let me write.\n\nHmm.\n\nOK.\n\n"
	firedAt := -1
	for i := 0; i < 40; i++ {
		if feed(t, g, block) {
			firedAt = i
			break
		}
	}
	if firedAt < 0 {
		t.Fatal("guard never fired on a short repeated phrase loop")
	}
	if firedAt < 7 {
		t.Fatalf("guard fired after %d repeats, want >= minRepeats", firedAt+1)
	}
	// Firing is one-shot: further deltas must not report again.
	if feed(t, g, block) {
		t.Fatal("guard fired more than once")
	}
}

func TestPerseverationGuardFiresOnSingleCharacterFlood(t *testing.T) {
	g := newPerseverationGuard()
	fired := false
	for i := 0; i < 100 && !fired; i++ {
		fired = feed(t, g, "ok ")
	}
	if !fired {
		t.Fatal("guard never fired on a repeated token flood")
	}
}

func TestPerseverationGuardShortBlockNeedsManyRepeats(t *testing.T) {
	// A few "Hmm " tokens are ordinary pondering; the guard must stay quiet
	// until the run is unmistakably degenerate.
	for _, reps := range []int{8, 20} {
		g := newPerseverationGuard()
		fired := false
		for i := 0; i < reps; i++ {
			fired = g.observe("Hmm ")
		}
		if fired {
			t.Fatalf("guard fired after %d 'Hmm ' repeats, want a higher bar", reps)
		}
	}
	g := newPerseverationGuard()
	fired := false
	for i := 0; i < 40 && !fired; i++ {
		fired = g.observe("Hmm ")
	}
	if !fired {
		t.Fatal("guard never fired on a long 'Hmm ' flood")
	}
}

func TestPerseverationGuardIgnoresOrdinaryText(t *testing.T) {
	g := newPerseverationGuard()
	var b strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "Sentence number %d has its own distinct words and length.\n", i)
	}
	if g.observe(b.String()) {
		t.Fatal("guard fired on non-repeating prose")
	}
}

func TestPerseverationGuardIgnoresWhitespaceAndShortBlocks(t *testing.T) {
	// A long run of identical blank lines, or a two-char block, must not count:
	// both occur legitimately (formatting, separators) and are filtered by the
	// meaningful-content and span floors.
	for _, block := range []string{"\n\n", "  ", "-\n"} {
		g := newPerseverationGuard()
		fired := false
		for i := 0; i < 200 && !fired; i++ {
			fired = g.observe(block)
		}
		if fired {
			t.Fatalf("guard fired on low-content block %q", block)
		}
	}
}

func TestPerseverationGuardIgnoresRepeatedStructureBelowThreshold(t *testing.T) {
	// Seven identical lines are below the repeat floor and must not fire.
	g := newPerseverationGuard()
	for i := 0; i < 7; i++ {
		if g.observe("identical row here\n") {
			t.Fatalf("guard fired after %d repeats, want >= %d", i+1, g.minRepeats)
		}
	}
}

func TestPerseverationGuardUsesSmallestPeriod(t *testing.T) {
	g := newPerseverationGuard()
	// A block that is itself a perseveration of "ab" should be caught at period 2?
	// No — minPeriod is 4, so the fundamental unit below that is ignored; the
	// guard reports at the smallest period >= minPeriod that qualifies.
	unit := "1234"
	fired := false
	for i := 0; i < 60 && !fired; i++ {
		fired = g.observe(unit)
	}
	if !fired {
		t.Fatal("guard never fired on a four-byte repeating unit")
	}
}

func TestStreamAbortsOnPerseverationLoop(t *testing.T) {
	block := "Let me write.\n\nHmm.\n\nOK.\n\n"
	var chunks []provider.Chunk
	for i := 0; i < 40; i++ {
		chunks = append(chunks, provider.Chunk{Type: provider.ChunkText, Text: block})
	}
	chunks = append(chunks, provider.Chunk{Type: provider.ChunkDone})
	prov := &mockProvider{name: "loop", chunks: chunks}
	var events []event.Event
	sink := event.FuncSink(func(e event.Event) { events = append(events, e) })
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{ModelRef: "loop/model"}, sink)

	st := a.stream(context.Background(), 1, sink)
	if !st.perseverationAborted {
		t.Fatalf("stream did not abort on perseveration loop: err=%v text=%d bytes", st.err, len(st.text))
	}
	if st.err != nil {
		t.Fatalf("perseveration abort must be a clean terminal, got err=%v", st.err)
	}
	if st.text == "" {
		t.Fatal("abort discarded the accumulated text")
	}
}

func TestStreamDoesNotAbortOnLongNormalAnswer(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&b, "Sentence number %d has its own distinct words and length.\n", i)
	}
	chunks := []provider.Chunk{
		{Type: provider.ChunkText, Text: b.String()},
		{Type: provider.ChunkDone},
	}
	prov := &mockProvider{name: "normal", chunks: chunks}
	sink := event.FuncSink(func(event.Event) {})
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{ModelRef: "normal/model"}, sink)

	st := a.stream(context.Background(), 1, sink)
	if st.perseverationAborted {
		t.Fatal("normal answer was mistaken for a perseveration loop")
	}
}

func perseverationLoopChunks() []provider.Chunk {
	block := "Let me write.\n\nHmm.\n\nOK.\n\n"
	var chunks []provider.Chunk
	for i := 0; i < 40; i++ {
		chunks = append(chunks, provider.Chunk{Type: provider.ChunkText, Text: block})
	}
	return append(chunks, provider.Chunk{Type: provider.ChunkDone})
}

func TestPerseverationRetryBudgetIsConfigurable(t *testing.T) {
	cases := []struct {
		name     string
		retries  *int
		wantReqs int
	}{
		{"zero disables the retry", intPtr(0), 1},
		{"unset keeps the default", nil, 2},
		{"two retries", intPtr(2), 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prov := &mockProvider{name: "loop", chunks: perseverationLoopChunks()}
			sink := &recordSink{}
			a := New(prov, tool.NewRegistry(), NewSession("system"),
				Options{ModelRef: "loop/model", MaxPerseverationRetries: tc.retries}, sink)

			if err := a.Run(context.Background(), "write the file"); err != nil {
				t.Fatalf("Run error = %v", err)
			}
			if len(prov.requests) != tc.wantReqs {
				t.Fatalf("provider requests = %d, want %d", len(prov.requests), tc.wantReqs)
			}
			warned := false
			for _, e := range sink.kinds(event.Notice) {
				if e.Code == event.NoticeCodePerseverationLoop {
					warned = true
				}
			}
			if !warned {
				t.Fatal("expected a perseveration_loop notice once the budget is spent")
			}
		})
	}
}

func TestRunRetriesOnceThenStopsOnRepeatedLoop(t *testing.T) {
	prov := &mockProvider{name: "loop", chunks: perseverationLoopChunks()}
	sink := &recordSink{}
	session := NewSession("system")
	a := New(prov, tool.NewRegistry(), session, Options{ModelRef: "loop/model"}, sink)

	if err := a.Run(context.Background(), "write the file"); err != nil {
		t.Fatalf("Run error = %v, want a clean terminal", err)
	}
	// One nudge-and-retry, then the second consecutive loop stops for the user.
	if len(prov.requests) != 2 {
		t.Fatalf("provider requests = %d, want 2 (one retry)", len(prov.requests))
	}
	if a.sess.perseverationStrikes != 1 {
		t.Fatalf("perseverationStrikes = %d, want 1 after abort", a.sess.perseverationStrikes)
	}
	warned := false
	for _, e := range sink.kinds(event.Notice) {
		if e.Code == event.NoticeCodePerseverationLoop {
			warned = true
		}
	}
	if !warned {
		t.Fatal("no perseveration_loop notice emitted on the final abort")
	}
	// The retry request must carry the host nudge, so it is not an exact resend.
	second := prov.requests[1]
	found := false
	for _, m := range second.Messages {
		if strings.Contains(m.Content, "retrying (1)") {
			found = true
		}
	}
	if !found {
		t.Fatal("retry request did not carry the host nudge message")
	}
}

func TestPerseverationStrikeResetsAfterSuccessfulResponse(t *testing.T) {
	normal := []provider.Chunk{
		{Type: provider.ChunkText, Text: "Here is the file content you asked for."},
		{Type: provider.ChunkDone},
	}
	prov := &mockProvider{name: "loop-then-normal", streams: [][]provider.Chunk{perseverationLoopChunks(), normal}}
	sink := &recordSink{}
	a := New(prov, tool.NewRegistry(), NewSession("system"), Options{ModelRef: "loop/model"}, sink)

	if err := a.Run(context.Background(), "write the file"); err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if len(prov.requests) != 2 {
		t.Fatalf("provider requests = %d, want 2 (retry succeeded)", len(prov.requests))
	}
	if a.sess.perseverationStrikes != 0 {
		t.Fatalf("perseverationStrikes = %d, want 0 after a successful response", a.sess.perseverationStrikes)
	}
	for _, e := range sink.kinds(event.Notice) {
		if e.Code == event.NoticeCodePerseverationLoop {
			t.Fatal("perseveration_loop abort notice fired despite the retry succeeding")
		}
	}
}
