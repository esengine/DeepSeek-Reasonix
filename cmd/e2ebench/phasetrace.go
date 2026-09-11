package main

// phaseTrace is the per-task privacy-safe latency trace: only counts and
// milliseconds, no prompts, arguments, or outputs. It stitches the harness
// clock (total, solved, tokens) to the trajectory digest's phase buckets.
type phaseTrace struct {
	TotalMs         int64 `json:"total_ms"`
	TTFTMs          int64 `json:"ttft_ms,omitempty"`
	TimeToFirstTool int64 `json:"time_to_first_tool_ms,omitempty"`

	Planner  phaseModel `json:"planner"`
	Executor phaseModel `json:"executor"`
	Tool     phaseTool  `json:"tool"`
	// Recovery counts extra requests forced by provider flakiness (stream and
	// header retries, missing-reasoning replays, empty-final retries); Ms is
	// the model-gap time of the rounds those events tainted.
	Recovery phaseModel `json:"recovery"`

	CompactionMs int64 `json:"compaction_ms,omitempty"`
	// NoProgressSignals counts progress-guard escalations (each threshold
	// fires once), not raw zero-evidence-gain rounds.
	NoProgressSignals int `json:"no_progress_signals,omitempty"`
	// Rounds is the outcome ledger: total classified rounds, how many bought
	// progress, the wasted gap total, and the per-outcome composition.
	Rounds phaseRounds `json:"rounds"`

	PromptTokens     int  `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	Solved           bool `json:"solved"`
}

type phaseModel struct {
	Requests int   `json:"requests"`
	Ms       int64 `json:"ms"`
}

type phaseTool struct {
	Calls          int   `json:"calls"`
	QueueMs        int64 `json:"queue_ms"`
	CriticalPathMs int64 `json:"critical_path_ms"`
}

type phaseRounds struct {
	Total    int              `json:"total"`
	Useful   int              `json:"useful"`
	WastedMs int64            `json:"wasted_ms"`
	Outcomes map[string]int   `json:"outcomes,omitempty"`
	MsByKind map[string]int64 `json:"ms_by_outcome,omitempty"`
}

func classifiedRounds(outcomes map[string]int) int {
	total := 0
	for _, n := range outcomes {
		total += n
	}
	return total
}

// buildPhaseTrace derives the trace from a graded result; nil without a
// recorded trajectory (the digest carries every phase input).
func buildPhaseTrace(r result) *phaseTrace {
	t := r.Trajectory
	if t == nil {
		return nil
	}
	return &phaseTrace{
		TotalMs:         r.WallMs,
		TTFTMs:          t.TTFTMs,
		TimeToFirstTool: t.FirstToolMs,
		Planner:         phaseModel{Requests: t.PlannerRequests, Ms: t.PlannerStreamMs},
		Executor:        phaseModel{Requests: t.ExecutorRequests, Ms: t.ModelStreamMs},
		Tool: phaseTool{
			Calls:          t.TopLevelCalls,
			QueueMs:        t.ToolQueueMs,
			CriticalPathMs: t.toolWall(),
		},
		Recovery: phaseModel{
			Requests: t.StreamRetries + t.HeaderRetries + t.ReasoningReplays + t.EmptyFinalRetries,
			Ms:       t.RecoveryGapMs,
		},
		CompactionMs:      t.CompactionMs,
		NoProgressSignals: t.NoProgressSignals,
		Rounds: phaseRounds{
			Total:    classifiedRounds(t.RoundOutcomes),
			Useful:   t.UsefulRounds,
			WastedMs: t.WastedGapMs,
			Outcomes: t.RoundOutcomes,
			MsByKind: t.RoundOutcomeMs,
		},
		PromptTokens:     r.PromptTokens,
		CompletionTokens: r.CompletionTokens,
		Solved:           r.Passed,
	}
}
