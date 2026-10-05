package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"reasonix/internal/safety/evidence"
)

// trajectorySummary is the harness-side digest of one run's trajectory file:
// where the wall clock went, split between tool execution and everything the
// model spent between calls (thinking, streaming, provider latency).
type trajectorySummary struct {
	Path    string `json:"path"`
	Records int    `json:"records"`
	SpanMs  int64  `json:"span_ms"`
	ToolMs  int64  `json:"tool_ms"`
	ModelMs int64  `json:"model_ms"` // SpanMs − tool wall clock, floored at zero
	// Round decomposition: a round's gap runs from the turn start (or the
	// batch's last tool_result) to the next top-level tool_dispatch; the
	// final segment to the last record is the answer round.
	ModelRounds     int   `json:"model_rounds"`
	ModelGapTotalMs int64 `json:"model_gap_total_ms"`
	ModelGapP95Ms   int64 `json:"model_gap_p95_ms"`
	Retries         int   `json:"retries,omitempty"`
	Compactions     int   `json:"compactions,omitempty"`

	// Batch decomposition: one batch is one round's top-level tool calls.
	// ToolWallMs is the union of execution intervals (parallel calls counted
	// once) plus durations of calls that carried no timestamps.
	ToolWallMs       int64 `json:"tool_wall_ms,omitempty"`
	ToolBatches      int   `json:"tool_batches,omitempty"`
	TopLevelCalls    int   `json:"top_level_calls,omitempty"`
	MaxBatchSize     int   `json:"max_batch_size,omitempty"`
	ParallelBatches  int   `json:"parallel_batches,omitempty"`   // ≥2 calls actually overlapped
	ParallelSavedMs  int64 `json:"parallel_saved_ms,omitempty"`  // Σ durations − batch wall
	SingleReadRounds int   `json:"single_read_rounds,omitempty"` // 1-call read-only batches
	SingleReadStreak int   `json:"single_read_streak,omitempty"` // longest consecutive run
	// StartDelayP95Ms is dispatch→start inside batches that actually overlapped,
	// so it prices scheduling. SerialWaitP95Ms is the same span in batches that
	// ran one at a time, where waiting for the previous call is the design.
	StartDelayP95Ms int64 `json:"start_delay_p95_ms,omitempty"`
	SerialWaitP95Ms int64 `json:"serial_wait_p95_ms,omitempty"`

	// RefusedCalls counts calls stopped before they ran, keyed by the gate that
	// stopped them. A misfiring gate repeats one phase across runs, which the
	// round count alone never shows.
	RefusedCalls map[string]int `json:"refused_calls,omitempty"`

	// Recovery decomposition: a round whose gap contained a provider retry,
	// missing-reasoning replay, or empty-final retry is a recovery round; clean
	// p95 excludes them so adapter flakiness reads as adapter cost, not agent.
	StreamRetries     int   `json:"stream_retries,omitempty"`
	HeaderRetries     int   `json:"header_retries,omitempty"`
	ReasoningReplays  int   `json:"reasoning_replays,omitempty"` // extra exact-replay requests
	EmptyFinalRetries int   `json:"empty_final_retries,omitempty"`
	RecoveryRounds    int   `json:"recovery_rounds,omitempty"`
	RecoveryGapMs     int64 `json:"recovery_gap_ms,omitempty"`
	CleanGapP95Ms     int64 `json:"clean_gap_p95_ms,omitempty"`

	// Wall decomposition: disjoint buckets partitioning the span, allocated by
	// priority (tools > retry backoff > compaction > model streaming with the
	// planner split out > agent overhead), so overlaps are never double-booked.
	RetryWaitMs     int64 `json:"retry_wait_ms,omitempty"`     // retrying → next attempt begin
	CompactionMs    int64 `json:"compaction_ms,omitempty"`     // compaction_started → done
	PlannerStreamMs int64 `json:"planner_stream_ms,omitempty"` // attempts closed by planner usage
	ModelStreamMs   int64 `json:"model_stream_ms,omitempty"`   // remaining sampling attempts
	AgentOtherMs    int64 `json:"agent_other_ms,omitempty"`    // span remainder: assembly, guards, idle

	// Phase-trace inputs: content-free firsts and counts for the per-task trace.
	TTFTMs           int64 `json:"ttft_ms,omitempty"`       // span start → first output delta
	FirstToolMs      int64 `json:"first_tool_ms,omitempty"` // span start → first tool start
	PlannerRequests  int   `json:"planner_requests,omitempty"`
	ExecutorRequests int   `json:"executor_requests,omitempty"`
	SubagentRequests int   `json:"subagent_requests,omitempty"`
	// RequestsBySource keeps every origin honest — goal-evaluator, compaction
	// and capability-router calls must not masquerade as executor rounds.
	RequestsBySource  map[string]int `json:"requests_by_source,omitempty"`
	ToolQueueMs       int64          `json:"tool_queue_ms,omitempty"`       // Σ dispatch→start delays
	NoProgressSignals int            `json:"no_progress_signals,omitempty"` // progress_guard escalations

	// Round outcomes: each classified round's gap booked to what it produced.
	// Productive = evidence_gain/mutation/verification/finalization; the rest
	// accumulates into WastedGapMs — the knife-target readout.
	RoundOutcomes  map[string]int   `json:"round_outcomes,omitempty"`
	RoundOutcomeMs map[string]int64 `json:"round_outcome_ms,omitempty"`
	UsefulRounds   int              `json:"useful_rounds,omitempty"`
	WastedGapMs    int64            `json:"wasted_gap_ms,omitempty"`

	// Mechanism ledger inputs: recovery gap split per taint kind, and the
	// executor-handoff nudge count (a correctness mechanism that buys a whole
	// extra model round each time it fires).
	HandoffNudges       int              `json:"handoff_nudges,omitempty"`
	RecoveryGapMsByKind map[string]int64 `json:"recovery_gap_ms_by_kind,omitempty"`

	// Tool surface: the schema tax every top-level request re-pays, and the
	// surface churn (connect_tool_source calls, provider prefix resets) that
	// trades that tax against mid-session cache invalidation.
	SchemaTokensMax   int64 `json:"schema_tokens_max,omitempty"`   // largest per-request schema footprint
	SchemaTokensTotal int64 `json:"schema_tokens_total,omitempty"` // Σ schema tokens across requests
	PromptTokensSeen  int64 `json:"prompt_tokens_seen,omitempty"`  // Σ prompt tokens (schema share denominator)
	PrefixResets      int   `json:"prefix_resets,omitempty"`       // usage events with prefixChanged
	ConnectCalls      int   `json:"connect_calls,omitempty"`       // connect_tool_source dispatches

	// Cold/warm evidence: the first top-level request's cache split. A cold
	// session pays the whole prefix as miss; a warmed one starts near-hit.
	FirstReqCacheHitTokens  int64 `json:"first_req_cache_hit_tokens,omitempty"`
	FirstReqCacheMissTokens int64 `json:"first_req_cache_miss_tokens,omitempty"`

	// Shadow contract audit (last of the turn): what the observing contract
	// concluded, priced against the hidden grader by the report.
	ShadowIntent   string `json:"shadow_intent,omitempty"`
	ShadowVerdict  string `json:"shadow_verdict,omitempty"`
	ShadowComplete bool   `json:"shadow_complete,omitempty"`

	// Completion report (last of the turn): the host-authored receipt's
	// verdict and the gaps it refused to hide, priced against the grader.
	CompletionVerdict  string   `json:"completion_verdict,omitempty"`
	CompletionGaps     int      `json:"completion_gaps,omitempty"`
	CompletionGapKinds []string `json:"completion_gap_kinds,omitempty"`
	ClaimsVerified     int      `json:"claims_verified,omitempty"`
	ClaimsUnbacked     int      `json:"claims_unbacked,omitempty"`

	// Evidence bundle (last of the run): the host-evidence outcome and how it
	// diverged from the completion report's verdict.
	BundleOutcome     string   `json:"bundle_outcome,omitempty"`
	CapabilitiesHash  string   `json:"capabilities_hash,omitempty"`
	DivergenceClass   string   `json:"divergence_class,omitempty"`
	DivergenceReasons []string `json:"divergence_reasons,omitempty"`

	// Outcome shadow: the runtime outcome scorer's per-round series condensed,
	// or a verification-receipt backfill for recordings that predate it.
	Outcome *outcomeSummary `json:"outcome,omitempty"`

	// Cognition: executor reasoning/completion joined per model round, plus a
	// census of slow rounds — gaps that bought unusually large thinking.
	ReasoningTokensTotal int64 `json:"reasoning_tokens_total,omitempty"`
	// ReasoningStreamed and ReasoningChars keep thinking measurable on a
	// provider that reports no token count: DeepSeek's Anthropic endpoint folds
	// it into output_tokens, while its OpenAI one reports reasoning_tokens.
	ReasoningStreamed        bool          `json:"reasoning_streamed,omitempty"`
	ReasoningChars           int64         `json:"reasoning_chars,omitempty"`
	CompletionTokensTotal    int64         `json:"completion_tokens_total,omitempty"`
	SlowRounds               int           `json:"slow_rounds,omitempty"`
	SlowRoundGapMs           int64         `json:"slow_round_gap_ms,omitempty"`
	SlowRoundReasoningTokens int64         `json:"slow_round_reasoning_tokens,omitempty"`
	Rounds                   []roundDigest `json:"rounds,omitempty"`
}

// toolWall is the best available tool wall-clock: interval union when the
// recording carried timestamps, else the duration sum (older trajectories).
func (s *trajectorySummary) toolWall() int64 {
	if s.ToolWallMs > 0 {
		return s.ToolWallMs
	}
	return s.ToolMs
}

// trajectoryRecord is the subset of trajectory.Record the summary needs.
type trajectoryRecord struct {
	TS               int64  `json:"ts"`
	ProtocolRecovery string `json:"protocol_recovery"`
	ContractShadow   *struct {
		Intent   string `json:"intent"`
		Verdict  string `json:"verdict"`
		Complete bool   `json:"complete"`
	} `json:"contract_shadow"`
	CompletionReport *struct {
		Verdict        string   `json:"verdict"`
		Gaps           int      `json:"gaps"`
		GapKinds       []string `json:"gap_kinds"`
		ClaimsVerified int      `json:"claims_verified"`
		ClaimsUnbacked int      `json:"claims_unbacked"`
	} `json:"completion_report"`
	RunHeader *struct {
		CapabilitiesHash string `json:"capabilities_hash"`
	} `json:"run_header"`
	EvidenceBundle *struct {
		Outcome           string   `json:"outcome"`
		DivergenceClass   string   `json:"divergence_class"`
		DivergenceReasons []string `json:"divergence_reasons"`
	} `json:"evidence_bundle"`
	OutcomeProgress *struct {
		Exploration    int `json:"exploration"`
		Verification   int `json:"verification"`
		Objective      int `json:"objective"`
		Regression     int `json:"regression"`
		Churn          int `json:"churn"`
		LegacyGain     int `json:"legacy_gain"`
		Discriminating int `json:"discriminating"`
		DebtAge        int `json:"debt_age"`
		BlindMutations int `json:"blind_mutations"`
	} `json:"outcome_progress"`
	Event *struct {
		Kind          string `json:"kind"`
		Text          string `json:"text"`
		Code          string `json:"code"`
		RetryScope    string `json:"retryScope"`
		StreamAttempt *struct {
			ID     string `json:"id"`
			Action string `json:"action"`
		} `json:"streamAttempt"`
		Usage *struct {
			Source           string `json:"source"`
			PromptTokens     int64  `json:"promptTokens"`
			CompletionTokens int64  `json:"completionTokens"`
			ReasoningTokens  int64  `json:"reasoningTokens"`
			CacheHitTokens   int64  `json:"cacheHitTokens"`
			CacheMissTokens  int64  `json:"cacheMissTokens"`
			CacheDiagnostics *struct {
				ToolSchemaTokens int64 `json:"toolSchemaTokens"`
				PrefixChanged    bool  `json:"prefixChanged"`
			} `json:"cacheDiagnostics"`
		} `json:"usage"`
		Tool *struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Args       string `json:"args"`
			Err        string `json:"err"`
			DurationMs int64  `json:"durationMs"`
			ParentID   string `json:"parentId"`
			ReadOnly   bool   `json:"readOnly"`
			Refreshed  bool   `json:"refreshed"`
			StartedAt  int64  `json:"startedAt"`
			EndedAt    int64  `json:"endedAt"`
			Execution  *struct {
				Verification string `json:"verification"`
				State        string `json:"state"`
				FailurePhase string `json:"failurePhase"`
			} `json:"execution"`
		} `json:"tool"`
	} `json:"event"`
}

// roundCall is one call's outcome-relevant facts for round classification.
type roundCall struct {
	name, verification string
	readOnly, errored  bool
	resolved, dup      bool
	// refused means the host stopped the call before launch (permission,
	// preflight): the round paid a full trip and the workspace never saw it.
	refused bool
}

// gapInfo carries one model gap until its batch closes and can classify it.
// The cognition fields are the executor tokens streamed during the gap — what
// the round's thinking actually bought.
type gapInfo struct {
	ms                                    int64
	tainted, planner, compaction, handoff bool
	reasonTok, complTok, promptTok        int64
}

// toolBatch accumulates one round's top-level calls between model gaps.
type toolBatch struct {
	dispatchTS map[string]int64
	infos      map[string]*roundCall
	names      []string
	calls      int
	results    int
	readOnly   int
	serialMs   int64
	intervals  [][2]int64
	waits      []callWait
}

// callWait is one call's dispatch→start window, kept whole so the batch can
// ask whether another call occupied it rather than guessing from batch shape.
type callWait struct {
	dispatch, start int64
}

// summarizeTrajectory reads a run's JSONL trajectory. A truncated final line
// (killed run) is skipped, matching the recorder's durability contract.
func summarizeTrajectory(path string) (*trajectorySummary, error) {
	scan, err := scanTrajectoryFile(path)
	if err != nil {
		return nil, err
	}
	return scan.finish(), nil
}

// verificationKey mirrors the host's identity for a verification, so an offline
// digest scores the same fail→pass edges the live tracker does instead of
// treating every output filter as a separate check.
func verificationKey(name, args string) string {
	raw := name + "\x00" + args
	if name != "bash" {
		return raw
	}
	var p struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil || strings.TrimSpace(p.Command) == "" {
		return raw
	}
	return name + "\x00" + evidence.VerificationIdentity(p.Command)
}

// scanTrajectoryFile runs the record pass without finishing, so callers that
// need the raw series (the live dashboard) can read it before finish folds it.
func scanTrajectoryFile(path string) (*trajScan, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scan := &trajScan{
		s:                &trajectorySummary{Path: path},
		batch:            newToolBatch(),
		attemptBegin:     map[string]int64{},
		lastAttempt:      -1,
		seen:             map[string]bool{},
		delegationToolMs: map[string]int64{},
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		var rec trajectoryRecord
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue
		}
		scan.record(rec)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return scan, nil
}

func (t *trajScan) record(rec trajectoryRecord) {
	t.s.Records++
	if t.firstTS == 0 {
		t.firstTS = rec.TS
		t.gapStart = rec.TS
		t.inModel = true
	}
	t.lastTS = rec.TS
	if rec.ProtocolRecovery == "missing_reasoning_retry_attempted" {
		t.s.ReasoningReplays++
		t.taintAs("reasoning_replay")
	}
	if cs := rec.ContractShadow; cs != nil {
		t.s.ShadowIntent = cs.Intent
		t.s.ShadowVerdict = cs.Verdict
		t.s.ShadowComplete = cs.Complete
	}
	if cr := rec.CompletionReport; cr != nil {
		t.s.CompletionVerdict = cr.Verdict
		t.s.CompletionGaps = cr.Gaps
		t.s.CompletionGapKinds = cr.GapKinds
		t.s.ClaimsVerified = cr.ClaimsVerified
		t.s.ClaimsUnbacked = cr.ClaimsUnbacked
	}
	if rh := rec.RunHeader; rh != nil && rh.CapabilitiesHash != "" {
		t.s.CapabilitiesHash = rh.CapabilitiesHash
	}
	if eb := rec.EvidenceBundle; eb != nil && eb.Outcome != "" {
		t.s.BundleOutcome = eb.Outcome
		t.s.DivergenceClass = eb.DivergenceClass
		t.s.DivergenceReasons = eb.DivergenceReasons
	}
	if op := rec.OutcomeProgress; op != nil {
		t.outcomePoints = append(t.outcomePoints, outcomePoint{
			ts: rec.TS, exploration: op.Exploration, verification: op.Verification,
			objective: op.Objective, regression: op.Regression, churn: op.Churn,
			legacyGain: op.LegacyGain, discriminating: op.Discriminating, debtAge: op.DebtAge,
			blindMutations: op.BlindMutations,
		})
	}
	if rec.Event == nil {
		return
	}
	switch rec.Event.Kind {
	case "retrying":
		t.s.Retries++
		t.pendingRetry = rec.TS
		switch rec.Event.RetryScope {
		case "stream":
			t.s.StreamRetries++
			t.taintAs("stream_retry")
		case "headers":
			t.s.HeaderRetries++
			t.taintAs("header_retry")
		default:
			t.taintAs("provider_retry")
		}
	case "notice":
		switch rec.Event.Code {
		case "empty_final":
			t.s.EmptyFinalRetries++
			t.taintAs("empty_final_retry")
		case "executor_handoff":
			t.s.HandoffNudges++
			t.gapHandoff = true
		case "progress_guard":
			t.s.NoProgressSignals++
		}
	case "reasoning", "text":
		if rec.Event.Kind == "reasoning" {
			t.s.ReasoningStreamed = true
			t.s.ReasoningChars += int64(len(rec.Event.Text))
		}
		if t.firstDelta == 0 {
			t.firstDelta = rec.TS
		}
	case "stream_attempt", "usage":
		t.recordModelPhase(rec)
	case "compaction_started":
		t.s.Compactions++
		t.compFrom = rec.TS
		t.gapCompact = true
	case "compaction_done":
		if t.compFrom > 0 && rec.TS > t.compFrom {
			t.compIvs = append(t.compIvs, [2]int64{t.compFrom, rec.TS})
		}
		t.compFrom = 0
	}
	if rec.Event.Tool == nil || rec.Event.Tool.ParentID != "" {
		// Subagent calls overlap the parent's wall clock; counting them
		// would double-book the span and split the parent's rounds.
		return
	}
	switch rec.Event.Kind {
	case "tool_dispatch":
		t.recordDispatch(rec)
	case "tool_result":
		t.recordResult(rec)
	}
}

// closeGap ends one model round; recovery-tainted rounds are booked apart so
// clean latency stays comparable across providers with different flake rates.
// The gap is queued until its batch closes and can classify the round.
func (t *trajScan) closeGap(gap int64) {
	t.gaps = append(t.gaps, gap)
	t.pendingGaps = append(t.pendingGaps, gapInfo{
		ms: gap, tainted: t.taint != "",
		planner: t.gapPlanner, compaction: t.gapCompact, handoff: t.gapHandoff,
		reasonTok: t.gapReason, complTok: t.gapCompl, promptTok: t.gapPrompt,
	})
	t.gapPlanner, t.gapCompact, t.gapHandoff = false, false, false
	t.gapReason, t.gapCompl, t.gapPrompt = 0, 0, 0
	if t.taint != "" {
		t.s.RecoveryRounds++
		t.s.RecoveryGapMs += gap
		if t.s.RecoveryGapMsByKind == nil {
			t.s.RecoveryGapMsByKind = map[string]int64{}
		}
		t.s.RecoveryGapMsByKind[t.taint] += gap
		t.taint = ""
		return
	}
	t.cleanGaps = append(t.cleanGaps, gap)
}

// taintAs marks the current gap as recovery; the first mechanism to fire in
// a gap owns its time, so per-kind splits stay disjoint.
func (t *trajScan) taintAs(kind string) {
	if t.taint == "" {
		t.taint = kind
	}
}

// recordModelPhase brackets sampling attempts (begin → commit/discard) and
// tags the just-closed attempt with its usage source. Subagent usage is
// skipped: subagent attempts never reach the parent sink, so a subagent usage
// arriving mid-parent-round must not claim the parent's attempt.
func (t *trajScan) recordModelPhase(rec trajectoryRecord) {
	if sa := rec.Event.StreamAttempt; sa != nil {
		switch sa.Action {
		case "begin":
			t.attemptBegin[sa.ID] = rec.TS
			if t.pendingRetry > 0 && rec.TS > t.pendingRetry {
				t.retryIvs = append(t.retryIvs, [2]int64{t.pendingRetry, rec.TS})
			}
			t.pendingRetry = 0
		case "commit", "discard":
			if begin, ok := t.attemptBegin[sa.ID]; ok && rec.TS > begin {
				t.attempts = append(t.attempts, modelAttempt{iv: [2]int64{begin, rec.TS}})
				t.lastAttempt = len(t.attempts) - 1
			}
			delete(t.attemptBegin, sa.ID)
		}
		return
	}
	if u := rec.Event.Usage; u != nil {
		source := u.Source
		if source == "" {
			source = "executor"
		}
		if t.s.RequestsBySource == nil {
			t.s.RequestsBySource = map[string]int{}
		}
		t.s.RequestsBySource[source]++
		switch source {
		case "planner":
			t.s.PlannerRequests++
			t.gapPlanner = true
		case "executor":
			t.s.ExecutorRequests++
			t.s.ReasoningTokensTotal += u.ReasoningTokens
			t.s.CompletionTokensTotal += u.CompletionTokens
			t.gapReason += u.ReasoningTokens
			t.gapCompl += u.CompletionTokens
			t.gapPrompt = max(t.gapPrompt, u.PromptTokens)
		case "subagent":
			t.s.SubagentRequests++
			return
		default:
			// Sidecar calls (goal-evaluator, compaction, capability-router)
			// have their own prompt shape; keep them out of the executor's
			// schema-tax and first-request accounting.
			return
		}
		if t.s.PromptTokensSeen == 0 && u.PromptTokens > 0 {
			t.s.FirstReqCacheHitTokens = u.CacheHitTokens
			t.s.FirstReqCacheMissTokens = u.CacheMissTokens
		}
		t.s.PromptTokensSeen += u.PromptTokens
		if d := u.CacheDiagnostics; d != nil {
			t.s.SchemaTokensTotal += d.ToolSchemaTokens
			t.s.SchemaTokensMax = max(t.s.SchemaTokensMax, d.ToolSchemaTokens)
			if d.PrefixChanged {
				t.s.PrefixResets++
			}
		}
		if t.lastAttempt >= 0 {
			t.attempts[t.lastAttempt].planner = u.Source == "planner"
			t.lastAttempt = -1
		}
	}
}

func (t *trajScan) recordDispatch(rec trajectoryRecord) {
	tl := rec.Event.Tool
	if t.inModel {
		t.closeGap(rec.TS - t.gapStart)
		t.inModel = false
		t.closeBatch()
	}
	// Refreshed dispatches re-announce a call already counted;
	// id-less records (older recordings) cannot be deduped.
	if tl.Refreshed {
		return
	}
	if tl.ID == "" || !t.batch.seen(tl.ID) {
		t.batch.calls++
	}
	// The full dispatch re-announces a streamed partial; keeping the later TS
	// anchors start-delay to pre-exec queueing, not the stream tail. The dup
	// check keys on the latest (fullest) name+args announcement.
	if tl.ID != "" {
		t.sawCallIDs = true
		t.batch.dispatchTS[tl.ID] = rec.TS
		key := tl.Name + "\x00" + tl.Args
		info := t.batch.infos[tl.ID]
		if info == nil {
			info = &roundCall{}
			t.batch.infos[tl.ID] = info
			t.batch.names = append(t.batch.names, tl.Name)
			if tl.Name == "connect_tool_source" {
				t.s.ConnectCalls++
			}
		}
		info.name = tl.Name
		info.dup = t.seen[key]
		t.seen[key] = true
	}
}

func (t *trajScan) recordResult(rec trajectoryRecord) {
	tl := rec.Event.Tool
	t.s.ToolMs += tl.DurationMs
	t.batch.results++
	if tl.ReadOnly {
		t.batch.readOnly++
	}
	if info, ok := t.batch.infos[tl.ID]; ok {
		info.resolved = true
		info.readOnly = tl.ReadOnly
		info.errored = tl.Err != ""
		if tl.Execution != nil {
			info.verification = tl.Execution.Verification
		}
	}
	if ex := tl.Execution; ex != nil && (ex.Verification == "passed" || ex.Verification == "failed") {
		t.observeVerification(verificationKey(tl.Name, tl.Args), ex.Verification == "passed", rec.TS)
	}
	if ex := tl.Execution; ex != nil && ex.State == "not_run" && ex.FailurePhase != "" {
		if info, ok := t.batch.infos[tl.ID]; ok {
			info.refused = true
		}
		if t.s.RefusedCalls == nil {
			t.s.RefusedCalls = map[string]int{}
		}
		t.s.RefusedCalls[ex.FailurePhase]++
	}
	if delegationTools[tl.Name] {
		t.delegationToolMs[tl.Name] += tl.DurationMs
	}
	t.batch.serialMs += tl.DurationMs
	if tl.StartedAt > 0 && tl.EndedAt >= tl.StartedAt {
		t.batch.intervals = append(t.batch.intervals, [2]int64{tl.StartedAt, tl.EndedAt})
		if t.firstToolTS == 0 || tl.StartedAt < t.firstToolTS {
			t.firstToolTS = tl.StartedAt
		}
		if disp, ok := t.batch.dispatchTS[tl.ID]; ok && tl.StartedAt >= disp {
			t.batch.waits = append(t.batch.waits, callWait{dispatch: disp, start: tl.StartedAt})
		}
	} else {
		t.orphanMs += tl.DurationMs
	}
	t.gapStart = rec.TS
	t.inModel = true
}

func (t *trajScan) closeBatch() {
	b, s := t.batch, t.s
	if b.calls == 0 {
		return
	}
	if len(t.pendingGaps) > 0 {
		gap := t.pendingGaps[0]
		t.pendingGaps = t.pendingGaps[1:]
		if len(b.infos) > 0 {
			t.recordRound(classifyRound(gap, b), gap, b)
		}
	}
	s.ToolBatches++
	s.TopLevelCalls += b.calls
	s.MaxBatchSize = max(s.MaxBatchSize, b.calls)
	if b.calls == 1 && b.results == 1 && b.readOnly == 1 {
		s.SingleReadRounds++
		t.streakRun++
		s.SingleReadStreak = max(s.SingleReadStreak, t.streakRun)
	} else {
		t.streakRun = 0
	}
	overlapped := false
	if len(b.intervals) > 1 {
		wall, ov := intervalSpan(b.intervals)
		overlapped = ov
		if overlapped {
			s.ParallelBatches++
		}
		if saved := b.serialMs - wall; saved > 0 {
			s.ParallelSavedMs += saved
		}
	}
	// A call waited on work, rather than on the scheduler, when another call in
	// the batch was still executing inside its dispatch→start window.
	for _, w := range b.waits {
		delay := w.start - w.dispatch
		t.queueMs += delay
		blocked := false
		for _, iv := range b.intervals {
			if iv[0] < w.start && iv[1] > w.dispatch {
				blocked = true
				break
			}
		}
		if blocked {
			t.serialWaits = append(t.serialWaits, delay)
		} else {
			t.delays = append(t.delays, delay)
		}
	}
	t.allIntervals = append(t.allIntervals, b.intervals...)
	t.batch = newToolBatch()
}

func (t *trajScan) finish() *trajectorySummary {
	s := t.s
	if t.inModel && t.lastTS > t.gapStart {
		t.closeGap(t.lastTS - t.gapStart) // final answer round
	}
	t.closeBatch()
	if t.sawCallIDs {
		for _, gap := range t.pendingGaps {
			t.recordRound(classifyRound(gap, nil), gap, nil)
		}
	}
	t.pendingGaps = nil
	s.ModelRounds = len(t.gaps)
	for _, g := range t.gaps {
		s.ModelGapTotalMs += g
	}
	s.ModelGapP95Ms = p95(t.gaps)
	s.CleanGapP95Ms = p95(t.cleanGaps)
	s.StartDelayP95Ms = p95(t.delays)
	s.SerialWaitP95Ms = p95(t.serialWaits)
	s.ToolQueueMs = t.queueMs
	if t.firstDelta > t.firstTS {
		s.TTFTMs = t.firstDelta - t.firstTS
	}
	if t.firstToolTS > t.firstTS {
		s.FirstToolMs = t.firstToolTS - t.firstTS
	}
	if len(t.allIntervals) > 0 {
		s.ToolWallMs = intervalUnion(t.allIntervals) + t.orphanMs
	}
	s.SpanMs = t.lastTS - t.firstTS
	if s.ModelMs = s.SpanMs - s.toolWall(); s.ModelMs < 0 {
		s.ModelMs = 0
	}
	s.Outcome = t.summarizeOutcome()
	t.decompose()
	return s
}

// decompose partitions the span into disjoint wall buckets by priority, so a
// second spent in two places is booked once, to the more specific bucket.
func (t *trajScan) decompose() {
	if len(t.attempts) == 0 {
		return // old recording without stream_attempt events
	}
	s := t.s
	var planIvs, execIvs [][2]int64
	for _, a := range t.attempts {
		if a.planner {
			planIvs = append(planIvs, a.iv)
		} else {
			execIvs = append(execIvs, a.iv)
		}
	}
	covered := mergeIntervals(t.allIntervals)
	retry := clipIntervals(t.retryIvs, covered)
	covered = mergeIntervals(append(covered, retry...))
	comp := clipIntervals(t.compIvs, covered)
	covered = mergeIntervals(append(covered, comp...))
	plan := clipIntervals(planIvs, covered)
	covered = mergeIntervals(append(covered, plan...))
	exec := clipIntervals(execIvs, covered)
	s.RetryWaitMs = ivsLen(retry)
	s.CompactionMs = ivsLen(comp)
	s.PlannerStreamMs = ivsLen(plan)
	s.ModelStreamMs = ivsLen(exec)
	rem := s.SpanMs - s.toolWall() - s.RetryWaitMs - s.CompactionMs - s.PlannerStreamMs - s.ModelStreamMs
	if rem > 0 {
		s.AgentOtherMs = rem
	}
}

func newToolBatch() *toolBatch {
	return &toolBatch{dispatchTS: map[string]int64{}, infos: map[string]*roundCall{}}
}

func (b *toolBatch) seen(id string) bool {
	_, ok := b.dispatchTS[id]
	return ok
}

// durMs renders small durations without the sub-second floor dur applies.
func durMs(ms int64) string {
	if ms <= 0 {
		return "0ms"
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return dur(ms)
}

func p95(values []int64) int64 {
	return pctile(values, 95)
}

func pctile(values []int64, p int) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	slices.Sort(sorted)
	index := min((len(sorted)*p+99)/100, len(sorted))
	return sorted[index-1]
}
