package recovery

import (
	"encoding/json"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
)

// Phase is a derived view of recovery progress for compatibility snapshots.
// Runtime truth is "has failure" and "has waiter", not a stored phase enum.
type Phase string

const (
	PhaseIdle             Phase = "idle"
	PhaseDiagnosing       Phase = "diagnosing"
	PhaseAwaitingDecision Phase = "awaiting_decision"
)

// ChangeKind classifies how the proposed recovery action differs from the
// original approach.
type ChangeKind string

const (
	ChangeSameStrategy ChangeKind = "same_strategy"
	ChangeStrategy     ChangeKind = "strategy"
	ChangeScope        ChangeKind = "scope"
	ChangeRisk         ChangeKind = "risk"
	ChangeUncertain    ChangeKind = "uncertain"
)

// ReviewOutcome is the independent recovery reviewer's decision.
type ReviewOutcome string

const (
	ReviewContinue ReviewOutcome = "continue"
	ReviewConfirm  ReviewOutcome = "confirm"
)

// FailureClass separates execution reliability from permission and product
// decisions. Permission/sandbox/user blocks never become FailureEvents.
type FailureClass string

const (
	FailureClassExecution    FailureClass = "execution"
	FailureClassMutation     FailureClass = "mutation"
	FailureClassTransient    FailureClass = "transient"
	FailureClassVerification FailureClass = "verification"
)

// ReviewVerdict is the strict JSON shape the recovery reviewer must produce.
// Host already knows failure/diagnosis/proposed action; only outcome fields are
// required. Extra fields from older models are tolerated on parse.
type ReviewVerdict struct {
	Outcome    ReviewOutcome `json:"outcome"`
	ChangeKind ChangeKind    `json:"change_kind"`
	Rationale  string        `json:"rationale"`

	// Legacy optional fields kept for older model outputs and tests.
	FailureSummary string `json:"failure_summary,omitempty"`
	Diagnosis      string `json:"diagnosis,omitempty"`
	ProposedAction string `json:"proposed_action,omitempty"`
}

// FailureEvent records failure evidence for diagnosis and reviewer context.
// SafeRetryLeft/RepeatCount/DiagnosisNotes remain on the wire for old
// snapshots; runtime budgets live on taskRuntime.
type FailureEvent struct {
	Class         FailureClass `json:"class,omitempty"`
	Tool          string       `json:"tool"`
	ArgsSummary   string       `json:"args_summary,omitempty"`
	Subject       string       `json:"subject,omitempty"`
	ErrSummary    string       `json:"err_summary,omitempty"`
	OutputExcerpt string       `json:"output_excerpt,omitempty"`
	SourceAgent   string       `json:"source_agent,omitempty"`
	TaskID        string       `json:"task_id,omitempty"`
	// TaskScopeID persists only stable goal scopes. Ordinary turn scopes are
	// runtime-local and intentionally omitted so a restart cannot revive a stale
	// technical latch for a new user turn. It never acts as the Episode budget key.
	TaskScopeID    string          `json:"task_scope_id,omitempty"`
	ReadOnly       bool            `json:"read_only,omitempty"`
	Verification   bool            `json:"verification,omitempty"`
	Mutates        bool            `json:"mutates,omitempty"`
	RepeatCount    int             `json:"repeat_count,omitempty"`
	CreatedAt      time.Time       `json:"created_at,omitempty"`
	Args           json.RawMessage `json:"args,omitempty"`
	Fingerprint    string          `json:"fingerprint,omitempty"`
	SafeRetryLeft  int             `json:"safe_retry_left,omitempty"`
	DiagnosisNotes []string        `json:"diagnosis_notes,omitempty"`
}

// PendingProposal is the mutation paused for user confirmation.
// It is held only in the temporary waiter table, never as durable task state.
type PendingProposal struct {
	Tool        string          `json:"tool"`
	Subject     string          `json:"subject,omitempty"`
	Preview     string          `json:"preview,omitempty"`
	Args        json.RawMessage `json:"args,omitempty"`
	Fingerprint string          `json:"fingerprint,omitempty"`
	SourceAgent string          `json:"source_agent,omitempty"`
	ChangeKind  ChangeKind      `json:"change_kind,omitempty"`
	Rationale   string          `json:"rationale,omitempty"`
	Diagnosis   string          `json:"diagnosis,omitempty"`
	Failure     string          `json:"failure,omitempty"`
	Proposed    string          `json:"proposed,omitempty"`
	PlanBefore  string          `json:"plan_before,omitempty"`
	PlanAfter   string          `json:"plan_after,omitempty"`
	// TaskGrant fields are transient host-classified scope. They are deliberately
	// omitted from snapshots and never supplied by the model or wire client.
	TaskGrantKey       string `json:"-"`
	TaskGrantTaskScope string `json:"-"`
	TaskGrantDisplay   string `json:"-"`
}

// TaskState is the persistable / debug view of one task's recovery state.
// Runtime truth is taskRuntime; Snapshot/Restore project to and from this shape.
//
// Persistence projection writes only LastFailure as historical evidence.
// Failure / ConsecutiveFails / ReviewBlocks may still appear in live Snapshot()
// for debugging, and old on-disk values are migrated to evidence without re-arming.
type TaskState struct {
	Phase            Phase            `json:"phase"`
	Failure          *FailureEvent    `json:"failure,omitempty"`
	LastFailure      *FailureEvent    `json:"last_failure,omitempty"`
	Pending          *PendingProposal `json:"pending,omitempty"`
	ApprovalID       string           `json:"approval_id,omitempty"`
	ConsecutiveFails int              `json:"consecutive_fails,omitempty"`
	ReviewBlocks     int              `json:"review_blocks,omitempty"`
	TailInjected     bool             `json:"tail_injected,omitempty"`
	// EpisodeID is runtime/debug only and never written by persistence projection.
	EpisodeID string `json:"episode_id,omitempty"`
	// EpisodeStopped / StopReason are live debug views only.
	EpisodeStopped bool   `json:"episode_stopped,omitempty"`
	StopReason     string `json:"stop_reason,omitempty"`
}

// Snapshot is the form of all task recovery state.
// Live Snapshot() includes debug fields; PersistenceSnapshot() strips temporary
// lock/budget state so disk never re-arms Auto blocks after restart.
type Snapshot struct {
	Tasks map[string]*TaskState `json:"tasks,omitempty"`
}

// Metrics are content-free counters for release observation.
// They never record parameters, paths, or error bodies.
type Metrics struct {
	FailureEvents      int64
	RuleContinues      int64
	ReviewContinues    int64
	HumanPrompts       int64
	HumanContinues     int64
	TaskGrantContinues int64
	TaskGrantUses      int64
	HumanRevises       int64
	ReviewErrors       int64
	ReviewLatencyMsSum int64
	ReviewLatencyCount int64
	RepeatPrompts      int64

	// Episode / generation counters (content-free).
	OperationStops           int64
	EpisodeFailureStops      int64
	ReviewStops              int64
	StoppedOpRetryStops      int64
	ModeResets               int64
	EpisodeRotations         int64
	StaleObservationsIgnored int64
}

// ApprovalKindRecovery is the Approval.Kind value for recovery cards.
const ApprovalKindRecovery = "recovery"

// ApprovalKindTool and ApprovalKindPlan keep ordinary approval kinds explicit.
const (
	ApprovalKindTool = "tool"
	ApprovalKindPlan = "plan"
)

// ToEventApproval builds the event payload for a recovery confirmation card.
func ToEventApproval(id string, pending PendingProposal, failure *FailureEvent) event.Approval {
	rec := &event.RecoveryApproval{
		SourceAgent:     pending.SourceAgent,
		FailedTool:      "",
		FailedSummary:   pending.Failure,
		Diagnosis:       pending.Diagnosis,
		NextTool:        pending.Tool,
		NextAction:      firstNonEmpty(pending.Proposed, pending.Subject, pending.Preview),
		ChangeKind:      string(pending.ChangeKind),
		ChangeRationale: pending.Rationale,
		ReviewRationale: pending.Rationale,
		PlanBefore:      pending.PlanBefore,
		PlanAfter:       pending.PlanAfter,
		CanGrantTask:    pending.TaskGrantKey != "",
		TaskGrantScope:  pending.TaskGrantDisplay,
	}
	if failure != nil {
		rec.FailedTool = failure.Tool
		if rec.FailedSummary == "" {
			rec.FailedSummary = failure.ErrSummary
		}
		if rec.SourceAgent == "" {
			rec.SourceAgent = failure.SourceAgent
		}
	}
	subject := firstNonEmpty(pending.Subject, pending.Preview, pending.Tool)
	reason := firstNonEmpty(pending.Rationale, pending.Diagnosis, "Plan change requires confirmation")
	return event.Approval{
		ID:       id,
		Tool:     pending.Tool,
		Subject:  subject,
		Reason:   reason,
		Fresh:    true,
		Kind:     ApprovalKindRecovery,
		Recovery: rec,
	}
}

// Observation aliases keep call sites readable when bridging agent types.
type Observation = agent.RecoveryObservation
type Proposal = agent.RecoveryProposal
type Decision = agent.RecoveryDecision
type Action = agent.RecoveryAction

const (
	ActionContinue     = agent.RecoveryActionContinue
	ActionContinueTask = agent.RecoveryActionContinueTask
	ActionRevise       = agent.RecoveryActionRevise
)

// DefaultReviseFeedback is injected when the user chooses "try another approach"
// without optional free-text feedback.
const DefaultReviseFeedback = "The pending mutation was rejected. Do not retry the same action. Summarize the failure cause, narrow the scope, and propose a safer alternative before attempting another mutation."

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
