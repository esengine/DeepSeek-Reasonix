package agent

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestRepetitionDetectorTripsOnSentenceLoop(t *testing.T) {
	d := newRepetitionDetector(defaultRepetitionTripLimit)
	line := "Let me create the PR body file and run both gate scripts.\n\n"
	tripped := false
	for i := 0; i < 40 && !tripped; i++ {
		// Odd-sized chunks exercise the carry across segment boundaries.
		tripped = slices.ContainsFunc([]string{line[:7], line[7:23], line[23:]}, d.observe)
		if tripped && i+1 < defaultRepetitionTripLimit {
			t.Fatalf("tripped after %d repeats, want at least %d", i+1, defaultRepetitionTripLimit)
		}
	}
	if !tripped {
		t.Fatal("detector never tripped on 40 identical sentences")
	}
}

func TestRepetitionDetectorTripsOnCycledSentences(t *testing.T) {
	d := newRepetitionDetector(defaultRepetitionTripLimit)
	cycle := []string{
		"Let me write the PR body and run the checks. ",
		"Let me do it now. This is taking too long already. ",
		"I will now execute the command for real. ",
	}
	for i := range 60 {
		if d.observe(cycle[i%len(cycle)]) {
			return
		}
	}
	t.Fatal("detector never tripped on an A/B/C sentence cycle")
}

// Stems taken from a real DeepSeek-V4-Flash looping session: the model
// paraphrases ("Let me check the git config." / "Let me check the git
// configuration now."), so exact-segment counting never trips. Prefix
// bucketing must pool the variants.
func TestRepetitionDetectorTripsOnParaphrasedLoop(t *testing.T) {
	d := newRepetitionDetector(defaultRepetitionTripLimit)
	variants := []string{
		"Let me check the git config. ",
		"Let me check the git configuration now. ",
		"Let me check the git config for the fork remote. ",
		"Let me check the git config once more, for real this time. ",
	}
	for i := range 80 {
		if d.observe(variants[i%len(variants)]) {
			if i+1 < repetitionStemTripFactor*defaultRepetitionTripLimit {
				t.Fatalf("tripped after %d segments, want at least %d", i+1, repetitionStemTripFactor*defaultRepetitionTripLimit)
			}
			return
		}
	}
	t.Fatal("detector never tripped on paraphrased same-stem loop")
}

// Parallel-construction enumerations legitimately share a sentence stem; the
// stem tier must tolerate them up to twice the exact limit.
func TestRepetitionDetectorToleratesParallelEnumeration(t *testing.T) {
	d := newRepetitionDetector(defaultRepetitionTripLimit)
	for i := range defaultRepetitionTripLimit + 3 {
		line := fmt.Sprintf("Returns an error when the input payload is variant %d of the schema. \n", i)
		if d.observe(line) {
			t.Fatalf("tripped on a legitimate %d-item parallel enumeration", i+1)
		}
	}
}

func TestRepetitionDetectorIgnoresVariedProseAndShortRepeats(t *testing.T) {
	d := newRepetitionDetector(defaultRepetitionTripLimit)
	heads := []string{
		"The parser", "Compaction", "Every provider", "Session state", "A retry",
		"Tool receipts", "The controller", "Prefix caching", "Steering", "Evidence",
		"The gate", "Recovery", "Billing", "Telemetry", "The sink", "Streams", "Budgets",
	}
	for i := range 300 {
		line := fmt.Sprintf("Case %03d: %s behaves differently here, as covered above. \n", i, heads[i%len(heads)])
		if d.observe(line) {
			t.Fatalf("tripped on varied prose at line %d", i)
		}
		if d.observe("```\n") || d.observe("}\n") {
			t.Fatalf("tripped on short structural line at %d", i)
		}
	}
}

func TestRepetitionDetectorCJKSentences(t *testing.T) {
	d := newRepetitionDetector(defaultRepetitionTripLimit)
	for range 40 {
		if d.observe("让我现在就创建这个文件并运行两个脚本。") {
			return
		}
	}
	t.Fatal("detector never tripped on repeated CJK sentences without newlines")
}

func TestRepetitionDetectorSeparatorlessStreamStaysBounded(t *testing.T) {
	d := newRepetitionDetector(defaultRepetitionTripLimit)
	filler := strings.Repeat("a", 1024)
	for range 200 {
		d.observe(filler)
		if len(d.carry) > repetitionCarryMaxBytes+len(filler) {
			t.Fatalf("carry grew unbounded: %d bytes", len(d.carry))
		}
	}
}

func TestRepetitionDetectorDisabled(t *testing.T) {
	if d := newRepetitionDetector(0); d != nil {
		t.Fatal("tripLimit 0 should return a nil detector")
	}
	var d *repetitionDetector
	for range 100 {
		if d.observe("identical degenerate sentence, over and over.\n") {
			t.Fatal("nil detector must never trip")
		}
	}
}

// repetitionLoopProvider streams the same sentence until the context is
// cancelled, recording how many stream bodies were requested.
type repetitionLoopProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *repetitionLoopProvider) Name() string { return "repetition-loop" }

func (p *repetitionLoopProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	ch := make(chan provider.Chunk)
	go func() {
		defer close(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case ch <- provider.Chunk{Type: provider.ChunkText, Text: "Let me run the gate scripts now. "}:
			}
		}
	}()
	return ch, nil
}

func (p *repetitionLoopProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestRunawayTextLoopStopsAtRepetitionGuard(t *testing.T) {
	prov := &repetitionLoopProvider{}
	sink := &recordSink{}
	a := New(prov, tool.NewRegistry(), NewSession(""), Options{}, sink)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := a.Run(ctx, "do the thing")
	if !errors.Is(err, errRepetitionLoop) {
		t.Fatalf("Run error = %v, want repetition loop guard", err)
	}
	if got, want := prov.callCount(), maxRepetitionRetries+1; got != want {
		t.Fatalf("provider stream calls = %d, want %d (initial + capped retries)", got, want)
	}
	var discards int
	for _, e := range sink.kinds(event.StreamAttempt) {
		if e.StreamAttempt.Action == event.StreamAttemptDiscard && e.StreamAttempt.Reason == repetitionDiscardReason {
			discards++
		}
	}
	if discards != maxRepetitionRetries {
		t.Fatalf("repetition discard events = %d, want %d", discards, maxRepetitionRetries)
	}
}

// repetitionRecoveryProvider degenerates on the first attempt and answers
// cleanly on the second.
type repetitionRecoveryProvider struct {
	mu    sync.Mutex
	calls int
}

func (p *repetitionRecoveryProvider) Name() string { return "repetition-recovery" }

func (p *repetitionRecoveryProvider) Stream(ctx context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	ch := make(chan provider.Chunk)
	go func() {
		defer close(ch)
		if call == 1 {
			for {
				select {
				case <-ctx.Done():
					return
				case ch <- provider.Chunk{Type: provider.ChunkText, Text: "Let me write the body and run the scripts. "}:
				}
			}
		}
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "All gate checks pass; the PR body is valid."}
		ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{FinishReason: "stop", PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return ch, nil
}

func TestRepetitionGuardRecoversWithCleanRetry(t *testing.T) {
	prov := &repetitionRecoveryProvider{}
	sess := NewSession("")
	a := New(prov, tool.NewRegistry(), sess, Options{}, event.Discard)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Run(ctx, "validate the PR body"); err != nil {
		t.Fatalf("Run after clean retry = %v, want nil", err)
	}
	msgs := sess.Snapshot()
	last := msgs[len(msgs)-1]
	if last.Role != provider.RoleAssistant || last.Content != "All gate checks pass; the PR body is valid." {
		t.Fatalf("last message = %+v, want the clean second attempt", last)
	}
	for _, m := range msgs {
		if strings.Contains(m.Content, "Let me write the body") {
			t.Fatalf("degenerate attempt leaked into the session: %q", m.Content)
		}
	}
}
