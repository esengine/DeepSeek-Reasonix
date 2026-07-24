package recovery

// AutoGuardLevel controls how aggressively Auto Guard blocks and escalates.
// It is configured per-controller and persisted as a user preference.
type AutoGuardLevel string

const (
	// AutoGuardGentle only blocks deterministic high-risk operations (rm, sudo,
	// chmod, etc.) and never escalates to Episode stop. Safe for vibecoding
	// users who work inside their workspace.
	AutoGuardGentle AutoGuardLevel = "gentle"
	// AutoGuardNormal (default) blocks repeated failures, reviewer rejects, and
	// stopped-op retries with the host-defined budgets.
	AutoGuardNormal AutoGuardLevel = "normal"
	// AutoGuardStrict tightens budgets and adds additional checks for sensitive
	// operations. Suitable for shared or regulated environments.
	AutoGuardStrict AutoGuardLevel = "strict"
)

// Fixed product defaults for Auto recovery budgets. These are not configurable
// via UI, CLI, or config files — they define the host-owned Auto safety envelope.
const (
	// MaxOperationFailures is how many times one exact operation may fail inside
	// one Episode before that operation alone is stopped.
	MaxOperationFailures = 3
	// MaxEpisodeFailures is how many qualifying execution failures one Task may
	// accumulate inside one Episode since the last real progress before the
	// whole turn stops. Parameter, command, or target changes do not reset it.
	MaxEpisodeFailures = 6
	// MaxReviewRejects is how many cumulative reviewer rejections one Task may
	// accumulate inside one Episode before the turn stops. Different candidates
	// share this budget.
	MaxReviewRejects = 3
	// MaxStoppedOperationRetries is how many times an already-stopped operation
	// may be re-proposed before the turn escalates to a hard Episode stop.
	MaxStoppedOperationRetries = 3
	// MaxGentleOperationFailures is the per-operation failure limit in gentle
	// mode before the operation is blocked. Higher than normal mode to give
	// vibecoding users more runway before intervention.
	MaxGentleOperationFailures = 5
)

// StopReason identifies why an Episode-level stop was raised. Values are
// internal; user-facing copy never exposes them.
type StopReason string

const (
	StopReasonNone              StopReason = ""
	StopReasonEpisodeFailures   StopReason = "episode_failures"
	StopReasonReviewRejects     StopReason = "review_rejects"
	StopReasonStoppedOpRetries  StopReason = "stopped_op_retries"
	StopReasonOperationFailures StopReason = "operation_failures"
)

// Recovery pause product copy. Wire/CLI surfaces use the English string so old
// clients that only read err still get a non-technical message.
const (
	// PauseMessageEN is the turn_done.err / CLI text for recovery_paused.
	PauseMessageEN = "This automatic recovery turn paused to avoid repeated execution. Completed work is kept; send more requirements or reply continue."
	// PauseMessageZH is the preferred desktop product copy (localized separately).
	PauseMessageZH = "这轮自动尝试已暂停，避免反复执行。已完成的工作已保留；可以直接补充要求或发送“继续”。"
	// FinalizationNudge tells the model it has exactly one summarize-only round.
	FinalizationNudge = "Auto recovery has reached its limit for this turn. Do not call any more tools. Summarize what was completed, what failed, and what the user should do next. The user can continue in the next message."
)
