package main

import (
	"fmt"
)

// renderTimeAttribution aggregates recorded runs into one report line; empty
// when no run in the suite carried a trajectory.
func renderTimeAttribution(results []result) string {
	var toolMs, modelMs, gapMs, savedMs, delayP95, serialWaitP95, recoveryGapMs, cleanP95 int64
	var retryWaitMs, compactionMs, plannerMs, modelStreamMs, agentOtherMs, startupMs int64
	runs, rounds, batches, calls, singleReads, parallelBatches := 0, 0, 0, 0, 0, 0
	recoveryRounds, streamRetries, headerRetries, replays, emptyFinals := 0, 0, 0, 0, 0
	for _, r := range results {
		if r.Trajectory != nil {
			runs++
			toolMs += r.Trajectory.toolWall()
			modelMs += r.Trajectory.ModelMs
			rounds += r.Trajectory.ModelRounds
			gapMs += r.Trajectory.ModelGapTotalMs
			batches += r.Trajectory.ToolBatches
			calls += r.Trajectory.TopLevelCalls
			singleReads += r.Trajectory.SingleReadRounds
			parallelBatches += r.Trajectory.ParallelBatches
			savedMs += r.Trajectory.ParallelSavedMs
			delayP95 = max(delayP95, r.Trajectory.StartDelayP95Ms)
			serialWaitP95 = max(serialWaitP95, r.Trajectory.SerialWaitP95Ms)
			recoveryRounds += r.Trajectory.RecoveryRounds
			recoveryGapMs += r.Trajectory.RecoveryGapMs
			cleanP95 = max(cleanP95, r.Trajectory.CleanGapP95Ms)
			streamRetries += r.Trajectory.StreamRetries
			headerRetries += r.Trajectory.HeaderRetries
			replays += r.Trajectory.ReasoningReplays
			emptyFinals += r.Trajectory.EmptyFinalRetries
			retryWaitMs += r.Trajectory.RetryWaitMs
			compactionMs += r.Trajectory.CompactionMs
			plannerMs += r.Trajectory.PlannerStreamMs
			modelStreamMs += r.Trajectory.ModelStreamMs
			agentOtherMs += r.Trajectory.AgentOtherMs
			if r.WallMs > r.Trajectory.SpanMs {
				startupMs += r.WallMs - r.Trajectory.SpanMs
			}
		}
	}
	if runs == 0 {
		return ""
	}
	line := fmt.Sprintf("**Time attribution** (%d recorded runs): **tools** %s (%s) · **model** %s (%s)",
		runs, dur(toolMs), pct(int(toolMs), int(toolMs+modelMs)),
		dur(modelMs), pct(int(modelMs), int(toolMs+modelMs)))
	if rounds > 0 {
		line += fmt.Sprintf(" · **model rounds** %d (avg gap %s)", rounds, dur(gapMs/int64(rounds)))
	}
	if batches > 0 {
		line += fmt.Sprintf("\n\n**Batching** (%d tool rounds): **calls/round** %.1f · **single-read rounds** %d (%s) · **parallel rounds** %d (saved %s) · **queue p95** %s · **serial wait p95** %s",
			batches, float64(calls)/float64(batches),
			singleReads, pct(singleReads, batches),
			parallelBatches, dur(savedMs), durMs(delayP95), durMs(serialWaitP95))
	}
	if recoveryRounds+streamRetries+headerRetries+replays+emptyFinals > 0 {
		line += fmt.Sprintf("\n\n**Recovery**: recovery rounds %d (%s of rounds, %s) · stream retries %d · header retries %d · reasoning replays %d · empty-final retries %d · clean gap p95 %s",
			recoveryRounds, pct(recoveryRounds, rounds), dur(recoveryGapMs),
			streamRetries, headerRetries, replays, emptyFinals, durMs(cleanP95))
	}
	if plannerMs+modelStreamMs > 0 {
		line += fmt.Sprintf("\n\n**Wall decomposition**: **startup** %s · **agent** %s · **planner** %s · **model** %s · **tools** %s · **retry** %s · **compaction** %s",
			dur(startupMs), dur(agentOtherMs), dur(plannerMs), dur(modelStreamMs),
			dur(toolMs), dur(retryWaitMs), dur(compactionMs))
	}
	line += renderRoundEfficiency(results)
	return line + "\n\n"
}
