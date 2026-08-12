package agent

import (
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
)

type ebmSink struct {
	notices []event.Event
}

func (s *ebmSink) Emit(e event.Event) { s.notices = append(s.notices, e) }

func ebmRound() ([]string, []toolOutcome) {
	return []string{"tool output"}, []toolOutcome{{output: "tool output"}}
}

// deliverEBM runs the signal and its delivery together, the pairing the batch
// guards use, so these tests still assert what reaches the model.
func deliverEBM(a *Agent, sample *evidence.OutcomeSample, results []string, outcomes []toolOutcome) {
	a.applyInterventions(results, outcomes, a.applyEBM(sample, outcomes))
}

func TestApplyEBMStampsEligibilityWithoutFiringByDefault(t *testing.T) {
	a := &Agent{sink: &ebmSink{}}
	sample := evidence.OutcomeSample{DebtAge: 2, BlindMutations: 3}
	results, outcomes := ebmRound()
	deliverEBM(a, &sample, results, outcomes)
	if !sample.EBMEligible || sample.EBMFired {
		t.Fatalf("sample = %+v, want eligible without firing (experiment off)", sample)
	}
	if results[0] != "tool output" {
		t.Fatalf("baseline arm must not touch results, got %q", results[0])
	}
}

func TestApplyEBMFiresOncePerTurnWhenEnabled(t *testing.T) {
	old := ebmEnabled
	ebmEnabled = true
	defer func() { ebmEnabled = old }()

	sink := &ebmSink{}
	a := &Agent{sink: sink}

	below := evidence.OutcomeSample{DebtAge: 1, BlindMutations: 2}
	results, outcomes := ebmRound()
	deliverEBM(a, &below, results, outcomes)
	if below.EBMEligible || below.EBMFired {
		t.Fatalf("below threshold = %+v, want untouched", below)
	}

	sample := evidence.OutcomeSample{DebtAge: 2, BlindMutations: 3}
	deliverEBM(a, &sample, results, outcomes)
	if !sample.EBMFired || !strings.Contains(results[0], "[evidence nudge]") {
		t.Fatalf("sample = %+v results = %q, want fired with nudge appended", sample, results[0])
	}
	if len(sink.notices) != 1 || sink.notices[0].Code != event.NoticeCodeEvidenceNudge {
		t.Fatalf("notices = %+v, want one evidence_nudge", sink.notices)
	}

	again := evidence.OutcomeSample{DebtAge: 3, BlindMutations: 4}
	results2, outcomes2 := ebmRound()
	deliverEBM(a, &again, results2, outcomes2)
	if again.EBMFired || !again.EBMEligible || results2[0] != "tool output" {
		t.Fatalf("second trigger = %+v results = %q, want eligible only (once per turn)", again, results2[0])
	}

	a.resetTurnEvidence()
	fresh := evidence.OutcomeSample{DebtAge: 2, BlindMutations: 3}
	results3, outcomes3 := ebmRound()
	deliverEBM(a, &fresh, results3, outcomes3)
	if !fresh.EBMFired {
		t.Fatalf("new turn = %+v, want the pass reset by resetTurnEvidence", fresh)
	}
}
