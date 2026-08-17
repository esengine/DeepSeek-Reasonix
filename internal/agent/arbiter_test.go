package agent

import (
	"strings"
	"testing"

	"reasonix/internal/event"
)

func arbiterRound() ([]string, []toolOutcome) {
	return []string{"tool output"}, []toolOutcome{{output: "tool output"}}
}

// Every signal used to write results[0] from outcomes[0].output, so a round
// that fired two of them delivered only the last one's guidance.
func TestApplyInterventionsKeepsEveryFiredGuidance(t *testing.T) {
	sink := &ebmSink{}
	a := &Agent{svc: agentServices{sink: sink}}
	results, outcomes := arbiterRound()

	got := a.applyInterventions(results, outcomes,
		intervention{verdict: verdictRedirect, guidance: "[loop guard] change approach",
			notice: noticeFor(event.NoticeCodeLoopGuard, event.LevelInfo, "loop", "loop detail")},
		intervention{verdict: verdictAdvise, guidance: "[evidence nudge] verify first",
			notice: noticeFor(event.NoticeCodeEvidenceNudge, event.LevelInfo, "evidence", "evidence detail")},
	)

	if got != verdictRedirect {
		t.Fatalf("verdict = %d, want the strongest (%d)", got, verdictRedirect)
	}
	for _, want := range []string{"tool output", "[loop guard] change approach", "[evidence nudge] verify first"} {
		if !strings.Contains(results[0], want) {
			t.Fatalf("results[0] = %q, missing %q", results[0], want)
		}
	}
	if len(sink.notices) != 2 {
		t.Fatalf("notices = %d, want one per fired signal", len(sink.notices))
	}
}

func TestApplyInterventionsLeavesQuietRoundsUntouched(t *testing.T) {
	sink := &ebmSink{}
	a := &Agent{svc: agentServices{sink: sink}}
	results, outcomes := arbiterRound()

	if got := a.applyInterventions(results, outcomes, intervention{}, intervention{}); got != verdictContinue {
		t.Fatalf("verdict = %d, want continue", got)
	}
	if results[0] != "tool output" || len(sink.notices) != 0 {
		t.Fatalf("results = %q notices = %d, want an untouched round", results[0], len(sink.notices))
	}
}

// A land must survive being raised beside a lower tier in the same round.
func TestApplyInterventionsTakesTheStrongestVerdict(t *testing.T) {
	a := &Agent{svc: agentServices{sink: &ebmSink{}}}
	results, outcomes := arbiterRound()

	got := a.applyInterventions(results, outcomes,
		intervention{verdict: verdictLand, guidance: "land now"},
		intervention{verdict: verdictAdvise, guidance: "just a nudge"},
	)
	if got != verdictLand {
		t.Fatalf("verdict = %d, want land (%d)", got, verdictLand)
	}
}
