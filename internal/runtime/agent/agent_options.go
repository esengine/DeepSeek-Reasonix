// agent_options.go — everything one Agent is constructed with.
package agent

import (
	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/extension/dispatch"
	"reasonix/internal/runtime/capability"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/instruction"
	"reasonix/internal/state/memory"
	"reasonix/internal/state/workspacelease"
	"reasonix/internal/tools/jobs"
)

// Options configures an Agent.
type Options struct {
	MaxSteps int
	// MaxStepsKey names the explicit runtime control shown when the MaxSteps guard
	// is hit. Empty defaults to the generic max_steps tool/runtime parameter.
	MaxStepsKey string
	// ReasoningByteLimit bounds the hidden reasoning bytes one stream retains;
	// it never stops generation. Zero uses the default; negative retains all.
	// Provider output budgets are a separate protocol/model capability.
	ReasoningByteLimit int
	// MaxOutputTokens overrides the provider's configured/default total output
	// budget. Zero delegates to the provider; a negative value asks optional
	// protocols to omit the budget (Anthropic still requires max_tokens).
	MaxOutputTokens int
	Temperature     float64
	// TaskBudget bounds a task's spend; zero uses DefaultTaskBudget.
	TaskBudget  TaskBudget
	Pricing     *provider.Pricing // optional, for per-turn cost display
	UsageSource string            // optional billable usage source; default executor
	// TriageProvider answers the host's small classifications (an unrecognized
	// shell command's read-only status, so far). nil leaves the static verdict
	// standing rather than borrowing the turn's own provider mid-conversation.
	TriageProvider provider.Provider
	// TriageModelRef and TriagePricing describe TriageProvider, so a
	// classification is reported as what it was and priced at the tier it ran
	// on rather than the turn's.
	TriageModelRef string
	TriagePricing  *provider.Pricing
	// ScreenExternalContent sends external tool results past TriageProvider for
	// an advisory prompt-injection verdict. Without a triage provider it is off.
	ScreenExternalContent bool
	// ModelRef names the canonical "provider/model" ref backing this agent's
	// provider instance. It is attached to emitted Usage events so downstream
	// usage accounting can attribute tokens to the exact model.
	ModelRef string
	// EventSource names the producer of this agent's settled frames — executor
	// unless a caller says otherwise, so a two-model stream says which model
	// wrote each one rather than leaving it to be inferred.
	EventSource string
	// RequireVisibleFinal makes internal callers reject reasoning-only responses.
	RequireVisibleFinal bool
	// Gate is the per-call permission gate. nil disables gating.
	Gate Gate
	// ReadOnlyExecution enables a permanent host-side read-only boundary for
	// planner and research agents. It is intentionally independent of Plan mode
	// so a stale collaboration flag cannot authorize a dynamic writer target.
	ReadOnlyExecution bool
	// PlannerMCPExecution enables Planner-trusted MCP through use_capability:
	// authorized, non-destructive tools may run without readOnlyHint. Only
	// NewPlannerAgent sets this; strict read-only sub-agents must not.
	PlannerMCPExecution bool

	// SandboxEscapeApprover confirms a one-shot unconfined shell rerun after an
	// enforced OS sandbox fails. nil keeps fail-closed behavior.
	SandboxEscapeApprover sandbox.EscapeApprover

	// ConfigWriteApprover confirms file-tool writes to Reasonix-managed config
	// files outside the workspace roots. nil keeps fail-closed behavior.
	ConfigWriteApprover tool.ConfigWriteApprover

	// Context management. ContextWindow <= 0 disables compaction. Ratios and
	// RecentKeep fall back to defaults when unset.
	ContextWindow     int
	CompactRatio      float64
	CompactionBudgets CompactionBudgets
	// Deprecated compatibility inputs. New agents ignore these fields; automatic
	// maintenance is controlled only by CompactRatio.
	RecentKeep             int
	ArchiveDir             string
	KeepPolicy             KeepPolicy
	SessionPath            string // projection sidecar path; empty = memory only
	WorkspaceID            string // prompt-cache lineage component
	StrictAlternatingRoles bool   // merge adjacent user turns for strict providers at request time
	ContextEditing         string // deprecated; native provider editing was removed

	// Hooks fires PreToolUse / PostToolUse shell hooks around tool calls. nil
	// disables hook firing.
	Hooks ToolHooks

	// MissingReasoningWarnStateDir, when non-empty, points at the shared
	// directory where missing tool-call thinking recovery retries are gated by
	// opaque provider-configuration fingerprint (#7059). The field name is kept
	// for source compatibility. Boot always supplies it; direct construction
	// with an empty value keeps in-memory gating.
	MissingReasoningWarnStateDir string

	// Jobs is the session's background-job manager (nil disables background tools).
	Jobs *jobs.Manager
	// MemoryQueue optionally gives a child agent an explicitly owned live-memory
	// queue. When nil, child construction shadows inherited queues.
	MemoryQueue memory.Queue

	// WriteScheduler is the session-scoped subagent concurrency/write-claim
	// controller. When set on the parent executor, write-capable tools reserve
	// paths for the duration of Execute so background writers cannot TOCTOU
	// race parent writes. Subagents leave this nil (or depth > 0 skips it).
	WriteScheduler *writeclaim.SubagentScheduler
	// WriteWorkspaceRoot normalizes parent write reservations.
	WriteWorkspaceRoot string
	// RenderRoot is the workspace a written page or image is opened from to look
	// at it. Empty when this agent has no browser or its model cannot see one.
	RenderRoot string
	// WorkspaceVCS names the workspace's version control ("" for none) for
	// the turn block. Resolved once by the host, never probed per turn.
	WorkspaceVCS string

	// WorkspaceLease serializes Delivery mutations across sessions that target
	// the same workspace. nil preserves source compatibility for direct Agent
	// construction; boot always supplies it for Delivery sessions.
	WorkspaceLease *workspacelease.Owner

	// ProjectChecks are host-observable structured checks extracted during boot.
	ProjectChecks         []instruction.VerifyCheck
	ProjectSensitivePaths []string

	// DeliveryProfile enforces acceptance criteria before mutations and requires
	// post-change review, verification, and evidence-backed sign-off before a
	// final answer. It changes host control flow, not tool schemas.
	// Deprecated: prefer AgentPreset + turn TaskPolicy. Still honored when
	// AgentPreset is empty for one compatibility version of direct constructors.
	DeliveryProfile bool

	// AgentPreset is the session role setting (balanced|delivery). Empty
	// falls back to balanced unless DeliveryProfile is true (then delivery).
	// Switching the preset mid-session does not rebuild the agent; the value is
	// frozen at turn admission into TaskPolicy.
	AgentPreset string

	// Ablation switches subsystems off for a benchmark arm. The zero value runs
	// everything, so ordinary callers leave it unset.
	Ablation ablation.Set

	// ClassifierTaskText, when non-empty, is the pristine task text, set by
	// sub-agent spawners before host framing is prepended. Delivery intent
	// classification judges it instead of the raw Run input, so framing verbs
	// cannot arm expectations; the delegation audit scores evidence origin
	// against it, so only locations the parent wrote itself count as hints.
	ClassifierTaskText     string
	ExpectCompletionReport bool   // set where the contract is appended, never inferred
	HandoffEntrance        string // delegation path, for telemetry only

	// CapabilityLedger is the optional turn-scoped capability route ledger for
	// Delivery require/prefer gates. Nil disables capability gates.
	CapabilityLedger *capability.Ledger
	// CapabilityAudit is the optional non-persisted metrics sink for routing.
	CapabilityAudit *capability.Audit

	// RequireReviewReportKind, when non-empty, makes RunSubAgentWithSession fail
	// unless the subagent recorded a successful review_report of this kind —
	// review/security subagents must return typed, host-verifiable reports.
	RequireReviewReportKind evidence.ReviewKind

	// ReasoningLanguage controls visible reasoning language preference as transient
	// user-turn context. Empty/auto injects nothing.
	ReasoningLanguage string

	// ResponseLanguage controls final-answer language preference as transient
	// user-turn context. Empty/auto keeps the stable same-as-user policy.
	ResponseLanguage string

	// RecoveryGate is the optional Auto Guard boundary. It checks deterministic
	// high-risk mutations and failure recovery before permission approval and
	// write-lock acquisition.
	RecoveryGate RecoveryGate
	// RecoveryAgentID labels this agent on recovery cards (empty = root).
	RecoveryAgentID string
	// RecoveryTaskID isolates recovery state for this agent (empty = root task).
	RecoveryTaskID string

	// SubagentDepth is the current nesting depth for this agent. Root sessions are
	// depth 0; child subagents are depth 1. MaxSubagentDepth caps delegation.
	SubagentDepth    int
	MaxSubagentDepth int

	// Extensions is the frozen extension dispatcher for this agent's controller
	// generation (Extension Protocol v2). Nil means no runtime packages are
	// installed; the run loop then passes every intercept point through
	// byte-identically. Boot installs it with SetExtensions once sidecars are
	// live (they start after the agent is constructed).
	Extensions *dispatch.Dispatcher

	// MutationObserver is the host-side file mutation observer shared with
	// (or cloned for) sub-agents. nil disables v2 capture. Does not affect
	// provider-visible tool schemas or prompts.
	MutationObserver *checkpoint.MutationObserver
}
