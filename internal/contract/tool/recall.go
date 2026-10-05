package tool

import "context"

// RecallRequest names folded transcript positions to bring back, by the #n
// addresses a compaction digest's folded-work index carries.
type RecallRequest struct {
	Positions []int
	// Query searches the folded region instead of reading it, against the
	// canonical transcript rather than the projection, so the addresses it
	// returns outlive the generations that lost them.
	Query string
	Limit int
}

// RecallHit is one search result: what matched, and the canonical position to
// read it at. Position always addresses the message a read should ask for —
// for a tool result that is the assistant call that produced it, so one read
// returns the call and its output together.
type RecallHit struct {
	Position int
	Kind     string
	Tool     string
	Snippet  string
}

// RecallResult is one recall's content plus what it cost. Text is what the
// model reads; the counters let it see the generation's budget draining.
type RecallResult struct {
	Text       string
	Recalled   []int
	Missing    []int
	Hits       []RecallHit
	Searched   int // messages the query ran against
	Tokens     int
	BudgetLeft int
}

// ContextRecaller is supplied by the Agent that owns the active session, the
// same way ContextCompressor is: the canonical transcript never leaves the
// agent, so a tool asks it for a position rather than reading a session file.
type ContextRecaller interface {
	RecallContext(context.Context, RecallRequest) (RecallResult, error)
}

type contextRecallerKey struct{}

// WithContextRecaller binds the active session's recaller to a tool call.
func WithContextRecaller(ctx context.Context, recaller ContextRecaller) context.Context {
	if recaller == nil {
		return ctx
	}
	return context.WithValue(ctx, contextRecallerKey{}, recaller)
}

// ContextRecallerFromContext returns the recaller bound to this tool call.
func ContextRecallerFromContext(ctx context.Context) (ContextRecaller, bool) {
	recaller, ok := ctx.Value(contextRecallerKey{}).(ContextRecaller)
	return recaller, ok && recaller != nil
}
