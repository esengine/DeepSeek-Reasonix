package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
	_ "reasonix/internal/tool/builtin"
)

func bashProgressReceipt(t *testing.T, command string, success bool) evidence.Receipt {
	t.Helper()
	args, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return evidence.ReceiptFromToolCall("bash", args, success, false)
}

// runRound executes one read-only batch and returns its result texts.
func runRound(t *testing.T, a *Agent, path string) []string {
	t.Helper()
	batch := a.executeBatch(context.Background(), []provider.ToolCall{
		{ID: "c", Name: "read_probe", Arguments: `{"path":"` + path + `"}`},
	})
	return batch.results
}

func TestProgressGuardEscalatesOnZeroGainRounds(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_probe", readOnly: true})
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	a.progress.reset()

	if got := runRound(t, a, "same.go"); strings.Contains(got[0], "[progress guard]") {
		t.Fatalf("round 1 (new read, +1 gain) must not trip the guard: %q", got[0])
	}
	// Every later round re-reads the same path: zero gain, streak +1 per round.
	if got := runRound(t, a, "same.go"); strings.Contains(got[0], "[progress guard]") {
		t.Fatalf("one zero-gain round must not trip the guard yet: %q", got[0])
	}
	got := runRound(t, a, "same.go")
	if !strings.Contains(got[0], "[progress guard]") || !strings.Contains(got[0], "Narrow the investigation") {
		t.Fatalf("streak %d must nudge: %q", progressNudgeStreak, got[0])
	}
	runRound(t, a, "same.go")
	got = runRound(t, a, "same.go")
	if !strings.Contains(got[0], "Change strategy now") {
		t.Fatalf("streak %d must force a pivot: %q", progressPivotStreak, got[0])
	}
	runRound(t, a, "same.go")
	got = runRound(t, a, "same.go")
	if !strings.Contains(got[0], "produce your final answer now") {
		t.Fatalf("streak %d must demand the final answer: %q", progressStopStreak, got[0])
	}
	if !a.loopGuardArmed {
		t.Fatal("stop tier must arm the loop-guard pass so readiness stands down")
	}
	// Past the stop tier the loop-guard pass carries the pressure; repeating
	// the injected text every round would only inflate prompts.
	got = runRound(t, a, "same.go")
	if strings.Contains(got[0], "[progress guard]") {
		t.Fatalf("thresholds fire once, not every round: %q", got[0])
	}
	if !a.loopGuardArmed {
		t.Fatal("loop-guard pass must remain armed past the stop tier")
	}
}

type outcomeSampleSink struct {
	samples []evidence.OutcomeSample
}

func (s *outcomeSampleSink) Emit(event.Event) {}
func (s *outcomeSampleSink) RecordOutcomeProgress(sample evidence.OutcomeSample) {
	s.samples = append(s.samples, sample)
}

func TestOutcomeShadowRecordsEveryRoundWithoutTouchingGuards(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_probe", readOnly: true})
	sink := &outcomeSampleSink{}
	a := New(nil, reg, NewSession(""), Options{}, sink)
	a.resetTurnEvidence()

	first := runRound(t, a, "a.go")
	second := runRound(t, a, "a.go")
	if len(sink.samples) != 2 {
		t.Fatalf("got %d shadow samples, want one per round", len(sink.samples))
	}
	if s := sink.samples[0]; s.Round != 1 || s.Exploration != 1 || s.Objective != 0 {
		t.Fatalf("round 1 sample = %+v, want exploration 1 objective 0", s)
	}
	if s := sink.samples[1]; s.Round != 2 || s.Exploration != 0 || s.LegacyGain != 0 {
		t.Fatalf("round 2 repeat sample = %+v, want all-zero with legacy gain 0", s)
	}
	// The shadow observes; the guard alone decides. Round texts stay untouched
	// below the nudge threshold exactly as before.
	if strings.Contains(first[0], "[progress guard]") || strings.Contains(second[0], "[progress guard]") {
		t.Fatalf("shadow must not change guard behavior: %q / %q", first[0], second[0])
	}
}

func TestProgressGuardResetsOnNewEvidence(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_probe", readOnly: true})
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	a.progress.reset()

	runRound(t, a, "a.go")
	runRound(t, a, "a.go")
	runRound(t, a, "a.go")
	if a.progress.streak < progressNudgeStreak {
		t.Fatalf("streak = %d, want >= %d before fresh evidence", a.progress.streak, progressNudgeStreak)
	}
	// A successful bash command receipt is fresh evidence: streak resets.
	a.evidence.Record(bashProgressReceipt(t, "go test ./pkg", true))
	mark := a.evidence.Len() - 1
	a.progress.observe(a.evidence.ReceiptsSince(mark))
	if a.progress.streak != 0 {
		t.Fatalf("fresh evidence must reset the streak, got %d", a.progress.streak)
	}
}
