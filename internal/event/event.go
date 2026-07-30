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
	// KindCount is a sentinel one past the last real Kind. New event kinds must
	// be inserted above it so completeness tests cover them automatically.
	KindCount
)

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
	FileDiff
	Profile *Profile // ToolDispatch: subagent model/effort (set for task/skill calls)
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

// Approval identifies a pending tool-call approval for an ApprovalRequest
// event. ID correlates the request with the controller's Approve(ID, …) reply.
type Approval struct {
	ID      string
	Tool    string
	Subject string
	Reason  string // optional annotation explaining why approval is needed
	Fresh   bool   // current human decision required; do not offer remembered grants
	// Kind classifies the approval surface: "tool" (default), "plan", or
	// "recovery". Empty means ordinary tool permission for backward compat.
	Kind string
	// Recovery carries Auto Guard card fields when Kind is "recovery".
	// Old frontends ignore it and still render a one-shot fresh approval.
	Recovery *RecoveryApproval
}

// RecoveryApproval is the backward-compatible structured payload for Auto
// Guard decisions. All fields are plain strings/bools so wire JSON stays simple
// and old clients can ignore unknown nested objects safely.
type RecoveryApproval struct {
	SourceAgent     string // agent that proposed the next mutation
	FailedTool      string // tool that failed; empty for pre-action boundaries
	FailedSummary   string // short failure/error summary; optional
	Diagnosis       string // agent/host diagnosis when failure recovery is active
	NextTool        string // tool about to run
	NextAction      string // concrete next command/file change/MCP action
	ChangeKind      string // same_strategy | strategy | scope | risk | uncertain
	ChangeRationale string // what changed vs the original approach
	ReviewRationale string // why the host/reviewer needs confirmation
	PlanBefore      string // active structured plan before a material transition
	PlanAfter       string // proposed structured plan after a material transition
	CanGrantTask    bool   // offer a semantic grant scoped to the current task
	TaskGrantScope  string // concise host-classified operation + exact target
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
	Attempts int
	Missing  []string
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
)

// Event is one increment in a turn's event stream. Read the field(s) documented
// for Kind; the others are zero.
// Notice codes are stable machine-readable identifiers for known notices.
// Frontends localize a notice's main copy by Code and fall back to matching
// the English Text (or showing it raw) when Code is empty or unknown, so
// wording edits in Go no longer silently break localization. Values are
// wire-stable: never rename or reuse one once shipped.
const (
	NoticeCodeFinalReadiness  = "final_readiness"
	NoticeCodeEmptyFinal      = "empty_final"
	NoticeCodeExecutorHandoff = "executor_handoff"
	NoticeCodeToolBudget      = "tool_budget"
	NoticeCodeLoopGuard       = "loop_guard"
	NoticeCodeWorkspaceLease  = "workspace_lease"
	NoticeCodeCancelledTurn   = "cancelled_turn_display"
)

type Event struct {
	Kind             Kind
	Text             string                    // Reasoning / Text / Message / Notice / Phase
	Detail           string                    // Notice: optional diagnostic text for expandable details
	Code             string                    // Notice: stable id for frontend localization; empty = unmapped
	Reasoning        string                    // Message: the full reasoning chain
	MemoryCitations  []provider.MemoryCitation // Message: local memory references displayed by rich frontends
	Tool             Tool                      // ToolDispatch / ToolResult
	Usage            *provider.Usage           // Usage
	Pricing          *provider.Pricing         // Usage: for cost display (nil = omit cost)
	Source           string                    // optional display/event source (executor, planner, subagent, ...)
	UsageSource      string                    // Usage: billable call source; empty means executor for compatibility
	CacheDiagnostics *CacheDiagnostics         // Usage: cache-churn attribution (nil = N/A)
	// SessionHit/SessionMiss carry cumulative cache tokens across the whole
	// session (Usage events only), so a frontend can show the aggregate hit-rate
	// — which doesn't crater on a short turn or after compaction — alongside
	// Usage's single-turn numbers.
	SessionHit   int             // Usage: cumulative cache-hit prompt tokens this session
	SessionMiss  int             // Usage: cumulative cache-miss prompt tokens this session
	Level        Level           // Notice
	Approval     Approval        // ApprovalRequest
	Ask          Ask             // AskRequest
	Err          error           // TurnDone: non-nil on failure
	Cancelled    bool            // TurnDone: Cancel was requested while the turn was active
	Outcome      string          // TurnDone: optional machine-readable recoverable outcome
	Readiness    *FinalReadiness // TurnDone: structured final-readiness recovery state
	Compaction   Compaction      // Compaction
	Guardian     GuardianResult
	RetryAttempt int // Retrying: 1-based attempt about to be made
	RetryMax     int // Retrying: total attempts before giving up
}

// ReadinessAuditSink is an optional sink capability. Sinks that do not care
// about readiness audit receipts can implement only Sink and will ignore them.
type ReadinessAuditSink interface {
	RecordReadinessAudit(evidence.ReadinessAudit)
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
