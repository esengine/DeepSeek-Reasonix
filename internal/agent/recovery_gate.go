package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"reasonix/internal/evidence"
)

// RecoveryGate is the host-side Auto Guard consulted by the agent around tool
// execution. It is independent of the permission Gate and of
// how the Controller surfaces confirmations (desktop card, bot prompt, headless
// blocker). A nil gate means the feature is off for this agent.
//
// ObserveResult runs after a tool result is produced. BeforeMutation also checks
// host-observed structured plan transitions; it runs after call resolution and
// mutation classification, and before permission approval and workspace
// write-lock acquisition, so a waiting decision never holds a write lease.
type RecoveryGate interface {
	// ObserveResult records a completed call and returns optional guidance for
	// the same agent's active turn. The caller, not the gate, owns delivery so a
	// root or sub-agent failure can never start a concurrent controller turn.
	ObserveResult(ctx context.Context, result RecoveryObservation) string
	BeforeMutation(ctx context.Context, proposal RecoveryProposal) (RecoveryDecision, error)
}

// RecoveryEpisodeControl is an optional Gate capability for host-owned Episode
// rotation, generation stamping, and turn-stop finalization. Controllers and
// the live recovery.Gate implement it; simple test doubles may omit it.
type RecoveryEpisodeControl interface {
	EpisodeID() string
	Generation() uint64
	BeginEpisode()
	EpisodeStopped(taskID string) bool
	MarkFinalizationOffered(taskID string)
	// ConsumeFinalization marks the one-shot finalization round as used when it
	// was already offered. Returns (offered, alreadyConsumed).
	ConsumeFinalization(taskID string) (offered, alreadyConsumed bool)
	// OnModeChange rotates Episode/generation on a real mode change and returns
	// dismissed recovery approval ids. Same-value replays are no-ops.
	OnModeChange(mode string) []string
}

// RecoveryObservation is one finished tool call the checkpoint may react to.
type RecoveryObservation struct {
	// AgentID identifies the agent that produced the result (root or sub-agent).
	// Empty means the root agent.
	AgentID string
	// TaskID isolates recovery state across concurrent top-level tasks.
	TaskID string
	// TaskScopeID is the host-owned Goal/task-grant scope that produced this
	// result. Ordinary user turns get a fresh scope; goal continuations reuse
	// one. It is independent of EpisodeID (failure/reviewer budgets).
	TaskScopeID string
	// EpisodeID is the host-owned temporary Recovery Episode. It is never
	// model-supplied and never persisted. Failure/reviewer/stop budgets key on it.
	EpisodeID string
	// Generation is the gate generation captured before tool execution. Stale
	// observations from an older generation are ignored after mode switches.
	Generation uint64
	// Tool is the permission/evidence name used for the call.
	Tool string
	// Args are the resolved arguments for the call.
	Args json.RawMessage
	// Subject is a short human-readable subject (command, path, MCP action).
	Subject string
	// ReadOnly is true when the host classified the call as non-mutating.
	ReadOnly bool
	// Mutates is true when the host classifies the call as state-changing.
	Mutates bool
	// Verification is true when the host recognizes a verification command
	// (test/lint/build/typecheck/compile or project check).
	Verification bool
	// Success is true when the tool completed without error.
	Success bool
	// Blocked is true when a host policy blocked the call before execution
	// (permission deny, plan mode, delivery gate, user rejection). These do not
	// activate recovery.
	Blocked bool
	// UserRejected is true when the user actively declined an approval prompt.
	UserRejected bool
	// ProviderError is true for transport/provider failures handled by the
	// existing retry path; they do not activate recovery.
	ProviderError bool
	// Cancelled is true for context cancellation / user cancel.
	Cancelled bool
	// EmptySearch is true for a successful empty search result (no matches).
	// It must not activate recovery.
	EmptySearch bool
	// ErrSummary is a short error summary for diagnosis cards.
	ErrSummary string
	// Output is a bounded tool output excerpt for diagnosis context.
	Output string
}

// RecoveryProposal is the next candidate action Auto Guard may classify.
type RecoveryProposal struct {
	AgentID string
	TaskID  string
	// TaskScopeID is a host-owned Goal/task-grant scope. Goal continuations
	// reuse their delivery scope; ordinary runs get a unique turn scope. It
	// never comes from model output and is independent of Episode budgets.
	TaskScopeID string
	// EpisodeID is the host-owned Recovery Episode (optional on proposal; the
	// gate uses its live Episode when empty).
	EpisodeID string
	// TaskSummary is the bounded task text for the agent proposing the action.
	// Sub-agents must carry their own task instead of borrowing the root
	// controller session's latest user message.
	TaskSummary  string
	Tool         string
	Args         json.RawMessage
	Subject      string
	Preview      string
	ReadOnly     bool
	Mutates      bool
	Verification bool
	// PlanTransition marks a structural rewrite of an already-active task plan.
	// The host derives it from canonical todo state; it is never model-asserted.
	PlanTransition bool
	// PlanBefore and PlanAfter are bounded, human-readable snapshots supplied to
	// the isolated reviewer. They are internal evidence, not persisted wire state.
	PlanBefore string
	PlanAfter  string
	// SafeRetry is true when the host can prove this is a same-strategy
	// verification/idempotent retry (e.g. re-running the same test command).
	SafeRetry bool
	// HighRisk is retained as reviewer evidence and for compatibility helpers.
	// Auto does not turn execution risk into a user decision; permission,
	// sandbox, and tool-specific policy own that boundary.
	HighRisk bool
	// ExpandedScope marks a write range wider than the failed event's range.
	ExpandedScope bool
	// StrategyChanged marks an explicit tool/method change vs the failed call.
	StrategyChanged bool
}

// RecoveryDecision is the host's decision for a proposed mutation.
type RecoveryDecision struct {
	// Allow continues without a user card.
	Allow bool
	// AuthorizePlanReplacement grants this one todo_write call permission to
	// replace the current in_progress step. Only the active Auto Gate may issue
	// it after reviewing a host-detected structural plan transition; it is never
	// derived from model arguments or persisted beyond the call.
	AuthorizePlanReplacement bool
	// Blocked is true when the mutation must not run (reviewer/user revise, or
	// headless blocker). Message is fed back to the model.
	Blocked bool
	// Message is model-facing text when Blocked is true.
	Message string
	// Generation is the gate generation that authorized or blocked this call.
	// Tool results must carry the same generation for ObserveResult.
	Generation uint64
	// StopTurn means the Recovery Episode execution budget is exhausted; the
	// current batch stops and the agent gets one summarize-only finalization.
	// Host-proven read-only diagnosis may be admitted before that final stop.
	StopTurn bool
	// StopReason is an internal classifier (episode_failures, review_rejects, …).
	// User-facing surfaces must not expose it.
	StopReason string
}

// RecoveryAction is the user decision for a recovery confirmation card.
type RecoveryAction string

const (
	RecoveryActionContinue     RecoveryAction = "continue"
	RecoveryActionContinueTask RecoveryAction = "continue_task"
	RecoveryActionRevise       RecoveryAction = "revise"
)

func (a *Agent) observeRecoveryResult(ctx context.Context, toolName string, args json.RawMessage, readOnly, mutates bool, result string, err error, blocked, userRejected bool, generation uint64) {
	if a == nil || a.recoveryGate == nil {
		return
	}
	verification := toolName == "bash" && evidence.IsDeliveryVerificationCommand(bashCommandFromArgs(args))
	success := err == nil && !blocked
	emptySearch := false
	if success && readOnly {
		emptySearch = recoveryEmptySearch(toolName, result)
	}
	errSummary := ""
	if err != nil {
		errSummary = firstLine(err.Error())
	} else if blocked {
		errSummary = firstLine(result)
	}
	// A tool may own a shorter internal deadline while the parent turn remains
	// active (for example an MCP call timeout). That is a qualifying transient
	// execution failure, not a user cancellation. Parent context state remains
	// the source of truth for turn cancellation/deadline; a direct Canceled
	// result is still treated as cancellation for adapters that return it first.
	cancelled := errors.Is(err, context.Canceled)
	if ctx != nil && ctx.Err() != nil {
		cancelled = true
	}
	episodeID := ""
	if ctrl, ok := a.recoveryGate.(RecoveryEpisodeControl); ok {
		episodeID = ctrl.EpisodeID()
		if generation == 0 {
			generation = ctrl.Generation()
		}
	}
	guidance := a.recoveryGate.ObserveResult(ctx, RecoveryObservation{
		AgentID:      a.recoveryAgentID,
		TaskID:       a.recoveryTaskID,
		TaskScopeID:  recoveryTaskScopeID(a.deliveryScopeID, a.recoveryRunSeq.Load()),
		EpisodeID:    episodeID,
		Generation:   generation,
		Tool:         toolName,
		Args:         args,
		Subject:      recoverySubject(toolName, args),
		ReadOnly:     readOnly,
		Mutates:      mutates,
		Verification: verification,
		Success:      success,
		Blocked:      blocked,
		UserRejected: userRejected,
		Cancelled:    cancelled,
		EmptySearch:  emptySearch,
		ErrSummary:   errSummary,
		Output:       result,
	})
	if strings.TrimSpace(guidance) != "" {
		// Tool execution happens inside Agent.Run, so this targets the exact root
		// or sub-agent turn that failed. Never fall back to Controller.Steer here:
		// synchronous headless Run does not participate in controller admission,
		// and a fallback would start a second Agent.Run concurrently.
		_ = a.Steer(guidance)
	}
}

func (a *Agent) recoveryEpisodeControl() RecoveryEpisodeControl {
	if a == nil || a.recoveryGate == nil {
		return nil
	}
	ctrl, _ := a.recoveryGate.(RecoveryEpisodeControl)
	return ctrl
}

func bashCommandFromArgs(args json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return ""
	}
	raw, ok := fields["command"]
	if !ok {
		return ""
	}
	var cmd string
	if err := json.Unmarshal(raw, &cmd); err != nil {
		return ""
	}
	return strings.TrimSpace(cmd)
}

func recoverySubject(toolName string, args json.RawMessage) string {
	// Prefer command/path fields for readable cards.
	if toolName == "bash" {
		if cmd := bashCommandFromArgs(args); cmd != "" {
			return cmd
		}
	}
	if len(args) > 0 {
		var fields map[string]any
		if err := json.Unmarshal(args, &fields); err == nil {
			for _, key := range []string{"path", "file_path", "file", "target", "command", "query", "pattern"} {
				if v, ok := fields[key].(string); ok && strings.TrimSpace(v) != "" {
					return strings.TrimSpace(v)
				}
			}
		}
	}
	return strings.TrimSpace(toolName)
}

func recoveryEmptySearch(toolName, output string) bool {
	switch strings.TrimSpace(toolName) {
	case "grep", "glob", "ls", "code_index", "codeindex":
	default:
		return false
	}
	out := strings.TrimSpace(output)
	if out == "" {
		return true
	}
	lower := strings.ToLower(out)
	for _, marker := range []string{"no matches", "no files found", "0 matches", "not found", "no results"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func boundedRecoveryTaskSummary(task string) string {
	task = strings.TrimSpace(task)
	const maxRunes = 800
	runes := []rune(task)
	if len(runes) <= maxRunes {
		return task
	}
	return string(runes[:maxRunes]) + "…"
}
