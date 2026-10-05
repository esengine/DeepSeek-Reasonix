package main

import (
	"strings"
	"testing"
)

func TestSummarizeTrajectoryJoinsCognitionPerRound(t *testing.T) {
	path := writeTrajectory(t, "cog.trajectory.jsonl", []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		// Round 1: a 9s gap that bought 900 reasoning tokens (slow round).
		`{"seq":2,"ts":9500,"event":{"kind":"usage","usage":{"source":"executor","promptTokens":15000,"reasoningTokens":900,"completionTokens":300}}}`,
		`{"seq":3,"ts":10000,"event":{"kind":"tool_dispatch","tool":{"id":"a","name":"bash"}}}`,
		`{"seq":4,"ts":10400,"event":{"kind":"tool_result","tool":{"id":"a","name":"bash","durationMs":300}}}`,
		// Round 2: a cheap gap into a research delegation; the subagent's own
		// usage must not pollute the executor cognition.
		`{"seq":5,"ts":11500,"event":{"kind":"usage","usage":{"source":"executor","promptTokens":16000,"reasoningTokens":50,"completionTokens":100}}}`,
		`{"seq":6,"ts":12000,"event":{"kind":"tool_dispatch","tool":{"id":"b","name":"research"}}}`,
		`{"seq":7,"ts":16000,"event":{"kind":"usage","usage":{"source":"subagent","promptTokens":90000,"reasoningTokens":9999,"completionTokens":9999}}}`,
		`{"seq":8,"ts":17000,"event":{"kind":"tool_result","tool":{"id":"b","name":"research","durationMs":5000}}}`,
		// Final answer round.
		`{"seq":9,"ts":18000,"event":{"kind":"usage","usage":{"source":"executor","promptTokens":17000,"reasoningTokens":20,"completionTokens":40}}}`,
		`{"seq":10,"ts":19000,"event":{"kind":"turn_done"}}`,
	})
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	if s.ReasoningTokensTotal != 970 || s.CompletionTokensTotal != 440 {
		t.Errorf("executor totals = %d/%d, want 970/440 (subagent excluded)",
			s.ReasoningTokensTotal, s.CompletionTokensTotal)
	}
	if len(s.Rounds) != 3 {
		t.Fatalf("got %d round digests, want 3", len(s.Rounds))
	}
	r1, r2, r3 := s.Rounds[0], s.Rounds[1], s.Rounds[2]
	if r1.GapMs != 9000 || r1.ReasoningTokens != 900 || r1.PromptTokens != 15000 || r1.ToolMs != 300 {
		t.Errorf("round 1 digest = %+v, want gap 9000 / reasoning 900 / prompt 15000 / tool 300", r1)
	}
	if r2.Outcome != "delegation" || r2.ReasoningTokens != 50 || r2.ToolMs != 5000 {
		t.Errorf("round 2 digest = %+v, want delegation / reasoning 50 / tool 5000", r2)
	}
	if len(r2.Actions) != 1 || r2.Actions[0] != "research" {
		t.Errorf("round 2 actions = %v, want [research]", r2.Actions)
	}
	if r3.Outcome != "finalization" || r3.ReasoningTokens != 20 || r3.GapMs != 2000 {
		t.Errorf("round 3 digest = %+v, want finalization / reasoning 20 / gap 2000", r3)
	}
	if s.SlowRounds != 1 || s.SlowRoundGapMs != 9000 || s.SlowRoundReasoningTokens != 900 {
		t.Errorf("slow census = %d/%d/%d, want 1/9000/900",
			s.SlowRounds, s.SlowRoundGapMs, s.SlowRoundReasoningTokens)
	}
}

func TestSlowRoundCensusExcludesRecoveryGaps(t *testing.T) {
	path := writeTrajectory(t, "recovery.trajectory.jsonl", []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		// A 12s gap dominated by a provider retry is recovery, not cognition.
		`{"seq":2,"ts":3000,"event":{"kind":"retrying","retryScope":"stream"}}`,
		`{"seq":3,"ts":13000,"event":{"kind":"tool_dispatch","tool":{"id":"a","name":"bash"}}}`,
		`{"seq":4,"ts":13300,"event":{"kind":"tool_result","tool":{"id":"a","name":"bash","durationMs":200}}}`,
		`{"seq":5,"ts":14000,"event":{"kind":"turn_done"}}`,
	})
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	if s.SlowRounds != 0 {
		t.Fatalf("slow rounds = %d, want 0 (recovery gap is not a cognition purchase)", s.SlowRounds)
	}
	if len(s.Rounds) == 0 || s.Rounds[0].Outcome != "recovery" || s.Rounds[0].GapMs != 12000 {
		t.Fatalf("round digest = %+v, want recovery round with its gap still booked", s.Rounds)
	}
}

func TestRenderCognitionPricesThinking(t *testing.T) {
	results := []result{
		{Passed: true, Trajectory: &trajectorySummary{
			ReasoningTokensTotal: 4000, CompletionTokensTotal: 6000,
			SlowRounds: 1, SlowRoundGapMs: 9000, SlowRoundReasoningTokens: 900,
			ModelGapTotalMs: 30000,
			Rounds: []roundDigest{
				{Index: 1, Outcome: "evidence_gain", GapMs: 2000, ReasoningTokens: 100, CompletionTokens: 300},
				{Index: 2, Outcome: "delegation", GapMs: 1500, ToolMs: 5000, ReasoningTokens: 50, CompletionTokens: 100},
			},
		}},
		{Passed: true, Trajectory: &trajectorySummary{}}, // no rounds: excluded
	}
	got := renderCognition(results)
	for _, want := range []string{
		"**Cognition** (1 recorded runs)",
		"**reasoning** 4,000 tok",
		"**2,000 reasoning/solved**",
		"**slow rounds** (≥8s) 1 = 30% of model time, 900 reasoning tok",
		"**delegation** 1 rounds (5.0s in subagents)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("render missing %q in:\n%s", want, got)
		}
	}
	if renderCognition(nil) != "" {
		t.Error("no runs must render nothing")
	}
}

func TestRenderCognitionSeparatesUnreportedReasoningFromZero(t *testing.T) {
	withRounds := func(streamed bool, reasoning int64) result {
		r := result{task: task{ID: "a"}, Passed: true}
		r.Trajectory = &trajectorySummary{
			ReasoningStreamed:        streamed,
			ReasoningChars:           9000,
			ReasoningTokensTotal:     reasoning,
			CompletionTokensTotal:    500,
			SlowRounds:               2,
			SlowRoundGapMs:           20000,
			SlowRoundReasoningTokens: 0,
			ModelGapTotalMs:          40000,
			Rounds:                   []roundDigest{{Index: 1, Outcome: "mutation", GapMs: 10000}},
		}
		return r
	}
	// The model streamed thinking but the provider folded it into output
	// tokens: the count is missing, not zero.
	got := renderCognition([]result{withRounds(true, 0)})
	if !strings.Contains(got, "this provider reports no token count") {
		t.Errorf("streamed reasoning with no count must say unreported, got: %s", got)
	}
	if !strings.Contains(got, "9,000 chars streamed") {
		t.Errorf("the volume actually received must still be reported, got: %s", got)
	}
	if strings.Contains(got, "0 reasoning tok") {
		t.Errorf("must not assert zero reasoning when the count is missing, got: %s", got)
	}
	// A provider that reports the count keeps the number.
	got = renderCognition([]result{withRounds(true, 1234)})
	if !strings.Contains(got, "1,234 tok") {
		t.Errorf("a reported count must still render, got: %s", got)
	}
	// No thinking streamed at all (effort disabled): zero is the measurement.
	got = renderCognition([]result{withRounds(false, 0)})
	if !strings.Contains(got, "0 tok") {
		t.Errorf("no streamed reasoning means a real zero, got: %s", got)
	}
}
