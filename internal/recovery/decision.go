package recovery

// Route is the pure decision outcome for a proposed action.
type Route int

const (
	// RouteBypass leaves the call to the ordinary Ask/YOLO approval path.
	RouteBypass Route = iota
	// RouteAllow lets Auto execute without a human card or reviewer call.
	RouteAllow
	// RouteReview hands an ambiguous recovery mutation to the isolated reviewer.
	RouteReview
	// RouteStop blocks one exact operation after repeated technical failure.
	// Other operations in the same Episode may still proceed.
	RouteStop
	// RouteStopTurn blocks further execution after an Episode-level hard limit.
	// Host-proven read-only diagnosis remains available.
	RouteStopTurn
)

// String returns a stable route name for tests and diagnostics.
func (r Route) String() string {
	switch r {
	case RouteBypass:
		return "bypass"
	case RouteAllow:
		return "allow"
	case RouteReview:
		return "review"
	case RouteStop:
		return "stop"
	case RouteStopTurn:
		return "stop_turn"
	default:
		return "unknown"
	}
}

// Facts are the host-observed inputs for the pure decision engine.
// The engine never locks, calls a model, shows UI, or mutates state.
type Facts struct {
	// AutoMode is true only when tool-approval mode is Auto.
	AutoMode bool

	// Proposal classification.
	ReadOnly     bool
	Mutates      bool
	Verification bool
	HighRisk     bool
	// PlanTransition is a host-observed structural rewrite of an active plan.
	PlanTransition bool

	// Active failure context (zero values when none).
	HasActiveFailure    bool
	SameFailedOperation bool
	ExpandedScope       bool
	StrategyChanged     bool
	SafeRetryAvailable  bool
	// FailureCount is the exact-operation failure count (1 = first failure).
	FailureCount uint8
	// EpisodeFailureCount is the Task's total qualifying failures since last
	// real progress inside the current Episode.
	EpisodeFailureCount uint8
	// ReviewRejects is the cumulative reviewer rejection count for the Episode.
	ReviewRejects uint8
	// OperationAlreadyStopped is true when this exact operation already hit its
	// per-operation limit earlier in the Episode.
	OperationAlreadyStopped bool
	// EpisodeStopped is true when a previous decision already exhausted the
	// Episode for this Task.
	EpisodeStopped bool
	// StopReason is the reason the Episode was stopped (when EpisodeStopped).
	StopReason StopReason
}

// DecisionResult is the pure routing result.
type DecisionResult struct {
	Route Route
	// ConsumeSafeRetry is set when RouteAllow was chosen because this is the
	// first safe verification retry; the coordinator must spend the budget.
	ConsumeSafeRetry bool
	// StopReason is set for RouteStop / RouteStopTurn.
	StopReason StopReason
}

// Decide is the pure Auto Guard decision engine.
//
// Order is fixed by product policy:
//  1. non-Auto → bypass ordinary approval
//  2. Episode already stopped → allow read-only diagnosis, stop execution
//  3. structured plan transition → reviewer
//  4. read-only diagnosis → allow
//  5. no active failure → allow ordinary mutations
//  6. operations other than the exact failed operation → allow
//  7. first safe verification retry → allow (+ consume budget)
//  8. already-stopped operation re-proposal is handled by the gate (retries)
//  9. three consecutive failures of the same operation → stop that operation
//  10. remaining exact-operation retries → reviewer
//
// Episode-level totals (6 failures / 3 review rejects / 3 stopped-op retries)
// are enforced by the gate before or after Decide; Decide focuses on pure
// routing of one proposal given Facts.
func Decide(f Facts) DecisionResult {
	if !f.AutoMode {
		return DecisionResult{Route: RouteBypass}
	}
	if f.EpisodeStopped {
		if f.ReadOnly && !f.Mutates && !f.Verification && !f.PlanTransition {
			return DecisionResult{Route: RouteAllow}
		}
		reason := f.StopReason
		if reason == StopReasonNone {
			reason = StopReasonEpisodeFailures
		}
		return DecisionResult{Route: RouteStopTurn, StopReason: reason}
	}
	if f.PlanTransition {
		return DecisionResult{Route: RouteReview}
	}
	// Non-mutating, non-verification calls (and host-proven read-only tools)
	// always continue so diagnosis can proceed without cards.
	if f.ReadOnly && !f.Mutates {
		return DecisionResult{Route: RouteAllow}
	}
	if !f.Mutates && !f.Verification {
		return DecisionResult{Route: RouteAllow}
	}
	if !f.HasActiveFailure {
		return DecisionResult{Route: RouteAllow}
	}
	if f.SafeRetryAvailable {
		return DecisionResult{Route: RouteAllow, ConsumeSafeRetry: true}
	}
	// A failure is an execution-reliability signal, not a task-wide safety
	// boundary. Keep unrelated work on the zero-confirmation path; permission,
	// sandbox, and the Episode ceiling still apply independently.
	if !f.SameFailedOperation {
		return DecisionResult{Route: RouteAllow}
	}
	// Already-stopped exact operation: gate escalates retries; Decide stops the
	// operation so the agent cannot re-run it.
	if f.OperationAlreadyStopped && f.SameFailedOperation {
		return DecisionResult{Route: RouteStop, StopReason: StopReasonOperationFailures}
	}
	// Repeated technical failure of the same exact operation is not a
	// user-owned product decision. Stop only that operation.
	if f.FailureCount >= MaxOperationFailures && f.SameFailedOperation {
		return DecisionResult{Route: RouteStop, StopReason: StopReasonOperationFailures}
	}
	return DecisionResult{Route: RouteReview}
}
