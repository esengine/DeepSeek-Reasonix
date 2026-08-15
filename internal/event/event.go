// Package event defines the typed event stream the agent emits as it runs a
// turn, and the Sink it emits to. It decouples "what happened" (the model
// produced reasoning, a tool was dispatched, a turn used N tokens) from "how to
// show it" (ANSI scrollback in a terminal, a card in a webview).
//
// The agent depends only on Sink; each frontend implements one. The chat TUI
// renders events to its scrollback; a headless run renders them to plain ANSI
// on stdout; a future GUI/serve transport forwards them to a webview or
// websocket. This replaces the old io.Writer contract, where the agent wrote
// pre-formatted ANSI and the consumer had to re-derive structure by matching
// line prefixes — fragile, and lossy for any frontend richer than a terminal.
package event

import (
	"reasonix/internal/billing"
	"reasonix/internal/evidence"
	"reasonix/internal/nilutil"
	"reasonix/internal/provider"
)

// Kind tags an Event. Read the field(s) documented for that kind.
type Kind int

const (
	// TurnStarted marks the start of one top-level Run (one user turn). Sinks
	// reset any per-turn rendering state on it. Carries no payload.
	TurnStarted Kind = iota
	// Reasoning is a thinking-mode reasoning delta (Text). Streamed before the
	// visible answer; sinks typically render it muted under a "thinking" header.
	Reasoning
	// Text is an answer-text delta (Text).
	Text
	// Message marks the assistant turn's text as complete: Text holds the full
	// answer and Reasoning the full chain-of-thought (both already streamed via
	// the deltas above). A sink may use it to re-render the streamed raw text as
	// styled markdown; a plain sink can ignore it.
	Message
	// ToolDispatch announces a tool call is about to run (Tool: ID/Name/Args/ReadOnly).
	ToolDispatch
	// ToolResult reports a finished tool call (Tool: Output/Err/Truncated set).
	ToolResult
	// Usage carries per-turn token telemetry (Usage; Pricing optional, for cost).
	Usage
	// Notice is an out-of-band message — a warning, truncation, block, or
	// compaction notice (Level + Text).
	Notice
	// Phase marks a coordinator boundary, e.g. planner→executor handoff (Text =
	// label such as "deepseek · planning").
	Phase
	// ApprovalRequest asks the frontend to approve a pending tool call
	// (Approval: ID/Tool/Subject). The run blocks until the controller's
	// Approve(ID, …) resolves it; a frontend shows a prompt and answers.
	ApprovalRequest
	// AskRequest asks the frontend to put one or more structured multiple-choice
	// questions to the user (Ask: ID + Questions). The run blocks until the
	// controller's AnswerQuestion(ID, …) resolves it. Powers the `ask` tool.
	AskRequest
	// TurnDone marks the end of one top-level Run (Err non-nil on failure;
	// nil also for a user cancellation, which is not an error). Always the
	// last event of a turn.
	TurnDone
	// CompactionStarted marks the start of a context-compaction pass (Compaction
	// payload: Trigger). A frontend shows a "compacting…" placeholder while the
	// summarizer runs; CompactionDone replaces it. Mirrors ToolDispatch/ToolResult.
	CompactionStarted
	// CompactionDone reports a finished compaction pass (Compaction payload:
	// Trigger/Messages/Summary/Archive). An aborted pass emits this with an empty
	// Summary so the placeholder still resolves. Replaces the older plain Notice
	// so a sink can render a distinct, expandable card.
	CompactionDone
	// ToolProgress streams a chunk of a still-running tool's combined output
	// (Tool: ID + Output = the new chunk). Emitted between ToolDispatch and
	// ToolResult for long tools like bash so a frontend can show live progress.
	// Appended last to keep the Kind values before it wire-stable.
	ToolProgress
	// MCPSurfaceReady fires once per server when its background-loaded surface
	// (prompts or resources) finishes after startup. Lets UIs refresh /mcp
	// status without polling. Text carries "<server>: <surface> ready (<count>
	// items)". Appended last to keep the Kind values before it wire-stable.
	MCPSurfaceReady
	// Retrying fires before each backoff sleep while the provider re-attempts the
	// connection+header phase after a transient failure (RetryAttempt of RetryMax).
	// A frontend shows a transient "retrying (n/m)" indicator that the next stream
	// event — or TurnDone — clears. Appended last to keep the Kind values before
	// it wire-stable.
	Retrying
	// Steer fires when a mid-turn steer message is consumed from the queue and
	// injected as a user message. Text carries the raw steer content (without the
	// wrapper prefix), so a frontend can display it to the user as confirmation.
	// Frontends use Steer to know a queued message has been delivered.
	Steer
	// GuardianAssessment reports the outcome of a guardian sub-agent safety review.
	// Carries GuardianResult payload (Outcome, RiskLevel, Rationale, etc.).
	GuardianAssessment
	// ExtensionSurface carries a structured UI surface published by an extension
	// sidecar (Extension payload with one of the Card/Form/Notification
	// sub-structs set). Appended last to keep the Kind values before it
	// wire-stable.
	ExtensionSurface
	// ExtensionStatus carries a one-line status contribution published by an
	// extension sidecar (Extension payload with Status set). Appended last to
	// keep the Kind values before it wire-stable.
	ExtensionStatus
	// StreamAttempt marks the local lifecycle of one sampling attempt within a
	// model round (StreamAttempt payload: begin | discard | commit). IDs are
	// host-local only — never persisted or sent to the model. Appended last to
	// keep earlier Kind values wire-stable; older clients ignore unknown kinds.
	StreamAttempt
	// ContextMaintenance reports a free tool-result maintenance or a durable
	// blocked/noop outcome. It is separate from CompactionStarted/Done so UIs do
	// not render a paid-summary card for a cache-preserving view update.
	ContextMaintenanceEvent
	// WorkspaceChanged reports a debounced host-side workspace mutation.
	WorkspaceChanged
	// TurnPhase reports a host-side work phase for the active turn (working |
	// checking | verifying | reviewing). Content-free; Text holds the phase.
	TurnPhase
	// CompletionSummary reports a content-free end-of-turn quality summary for
	// role-setting strategies (preset, verdict, check counts, review status).
	CompletionSummary
	// KindCount is a sentinel one past the last real Kind. New event kinds must
	// be inserted above it so completeness tests cover them automatically.
	KindCount
)

// TurnPhaseName is the machine-readable phase on TurnPhase events.
type TurnPhaseName string

const (
	TurnPhaseWorking   TurnPhaseName = "working"
	TurnPhaseChecking  TurnPhaseName = "checking"
	TurnPhaseVerifying TurnPhaseName = "verifying"
	TurnPhaseReviewing TurnPhaseName = "reviewing"
)

// CompletionSummaryInfo is the content-free quality summary on CompletionSummary
// events. It never carries user prompts, file contents, command args, or
// reviewer reasoning.
type CompletionSummaryInfo struct {
	Preset             string // deprecated wire-compat label; pinned to "balanced"
	Verdict            string // complete | partial | blocked | continue
	Mutations          int
	ChecksPassed       int
	ChecksFailed       int
	ChecksSuppressed   int
	Review             string // none | passed | warned | failed | unavailable
	GapKinds           []string
	ConstraintDegraded bool
}

// StreamAttemptAction is the lifecycle phase of a local sampling attempt.
type StreamAttemptAction string

const (
	StreamAttemptBegin   StreamAttemptAction = "begin"
	StreamAttemptDiscard StreamAttemptAction = "discard"
	StreamAttemptCommit  StreamAttemptAction = "commit"
)

// RetryScope distinguishes connection+header retries from body-phase stream
// retries. Older clients ignore the empty/unknown value.
type RetryScope string

const (
	RetryScopeHeaders RetryScope = "headers"
	RetryScopeStream  RetryScope = "stream"
)

// StreamAttemptInfo carries host-local bookkeeping for one sampling attempt.
// Reason is a fixed enum (connection_reset | premature_eof | idle_timeout).
type StreamAttemptInfo struct {
	ID      string
	Action  StreamAttemptAction
	Attempt int // 1-based attempt number
	Max     int // total attempts including the first (typically 6)
	Reason  string
}

const TurnOutcomeFinalReadiness = "final_readiness"

// TurnOutcomeRecoveryPaused marks an Auto recovery Episode budget stop. New
// clients show an informational status (not send-failed); older clients still
// read Err text and ignore the unknown outcome.
const TurnOutcomeRecoveryPaused = "recovery_paused"

// Level classifies a Notice so sinks can style or filter it.
type Level int

const (
	LevelInfo Level = iota
	LevelWarn
)

// NoticeAudience separates a notice's recipient from its severity. The empty
// default preserves the existing contract: ordinary notices are eligible for
// every frontend. Operator notices describe local runtime maintenance and must
// not be forwarded as end-user chat messages. Local frontends and diagnostics
// remain free to surface or quietly record them under their own policy.
type NoticeAudience string

const (
	NoticeAudienceDefault  NoticeAudience = ""
	NoticeAudienceOperator NoticeAudience = "operator"
)

// Profile carries the subagent model/effort resolved for this call.
type Profile struct {
	Model  string
	Effort string
}

// Tool describes a tool call for ToolDispatch / ToolResult events. On dispatch
// ID/Name/Args/ReadOnly and optional preview metadata are set; on result
// Output/Err/Truncated are filled in. Args is the raw JSON arguments — a sink
// compacts it for display.
type Tool struct {
	ID   string
	Name string
	Args string
	// ResolvedName/CapabilityID describe the real target behind a stable proxy
	// while Name/Args remain the provider-visible call. They are optional local
	// display metadata and never enter provider requests.
	ResolvedName string
	CapabilityID string
	Output       string // ToolResult: the result text fed to the model
	Err          string // ToolResult: non-empty when the call failed or was blocked
	ReadOnly     bool
	Truncated    bool  // ToolResult: Output was head+tailed before display/model
	DurationMs   int64 // ToolResult: wall-clock execution time in milliseconds
	// StartedAt/EndedAt are unix-millisecond execution bounds (ToolResult).
	// Zero when the call never ran (dependency-skipped, cancelled, synthetic).
	StartedAt int64
	EndedAt   int64
	// Partial marks an early ToolDispatch emitted when a call begins (ID/Name set,
	// Args still streaming) so a frontend can show the card immediately; a second,
	// full ToolDispatch (Partial false, Args set) follows when the call completes.
	Partial bool
	// ArgChars is the cumulative argument characters received so far for a
	// Partial dispatch — a liveness signal while a large payload streams. Zero
	// on the initial start dispatch and on full dispatches.
	ArgChars int
	// Refreshed marks a repeated full ToolDispatch for the same ID whose file
	// preview or resolved proxy metadata changed after the initial dispatch.
	// Frontends that can upsert by ID should replace the existing card;
	// append-only sinks should ignore it to avoid duplicate tool cards.
	Refreshed bool
	// ParentID, when set, is the ID of the tool call that spawned this one — a
	// sub-agent's calls carry the parent `task` call's ID so a frontend can nest
	// them under it. Empty for top-level calls.
	ParentID string
	// AttemptID is the host-local stream_attempt id that produced a speculative
	// partial ToolDispatch. Empty for committed/full dispatches and for nested
	// sub-agent tools. Frontends must only journal partial events whose
	// AttemptID matches the active stream_attempt begin.
	AttemptID string
	FileDiff
	Profile *Profile // ToolDispatch: subagent model/effort (set for task/skill calls)
	// Execution is optional local shell metadata (ToolResult). Never sent to
	// model providers; omitempty keeps old wire readers compatible.
	Execution *ShellExecution
	// Workspace mutation metadata is host-only and is omitted from eventwire.
	WorkspaceMutation bool
	WorkspacePaths    []string
	WorkspaceAllPaths bool
}

// ShellExecution mirrors tool.ShellExecution for event sinks without importing
// the tool package (event is a lower-level dependency of tool consumers).
type ShellExecution struct {
	Kind           string `json:"kind,omitempty"`
	Shell          string `json:"shell,omitempty"`
	ShellVersion   string `json:"shellVersion,omitempty"`
	Platform       string `json:"platform,omitempty"`
	SupportsAndAnd bool   `json:"supportsAndAnd"`
	State          string `json:"state,omitempty"`
	FailurePhase   string `json:"failurePhase,omitempty"`
	ExitCode       *int   `json:"exitCode,omitempty"`
	OutputTail     string `json:"outputTail,omitempty"`
	MutationRisk   string `json:"mutationRisk,omitempty"`
	Verification   string `json:"verification,omitempty"`
	DurationMs     int64  `json:"durationMs,omitempty"`
}

// FileDiff is a previewed change carried on a writer tool's full ToolDispatch
// and on its ApprovalRequest, so a frontend can render +/- lines before the
// call runs. Diff is the unified diff (empty for read-only tools, binary files,
// or no-op changes); Added/Removed are its line tallies.
type FileDiff struct {
	Diff    string
	Added   int
	Removed int
}

// AskOption is one choice the user can pick for an AskQuestion.
type AskOption struct {
	Label       string
	Description string // optional one-line explanation shown under the label
}

// AskQuestion is one structured question the `ask` tool puts to the user.
type AskQuestion struct {
	ID      string // stable per-question id, so answers correlate back
	Header  string // short label (the tab title)
	Prompt  string // the question text
	Options []AskOption
	Multi   bool // allow selecting more than one option
}

// Ask carries an AskRequest: a batch of questions and the ID that correlates the
// controller's AnswerQuestion(ID, …) reply.
type Ask struct {
	ID        string
	Questions []AskQuestion
}

// Extension surface kind values carried by ExtensionSurfacePayload.Kind. They
// mirror the extension protocol's structured surface kinds; "request" is
// reserved for stage-8b request surfaces (stage 8a routes blocking prompts
// through the ordinary AskRequest channel instead).
const (
	ExtensionSurfaceStatus       = "status"
	ExtensionSurfaceCard         = "card"
	ExtensionSurfaceForm         = "form"
	ExtensionSurfaceNotification = "notification"
	ExtensionSurfaceRequest      = "request"
)

// ExtensionSurfacePayload carries one extension sidecar's structured UI
// contribution for the ExtensionSurface / ExtensionStatus kinds. The structs
// mirror the Extension Protocol v2 UI payload DTOs field-for-field so any
// frontend can render them with native widgets; the protocol stays
// structured-only (no HTML/CSS/JS/URLs). All user-visible strings are already
// credential-redacted by the host UI hub before the event is emitted. Exactly
// one sub-struct is set, selected by Kind.
type ExtensionSurfacePayload struct {
	PluginID     string
	SurfaceID    string
	SessionID    string
	Generation   uint64
	Kind         string // status | card | form | notification (request reserved)
	Status       *ExtensionStatusView
	Card         *ExtensionCardView
	Form         *ExtensionFormView
	Notification *ExtensionNotificationView
}

// ExtensionStatusView is a one-line status contribution (mirrors the
// protocol's UIStatusPayload).
type ExtensionStatusView struct {
	Label    string
	Detail   string
	Severity string // info | warn | error
	Progress *float64
}

// ExtensionKeyValue is one labelled value row in a card (mirrors UIKeyValue).
type ExtensionKeyValue struct {
	Key   string
	Value string
}

// ExtensionActionRef renders a button invoking a declared extension action
// (mirrors UIActionRef).
type ExtensionActionRef struct {
	ActionID string
	Label    string
}

// ExtensionCardView is a rich read-only surface (mirrors UICardPayload).
type ExtensionCardView struct {
	Title    string
	Markdown string
	Text     string
	Fields   []ExtensionKeyValue
	Progress *float64
	Actions  []ExtensionActionRef
}

// ExtensionFormField is one input row of a form surface (mirrors UIFormField).
type ExtensionFormField struct {
	Key      string
	Label    string
	Kind     string // confirm | input | select | multiselect
	Options  []string
	Default  any
	Required bool
}

// ExtensionFormView is an editable surface; submissions return to the
// extension through the UI hub (mirrors UIFormPayload).
type ExtensionFormView struct {
	Title   string
	Message string
	Fields  []ExtensionFormField
}

// ExtensionNotificationView is a transient toast-style message (mirrors
// UINotificationPayload).
type ExtensionNotificationView struct {
	Title    string
	Body     string
	Severity string // info | warn | error
}

// Compaction carries a context-compaction pass for the CompactionStarted /
// CompactionDone events. On CompactionStarted only Trigger is set. On
// CompactionDone, Messages/Summary/Archive are filled in (an aborted pass leaves
// Summary empty). Trigger is "auto" (the prompt reached the window threshold) or
// "manual" (the user ran /compact).
type Compaction struct {
	Trigger  string // "auto" | "manual"
	Messages int    // Done: how many messages were folded into the summary
	Summary  string // Done: the briefing the agent keeps relying on
	Archive  string // Done: path the dropped originals were archived to ("" if none)
}

// ContextMaintenance is the typed wire-safe receipt for snip/prune/noop/
// blocked operations. Transcript bytes are represented by hashes and counts.
type ContextMaintenance struct {
	Status              string `json:"status,omitempty"`
	Action              string `json:"action,omitempty"`
	Trigger             string `json:"trigger,omitempty"`
	OperationID         string `json:"operationId,omitempty"`
	InputTokens         int    `json:"inputTokens,omitempty"`
	ResultTokens        int    `json:"resultTokens,omitempty"`
	SavedTokens         int    `json:"savedTokens,omitempty"`
	AffectedToolResults int    `json:"affectedToolResults,omitempty"`
	ProjectionVersion   uint64 `json:"projectionVersion,omitempty"`
	CacheBreak          bool   `json:"cacheBreak,omitempty"`
	Reason              string `json:"reason,omitempty"`
}

// GuardianResult carries the outcome of a guardian sub-agent safety review.
// Emitted with Kind=GuardianAssessment after each review completes.
type GuardianResult struct {
	ID                string            // unique review id
	Tool              string            // tool being reviewed (e.g. "bash")
	Subject           string            // call subject (e.g. "rm -rf /tmp/build")
	Outcome           string            // "allow" | "deny"
	RiskLevel         string            // "low" | "medium" | "high" | "critical"
	UserAuthorization string            // "unknown" | "low" | "medium" | "high"
	Rationale         string            // one-sentence reason
	DurationMs        int64             // wall-clock review time
	Usage             *provider.Usage   // guardian review token telemetry
	Pricing           *provider.Pricing // for cost display (nil = omit cost)
}

// AskAnswer is the user's reply to one AskQuestion: the chosen option label(s)
// (a free-typed answer is carried as a single Selected entry).
type AskAnswer struct {
	QuestionID string
	Selected   []string
}

// CacheDiagnostics describes whether and why the cacheable prefix changed since
// the last turn. It rides on the Usage event so every frontend can show
// cache-churn attribution.
type CacheDiagnostics struct {
	PrefixHash          string
	PrefixChanged       bool
	PrefixChangeReasons []string // "system", "tools", "log_rewrite"
	SystemHash          string
	ToolsHash           string
	LogRewriteVersion   int
	ToolSchemaTokens    int
	CacheMissTokens     int
	CacheHitTokens      int
}

// FinalReadiness carries machine-readable recovery requirements on TurnDone.
// Missing values are stable category ids; user-facing detail stays localized in
// the frontend instead of scraping the diagnostic error string.
type FinalReadiness struct {
	Attempts int      `json:"attempts,omitempty"`
	Missing  []string `json:"missing,omitempty"`
}

const (
	UsageSourceExecutor         = "executor"
	UsageSourcePlanner          = "planner"
	UsageSourceSubagent         = "subagent"
	UsageSourceCompaction       = "compaction"
	UsageSourceClassifier       = "classifier"
	UsageSourceTitle            = "title"
	UsageSourceCapabilityRouter = "capability-router"
	UsageSourceRecoveryReviewer = "recovery-reviewer"
	UsageSourceGoalEvaluator    = "goal-evaluator"
)

// Event is one increment in a turn's event stream. Read the field(s) documented
// for Kind; the others are zero.
// Notice codes are stable machine-readable identifiers for known notices.
// Frontends localize a notice's main copy by Code and fall back to matching
// the English Text (or showing it raw) when Code is empty or unknown, so
// wording edits in Go no longer silently break localization. Values are
// wire-stable: never rename or reuse one once shipped.
const (
	NoticeCodeFinalReadiness                                    = "final_readiness"
	NoticeCodeEmptyFinal                                        = "empty_final"
	NoticeCodeExecutorHandoff                                   = "executor_handoff"
	NoticeCodeToolBudget                                        = "tool_budget"
	NoticeCodePromptQueued                                      = "prompt_queued"
	NoticeCodeLoopGuard                                         = "loop_guard"
	NoticeCodeProgressGuard                                     = "progress_guard"
	NoticeCodeEvidenceNudge                                     = "evidence_nudge"
	NoticeCodeReasoningGovernor                                 = "reasoning_governor"
	NoticeCodeWorkspaceLease                                    = "workspace_lease"
	NoticeCodeCancelledTurn                                     = "cancelled_turn_display"
	NoticeCodeUnappliedSteer                                    = "unapplied_steer"
	NoticeCodeSessionRecoveryForked                             = "session_recovery_forked"
	NoticeCodeSessionRecoveryAdopted                            = "session_recovery_adopted"
	NoticeCodeSessionRecoveryAdoptedCovered                     = "session_recovery_adopted_covered"
	NoticeCodeSessionRecoveryDepthCap                           = "session_recovery_depth_cap"
	NoticeCodeSessionShutdownRecoveryForked                     = "session_shutdown_recovery_forked"
	NoticeCodeDecisionReceipt, NoticeCodeContextEditingFallback = "decision_receipt", "context_editing_fallback"
)

type Event struct {
	Kind             Kind
	Text             string                    // Reasoning / Text / Message / Notice / Phase
	ModelRef         string                    // Usage: canonical "provider/model" ref that produced this usage
	Detail           string                    // Notice: optional diagnostic text for expandable details
	Code             string                    // Notice: stable id for frontend localization; empty = unmapped
	Reasoning        string                    // Message: the full reasoning chain
	MemoryCitations  []provider.MemoryCitation // Message: local memory references displayed by rich frontends
	Tool             Tool                      // ToolDispatch / ToolResult
	Usage            *provider.Usage           // Usage
	Pricing          *provider.Pricing         // Usage: rate card for quote middleware (nil = omit cost)
	CostQuote        *billing.CostQuote        // Usage: host-side quote; sinks must not reprice
	Source           string                    // optional display/event source (executor, planner, subagent, ...)
	UsageSource      string                    // Usage: billable call source; empty means executor for compatibility
	CacheDiagnostics *CacheDiagnostics         // Usage: cache-churn attribution (nil = N/A)
	// SessionHit/SessionMiss carry cumulative cache tokens across the whole
	// session (Usage events only), so a frontend can show the aggregate hit-rate
	// — which doesn't crater on a short turn or after compaction — alongside
	// Usage's single-turn numbers.
	SessionHit      int                      // Usage: cumulative cache-hit prompt tokens this session
	SessionMiss     int                      // Usage: cumulative cache-miss prompt tokens this session
	Level           Level                    // Notice
	Audience        NoticeAudience           // Notice: empty = ordinary frontend delivery; operator = no end-user chat forwarding
	Approval        Approval                 // ApprovalRequest
	Ask             Ask                      // AskRequest
	Extension       *ExtensionSurfacePayload // ExtensionSurface / ExtensionStatus (nil for every other kind)
	Err             error                    // TurnDone: non-nil on failure
	Cancelled       bool                     // TurnDone: Cancel was requested while the turn was active
	Outcome         string                   // TurnDone: optional machine-readable recoverable outcome
	Readiness       *FinalReadiness          // TurnDone: structured final-readiness recovery state
	Receipt         *CompletionReceipt       // TurnDone: what the host verified, and what it could not
	CheckpointTurn  *int                     // TurnDone: authoritative checkpoint for this turn's visible user message
	Compaction      Compaction               // Compaction
	Maintenance     *ContextMaintenance      // ContextMaintenanceEvent
	Guardian        GuardianResult
	DecisionReceipt *provider.DecisionReceipt // Notice: durable user decision receipt
	RetryAttempt    int                       // Retrying: 1-based attempt about to be made
	RetryMax        int                       // Retrying: total attempts before giving up
	RetryScope      RetryScope                // Retrying: optional "headers" | "stream"; empty for older emitters
	StreamAttempt   StreamAttemptInfo         // StreamAttempt lifecycle
	// ItemID correlates Steer / unapplied-steer / TurnDone with a durable
	// session-inbox entry. Empty for legacy callers that still use text only.
	ItemID    string
	Workspace *WorkspaceChangedPayload // WorkspaceChanged (host-local)
	// PhaseName is set on TurnPhase events (working|checking|verifying|reviewing).
	PhaseName TurnPhaseName
	// Completion is set on CompletionSummary events.
	Completion *CompletionSummaryInfo
}

type WorkspaceWatchState string

const (
	WorkspaceWatchActive      WorkspaceWatchState = "active"
	WorkspaceWatchDegraded    WorkspaceWatchState = "degraded"
	WorkspaceWatchUnavailable WorkspaceWatchState = "unavailable"
)

type WorkspaceRevision struct {
	Content     uint64 `json:"content"`
	Tree        uint64 `json:"tree"`
	WorkingTree uint64 `json:"workingTree"`
	GitMeta     uint64 `json:"gitMeta"`
	Session     uint64 `json:"session"`
}

type WorkspacePathChange struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	Op      string `json:"op"`
}

type WorkspaceChangedPayload struct {
	Revisions  WorkspaceRevision
	Changes    []WorkspacePathChange
	AllPaths   bool
	Source     string
	WatchState WorkspaceWatchState
}

// ReadinessAuditSink is an optional sink capability. Sinks that do not care
// about readiness audit receipts can implement only Sink and will ignore them.
type ReadinessAuditSink interface {
	RecordReadinessAudit(evidence.ReadinessAudit)
}

// TurnCompletionSink is an optional sink capability for synchronous controller
// entry points that do not publish a TurnDone UI event. It keeps accounting
// independent from frontend event lifecycles without synthesizing an event that
// transports may mistake for an interactive completion.
type TurnCompletionSink interface {
	RecordTurnCompletion()
}

// RecordTurnCompletion records one successfully admitted top-level controller
// run on sinks that opt into completion accounting.
func RecordTurnCompletion(s Sink) {
	if nilutil.IsNil(s) {
		return
	}
	if ts, ok := s.(TurnCompletionSink); ok {
		ts.RecordTurnCompletion()
	}
}

// RecordReadinessAudit forwards a readiness audit receipt to sinks that opt in.
func RecordReadinessAudit(s Sink, a evidence.ReadinessAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if rs, ok := s.(ReadinessAuditSink); ok {
		rs.RecordReadinessAudit(a)
	}
}

// ProtocolRecoveryKind is a content-free internal observation about a provider
// protocol repair. It is deliberately separate from Event/Notice so recovery
// stays invisible in chat transcripts and frontends do not need to understand
// provider implementation details.
type ProtocolRecoveryKind string

const (
	ProtocolRecoveryMissingReasoningDetected        ProtocolRecoveryKind = "missing_reasoning_detected"
	ProtocolRecoveryMissingReasoningRetryAttempted  ProtocolRecoveryKind = "missing_reasoning_retry_attempted"
	ProtocolRecoveryMissingReasoningRetryRecovered  ProtocolRecoveryKind = "missing_reasoning_retry_recovered"
	ProtocolRecoveryMissingReasoningRetryReplaced   ProtocolRecoveryKind = "missing_reasoning_retry_replaced_response"
	ProtocolRecoveryMissingReasoningRetrySuppressed ProtocolRecoveryKind = "missing_reasoning_retry_suppressed"
	ProtocolRecoveryMissingReasoningFallback        ProtocolRecoveryKind = "missing_reasoning_fallback_used"
)

type ProtocolRecoveryAudit struct {
	Kind ProtocolRecoveryKind
}

// ContractShadowAudit is the shadow task-contract's end-of-turn summary:
// counts and enums only, never requirement text. Shadow means observed, not
// enforced — the old control logic still decides behavior.
type ContractShadowAudit struct {
	Intent                string
	Requirements          int
	RequirementsSatisfied int
	Checks                int
	ChecksSatisfied       int
	Epoch                 uint64
	Verdict               string
	Complete              bool
	ReadyToFinalize       bool
}

// ContractShadowAuditSink is an optional sink capability; implementations
// must keep it content-free, like every other audit channel.
type ContractShadowAuditSink interface {
	RecordContractShadow(ContractShadowAudit)
}

// RecordContractShadow forwards the shadow contract summary only to sinks
// that explicitly opt in. Ordinary UI sinks receive nothing.
func RecordContractShadow(s Sink, a ContractShadowAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if cs, ok := s.(ContractShadowAuditSink); ok {
		cs.RecordContractShadow(a)
	}
}

// CompletionReportAudit is the host-authored completion report's end-of-turn
// summary: counts, enums, and gap kinds only, never paths or command text.
// The gap counters carry the point — what the turn left unproven.
type CompletionReportAudit struct {
	Verdict             string
	Risk                string
	Criteria            int
	CriteriaSatisfied   int
	Changes             int
	ChangesUnreviewed   int
	Verifications       int
	VerificationsFailed int
	VerificationsStale  int
	Gaps                int
	GapKinds            []string
	// ClaimsVerified counts the turn's own asserted verifications;
	// ClaimsUnbacked is how many of them the ledger did not support.
	ClaimsVerified int
	ClaimsUnbacked int
}

// CompletionReportAuditSink is an optional sink capability; implementations
// must keep it content-free, like every other audit channel.
type CompletionReportAuditSink interface {
	RecordCompletionReport(CompletionReportAudit)
}

// RecordCompletionReport forwards the completion summary only to sinks that
// explicitly opt in. Ordinary UI sinks receive nothing.
func RecordCompletionReport(s Sink, a CompletionReportAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if cs, ok := s.(CompletionReportAuditSink); ok {
		cs.RecordCompletionReport(a)
	}
}

// MemoryRecallAudit summarizes one automatic-recall decision: identifiers,
// scores, and budget numbers only — never the query or fact text.
type MemoryRecallAudit struct {
	Hits       []MemoryRecallHit
	UsedChars  int
	Omitted    int
	Suppressed string // reason recall stayed silent; "" when hits were injected
	// Shadow is the Retrieval V2 ranking (telemetry only, never served).
	Shadow []MemoryRecallHit
}

// MemoryRecallHit is one recalled fact's content-free fingerprint.
type MemoryRecallHit struct {
	ID        string
	Revision  int
	Scope     string
	Type      string
	Freshness string
	Score     float64
}

// MemoryRecallSink is an optional sink capability; implementations must keep
// it content-free, like every other audit channel.
type MemoryRecallSink interface {
	RecordMemoryRecall(MemoryRecallAudit)
}

// RecordMemoryRecall forwards a recall decision only to sinks that explicitly
// opt in. Ordinary UI sinks receive nothing.
func RecordMemoryRecall(s Sink, a MemoryRecallAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if mr, ok := s.(MemoryRecallSink); ok {
		mr.RecordMemoryRecall(a)
	}
}

// DelegationAdmissionAudit is the shadow admission verdict for one expensive
// delegation call: tool name and enums only, never the query or prompt text.
// Shadow means observed, not enforced — no call is blocked.
type DelegationAdmissionAudit struct {
	Tool    string
	Verdict string // "allow" | "deny"
	Reason  string // e.g. "local_fix_no_external_need"
	Intent  string // taskintent class of the turn
}

// DelegationAdmissionSink is an optional sink capability; implementations
// must keep it content-free, like every other audit channel.
type DelegationAdmissionSink interface {
	RecordDelegationAdmission(DelegationAdmissionAudit)
}

// RecordDelegationAdmission forwards a shadow admission verdict only to sinks
// that explicitly opt in. Ordinary UI sinks receive nothing.
func RecordDelegationAdmission(s Sink, a DelegationAdmissionAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if da, ok := s.(DelegationAdmissionSink); ok {
		da.RecordDelegationAdmission(a)
	}
}

// OutcomeProgressSink is an optional sink capability for the shadow outcome
// scorer's per-round samples: counts only, never paths or commands. Shadow
// means observed, not enforced — the novelty guard still decides behavior.
type OutcomeProgressSink interface {
	RecordOutcomeProgress(evidence.OutcomeSample)
}

// RecordOutcomeProgress forwards a shadow outcome sample only to sinks that
// explicitly opt in. Ordinary UI sinks receive nothing.
func RecordOutcomeProgress(s Sink, sample evidence.OutcomeSample) {
	if nilutil.IsNil(s) {
		return
	}
	if op, ok := s.(OutcomeProgressSink); ok {
		op.RecordOutcomeProgress(sample)
	}
}

// ProtocolRecoveryAuditSink is an optional sink capability. Implementations
// must keep it content-free; prompts, responses, endpoints, model names, and
// tool arguments do not belong in this audit channel.
type ProtocolRecoveryAuditSink interface {
	RecordProtocolRecovery(ProtocolRecoveryAudit)
}

// RecordProtocolRecovery forwards a content-free recovery observation only to
// sinks that explicitly opt in. Ordinary UI sinks receive nothing.
func RecordProtocolRecovery(s Sink, a ProtocolRecoveryAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if rs, ok := s.(ProtocolRecoveryAuditSink); ok {
		rs.RecordProtocolRecovery(a)
	}
}

// Sink consumes a turn's events. The agent calls Emit serially from its run
// loop (tool execution may fan out across goroutines, but emission does not),
// so an implementation need not be safe for concurrent Emit. Emit must not
// block indefinitely — a channel-backed sink should be buffered or drained by
// a live reader.
type Sink interface {
	Emit(Event)
}

// FuncSink adapts a plain function to a Sink.
type FuncSink func(Event)

// Emit calls the wrapped function.
func (f FuncSink) Emit(e Event) {
	if f != nil {
		f(e)
	}
}

// Discard is a Sink that drops every event. Useful in tests and for runs that
// only care about the final session state.
var Discard Sink = FuncSink(func(Event) {})
