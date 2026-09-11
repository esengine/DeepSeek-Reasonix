package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummarizeTrajectoryAttributesTimeAndSkipsTruncatedTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.trajectory.jsonl")
	lines := []string{
		`{"schema_version":1,"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		`{"schema_version":1,"seq":2,"ts":1500,"event":{"kind":"tool_dispatch","tool":{"name":"bash"}}}`,
		`{"schema_version":1,"seq":3,"ts":2000,"event":{"kind":"tool_result","tool":{"name":"bash","durationMs":400}}}`,
		`{"schema_version":1,"seq":4,"ts":2500,"event":{"kind":"tool_result","tool":{"name":"grep","durationMs":300,"parentId":"task-1"}}}`,
		`{"schema_version":1,"seq":5,"ts":4000,"event":{"kind":"turn_done"}}`,
		`{"schema_version":1,"seq":6,"ts":4100,"event":{"kind":"tool_res`, // killed mid-write
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	if s.Records != 5 {
		t.Errorf("records = %d, want 5 (truncated tail skipped)", s.Records)
	}
	if s.SpanMs != 3000 {
		t.Errorf("span = %d, want 3000", s.SpanMs)
	}
	if s.ToolMs != 400 {
		t.Errorf("tool ms = %d, want 400 (subagent call must not double-book)", s.ToolMs)
	}
	if s.ModelMs != 2600 {
		t.Errorf("model ms = %d, want 2600", s.ModelMs)
	}
}

func TestSummarizeTrajectoryDecomposesModelRounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rounds.trajectory.jsonl")
	lines := []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		`{"seq":2,"ts":3000,"event":{"kind":"tool_dispatch","tool":{"name":"read_file","partial":true}}}`, // round 1 gap: 2000
		`{"seq":3,"ts":3400,"event":{"kind":"tool_dispatch","tool":{"name":"read_file"}}}`,                // same batch: no new round
		`{"seq":4,"ts":5000,"event":{"kind":"tool_result","tool":{"name":"read_file","durationMs":400}}}`,
		`{"seq":5,"ts":5100,"event":{"kind":"tool_dispatch","tool":{"name":"grep","parentId":"task-1"}}}`, // subagent: ignored
		`{"seq":6,"ts":5200,"event":{"kind":"tool_result","tool":{"name":"grep","durationMs":50,"parentId":"task-1"}}}`,
		`{"seq":7,"ts":6000,"event":{"kind":"retrying"}}`,
		`{"seq":8,"ts":9000,"event":{"kind":"tool_dispatch","tool":{"name":"bash"}}}`, // round 2 gap: 9000-5000=4000
		`{"seq":9,"ts":9500,"event":{"kind":"tool_result","tool":{"name":"bash","durationMs":450}}}`,
		`{"seq":10,"ts":12000,"event":{"kind":"turn_done"}}`, // final answer round gap: 2500
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	if s.ModelRounds != 3 {
		t.Errorf("model rounds = %d, want 3 (two tool rounds + final answer)", s.ModelRounds)
	}
	if s.ModelGapTotalMs != 8500 {
		t.Errorf("model gap total = %d, want 8500", s.ModelGapTotalMs)
	}
	if s.ModelGapP95Ms != 4000 {
		t.Errorf("model gap p95 = %d, want 4000", s.ModelGapP95Ms)
	}
	if s.Retries != 1 {
		t.Errorf("retries = %d, want 1", s.Retries)
	}
	if s.ToolMs != 850 {
		t.Errorf("tool ms = %d, want 850 (subagent excluded)", s.ToolMs)
	}
}

func TestSummarizeTrajectoryDecomposesBatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "batches.trajectory.jsonl")
	lines := []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		// Batch 1: three reads dispatched together, executed overlapping.
		`{"seq":2,"ts":2000,"event":{"kind":"tool_dispatch","tool":{"id":"a","name":"read_file","readOnly":true}}}`,
		`{"seq":3,"ts":2010,"event":{"kind":"tool_dispatch","tool":{"id":"b","name":"read_file","readOnly":true}}}`,
		`{"seq":4,"ts":2020,"event":{"kind":"tool_dispatch","tool":{"id":"c","name":"read_file","readOnly":true}}}`,
		`{"seq":5,"ts":2500,"event":{"kind":"tool_result","tool":{"id":"a","name":"read_file","readOnly":true,"durationMs":400,"startedAt":2050,"endedAt":2450}}}`,
		`{"seq":6,"ts":2500,"event":{"kind":"tool_result","tool":{"id":"b","name":"read_file","readOnly":true,"durationMs":300,"startedAt":2060,"endedAt":2360}}}`,
		`{"seq":7,"ts":2510,"event":{"kind":"tool_result","tool":{"id":"c","name":"read_file","readOnly":true,"durationMs":200,"startedAt":2070,"endedAt":2270}}}`,
		// Batches 2+3: the serialized single-read anti-pattern, streak of two.
		`{"seq":8,"ts":4000,"event":{"kind":"tool_dispatch","tool":{"id":"d","name":"read_file","readOnly":true}}}`,
		`{"seq":9,"ts":4300,"event":{"kind":"tool_result","tool":{"id":"d","name":"read_file","readOnly":true,"durationMs":280,"startedAt":4010,"endedAt":4290}}}`,
		`{"seq":10,"ts":5000,"event":{"kind":"tool_dispatch","tool":{"id":"e","name":"grep","readOnly":true}}}`,
		`{"seq":11,"ts":5200,"event":{"kind":"tool_result","tool":{"id":"e","name":"grep","readOnly":true,"durationMs":180,"startedAt":5010,"endedAt":5190}}}`,
		`{"seq":12,"ts":6000,"event":{"kind":"turn_done"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	if s.ToolBatches != 3 || s.TopLevelCalls != 5 || s.MaxBatchSize != 3 {
		t.Errorf("batches=%d calls=%d max=%d, want 3/5/3", s.ToolBatches, s.TopLevelCalls, s.MaxBatchSize)
	}
	if s.ParallelBatches != 1 || s.ParallelSavedMs != 500 {
		t.Errorf("parallel=%d saved=%d, want 1/500 (900 serial vs 400 wall)", s.ParallelBatches, s.ParallelSavedMs)
	}
	if s.SingleReadRounds != 2 || s.SingleReadStreak != 2 {
		t.Errorf("singleReads=%d streak=%d, want 2/2", s.SingleReadRounds, s.SingleReadStreak)
	}
	if s.ToolWallMs != 860 {
		t.Errorf("tool wall = %d, want 860 (400 union + 280 + 180)", s.ToolWallMs)
	}
	if s.ToolMs != 1360 {
		t.Errorf("tool ms = %d, want 1360 (duration sum)", s.ToolMs)
	}
	if s.ModelMs != 4140 {
		t.Errorf("model ms = %d, want 4140 (span 5000 − wall 860)", s.ModelMs)
	}
	if s.ModelRounds != 4 {
		t.Errorf("model rounds = %d, want 4 (three tool rounds + final answer)", s.ModelRounds)
	}
	if s.StartDelayP95Ms != 50 {
		t.Errorf("start delay p95 = %d, want 50", s.StartDelayP95Ms)
	}
}

func TestSummarizeTrajectoryAnchorsStartDelayToFullDispatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.trajectory.jsonl")
	lines := []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		// Streamed partial announcement ~900ms before the full dispatch; the
		// stream tail must not be booked as pre-exec queueing.
		`{"seq":2,"ts":2000,"event":{"kind":"tool_dispatch","tool":{"id":"a","name":"bash","partial":true}}}`,
		`{"seq":3,"ts":2900,"event":{"kind":"tool_dispatch","tool":{"id":"a","name":"bash"}}}`,
		`{"seq":4,"ts":3000,"event":{"kind":"tool_result","tool":{"id":"a","name":"bash","durationMs":90,"startedAt":2910,"endedAt":3000}}}`,
		`{"seq":5,"ts":4000,"event":{"kind":"turn_done"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	if s.TopLevelCalls != 1 || s.ToolBatches != 1 {
		t.Errorf("calls=%d batches=%d, want 1/1 (partial+full is one call)", s.TopLevelCalls, s.ToolBatches)
	}
	if s.StartDelayP95Ms != 10 {
		t.Errorf("start delay p95 = %d, want 10 (2910 − full dispatch 2900)", s.StartDelayP95Ms)
	}
}

func TestSummarizeTrajectorySplitsCleanAndRecoveryRounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery.trajectory.jsonl")
	lines := []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		// Round 1 (clean, gap 1000).
		`{"seq":2,"ts":2000,"event":{"kind":"tool_dispatch","tool":{"id":"a","name":"bash"}}}`,
		`{"seq":3,"ts":2100,"event":{"kind":"tool_result","tool":{"id":"a","name":"bash","durationMs":100,"startedAt":2000,"endedAt":2100}}}`,
		// Round 2 (gap 8900): stream-interrupted retry mid-gap.
		`{"seq":4,"ts":4000,"event":{"kind":"retrying","retryAttempt":1,"retryScope":"stream"}}`,
		`{"seq":5,"ts":11000,"event":{"kind":"tool_dispatch","tool":{"id":"b","name":"bash"}}}`,
		`{"seq":6,"ts":11100,"event":{"kind":"tool_result","tool":{"id":"b","name":"bash","durationMs":100,"startedAt":11000,"endedAt":11100}}}`,
		// Round 3 (gap 2900): missing-reasoning exact replay.
		`{"seq":7,"ts":12000,"protocol_recovery":"missing_reasoning_retry_attempted"}`,
		`{"seq":8,"ts":14000,"event":{"kind":"tool_dispatch","tool":{"id":"c","name":"bash"}}}`,
		`{"seq":9,"ts":14100,"event":{"kind":"tool_result","tool":{"id":"c","name":"bash","durationMs":100,"startedAt":14000,"endedAt":14100}}}`,
		// Final answer round (gap 5900): empty-final retry.
		`{"seq":10,"ts":16000,"event":{"kind":"notice","code":"empty_final"}}`,
		`{"seq":11,"ts":20000,"event":{"kind":"turn_done"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	if s.StreamRetries != 1 || s.HeaderRetries != 0 || s.Retries != 1 {
		t.Errorf("stream=%d header=%d retries=%d, want 1/0/1", s.StreamRetries, s.HeaderRetries, s.Retries)
	}
	if s.ReasoningReplays != 1 || s.EmptyFinalRetries != 1 {
		t.Errorf("replays=%d emptyFinal=%d, want 1/1", s.ReasoningReplays, s.EmptyFinalRetries)
	}
	if s.ModelRounds != 4 || s.RecoveryRounds != 3 {
		t.Errorf("rounds=%d recovery=%d, want 4/3", s.ModelRounds, s.RecoveryRounds)
	}
	if s.RecoveryGapMs != 8900+2900+5900 {
		t.Errorf("recovery gap = %d, want 17700", s.RecoveryGapMs)
	}
	if s.CleanGapP95Ms != 1000 {
		t.Errorf("clean gap p95 = %d, want 1000 (only round 1 is clean)", s.CleanGapP95Ms)
	}
	if s.ModelGapP95Ms != 8900 {
		t.Errorf("recovery-inclusive gap p95 = %d, want 8900", s.ModelGapP95Ms)
	}
}

func TestClipIntervals(t *testing.T) {
	got := clipIntervals([][2]int64{{0, 10}, {20, 30}}, [][2]int64{{5, 25}})
	want := [][2]int64{{0, 5}, {25, 30}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("clip = %v, want %v", got, want)
	}
	if rest := clipIntervals([][2]int64{{5, 8}}, [][2]int64{{0, 10}}); len(rest) != 0 {
		t.Fatalf("fully covered base must clip to empty, got %v", rest)
	}
}

func TestSummarizeTrajectoryDecomposesWallClock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wall.trajectory.jsonl")
	lines := []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		// Planner call: 3000ms, tagged by its usage source.
		`{"seq":2,"ts":1000,"event":{"kind":"stream_attempt","streamAttempt":{"id":"p1","action":"begin"}}}`,
		`{"seq":3,"ts":4000,"event":{"kind":"stream_attempt","streamAttempt":{"id":"p1","action":"commit"}}}`,
		`{"seq":4,"ts":4010,"event":{"kind":"usage","usage":{"source":"planner"}}}`,
		// Executor call: 1900ms, then one 100ms tool.
		`{"seq":5,"ts":4100,"event":{"kind":"stream_attempt","streamAttempt":{"id":"e1","action":"begin"}}}`,
		`{"seq":6,"ts":6000,"event":{"kind":"stream_attempt","streamAttempt":{"id":"e1","action":"commit"}}}`,
		`{"seq":7,"ts":6010,"event":{"kind":"usage","usage":{"source":"executor"}}}`,
		`{"seq":8,"ts":6100,"event":{"kind":"tool_dispatch","tool":{"id":"a","name":"bash"}}}`,
		`{"seq":9,"ts":6200,"event":{"kind":"tool_result","tool":{"id":"a","name":"bash","durationMs":100,"startedAt":6100,"endedAt":6200}}}`,
		// Retry backoff 2000ms, then a 500ms executor attempt.
		`{"seq":10,"ts":7000,"event":{"kind":"retrying","retryScope":"stream"}}`,
		`{"seq":11,"ts":9000,"event":{"kind":"stream_attempt","streamAttempt":{"id":"e2","action":"begin"}}}`,
		`{"seq":12,"ts":9500,"event":{"kind":"stream_attempt","streamAttempt":{"id":"e2","action":"commit"}}}`,
		`{"seq":13,"ts":9510,"event":{"kind":"usage","usage":{"source":"executor"}}}`,
		// Compaction 1000ms; its inner summarize attempt must book as compaction.
		`{"seq":14,"ts":10000,"event":{"kind":"compaction_started"}}`,
		`{"seq":15,"ts":10100,"event":{"kind":"stream_attempt","streamAttempt":{"id":"c1","action":"begin"}}}`,
		`{"seq":16,"ts":10800,"event":{"kind":"stream_attempt","streamAttempt":{"id":"c1","action":"commit"}}}`,
		`{"seq":17,"ts":10810,"event":{"kind":"usage","usage":{"source":"executor"}}}`,
		`{"seq":18,"ts":11000,"event":{"kind":"compaction_done"}}`,
		`{"seq":19,"ts":12000,"event":{"kind":"turn_done"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	if s.RetryWaitMs != 2000 {
		t.Errorf("retry wait = %d, want 2000", s.RetryWaitMs)
	}
	if s.CompactionMs != 1000 {
		t.Errorf("compaction = %d, want 1000", s.CompactionMs)
	}
	if s.PlannerStreamMs != 3000 {
		t.Errorf("planner stream = %d, want 3000", s.PlannerStreamMs)
	}
	if s.ModelStreamMs != 2400 {
		t.Errorf("model stream = %d, want 2400 (1900+500; compaction-inner attempt clipped)", s.ModelStreamMs)
	}
	if s.AgentOtherMs != 2500 {
		t.Errorf("agent other = %d, want 2500 (span 11000 − 100 − 2000 − 1000 − 3000 − 2400)", s.AgentOtherMs)
	}
	if s.Compactions != 1 {
		t.Errorf("compactions = %d, want 1", s.Compactions)
	}
}

func TestSummarizeTrajectoryCollectsPhaseTraceInputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "phase.trajectory.jsonl")
	lines := []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		`{"seq":2,"ts":1870,"event":{"kind":"reasoning"}}`,
		`{"seq":3,"ts":2000,"event":{"kind":"usage","usage":{"source":"planner"}}}`,
		`{"seq":4,"ts":2400,"event":{"kind":"reasoning"}}`,
		`{"seq":5,"ts":3000,"event":{"kind":"tool_dispatch","tool":{"id":"a","name":"bash"}}}`,
		`{"seq":6,"ts":3300,"event":{"kind":"tool_result","tool":{"id":"a","name":"bash","durationMs":90,"startedAt":3210,"endedAt":3300}}}`,
		`{"seq":7,"ts":3400,"event":{"kind":"usage","usage":{"source":"executor"}}}`,
		`{"seq":8,"ts":3500,"event":{"kind":"usage","usage":{"source":"subagent"}}}`,
		`{"seq":9,"ts":3600,"event":{"kind":"notice","code":"progress_guard"}}`,
		`{"seq":10,"ts":4000,"event":{"kind":"turn_done"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	if s.TTFTMs != 870 {
		t.Errorf("ttft = %d, want 870 (first reasoning delta)", s.TTFTMs)
	}
	if s.FirstToolMs != 2210 {
		t.Errorf("first tool = %d, want 2210 (startedAt 3210 − span start)", s.FirstToolMs)
	}
	if s.PlannerRequests != 1 || s.ExecutorRequests != 1 || s.SubagentRequests != 1 {
		t.Errorf("requests planner=%d executor=%d subagent=%d, want 1/1/1",
			s.PlannerRequests, s.ExecutorRequests, s.SubagentRequests)
	}
	if s.ToolQueueMs != 210 {
		t.Errorf("tool queue = %d, want 210 (start 3210 − dispatch 3000)", s.ToolQueueMs)
	}
	if s.NoProgressSignals != 1 {
		t.Errorf("no-progress signals = %d, want 1", s.NoProgressSignals)
	}
}

func TestBuildPhaseTrace(t *testing.T) {
	r := result{task: task{ID: "a"}, Passed: true, WallMs: 92341}
	r.PromptTokens = 84320
	r.CompletionTokens = 12013
	r.Trajectory = &trajectorySummary{
		TTFTMs: 870, FirstToolMs: 4210,
		PlannerRequests: 1, PlannerStreamMs: 11020,
		ExecutorRequests: 7, ModelStreamMs: 56100,
		TopLevelCalls: 14, ToolQueueMs: 430, ToolWallMs: 8120,
		StreamRetries: 1, RecoveryGapMs: 9530,
		CompactionMs: 800, NoProgressSignals: 3,
	}
	p := buildPhaseTrace(r)
	if p == nil {
		t.Fatal("trace must be built when a trajectory exists")
	}
	if p.TotalMs != 92341 || p.TTFTMs != 870 || p.TimeToFirstTool != 4210 {
		t.Errorf("totals = %d/%d/%d, want 92341/870/4210", p.TotalMs, p.TTFTMs, p.TimeToFirstTool)
	}
	if p.Planner != (phaseModel{Requests: 1, Ms: 11020}) || p.Executor != (phaseModel{Requests: 7, Ms: 56100}) {
		t.Errorf("planner=%+v executor=%+v", p.Planner, p.Executor)
	}
	if p.Tool != (phaseTool{Calls: 14, QueueMs: 430, CriticalPathMs: 8120}) {
		t.Errorf("tool = %+v", p.Tool)
	}
	if p.Recovery != (phaseModel{Requests: 1, Ms: 9530}) {
		t.Errorf("recovery = %+v", p.Recovery)
	}
	if p.CompactionMs != 800 || p.NoProgressSignals != 3 || !p.Solved {
		t.Errorf("compaction=%d signals=%d solved=%v", p.CompactionMs, p.NoProgressSignals, p.Solved)
	}
	if p.PromptTokens != 84320 || p.CompletionTokens != 12013 {
		t.Errorf("tokens = %d/%d", p.PromptTokens, p.CompletionTokens)
	}
	if buildPhaseTrace(result{}) != nil {
		t.Fatal("no trajectory must mean no trace")
	}
}

func TestSummarizeTrajectoryClassifiesRoundOutcomes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "outcomes.trajectory.jsonl")
	lines := []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		// Round 1: planner usage in the gap outranks the batch's mutation.
		`{"seq":2,"ts":1500,"event":{"kind":"usage","usage":{"source":"planner"}}}`,
		`{"seq":3,"ts":2000,"event":{"kind":"tool_dispatch","tool":{"id":"a","name":"write_file","args":"{\"p\":1}"}}}`,
		`{"seq":4,"ts":2100,"event":{"kind":"tool_result","tool":{"id":"a","name":"write_file","durationMs":100,"startedAt":2000,"endedAt":2100}}}`,
		// Round 2: successful write → mutation.
		`{"seq":5,"ts":3000,"event":{"kind":"tool_dispatch","tool":{"id":"b","name":"write_file","args":"{\"p\":2}"}}}`,
		`{"seq":6,"ts":3100,"event":{"kind":"tool_result","tool":{"id":"b","name":"write_file","durationMs":100,"startedAt":3000,"endedAt":3100}}}`,
		// Round 3: first read of this target → evidence_gain.
		`{"seq":7,"ts":4000,"event":{"kind":"tool_dispatch","tool":{"id":"c","name":"read_file","args":"{\"f\":\"x\"}","readOnly":true}}}`,
		`{"seq":8,"ts":4100,"event":{"kind":"tool_result","tool":{"id":"c","name":"read_file","readOnly":true,"durationMs":100,"startedAt":4000,"endedAt":4100}}}`,
		// Round 4: the exact same read again → duplicate_work.
		`{"seq":9,"ts":5000,"event":{"kind":"tool_dispatch","tool":{"id":"d","name":"read_file","args":"{\"f\":\"x\"}","readOnly":true}}}`,
		`{"seq":10,"ts":5100,"event":{"kind":"tool_result","tool":{"id":"d","name":"read_file","readOnly":true,"durationMs":100,"startedAt":5000,"endedAt":5100}}}`,
		// Round 5: verification command → verification.
		`{"seq":11,"ts":6000,"event":{"kind":"tool_dispatch","tool":{"id":"e","name":"bash","args":"{\"cmd\":\"go test\"}","readOnly":true}}}`,
		`{"seq":12,"ts":6200,"event":{"kind":"tool_result","tool":{"id":"e","name":"bash","readOnly":true,"durationMs":200,"startedAt":6000,"endedAt":6200,"execution":{"verification":"passed"}}}}`,
		// Round 6: ledger-only batch → bookkeeping.
		`{"seq":13,"ts":7000,"event":{"kind":"tool_dispatch","tool":{"id":"f","name":"complete_step","args":"{\"s\":1}"}}}`,
		`{"seq":14,"ts":7100,"event":{"kind":"tool_result","tool":{"id":"f","name":"complete_step","durationMs":100,"startedAt":7000,"endedAt":7100}}}`,
		`{"seq":15,"ts":8000,"event":{"kind":"turn_done"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	want := map[string]int{
		"planning": 1, "mutation": 1, "evidence_gain": 1, "duplicate_work": 1,
		"verification": 1, "bookkeeping": 1, "finalization": 1,
	}
	for outcome, n := range want {
		if s.RoundOutcomes[outcome] != n {
			t.Errorf("outcome %s = %d, want %d (all: %v)", outcome, s.RoundOutcomes[outcome], n, s.RoundOutcomes)
		}
	}
	if s.UsefulRounds != 4 {
		t.Errorf("useful rounds = %d, want 4 (mutation, evidence, verification, finalization)", s.UsefulRounds)
	}
	if s.WastedGapMs != 1000+900+800 {
		t.Errorf("wasted gap = %d, want 2700 (planning 1000 + duplicate 900 + bookkeeping 800)", s.WastedGapMs)
	}
	if s.RoundOutcomeMs["planning"] != 1000 {
		t.Errorf("planning ms = %d, want 1000", s.RoundOutcomeMs["planning"])
	}
}

func TestRenderTimeAttributionIncludesRoundEfficiency(t *testing.T) {
	r := result{task: task{ID: "a"}, Passed: true}
	r.Trajectory = &trajectorySummary{
		Records: 5, SpanMs: 8000, ToolMs: 700, ModelMs: 7300, ModelRounds: 7,
		UsefulRounds: 4, WastedGapMs: 2700,
		RoundOutcomes: map[string]int{
			"planning": 1, "mutation": 1, "evidence_gain": 1, "duplicate_work": 1,
			"verification": 1, "bookkeeping": 1, "finalization": 1,
		},
		RoundOutcomeMs: map[string]int64{
			"planning": 1000, "mutation": 900, "evidence_gain": 900, "duplicate_work": 900,
			"verification": 900, "bookkeeping": 800, "finalization": 900,
		},
	}
	got := renderTimeAttribution([]result{r})
	for _, want := range []string{
		"**Round efficiency**: **useful rounds** 4/7 (57%)",
		"**wasted model time** 2.7s (**2.7s/solved**)",
		"**waste breakdown**: planning ×1 (1.0s) · duplicate_work ×1 (0.9s) · bookkeeping ×1 (0.8s)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("round efficiency missing %q:\n%s", want, got)
		}
	}
}

func TestRunTrajModeRedigestsRecordedFiles(t *testing.T) {
	dir := t.TempDir()
	lines := []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		`{"seq":2,"ts":1000,"event":{"kind":"stream_attempt","streamAttempt":{"id":"e1","action":"begin"}}}`,
		`{"seq":3,"ts":2000,"event":{"kind":"stream_attempt","streamAttempt":{"id":"e1","action":"commit"}}}`,
		`{"seq":4,"ts":2010,"event":{"kind":"usage","usage":{"source":"executor"}}}`,
		`{"seq":5,"ts":3000,"event":{"kind":"turn_done"}}`,
	}
	if err := os.WriteFile(filepath.Join(dir, "t1.trajectory.jsonl"), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	got, err := runTrajMode(dir)
	if err != nil {
		t.Fatalf("runTrajMode: %v", err)
	}
	for _, want := range []string{"Trajectory digest", "Wall decomposition", "### `t1`", `"model_stream_ms": 1000`} {
		if !strings.Contains(got, want) {
			t.Fatalf("traj mode output missing %q:\n%s", want, got)
		}
	}
	if _, err := runTrajMode(filepath.Join(dir, "empty")); err == nil {
		t.Fatalf("missing dir must error")
	}
}

func TestRenderTimeAttributionIncludesBatchingLine(t *testing.T) {
	r := result{task: task{ID: "a"}, Passed: true}
	r.Trajectory = &trajectorySummary{
		Records: 12, SpanMs: 5000, ToolMs: 1360, ToolWallMs: 860, ModelMs: 4140,
		ModelRounds: 4, ModelGapTotalMs: 3990,
		ToolBatches: 3, TopLevelCalls: 5, MaxBatchSize: 3,
		ParallelBatches: 1, ParallelSavedMs: 500,
		SingleReadRounds: 2, SingleReadStreak: 2, StartDelayP95Ms: 50,
	}
	got := renderTimeAttribution([]result{r})
	for _, want := range []string{
		"**Batching** (3 tool rounds)",
		"**calls/round** 1.7",
		"**single-read rounds** 2 (67%)",
		"**parallel rounds** 1 (saved 0.5s)",
		"**start-delay p95** 50ms",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("batching line missing %q:\n%s", want, got)
		}
	}
}

func TestFilterTasks(t *testing.T) {
	tasks := []task{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	got, err := filterTasks(tasks, " b, a ")
	if err != nil {
		t.Fatalf("filterTasks: %v", err)
	}
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "a" {
		t.Fatalf("filtered = %v, want [b a] in request order", got)
	}
	if all, err := filterTasks(tasks, ""); err != nil || len(all) != 3 {
		t.Fatalf("empty filter must keep the suite: %v %v", all, err)
	}
	if _, err := filterTasks(tasks, "typo"); err == nil || !strings.Contains(err.Error(), "available: a, b, c") {
		t.Fatalf("unknown id must fail loudly with the available set, got %v", err)
	}
}

func TestRenderBodyReportsTTCSKPIs(t *testing.T) {
	a := result{task: task{ID: "a"}, Passed: true, WallMs: 60_000, Attempt: 1, TTCSMs: 60_000}
	b1 := result{task: task{ID: "b"}, WallMs: 40_000, Attempt: 1}
	b2 := result{task: task{ID: "b"}, Passed: true, WallMs: 50_000, Attempt: 2, TTCSMs: 90_000}
	c1 := result{task: task{ID: "c"}, WallMs: 30_000, Attempt: 1}
	c2 := result{task: task{ID: "c"}, WallMs: 20_000, Attempt: 2}

	got := renderBody([]result{a, b1, b2, c1, c2})
	for _, want := range []string{
		"**Solved:** 2/3 (67%)", // attempts must not inflate the task denominator
		"**Pass@1** 33%",
		"**Pass@≤2** 67%",
		"**TTCS median** 1m30s", // b's solve charges its failed first attempt
		"**TTCS p90** 1m30s",
		"**Solved/hour** 36.0", // 2 solves over 200s of total attempt wall
		"`b` (try 2)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("KPI line missing %q:\n%s", want, got)
		}
	}

	single := renderBody([]result{a})
	if strings.Contains(single, "Pass@≤") {
		t.Fatalf("single-attempt suite must not report Pass@≤k:\n%s", single)
	}
	if !strings.Contains(single, "**Pass@1** 100%") {
		t.Fatalf("single-attempt suite missing Pass@1:\n%s", single)
	}
}

func TestRenderBodyReportsPerSolvedEfficiency(t *testing.T) {
	solved := result{task: task{ID: "a"}, Passed: true, WallMs: 60_000}
	solved.Steps = 6
	solved.ToolCalls = 9
	solved.Trajectory = &trajectorySummary{ModelRounds: 5}
	failed := result{task: task{ID: "b"}, WallMs: 40_000}
	failed.Steps = 8
	failed.ToolCalls = 3
	failed.Trajectory = &trajectorySummary{ModelRounds: 7}

	got := renderBody([]result{solved, failed})
	for _, want := range []string{
		"**Per solved task:**",
		"**model requests** 14.0", // failures' spend charged to the solve
		"tool calls 12.0",
		"wall 1m40s",
		"model rounds 12.0",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("per-solved line missing %q:\n%s", want, got)
		}
	}

	if got := renderBody([]result{failed}); strings.Contains(got, "Per solved task") {
		t.Fatalf("no solves must mean no per-solved line:\n%s", got)
	}
}

func TestRenderBodyIncludesTimeAttributionOnlyForRecordedRuns(t *testing.T) {
	plain := result{task: task{ID: "a"}, Passed: true}
	if got := renderBody([]result{plain}); strings.Contains(got, "Time attribution") {
		t.Fatalf("unrecorded run must not report time attribution:\n%s", got)
	}

	recorded := plain
	recorded.Trajectory = &trajectorySummary{Records: 5, SpanMs: 3000, ToolMs: 1000, ModelMs: 2000}
	got := renderBody([]result{recorded})
	if !strings.Contains(got, "Time attribution") || !strings.Contains(got, "(1 recorded runs)") {
		t.Fatalf("recorded run missing time attribution:\n%s", got)
	}
}
