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
}

// DecisionResult is the pure routing result.
type DecisionResult struct {
	Route Route
	// ConsumeSafeRetry is set when RouteAllow was chosen because this is the
	// first safe verification retry; the coordinator must spend the budget.
	ConsumeSafeRetry bool
}

// Decide is the pure Auto Guard decision engine.
//
// Order is fixed by product policy:
//  1. non-Auto → bypass ordinary approval
//  2. structured plan transition → reviewer
//  3. read-only diagnosis → allow
//  4. no active failure → allow ordinary mutations
//  5. first safe verification retry → allow (+ consume budget)
//  6. operations other than the exact failed operation → allow
//  7. retrying the exact failed operation → reviewer
//
// Nothing here counts attempts. A repeated failure keeps routing to the
// reviewer, which judges the proposal in front of it; the host does not decide
// that some number of tries is the point where the user stops wanting the work.
func Decide(f Facts) DecisionResult {
	if !f.AutoMode {
		return DecisionResult{Route: RouteBypass}
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
	// boundary. Keep unrelated work on the zero-confirmation path; permission
	// and sandbox still apply independently.
	if !f.SameFailedOperation {
		return DecisionResult{Route: RouteAllow}
	}
	return DecisionResult{Route: RouteReview}
}
