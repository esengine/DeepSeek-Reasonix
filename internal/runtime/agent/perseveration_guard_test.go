package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
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
// repeated block would trip the guard.
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
	for i := range 400 {
		if feed(t, g, block) {
			firedAt = i
			break
		}
	}
	if firedAt < 0 {
		t.Fatal("guard never fired on a repeated phrase loop")
	}
	if firedAt < g.minRepeats {
		t.Fatalf("guard fired after %d repeats, want >= minRepeats", firedAt+1)
	}
	// Firing is one-shot: further deltas must not report again.
	if feed(t, g, block) {
		t.Fatal("guard fired more than once")
	}
}

// A notice is said once per loop episode: a scan that trips again while the
// previous scan also tripped stays silent, and the next scan that does not trip
// re-arms the guard so the following loop is reported again.
func TestPerseverationGuardReArmsAfterACleanBlock(t *testing.T) {
	block := "Let me write.\n\nHmm.\n\nOK.\n\n"
	g := newPerseverationGuard()

	fired := 0
	for range 400 {
		if g.observe(block) {
			fired++
		}
	}
	if fired != 1 {
		t.Fatalf("one continuous loop fired %d times, want exactly 1", fired)
	}
	// A block that does not trip re-arms the guard.
	if g.observe(nonRepeatingText(2 * scanIntervalBytes)) {
		t.Fatal("a clean block was reported as a loop")
	}
	for i := 0; i < 400 && fired < 2; i++ {
		if g.observe(block) {
			fired++
		}
	}
	if fired != 2 {
		t.Fatalf("a loop after a clean block fired %d times total, want 2", fired)
	}
}

func TestPerseverationGuardFiresOnSingleCharacterFlood(t *testing.T) {
	g := newPerseverationGuard()
	fired := false
	for i := 0; i < 2000 && !fired; i++ {
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
		for range reps {
			fired = g.observe("Hmm ")
		}
		if fired {
			t.Fatalf("guard fired after %d 'Hmm ' repeats, want a higher bar", reps)
		}
	}
	g := newPerseverationGuard()
	fired := false
	for i := 0; i < 2000 && !fired; i++ {
		fired = g.observe("Hmm ")
	}
	if !fired {
		t.Fatal("guard never fired on a long 'Hmm ' flood")
	}
}

func TestPerseverationGuardIgnoresOrdinaryText(t *testing.T) {
	g := newPerseverationGuard()
	var b strings.Builder
	for i := range 200 {
		fmt.Fprintf(&b, "Sentence number %d has its own distinct words and length.\n", i)
	}
	if g.observe(b.String()) {
		t.Fatal("guard fired on non-repeating prose")
	}
}

func TestPerseverationGuardFiresOnLowContentFloods(t *testing.T) {
	// No meaningful-content filter: any byte-identical block long enough to
	// clear the repeat floor trips the guard, including the whitespace and
	// punctuation a stuck model emits as readily as words.
	for _, block := range []string{"\n\n", "  ", "-\n", "A", "B\n"} {
		g := newPerseverationGuard()
		fired := false
		for i := 0; i < 4000 && !fired; i++ {
			fired = g.observe(block)
		}
		if !fired {
			t.Fatalf("guard never fired on repeated low-content block %q", block)
		}
	}
}

func TestPerseverationGuardIgnoresRepeatedStructureBelowThreshold(t *testing.T) {
	// Seven identical lines are below the repeat floor and must not fire.
	g := newPerseverationGuard()
	for i := range 7 {
		if g.observe("identical row here\n") {
			t.Fatalf("guard fired after %d repeats, want >= %d", i+1, g.minRepeats)
		}
	}
}

func TestPerseverationGuardUsesSmallestPeriod(t *testing.T) {
	g := newPerseverationGuard()
	// A four-byte unit is below minPeriod, so it is not a period itself — but a
	// period-4 tail is also periodic at every multiple of 4, so the guard still
	// catches it at the smallest multiple of 4 that clears minPeriod.
	unit := "1234"
	fired := false
	for i := 0; i < 2000 && !fired; i++ {
		fired = g.observe(unit)
	}
	if !fired {
		t.Fatal("guard never fired on a four-byte repeating unit")
	}
}

func TestPerseverationGuardsRouteByChannel(t *testing.T) {
	g := newPerseverationGuards()
	cases := []struct {
		t    provider.ChunkType
		want *perseverationGuard
	}{
		{provider.ChunkReasoning, g.reasoning},
		{provider.ChunkText, g.text},
		{provider.ChunkToolCall, nil},
		{provider.ChunkUsage, nil},
		{provider.ChunkDone, nil},
	}
	for _, tc := range cases {
		if got := g.forChunk(tc.t); got != tc.want {
			t.Errorf("forChunk(%v) = %p, want %p", tc.t, got, tc.want)
		}
	}
	if g.reasoning == g.text {
		t.Fatal("reasoning and text must use distinct guard buffers")
	}
}

func TestResolvePerseverationGuardDefaults(t *testing.T) {
	cases := []struct {
		name    string
		opt     *int
		enabled bool
		retries int
	}{
		{"undefined detects without cutting", nil, true, 0},
		{"negative never guards", new(-1), false, 0},
		{"zero guards but does not cut", new(0), true, 0},
		{"positive guards and cuts", new(2), true, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enabled, retries := resolvePerseverationGuard(tc.opt)
			if enabled != tc.enabled || retries != tc.retries {
				t.Fatalf("resolvePerseverationGuard(%v) = (%t, %d), want (%t, %d)",
					tc.opt, enabled, retries, tc.enabled, tc.retries)
			}
		})
	}
}

func perseverationLoopChunks() []provider.Chunk {
	block := "Let me write.\n\nHmm.\n\nOK.\n\n"
	var chunks []provider.Chunk
	for range 400 {
		chunks = append(chunks, provider.Chunk{Type: provider.ChunkText, Text: block})
	}
	return append(chunks, provider.Chunk{Type: provider.ChunkDone})
}

// By default the guard detects but does not cut: a byte-identical loop is
// reported through the progress-watch channel, the stream runs to its own
// terminal, and the model is never nudged.
func TestDefaultDetectsWithoutCutting(t *testing.T) {
	prov := &mockProvider{name: "loop", chunks: perseverationLoopChunks()}
	sink := &recordSink{}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession("system"), Options{ModelRef: "loop/model"}, sink)

	if err := a.Run(context.Background(), "write the file"); err != nil {
		t.Fatalf("Run error = %v, want a clean terminal", err)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1: the default must not cut or retry", len(prov.requests))
	}
	reports := progressReports(sink)
	if len(reports) == 0 {
		t.Fatal("default run did not report the loop through the progress-watch channel")
	}
	if last := reports[len(reports)-1]; !last.Stalled || last.Cause != event.ProgressWatchCausePerseveration {
		t.Fatalf("default report = %+v, want a perseveration stall", last)
	}
	for _, e := range sink.kinds(event.Steer) {
		t.Fatalf("default run nudged the model: %q", e.Text)
	}
	for _, e := range sink.kinds(event.Notice) {
		if e.Code == event.NoticeCodePerseverationLoop || e.Code == event.NoticeCodePerseverationRetry {
			t.Fatalf("default run emitted a %s notice", e.Code)
		}
	}
}

// A negative budget is the same as undefined: never guard.
func TestNegativeBudgetNeverGuards(t *testing.T) {
	prov := &mockProvider{name: "loop", chunks: perseverationLoopChunks()}
	sink := &recordSink{}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession("system"),
		Options{ModelRef: "loop/model", MaxPerseverationRetries: new(-1)}, sink)

	if err := a.Run(context.Background(), "write the file"); err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1: a negative budget must not guard", len(prov.requests))
	}
	if reports := progressReports(sink); len(reports) != 0 {
		t.Fatalf("negative budget reported a progress-watch event: %+v", reports)
	}
}

func progressReports(s *recordSink) []event.ProgressWatch {
	var out []event.ProgressWatch
	for _, e := range s.kinds(event.ProgressWatchEvent) {
		if e.ProgressWatch != nil {
			out = append(out, *e.ProgressWatch)
		}
	}
	return out
}

// A zero budget enables detection only: the loop is reported through the
// progress-watch channel, the stream is not cut, and the model is not nudged.
func TestZeroBudgetReportsPerseverationWithoutCutting(t *testing.T) {
	prov := &mockProvider{name: "loop", chunks: perseverationLoopChunks()}
	sink := &recordSink{}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession("system"),
		Options{ModelRef: "loop/model", MaxPerseverationRetries: new(0)}, sink)

	if err := a.Run(context.Background(), "write the file"); err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if len(prov.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1: zero budget must not cut or retry", len(prov.requests))
	}
	reports := progressReports(sink)
	if len(reports) == 0 {
		t.Fatal("no perseveration report reached the sink")
	}
	last := reports[len(reports)-1]
	if !last.Stalled || last.Cause != event.ProgressWatchCausePerseveration || last.Pausing {
		t.Fatalf("perseveration report = %+v, want a non-pausing perseveration stall", last)
	}
}

// With the pause setting on, a perseveration report ends the run resumably.
func TestPerseverationPausesUnderTheUserSetting(t *testing.T) {
	prov := &mockProvider{name: "loop", chunks: perseverationLoopChunks()}
	sink := &recordSink{}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession("system"),
		Options{ModelRef: "loop/model", MaxPerseverationRetries: new(0)}, sink)
	a.SetProgressWatch(ProgressWatch{Pause: true, Rounds: 1000, TokenMultiple: 1000})

	err := a.Run(context.Background(), "write the file")
	info, ok := InspectRunPause(err)
	if !ok || info.Kind != PauseKindPerseveration {
		t.Fatalf("Run error = %v (pause %+v, %v), want a perseveration pause", err, info, ok)
	}
	reports := progressReports(sink)
	if len(reports) == 0 || !reports[len(reports)-1].Pausing {
		t.Fatalf("perseveration reports = %+v, want a pausing one", reports)
	}
}

// A detected loop under the pause setting ends the run before the calls it
// streamed can run, so the committed turn carries none of them: a committed call
// with no result is a pairing the next request has to backfill.
func TestPerseverationPauseCommitsNoUnexecutedCalls(t *testing.T) {
	block := "Let me write.\n\nHmm.\n\nOK.\n\n"
	var chunks []provider.Chunk
	for range 400 {
		chunks = append(chunks, provider.Chunk{Type: provider.ChunkText, Text: block})
	}
	chunks = append(chunks,
		provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{"path":"x"}`}},
		provider.Chunk{Type: provider.ChunkDone},
	)
	prov := &mockProvider{name: "loop-tools", chunks: chunks}
	sink := &recordSink{}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession("system"),
		Options{ModelRef: "loop-tools/model", MaxPerseverationRetries: new(0)}, sink)
	a.SetProgressWatch(ProgressWatch{Pause: true, Rounds: 1000, TokenMultiple: 1000})

	err := a.Run(context.Background(), "write the file")
	if info, ok := InspectRunPause(err); !ok || info.Kind != PauseKindPerseveration {
		t.Fatalf("Run error = %v (pause %+v, %v), want a perseveration pause", err, info, ok)
	}
	for _, m := range a.Session().Messages {
		if m.Role == provider.RoleTool {
			t.Fatalf("a tool result was stored for a call that never ran: %q", m.Content)
		}
		if m.Role == provider.RoleAssistant && len(m.ToolCalls) != 0 {
			t.Fatalf("the committed turn carried %d unexecuted tool calls", len(m.ToolCalls))
		}
	}
}

// The pause names the setting that actually stopped the run: the user's pause
// switch for a detected loop, the retry budget once it is spent.
func TestPerseverationPauseNamesTheStoppingKnob(t *testing.T) {
	paused := &mockProvider{name: "loop", chunks: perseverationLoopChunks()}
	a := New(paused, tool.NewRegistry(), sessionstore.NewSession("system"),
		Options{ModelRef: "loop/model", MaxPerseverationRetries: new(0)}, &recordSink{})
	a.SetProgressWatch(ProgressWatch{Pause: true, Rounds: 1000, TokenMultiple: 1000})
	if info, ok := InspectRunPause(a.Run(context.Background(), "write the file")); !ok || info.Key != "progress_watch.pause" {
		t.Fatalf("pause = %+v (ok %v), want key progress_watch.pause", info, ok)
	}

	spent := &mockProvider{name: "loop", chunks: perseverationLoopChunks()}
	b := New(spent, tool.NewRegistry(), sessionstore.NewSession("system"),
		Options{ModelRef: "loop/model", MaxPerseverationRetries: new(1)}, &recordSink{})
	if info, ok := InspectRunPause(b.Run(context.Background(), "write the file")); !ok || info.Key != "progress_watch.perseveration_retries" {
		t.Fatalf("pause = %+v (ok %v), want key progress_watch.perseveration_retries", info, ok)
	}
}

func TestStreamCutsOnPerseverationOnlyWhenOptedIn(t *testing.T) {
	block := "Let me write.\n\nHmm.\n\nOK.\n\n"
	var chunks []provider.Chunk
	for range 400 {
		chunks = append(chunks, provider.Chunk{Type: provider.ChunkText, Text: block})
	}
	chunks = append(chunks, provider.Chunk{Type: provider.ChunkDone})

	for _, tc := range []struct {
		name     string
		opt      *int
		cut      bool
		detected bool
	}{
		{"default detects without cutting", nil, false, true},
		{"zero budget reports without cutting", new(0), false, true},
		{"positive budget cuts", new(1), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prov := &mockProvider{name: "loop", chunks: chunks}
			sink := event.FuncSink(func(event.Event) {})
			a := New(prov, tool.NewRegistry(), sessionstore.NewSession(""),
				Options{ModelRef: "loop/model", MaxPerseverationRetries: tc.opt}, sink)

			st := a.stream(context.Background(), 1, sink)
			if st.err != nil {
				t.Fatalf("stream err = %v", st.err)
			}
			if st.perseverationAborted != tc.cut {
				t.Fatalf("perseverationAborted = %t, want %t", st.perseverationAborted, tc.cut)
			}
			if st.perseverationDetected != tc.detected {
				t.Fatalf("perseverationDetected = %t, want %t", st.perseverationDetected, tc.detected)
			}
			if st.text == "" {
				t.Fatal("stream discarded the accumulated text")
			}
		})
	}
}

func TestStreamDoesNotGuardLongNormalAnswer(t *testing.T) {
	var b strings.Builder
	for i := range 200 {
		fmt.Fprintf(&b, "Sentence number %d has its own distinct words and length.\n", i)
	}
	chunks := []provider.Chunk{
		{Type: provider.ChunkText, Text: b.String()},
		{Type: provider.ChunkDone},
	}
	prov := &mockProvider{name: "normal", chunks: chunks}
	sink := event.FuncSink(func(event.Event) {})
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession(""),
		Options{ModelRef: "normal/model", MaxPerseverationRetries: new(1)}, sink)

	st := a.stream(context.Background(), 1, sink)
	if st.perseverationAborted || st.perseverationDetected {
		t.Fatal("normal answer was mistaken for a perseveration loop")
	}
}

func TestStreamCutsOnReasoningLoop(t *testing.T) {
	block := "Let me think.\n\nHmm.\n\nOK.\n\n"
	var chunks []provider.Chunk
	for range 400 {
		chunks = append(chunks, provider.Chunk{Type: provider.ChunkReasoning, Text: block})
	}
	chunks = append(chunks, provider.Chunk{Type: provider.ChunkDone})
	prov := &mockProvider{name: "think-loop", chunks: chunks}
	sink := event.FuncSink(func(event.Event) {})
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession(""),
		Options{ModelRef: "think-loop/model", MaxPerseverationRetries: new(1)}, sink)

	st := a.stream(context.Background(), 1, sink)
	if !st.perseverationAborted {
		t.Fatalf("stream did not cut a reasoning loop: err=%v", st.err)
	}
}

// A thinking loop interleaved with distinct answer text must still be caught:
// the reasoning deltas are judged on their own buffer, so the answer text
// cannot break their periodicity.
func TestStreamCutsOnReasoningLoopInterleavedWithAnswer(t *testing.T) {
	block := "Let me think.\n\nHmm.\n\nOK.\n\n"
	var chunks []provider.Chunk
	for i := range 400 {
		chunks = append(chunks,
			provider.Chunk{Type: provider.ChunkReasoning, Text: block},
			provider.Chunk{Type: provider.ChunkText, Text: fmt.Sprintf("Distinct answer line %d.\n", i)},
		)
	}
	chunks = append(chunks, provider.Chunk{Type: provider.ChunkDone})
	prov := &mockProvider{name: "mixed-loop", chunks: chunks}
	sink := event.FuncSink(func(event.Event) {})
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession(""),
		Options{ModelRef: "mixed-loop/model", MaxPerseverationRetries: new(1)}, sink)

	st := a.stream(context.Background(), 1, sink)
	if !st.perseverationAborted {
		t.Fatalf("stream did not cut an interleaved reasoning loop: err=%v", st.err)
	}
}

// The retry budget is opt-in and counts the cut attempts. Once it is spent the
// run ends with a perseveration pause, not a clean answer.
func TestPerseverationRetryBudgetIsConfigurable(t *testing.T) {
	cases := []struct {
		name     string
		retries  *int
		wantReqs int
		paused   bool
	}{
		{"undefined detects without retrying", nil, 1, false},
		{"zero reports without retrying", new(0), 1, false},
		{"one retry then stop", new(1), 2, true},
		{"two retries then stop", new(2), 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prov := &mockProvider{name: "loop", chunks: perseverationLoopChunks()}
			sink := &recordSink{}
			a := New(prov, tool.NewRegistry(), sessionstore.NewSession("system"),
				Options{ModelRef: "loop/model", MaxPerseverationRetries: tc.retries}, sink)

			err := a.Run(context.Background(), "write the file")
			if tc.paused {
				if info, ok := InspectRunPause(err); !ok || info.Kind != PauseKindPerseveration {
					t.Fatalf("Run error = %v (pause %+v), want a perseveration pause", err, info)
				}
			} else if err != nil {
				t.Fatalf("Run error = %v, want a clean terminal", err)
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
			if warned != tc.paused {
				t.Fatalf("perseveration_loop notice present = %t, want %t", warned, tc.paused)
			}
		})
	}
}

func TestRunRetriesOnceThenStopsOnRepeatedLoop(t *testing.T) {
	prov := &mockProvider{name: "loop", chunks: perseverationLoopChunks()}
	sink := &recordSink{}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession("system"),
		Options{ModelRef: "loop/model", MaxPerseverationRetries: new(1)}, sink)

	err := a.Run(context.Background(), "write the file")
	if info, ok := InspectRunPause(err); !ok || info.Kind != PauseKindPerseveration {
		t.Fatalf("Run error = %v (pause %+v, %v), want a perseveration pause", err, info, ok)
	}
	// One nudge-and-retry, then the second consecutive loop stops the run.
	if len(prov.requests) != 2 {
		t.Fatalf("provider requests = %d, want 2 (one retry)", len(prov.requests))
	}
	// The retry request must carry the host nudge, so it is not an exact resend.
	found := false
	for _, m := range prov.requests[1].Messages {
		if strings.Contains(m.Content, "retrying (1)") {
			found = true
		}
	}
	if !found {
		t.Fatal("retry request did not carry the host nudge message")
	}
}

// The nudge rides the host-marked steer path and announces itself, so a
// frontend can show it and replay never reads as something the user typed.
func TestPerseverationNudgeIsAHostSteer(t *testing.T) {
	prov := &mockProvider{name: "loop", chunks: perseverationLoopChunks()}
	sink := &recordSink{}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession("system"),
		Options{ModelRef: "loop/model", MaxPerseverationRetries: new(1)}, sink)

	if err := a.Run(context.Background(), "write the file"); err == nil {
		t.Fatal("Run error = nil, want a perseveration pause")
	}
	steers := sink.kinds(event.Steer)
	if len(steers) == 0 {
		t.Fatal("no Steer event was emitted for the retry nudge")
	}
	if !steers[0].HostAuthored || !strings.Contains(steers[0].Text, "retrying (1)") {
		t.Fatalf("Steer event = %+v, want a host-authored retry nudge", steers[0])
	}
	// The user sees the interjection as a notice: a host-authored steer is hidden
	// from both frontends, so the nudge alone would show nothing.
	shown := false
	for _, e := range sink.kinds(event.Notice) {
		if e.Code == event.NoticeCodePerseverationRetry && strings.Contains(e.Text, "retrying (1)") {
			shown = true
		}
	}
	if !shown {
		t.Fatal("no visible interjection notice was emitted for the retry")
	}
}

// The opt-in cut path trims the loop from the stored turn before nudging, so
// the unattended retry resubmits the phrase and its last repetition rather than
// the whole loop.
func TestPerseverationRetryTrimsTheStoredLoop(t *testing.T) {
	block := "Let me write.\n\nHmm.\n\nOK.\n\n"
	prov := &mockProvider{name: "loop", chunks: perseverationLoopChunks()}
	sink := &recordSink{}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession("system"),
		Options{ModelRef: "loop/model", MaxPerseverationRetries: new(1)}, sink)

	if err := a.Run(context.Background(), "write the file"); err == nil {
		t.Fatal("Run error = nil, want a perseveration pause")
	}
	if len(prov.requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(prov.requests))
	}
	var lastAssistant string
	for _, m := range prov.requests[1].Messages {
		if m.Role == provider.RoleAssistant {
			lastAssistant = m.Content
		}
	}
	if want := block + block; lastAssistant != want {
		t.Fatalf("retry carried %d bytes of the loop, want the phrase and its last repetition %q", len(lastAssistant), want)
	}
}

func TestTrimPerseverationTailKeepsTwoPhrases(t *testing.T) {
	block := "Let me write.\n\nHmm.\n\nOK.\n\n"
	s := strings.Repeat(block, 60)
	if got, want := trimPerseverationTail(s), block+block; got != want {
		t.Fatalf("trim = %q (%d bytes), want two phrases %q", got, len(got), want)
	}
}

func TestTrimPerseverationTailKeepsDistinctPrefix(t *testing.T) {
	prefix := "Here is the plan I will follow.\n"
	block := "Let me write.\n\nHmm.\n\nOK.\n\n"
	s := prefix + strings.Repeat(block, 60)
	if got, want := trimPerseverationTail(s), prefix+block+block; got != want {
		t.Fatalf("trim = %q, want %q", got, want)
	}
}

func TestTrimPerseverationTailLeavesOrdinaryTextAlone(t *testing.T) {
	s := nonRepeatingText(4096)
	if got := trimPerseverationTail(s); got != s {
		t.Fatalf("trim changed ordinary text: %d -> %d bytes", len(s), len(got))
	}
}

// Strikes live on the run, so the next user message starts fresh and retries
// again instead of stopping on its first loop.
func TestPerseverationStrikesResetPerRun(t *testing.T) {
	prov := &mockProvider{name: "loop", chunks: perseverationLoopChunks()}
	sink := &recordSink{}
	a := New(prov, tool.NewRegistry(), sessionstore.NewSession("system"),
		Options{ModelRef: "loop/model", MaxPerseverationRetries: new(1)}, sink)

	// First run: one retry, then a perseveration pause.
	if err := a.Run(context.Background(), "write the file"); err == nil {
		t.Fatal("first Run error = nil, want a perseveration pause")
	}
	if a.turn.perseverationStrikes != 1 {
		t.Fatalf("strikes after first run = %d, want 1", a.turn.perseverationStrikes)
	}
	// Second user message: the counter starts over, so it retries again rather
	// than stopping on its first loop.
	if err := a.Run(context.Background(), "try again"); err == nil {
		t.Fatal("second Run error = nil, want a perseveration pause")
	}
	if got := len(prov.requests); got != 4 {
		t.Fatalf("provider requests = %d, want 4 (two runs, one retry each)", got)
	}
}
