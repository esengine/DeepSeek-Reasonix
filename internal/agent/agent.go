package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"mvdan.cc/sh/v3/syntax"

	"reasonix/internal/ablation"
	"reasonix/internal/capability"
	"reasonix/internal/checkpoint"
	"reasonix/internal/diff"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/instruction"
	"reasonix/internal/jobs"
	"reasonix/internal/memory"
	"reasonix/internal/nilutil"
	"reasonix/internal/plancontract"
	"reasonix/internal/planmode"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/shellparse"
	"reasonix/internal/tool"
	"reasonix/internal/workspacelease"
)

// maxToolOutputBytes caps a single tool result before it goes into the model's
// context. ~32KB is roughly 8K tokens — enough for a full file read or a busy
// grep, while preventing one accidental "read this 5 MB log" from blowing the
// window before the next compaction runs.
const maxToolOutputBytes = 32 * 1024

const maxEmptyFinalBlocks = 3

// maxStreamRecoveries is the number of body-phase stream retries after the
// initial sampling attempt (Codex-aligned default: 1 + 5 = 6 attempts total).
const maxStreamRecoveries = 5
const maxSamplingAttempts = maxStreamRecoveries + 1
const maxExecutorHandoffNudges = 1

const defaultReasoningByteLimit = 128 * 1024

const finishReasonClientReasoningLimit = "client_reasoning_limit"

var errReasoningByteLimitExceeded = errors.New("reasoning output exceeded client byte limit")

// DeliveryRuntimeMarker is the delivery-mode contract block appended to user
// turns (withTurnPreferences). Exported as the single source of truth for the
// byte-exact suffix strip in preview derivation and for cross-package tests;
// its text is cache-frozen — changing it breaks steer replay matching and the
// prefix stability of every live delivery session.
const DeliveryRuntimeMarker = `<delivery-runtime>
This session is in delivery-first mode. Before any state-changing tool call,
establish concrete, verifiable acceptance criteria with todo_write. After the
change, inspect the result, run relevant verification, and sign off each step
with complete_step citing the successful verification command. The host enforces
these gates and will reject mutation or finalization when evidence is missing.
</delivery-runtime>`

// Renderer redraws the assistant's final-answer text as styled output. It is
// applied only after a turn's text stream completes, so the user sees raw
// markdown stream live, then a single redraw replaces it with formatted
// output. The renderer is intentionally interface-shaped so the agent stays
// independent of the cli's markdown library choice. Consumed by TextSink.
type Renderer interface {
	Render(text string) string
}

// Asker puts structured multiple-choice questions to the user and blocks for the
// answers. The agent consults it for the `ask` tool. It is interface-shaped so
// the agent stays independent of the frontend; a nil asker means no interactive
// user (headless runs), where `ask` returns a "decide for yourself" result. The
// interactive frontends wire the controller in as the Asker.
type Asker interface {
	Ask(ctx context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error)
}

// callContextKey carries the executing tool call's identity into Execute.
type callContextKey struct{}
type parentSessionContextKey struct{}
type subagentDepthContextKey struct{}
type userImagesContextKey struct{}

// callContext is the per-call context a tool can read. parentID is the call being
// executed and sink is the agent's event sink (the `task` tool uses both to nest
// a sub-agent's events under this call); asker lets the `ask` tool reach the user.
type callContext struct {
	parentID string
	sink     event.Sink
	asker    Asker
	planMode bool
}

// withCallContext stamps ctx with the executing call's ID, the agent's sink, and
// the asker. executeOne sets this before every Execute; `task` reads it (via
// CallContext) to nest sub-agent events, and `ask` reads the asker to prompt.
// The plan-mode flag is mirrored onto the leaf planmode key so tools that must
// not import this package (for example internal/tool/builtin) can still read it.
func withCallContext(ctx context.Context, parentID string, sink event.Sink, asker Asker, planMode bool) context.Context {
	ctx = planmode.WithActive(ctx, planMode)
	return context.WithValue(ctx, callContextKey{}, callContext{parentID: parentID, sink: sink, asker: asker, planMode: planMode})
}

// WithToolCallContext stamps ctx as a host-initiated top-level tool call.
// Normal model-selected tools receive this context from executeOne; controller
// entry points that deliberately invoke the same tool machinery (for example a
// user typing /<subagent-skill>) use this exported wrapper so nested sub-agent
// activity still reaches the parent event stream and plan-mode policy remains
// visible to the invoked runner.
func WithToolCallContext(ctx context.Context, parentID string, sink event.Sink, asker Asker, planMode bool) context.Context {
	return withCallContext(ctx, parentID, sink, asker, planMode)
}

// CallContext returns the executing call's ID, the agent's sink, and the asker,
// if the context was set by an agent's executeOne. ok is false for a plain
// context (headless tool tests, calls made outside the run loop).
func CallContext(ctx context.Context) (parentID string, sink event.Sink, asker Asker, ok bool) {
	cc, ok := ctx.Value(callContextKey{}).(callContext)
	if !ok {
		return "", nil, nil, false
	}
	return cc.parentID, cc.sink, cc.asker, true
}

// PlanModeFromContext reports whether the tool call is executing during the
// plan-first workflow. Tools may use it for phase-specific behavior, but it is
// not a permission or read-only boundary.
func PlanModeFromContext(ctx context.Context) bool {
	cc, ok := ctx.Value(callContextKey{}).(callContext)
	return ok && cc.planMode
}

// withAgentContext establishes the agent-owned workflow capabilities for a
// model round and for tool availability checks. Missing capabilities shadow
// inherited values so child agents cannot reach parent Goal, Jobs, or memory
// state accidentally.
func (a *Agent) withAgentContext(ctx context.Context) context.Context {
	if a == nil {
		return ctx
	}
	if a.jobs != nil {
		ctx = jobs.WithManager(ctx, a.jobs)
	} else {
		ctx = jobs.WithoutManager(ctx)
	}
	if a.memQueue != nil {
		ctx = memory.WithQueue(ctx, a.memQueue)
	} else {
		ctx = memory.WithoutQueue(ctx)
	}
	return planmode.WithActive(ctx, a.planMode.Load())
}

// WithParentSession stamps the active parent session ID onto a turn context so
// persisted sub-agents can record and enforce their owning conversation.
func WithParentSession(ctx context.Context, parentSession string) context.Context {
	return context.WithValue(ctx, parentSessionContextKey{}, strings.TrimSpace(parentSession))
}

// ParentSession returns the active parent session ID carried by a turn context.
func ParentSession(ctx context.Context) string {
	parentSession, _ := ctx.Value(parentSessionContextKey{}).(string)
	return strings.TrimSpace(parentSession)
}

// WithSubagentDepth carries the current subagent depth through nested tool calls.
// The root agent runs at depth 0; each spawned subagent increments by one.
func WithSubagentDepth(ctx context.Context, depth int) context.Context {
	if depth < 0 {
		depth = 0
	}
	return context.WithValue(ctx, subagentDepthContextKey{}, depth)
}

// SubagentDepth returns the current subagent depth carried by a turn context.
func SubagentDepth(ctx context.Context) int {
	depth, _ := ctx.Value(subagentDepthContextKey{}).(int)
	if depth < 0 {
		return 0
	}
	return depth
}

// WithUserImages carries the data URLs of images the user attached to this turn,
// resolved by the controller (which owns attachments) since the agent must not
// depend on it. Run embeds them on the user message; the provider sends them only
// when the model is vision-capable.
func WithUserImages(ctx context.Context, images []string) context.Context {
	return context.WithValue(ctx, userImagesContextKey{}, images)
}

func userImages(ctx context.Context) []string {
	images, _ := ctx.Value(userImagesContextKey{}).([]string)
	return images
}

// Gate decides, per tool call, whether it may run. The agent consults it at
// execute time after any explicit planning-phase opt-out. It is interface-shaped so the agent
// stays independent of the permission package and of how "ask" is resolved
// (silently in headless runs, interactively in the chat TUI). A nil gate means
// no gating — every call runs, preserving behaviour for callers that don't wire
// one in. reason is fed back to the model when allow is false; a non-nil err
// (e.g. ctx cancelled awaiting approval) is treated as a block for that call.
type Gate interface {
	Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (allow bool, reason string, err error)
}

// ExplicitDenyGate exposes the only global permission decision that applies to
// an already-authorized MCP server. Installing or approving a server is the
// user's authorization boundary; ordinary ask/fallback posture must not add a
// second per-call prompt, while explicit deny rules remain authoritative.
type ExplicitDenyGate interface {
	ExplicitlyDenies(toolName string, args json.RawMessage) bool
}

const PlanModeReadOnlyCommandApprovalTool = "plan_mode_read_only_command"

// PlanModeReadOnlyTrustRequest describes a bash command that is safe enough to
// ask the user to accept as read-only during planning. Command is the concrete
// attempted command and Prefix is the reusable prefix to trust.
type PlanModeReadOnlyTrustRequest struct {
	ToolName string
	Command  string
	Prefix   string
	Args     json.RawMessage
}

// PlanModeReadOnlyTrustGate is the legacy Plan bash trust bridge. It remains in
// the internal API for controller compatibility, but ordinary Plan execution no
// longer invokes it; bash calls use the normal permission gate.
type PlanModeReadOnlyTrustGate interface {
	CheckPlanModeReadOnlyTrust(ctx context.Context, req PlanModeReadOnlyTrustRequest) (allow bool, reason string, err error)
}

const DefaultMaxSubagentDepth = 2

// NormalizeMaxSubagentDepth applies the public config contract: values below 1
// preserve the old single-delegation boundary.
func NormalizeMaxSubagentDepth(depth int) int {
	if depth < 1 {
		return 1
	}
	return depth
}

// ToolHooks fires user-configured shell hooks around each tool call. PreToolUse
// runs before the call and may block it (block=true; message is the reason fed
// back to the model); PostToolUse runs after and only surfaces output to the
// user (it can't block). It is interface-shaped so the agent stays independent
// of the hook package — a nil hooks field disables hook firing entirely.
type ToolHooks interface {
	PreToolUse(ctx context.Context, name string, args json.RawMessage) (block bool, message string)
	PostToolUse(ctx context.Context, name string, args json.RawMessage, result string)
	PostToolUseFailure(ctx context.Context, name string, args json.RawMessage, result string, err error)
	// PostLLMCall fires after each model turn completes (streaming finishes)
	// but before reasoning_content is stored. It returns the (possibly
	// translated) reasoning string — the original when no hook is configured.
	// HasPostLLMCall reports whether such a hook exists, so the agent keeps
	// streaming reasoning live when none is wired up.
	PostLLMCall(ctx context.Context, reasoning string, turn int) string
	HasPostLLMCall() bool
	// SubagentStop fires when a `task` sub-agent finishes (foreground). PreCompact
	// fires just before a compaction pass and returns extra summary guidance (its
	// hooks' stdout) to fold into the summary prompt; "" when no hook contributes.
	SubagentStop(ctx context.Context, last string)
	PreCompact(ctx context.Context, trigger string) string
}

// Agent drives a single task: a Provider, a tool Registry, and a Session wired
// into the main loop.
type Agent struct {
	prov               provider.Provider
	tools              *tool.Registry
	session            *Session
	sessMu             sync.Mutex // guards the session pointer for external Session()/SetSession
	maxSteps           int
	maxStepsKey        string
	reasoningByteLimit int
	maxOutputTokens    int
	// executorHandoffGuard is enabled by Coordinator only for the executor agent.
	executorHandoffGuard bool
	temperature          float64
	pricing              *provider.Pricing
	usageSource          string
	modelRef             string
	responseLanguage     atomic.Value // string: auto|zh|en
	reasoningLanguage    atomic.Value // string: auto|zh|en

	// sink receives the turn's typed event stream (reasoning/text deltas, tool
	// dispatch/results, usage, notices). Frontends decide how to render it;
	// never nil because New defaults it to event.Discard.
	sink                event.Sink
	requireVisibleFinal bool // internal callers require final Content
	// lastUsage caches the latest provider telemetry for per-turn readouts.
	// The run loop writes it while a frontend reads it, so it is atomic.
	lastUsage atomic.Pointer[provider.Usage]
	outputBudgetState

	// sessCacheHit/sessCacheMiss accumulate cache tokens across every API call
	// this session, so frontends can show the aggregate hit-rate (Σhit/Σ(hit+miss))
	// — a steadier, cost-oriented number than the single-turn rate. They are NOT
	// reset on compaction, so the aggregate never craters when the model-visible
	// prefix is summarized away. Atomic: the run
	// loop accumulates them while the status line reads them.
	sessCacheHit  atomic.Int64
	sessCacheMiss atomic.Int64

	// lastPrefixShape records the previous provider request's cacheable prefix
	// so usage events can explain prefix churn on the next request.
	lastPrefixShape     PrefixShape
	haveLastPrefixShape bool

	// warnedMissingToolCallReasoning marks one active missing-reasoning incident
	// within this agent. The legacy name is retained because the persisted state
	// predates silent recovery; it now gates one automatic retry rather than a
	// user-visible warning. A healthy tool-call turn clears it. Loop-owned;
	// reset by SetSession.
	warnedMissingToolCallReasoning bool
	// missingReasoningWarnStateChecked avoids a file transaction on every
	// healthy tool-call turn. It resets with the session so a new Agent can
	// continue or confirm an incident persisted by an earlier process.
	missingReasoningWarnStateChecked bool
	// missingReasoningHealthyStreak provides the same three-turn anti-flapping
	// policy when no cross-process state directory is configured.
	missingReasoningHealthyStreak int
	// missingReasoningWarnPendingResolveAt keeps a healthy observation retryable
	// when its state write fails. The next missing turn retries that watermark
	// before consulting the persisted incident and otherwise fails visible.
	missingReasoningWarnPendingResolveAt time.Time

	// missingReasoningWarnState rate-limits recovery retries across sessions and
	// processes by an opaque provider-configuration fingerprint (#7059). The
	// legacy type/file names preserve the on-disk v2 contract. nil (no dir in
	// Options) keeps in-memory active-incident gating only.
	missingReasoningWarnState *missingReasoningWarnState

	// planMode enables planning workflow instructions and explicit phase opt-outs.
	// It does not replace the permission or sandbox boundary. The system prompt and
	// tool list never change with the toggle, preserving the provider-cache prefix.
	planMode atomic.Bool

	// readOnlyExecution is a construction-time defense for planner/research
	// agents. Unlike planMode it is not a collaboration toggle: it remains on
	// for the agent's lifetime and validates proxy calls after resolution.
	readOnlyExecution bool

	// mutationDependencyBarrier is set for the remainder of a provider tool
	// batch after any mutating call fails or is blocked. executeOne re-checks
	// it after proxy resolution so use_capability cannot bypass the barrier by
	// advertising schema-level ReadOnly()==true. Parallel read-only segments
	// never set it. Cleared at the start of each executeBatch.
	mutationDependencyBarrier atomic.Bool

	// plannerMCPExecution relaxes the strict read-only MCP boundary for the
	// two-model Planner only: authorized, non-destructive MCP targets may run
	// through use_capability even without readOnlyHint. Ordinary writers, bash,
	// and destructive MCP stay blocked. Strict read-only sub-agents leave this
	// false and still require readOnlyHint.
	plannerMCPExecution bool

	// gate, when non-nil, is the per-call permission gate for both standard and
	// Plan workflows. nil disables gating entirely.
	gate Gate

	// extensions, when non-nil, is the frozen Extension Protocol v2 dispatcher
	// for this controller generation. The run loop consults it at the
	// agent-side intercept points (see extensions.go); nil means no runtime
	// packages are installed and every point passes through byte-identically.
	extensions *dispatch.Dispatcher

	// recoveryGate, when non-nil, is the Auto Guard boundary for Auto mode.
	// Shared by root and sub-agents for the same controller task. nil disables
	// recovery checks (Ask/YOLO, headless without wiring, or feature off).
	recoveryGate RecoveryGate
	// recoveryAgentID labels this agent on recovery cards (empty = root).
	recoveryAgentID string
	// recoveryTaskID isolates recovery state across concurrent top-level tasks.
	// Empty shares the root task bucket.
	recoveryTaskID string
	// recoveryRunSeq gives ordinary (non-goal) runs a collision-free host scope.
	// Goal runs use their stable delivery scope instead.
	recoveryRunSeq atomic.Uint64

	// planModeReadOnlyTrust is retained for legacy controller wiring. The main
	// Plan execution path no longer consults it.
	planModeReadOnlyTrust PlanModeReadOnlyTrustGate

	// sandboxEscapeApprover, when non-nil, can ask the user whether one shell
	// command may rerun unconfined after the OS sandbox failed to start.
	sandboxEscapeApprover sandbox.EscapeApprover

	// configWriteApprover, when non-nil, can ask the user whether a file tool
	// may write a Reasonix-managed config file outside the workspace roots.
	configWriteApprover tool.ConfigWriteApprover

	// hooks, when non-nil, fires PreToolUse / PostToolUse shell hooks around each
	// tool call. nil disables hook firing.
	hooks ToolHooks

	// asker, when non-nil, lets the `ask` tool put questions to the user. nil in
	// headless runs (no interactive user). Set via SetAsker.
	asker Asker

	// onPreEdit, when non-nil, is called with a writer tool's previewed change
	// just before it runs — the seam the checkpoint store uses to snapshot a
	// file's pre-edit content. Only fires for non-ReadOnly tools that implement
	// tool.Previewer (so bash, whose targets are unknowable, is never tracked).
	// Set via SetPreEditHook. Prefer mutationObserver when both are set.
	onPreEdit func(diff.Change)

	// mutationObserver is the host-side unified file mutation observer. It
	// captures preimages before tools run and after-fingerprints regardless of
	// success/failure. Passed through Options to sub-agents; never changes
	// provider-visible tool schemas or prompts.
	mutationObserver *checkpoint.MutationObserver

	// jobs, when non-nil, is the session's background-job manager. executeOne
	// stamps it onto each tool call's context so the background tools (bash
	// run_in_background, task run_in_background, bash_output/kill_shell/wait) can
	// reach it. nil leaves those tools to degrade gracefully.
	jobs *jobs.Manager

	// writeScheduler coordinates parent-agent writes against background
	// subagent write claims. Set on the parent executor only (subagentDepth 0);
	// reservation is taken around Execute so late-loaded MCP/Economy tools are
	// covered without registry wrapping. Provider-visible schemas are unchanged.
	writeScheduler *SubagentScheduler
	// writeWorkspaceRoot is the workspace used to normalize parent write
	// reservations when writeScheduler is set.
	writeWorkspaceRoot string

	// workspaceLease is shared by every writer-capable agent in one Delivery
	// session. It is acquired lazily on the first mutation and held through the
	// final participating run/background job so verification remains isolated.
	workspaceLease *workspacelease.Owner

	// steerQueue holds mid-turn guidance admitted while the agent is running.
	// Entries keep a durable inbox item ID plus a loader so full bodies are not
	// retained in the agent heap beyond need. Cache miss for the next API call
	// is unavoidable but limited to one call — the prefix stays stable otherwise.
	steerMu       sync.Mutex
	steerQueue    []steerEntry
	steerConsumed bool
	// steerRunActive is true while Run is executing. Steer only queues while
	// it is set; once the turn's exit flush has drained the queue, later
	// steers are rejected so the caller can deliver them as a regular turn
	// instead of leaving them in a queue no loop will ever consume.
	steerRunActive bool

	// evidence is a per-user-turn ledger of host-observed tool receipts. It lets
	// complete_step validate that cited evidence happened before the claim.
	evidence *evidence.Ledger

	// todoState is the host's canonical task list: the latest successful
	// todo_write with completions applied by complete_step. Unlike the per-turn
	// ledger it survives turn boundaries and compaction (it never rides in the
	// prompt), so the final-answer gate still sees an unfinished plan a later
	// turn would otherwise hide. Rebuilt from the session in SetSession.
	todoMu       sync.Mutex
	todoState    []evidence.TodoItem
	planContract *plancontract.Plan // approved plan this turn executes, if any

	// hostAdvanceSeq guarantees unique tool IDs across turns: every
	// emitTodoState call increments it so the frontend always sees a fresh
	// dispatch even when the same panel index is signed off in different turns.
	hostAdvanceSeq atomic.Int64

	// projectChecks are structured project instructions that complete_step can
	// verify against same-turn bash receipts after a write-backed completion.
	projectChecks []instruction.VerifyCheck

	// deliveryProfile enables the runtime-enforced delivery contract. The stable
	// profile prompt explains intent; these fields are host state and never enter
	// the provider-cached prefix. deliveryScopeID and deliveryCheckpoint survive
	// turns while a stable delivery scope continues; the per-turn expectations
	// live in perTurnState.
	deliveryProfile    bool
	deliveryScopeID    string
	deliveryCheckpoint evidence.DeliveryCheckpoint

	// perTurnState groups the host flags that are valid for exactly one
	// Agent.Run. beginRunTurn zeroes the whole struct in one assignment, so a
	// field added here can never be forgotten in the reset; state that must
	// survive turns stays directly on Agent.
	perTurnState

	// ablation names the subsystems a benchmark arm switched off. The zero value
	// is the control arm.
	ablation ablation.Set

	// classifierTaskText is the host-trusted task text for delivery intent
	// classification, set by sub-agent spawners whose Run input carries host
	// framing. Empty means classify the raw input verbatim.
	classifierTaskText string

	// preserveEvidenceOnce makes the next Run keep the turn evidence ledger
	// instead of resetting it. RunSubAgentWithSession sets it before a
	// review_report completion nudge so the retry can cite the read receipts
	// the subagent already earned; consumed (cleared) by that Run.
	preserveEvidenceOnce bool
	// deliveryRecoveryPending is armed only when this agent exhausts final
	// readiness. An explicit host recovery action can consume it to preserve the
	// failed turn's receipts once; an ordinary user turn still resets evidence.
	deliveryRecoveryPending bool

	// capabilityLedger tracks require/prefer outcomes for this user turn only.
	// Never serialized into prompts or session state.
	capabilityLedger *capability.Ledger
	// capabilityAudit accumulates non-persisted routing/proxy counters.
	capabilityAudit *capability.Audit
	// lastCapabilityGate tracks prefer-reminder state across final-answer retries.
	capabilityPreferReminded bool
	// capabilityRequireMissSeen / capabilityPreferMissSeen remember that the
	// final gate reported a miss earlier this turn, so a later clean gate is
	// audited as a recovery. Reset per turn in SeedCapabilityRoute.
	capabilityRequireMissSeen bool
	capabilityPreferMissSeen  bool
	// pendingReviewWarnings are warn-level findings to surface in the final summary.
	pendingReviewWarnings []string

	// memQueue, when non-nil, lets the remember/forget tools fold a turn-tail note
	// about a just-made memory change into the next turn, so it applies this
	// session without touching the cache-stable prefix. Set via SetMemoryQueue.
	memQueue memory.Queue

	// subagentDepth tracks the current agent's nesting depth. maxSubagentDepth
	// caps delegation; when reached, recursive agent/skill tools are excluded.
	subagentDepth    int
	maxSubagentDepth int

	// Context management keeps the canonical transcript immutable and installs
	// at most one provider-visible checkpoint each time compactRatio is crossed.
	contextWindow       int
	compactRatio        float64
	recentKeep          int
	archiveDir          string
	keepPolicy          KeepPolicy
	compactStuck        bool
	consecutiveCompacts int
	sessionPath         string // bound transcript path for projection sidecars
	workspaceID         string // stable prompt-cache lineage component
	cacheState          string // legacy resume telemetry; never provider-visible
	checkpointState     string // none|restored|applied; runtime-only
	compactionState     CompactionState
	// compactionMu guards projection snapshots/install and the in-memory sidecar
	// generation. Network summarization never runs while this lock is held.
	compactionMu sync.Mutex
	// compactionRunMu singleflights the expensive summary transaction without
	// holding the session lock during network I/O.
	compactionRunMu sync.Mutex
	// lastCompactionTurn prevents the post-turn observer and pre-send preflight
	// from paying for two summaries during one active tool loop.
	lastCompactionTurn     atomic.Int64
	strictAlternatingRoles bool // coalesce adjacent user turns on provider request copies
	// activeTurnCreatedAt identifies the real/synthetic user message that began
	// the currently running turn. Compaction may rewrite older history while a
	// tool loop is active, but it must keep this message and everything after it
	// verbatim so cancellation/crash recovery can retain completed tool pairs.
	activeTurnCreatedAt atomic.Int64

	// stormSig / stormCount track a run of turns that keep failing or getting
	// blocked the same way so the loop can break a death-spiral. The signature is
	// each call's (tool, error/blocker) in order, NOT (tool, args): a stuck model
	// reliably reworks the arguments cosmetically (a re-worded essay, a reordered
	// object, a different shell command) while the host returns the same refusal or
	// failure every time — keying on args misses the loop entirely. Because errors
	// that embed their subject (e.g. "file not found: /x") differ per target,
	// genuine varied probing does not collapse to one signature. Reset whenever a
	// turn does anything else (a different failure/block shape, or any success).
	// See applyStormBreaker.
	stormSig   string
	stormCount int

	// progress escalates adaptively on consecutive zero-evidence-gain rounds
	// (see progress_guard.go); reset with the evidence ledger each turn.
	progress progressGuard

	// outcome shadows progress with an outcome-decomposed scorer whose samples
	// only feed trajectory recording; it never influences guard behavior.
	outcome *evidence.OutcomeTracker

	// taskBudget accumulates spend across every Run continuing one task and
	// resets with the evidence ledger: one ledger, one task, one bill. A
	// per-Run total cannot see the failures worth stopping — those are
	// measured in hours, and every "continue" starts a fresh Run.
	taskBudget runBudget

	// ebm is the Evidence-Before-More-Mutation nudge's once-per-turn state.
	ebm ebmState

	// governor is the reasoning governor's per-turn engagement state.
	governor governorState

	// forkRestore, when armed, swaps the frozen fork-bundle conversation in
	// right after beginRunTurn — the counterfactual-continuation seam.
	forkRestore func(*runLoopState)

	// lastReasoning is the previous executor round's reasoning-token spend,
	// read by the governor trigger (live policy and fork capture alike).
	lastReasoning int

	// repeatFailureCounts tracks semantically identical write-like calls that
	// keep failing with the same failure class. Unlike stormSig, successful
	// reads do not blindly clear this state: re-reading a file and then
	// resending the same stale anchor is still zero progress. Stale-anchor
	// records also survive target mutations until Preview proves the anchor is
	// applicable again. Ordinary turns reset the map at Run start; Goal
	// continuations retain it while their stable delivery scope is unchanged.
	repeatFailureCounts map[string]repeatFailureRecord
	repeatFailureScope  string
}

type repeatFailureRecord struct {
	count        int
	errClass     string
	paths        []string
	stateRecheck bool
}

// KeepPolicy is a bitmask controlling which messages are preserved beyond the
// recent tail during compaction.
type KeepPolicy int

const (
	KeepErrors KeepPolicy = 1 << iota
	KeepUserMarked
)

// SetPlanMode toggles the plan-first workflow flag. Ordinary calls still use
// Permissions/Sandbox; only explicit phase opt-outs are refused. The system
// prompt and tool schemas stay untouched, while the caller supplies the
// model-facing Marker in a user turn.
func (a *Agent) SetPlanMode(v bool) { a.planMode.Store(v) }

// SetTools replaces the agent's tool registry. The next API call picks up the
// new tool schema; tools already cached in the provider prefix are unaffected
// until the prefix is invalidated. Safe to call between turns.
func (a *Agent) SetTools(tools *tool.Registry) {
	if a == nil {
		return
	}
	a.tools = tools
}

// SetReasoningLanguage updates the visible reasoning language preference for
// subsequent user-role messages emitted by this agent.
func (a *Agent) SetReasoningLanguage(lang string) {
	if a == nil {
		return
	}
	a.reasoningLanguage.Store(NormalizeReasoningLanguage(lang))
}

// SetResponseLanguage updates the final-answer language preference for
// subsequent user-role messages emitted by this agent.
func (a *Agent) SetResponseLanguage(lang string) {
	if a == nil {
		return
	}
	a.responseLanguage.Store(NormalizeResponseLanguage(lang))
}

// SetGate installs the per-call permission gate. Used by interactive CLI sessions to swap the
// headless gate built in setup for an interactive one that prompts the user;
// nil disables gating. Safe to call before the run loop starts.
func (a *Agent) SetGate(g Gate) {
	if nilutil.IsNil(g) {
		g = nil
	}
	a.gate = g
}

// SetExtensions installs the extension dispatcher after construction. Boot
// uses it because sidecars — and therefore the dispatcher — only exist after
// snapshot assembly, which runs after the agent is built. Safe to call before
// the run loop starts; nil disables interception.
func (a *Agent) SetExtensions(d *dispatch.Dispatcher) {
	if a == nil {
		return
	}
	a.extensions = d
}

// SetRecoveryGate installs Auto Guard. Safe to call before the run loop starts;
// nil disables its checks.
func (a *Agent) SetRecoveryGate(g RecoveryGate) {
	if a == nil {
		return
	}
	if nilutil.IsNil(g) {
		g = nil
	}
	a.recoveryGate = g
}

// SetRecoveryIdentity sets the agent/task labels used on recovery cards.
func (a *Agent) SetRecoveryIdentity(agentID, taskID string) {
	if a == nil {
		return
	}
	a.recoveryAgentID = strings.TrimSpace(agentID)
	a.recoveryTaskID = strings.TrimSpace(taskID)
}

// RecoveryGate returns the attached Auto Guard (may be nil).
func (a *Agent) RecoveryGate() RecoveryGate {
	if a == nil {
		return nil
	}
	return a.recoveryGate
}

// SetPlanModeReadOnlyTrustGate retains the legacy confirmation bridge for old
// controller/session data. Main Plan execution no longer calls it.
func (a *Agent) SetPlanModeReadOnlyTrustGate(g PlanModeReadOnlyTrustGate) {
	if nilutil.IsNil(g) {
		g = nil
	}
	a.planModeReadOnlyTrust = g
}

// SetSandboxEscapeApprover installs the optional one-shot approval path used by
// the bash tool when an enforced OS sandbox fails to start.
func (a *Agent) SetSandboxEscapeApprover(g sandbox.EscapeApprover) {
	if nilutil.IsNil(g) {
		g = nil
	}
	a.sandboxEscapeApprover = g
}

// SetConfigWriteApprover installs the optional per-write approval path used by
// the file tools when a target is a Reasonix-managed config file outside the
// workspace write roots.
func (a *Agent) SetConfigWriteApprover(g tool.ConfigWriteApprover) {
	if nilutil.IsNil(g) {
		g = nil
	}
	a.configWriteApprover = g
}

func (a *Agent) withTurnPreferences(input string) string {
	if a == nil {
		return input
	}
	responseLang := "auto"
	if v := a.responseLanguage.Load(); v != nil {
		if s, ok := v.(string); ok {
			responseLang = s
		}
	}
	input = WithResponseLanguage(input, responseLang)

	lang := "auto"
	if v := a.reasoningLanguage.Load(); v != nil {
		if s, ok := v.(string); ok {
			lang = s
		}
	}
	input = WithReasoningLanguage(input, lang)
	if a.deliveryProfile && !strings.Contains(input, "<delivery-runtime>") {
		input = strings.TrimSpace(input) + "\n\n" + DeliveryRuntimeMarker
	}
	return input
}

// SetAsker installs the asker the `ask` tool uses to question the user.
// Interactive frontends wire one in; headless runs leave it nil.
func (a *Agent) SetAsker(as Asker) { a.asker = as }

// SetMemoryQueue installs the sink the remember/forget tools use to apply a
// memory change in the current session. The controller wires itself in.
func (a *Agent) SetMemoryQueue(q memory.Queue) { a.memQueue = q }

// SetPreEditHook installs the pre-edit snapshot hook (see onPreEdit). The
// controller wires it to its per-session checkpoint store; nil disables capture.
// Prefer SetMutationObserver for v2 capture (before+after fingerprints).
func (a *Agent) SetPreEditHook(fn func(diff.Change)) { a.onPreEdit = fn }

// SetMutationObserver installs the unified mutation observer. When set, it
// supersedes onPreEdit for capture and also records after-mutation fingerprints.
// When a task tool is already registered it inherits the observer for sub-agents.
func (a *Agent) SetMutationObserver(obs *checkpoint.MutationObserver) {
	a.mutationObserver = obs
	if a.tools == nil || obs == nil {
		return
	}
	if t, ok := a.tools.Get("task"); ok {
		if task, ok := t.(*TaskTool); ok {
			task.WithMutationObserver(obs)
		}
	}
}

// MutationObserver returns the installed observer (may be nil).
func (a *Agent) MutationObserver() *checkpoint.MutationObserver {
	if a == nil {
		return nil
	}
	return a.mutationObserver
}

// Session returns the agent's current conversation, useful for persistence
// hooks that need to read the message log between turns. sessMu serialises this
// pointer read against SetSession, so a frontend (serve's concurrent /history and
// /new handlers) can't race the swap. The run loop touches a.session directly and
// only swaps it via SetSession while idle, so its reads need no lock.
func (a *Agent) Session() *Session {
	a.sessMu.Lock()
	defer a.sessMu.Unlock()
	return a.session
}

// SetSession replaces the agent's conversation wholesale. Used by
// `reasonix --resume` to load a saved JSONL transcript before the first turn,
// so the model picks up exactly where it left off. Callers serialise it against a
// running turn (it only fires while idle); sessMu guards the pointer swap itself.
func (a *Agent) SetSession(s *Session) {
	a.sessMu.Lock()
	a.session = s
	a.sessMu.Unlock()
	a.sessCacheHit.Store(0)
	a.sessCacheMiss.Store(0)
	a.resetOutputBudgetState()
	a.warnedMissingToolCallReasoning = false
	a.missingReasoningWarnStateChecked = false
	a.missingReasoningHealthyStreak = 0
	a.repeatFailureCounts = nil
	a.repeatFailureScope = ""
	a.compactionMu.Lock()
	a.compactionState = CompactionState{} // lineage change; disk reloaded on Resume
	a.cacheState = CacheStateUnknown
	a.compactionMu.Unlock()
	a.compactStuck = false
	a.consecutiveCompacts = 0
	a.lastCompactionTurn.Store(0)
	if s != nil {
		a.rebuildTodoState(s.Snapshot())
	}
}

// LastUsage returns the most recent per-turn token telemetry the provider
// reported (nil if no turn has run yet). The TUI uses it to show a context
// gauge alongside the prompt; ContextManager.Prepare owns cache-breaking
// maintenance decisions.
func (a *Agent) LastUsage() *provider.Usage { return a.lastUsage.Load() }

// SessionCache returns the cumulative cache hit/miss prompt tokens across every
// API call this session — the basis for the status line's aggregate hit-rate.
func (a *Agent) SessionCache() (hit, miss int) {
	return int(a.sessCacheHit.Load()), int(a.sessCacheMiss.Load())
}

// ContextWindow returns the configured context-window size in tokens. 0
// means compaction is disabled for this agent.
func (a *Agent) ContextWindow() int { return a.contextWindow }

// mid-turn steer marker.
// MidTurnSteerPrefix marks user messages that were injected mid-turn as
// guidance (via Steer). The model sees them as instructions; frontends
// display them as a notice, not a regular user bubble.
const MidTurnSteerPrefix = "[Mid-turn steer queued by the user. Do not treat this as a new task; use it only as additional guidance for the current task after completing the current step.]"

func midTurnSteerMessage(text string) string {
	return MidTurnSteerPrefix + "\n" + text
}

// SteerText checks whether content is a mid-turn steer message and, if so,
// returns the original user text without the wrapper prefix. The returned
// text preserves the user's exact input — it only strips the prefix and the
// "\n" separator that midTurnSteerMessage inserts between the prefix and the
// user text; it does not trim spaces so the history replay matches the live
// Steer event rendering character-for-character.
//
// Steers are persisted through withTurnPreferences, which can prepend
// transient language blocks (for Chinese text even in auto mode) and append
// the delivery-runtime marker. Both are transport framing, not steer text:
// leading blocks are skipped before matching the prefix and a trailing
// marker is cut from the returned text, so replay recognizes steers
// regardless of the session's language and profile settings.
func SteerText(content string) (string, bool) {
	s := content
	for {
		if after, found := strings.CutPrefix(s, MidTurnSteerPrefix); found {
			// Strip only the "\n" separator, preserving the user's original text.
			after = strings.TrimPrefix(after, "\n")
			if trimmed, cut := strings.CutSuffix(after, "\n\n"+DeliveryRuntimeMarker); cut {
				after = trimmed
			}
			return after, true
		}
		next, ok := trimLeadingSteerWrapper(s)
		if !ok {
			return "", false
		}
		s = next
	}
}

// trimLeadingSteerWrapper removes one leading transient preference block that
// withTurnPreferences may have placed ahead of the steer prefix. It reports
// false when content does not start with such a block.
func trimLeadingSteerWrapper(content string) (string, bool) {
	s := strings.TrimLeft(content, " \t\r\n")
	for _, tag := range []string{"response-language", "reasoning-language"} {
		if !strings.HasPrefix(s, "<"+tag+">") {
			continue
		}
		if rest, ok := trimLeadingTransientBlock(s, tag); ok {
			return rest, true
		}
	}
	return content, false
}

// steerEntry is one mid-turn guidance admission.
type steerEntry struct {
	itemID string
	load   func() (string, error)
	// text is a fallback when load is nil (legacy Steer(string) path).
	text string
}

// Steer queues a message for mid-turn injection. It reports whether an active
// turn accepted the text; on false nothing was queued and the caller must
// deliver it another way (typically as a new turn). Without the active check,
// a steer landing in the window between the turn's exit flush and the
// controller observing running=false would sit in the queue unconsumed and
// unpersisted — invisible to both the model and history.
func (a *Agent) Steer(text string) bool {
	return a.SteerItem("", func() (string, error) { return text, nil })
}

// SteerItem queues durable-inbox guidance identified by itemID. load is called
// only when the entry is consumed so the agent does not retain every body.
func (a *Agent) SteerItem(itemID string, load func() (string, error)) bool {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	if !a.steerRunActive {
		return false
	}
	a.steerQueue = append(a.steerQueue, steerEntry{itemID: itemID, load: load})
	a.steerConsumed = false
	return true
}

// SteerConsumed returns true when the steer queue became empty after the last consume.
func (a *Agent) SteerConsumed() bool {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	return a.steerConsumed
}

// SetSink replaces the agent's event sink. Controllers use this to wrap the
// sink after construction (e.g. durable inbox observation) without rebuilding
// the agent.
func (a *Agent) SetSink(sink event.Sink) {
	if a == nil {
		return
	}
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	a.sink = sink
}

func (a *Agent) consumeSteer() (text, itemID string, ok bool) {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	if len(a.steerQueue) == 0 {
		return "", "", false
	}
	e := a.steerQueue[0]
	a.steerQueue = a.steerQueue[1:]
	a.steerConsumed = len(a.steerQueue) == 0
	if e.load != nil {
		t, err := e.load()
		if err != nil {
			return "", e.itemID, false
		}
		return t, e.itemID, true
	}
	return e.text, e.itemID, true
}

// closeSteerIntakeIfIdle atomically closes the normal-completion race between
// the final queue check and Run returning. A steer accepted before this check
// keeps the loop alive; one arriving after it is rejected so the host can keep
// the user's draft and retry it as a regular follow-up.
func (a *Agent) closeSteerIntakeIfIdle() bool {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	if len(a.steerQueue) > 0 {
		return false
	}
	a.steerRunActive = false
	return true
}

// flushSteerQueue ends the turn's steer intake. Guidance that arrived too late
// to be consumed is persisted for transcript visibility but marked local-only:
// replaying it to the model on the next unrelated user turn can execute a stale
// historical task (#7045). An explicit warning keeps the transcript honest
// without presenting the text as successfully applied guidance (#6238).
func (a *Agent) flushSteerQueue() {
	a.steerMu.Lock()
	pending := a.steerQueue
	a.steerQueue = nil
	if len(pending) > 0 {
		a.steerConsumed = true
	}
	a.steerRunActive = false
	a.steerMu.Unlock()
	for _, e := range pending {
		text := e.text
		if e.load != nil {
			if t, err := e.load(); err == nil {
				text = t
			}
		}
		a.RecordUnappliedSteer(text, e.itemID)
	}
}

// UnappliedSteerNotice returns the durable warning shown for guidance that was
// accepted during an abnormal turn exit but never reached a provider request.
func UnappliedSteerNotice(text string) string {
	return "Guidance was not applied because the turn ended before it could be processed. Send it again if it is still needed:\n" + text
}

// RecordUnappliedSteer stores guidance that could not affect its intended
// in-flight turn. The orphan-tool sentinel makes older readers drop the record
// during wire normalization, while current readers use LocalOnly to exclude it
// before every provider request. itemID correlates the notice with the durable
// session inbox entry when one exists.
func (a *Agent) RecordUnappliedSteer(text string, itemID ...string) {
	if a == nil || a.session == nil {
		return
	}
	id := ""
	if len(itemID) > 0 {
		id = itemID[0]
	}
	a.session.Add(provider.Message{
		Role:       provider.RoleTool,
		Content:    a.withTurnPreferences(midTurnSteerMessage(text)),
		ToolCallID: provider.LocalOnlyToolID,
		Name:       provider.LocalOnlyToolName,
		LocalOnly:  true,
	})
	a.sink.Emit(event.Event{
		Kind:   event.Notice,
		Level:  event.LevelWarn,
		Code:   event.NoticeCodeUnappliedSteer,
		Text:   UnappliedSteerNotice(text),
		ItemID: id,
	})
}

func (a *Agent) steerQueueLen() int {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	return len(a.steerQueue)
}

// CompactRatio returns the fraction of the window at which auto-compaction
// fires (e.g. 0.8). The status line uses it to show headroom to the next compact.
func (a *Agent) CompactRatio() float64 { return a.compactRatio }

// CompactNow forces one projection compaction (canonical transcript untouched).
func (a *Agent) CompactNow(ctx context.Context, instructions string) error {
	_, err := a.contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: CompactionTriggerManual, Instructions: instructions, Force: true})
	return err
}

// Options configures an Agent.
type Options struct {
	MaxSteps int
	// MaxStepsKey names the explicit runtime control shown when the MaxSteps guard
	// is hit. Empty defaults to the generic max_steps tool/runtime parameter.
	MaxStepsKey string
	// ReasoningByteLimit bounds a single stream's hidden reasoning bytes. Zero
	// uses the default guard; a negative value disables only this client guard.
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
	// ModelRef names the canonical "provider/model" ref backing this agent's
	// provider instance. It is attached to emitted Usage events so downstream
	// usage accounting can attribute tokens to the exact model.
	ModelRef string
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

	// PlanModeReadOnlyTrustGate is retained for legacy controller compatibility.
	// The main Plan execution path no longer invokes it.
	PlanModeReadOnlyTrustGate PlanModeReadOnlyTrustGate

	// SandboxEscapeApprover confirms a one-shot unconfined shell rerun after an
	// enforced OS sandbox fails. nil keeps fail-closed behavior.
	SandboxEscapeApprover sandbox.EscapeApprover

	// ConfigWriteApprover confirms file-tool writes to Reasonix-managed config
	// files outside the workspace roots. nil keeps fail-closed behavior.
	ConfigWriteApprover tool.ConfigWriteApprover

	// Context management. ContextWindow <= 0 disables compaction. Ratios and
	// RecentKeep fall back to defaults when unset.
	ContextWindow int
	CompactRatio  float64
	// Deprecated compatibility inputs. New agents ignore these fields; automatic
	// maintenance is controlled only by CompactRatio.
	SoftCompactRatio       float64
	ToolResultSnipRatio    float64
	CompactForceRatio      float64
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
	WriteScheduler *SubagentScheduler
	// WriteWorkspaceRoot normalizes parent write reservations.
	WriteWorkspaceRoot string

	// WorkspaceLease serializes Delivery mutations across sessions that target
	// the same workspace. nil preserves source compatibility for direct Agent
	// construction; boot always supplies it for Delivery sessions.
	WorkspaceLease *workspacelease.Owner

	// ProjectChecks are host-observable structured checks extracted during boot.
	ProjectChecks []instruction.VerifyCheck

	// DeliveryProfile enforces acceptance criteria before mutations and requires
	// post-change review, verification, and evidence-backed sign-off before a
	// final answer. It changes host control flow, not tool schemas.
	DeliveryProfile bool

	// Ablation switches subsystems off for a benchmark arm. The zero value runs
	// everything, so ordinary callers leave it unset.
	Ablation ablation.Set

	// ClassifierTaskText, when non-empty, is the pristine task text, set by
	// sub-agent spawners before host framing is prepended. Delivery intent
	// classification judges it instead of the raw Run input, so framing verbs
	// cannot arm expectations; the delegation audit scores evidence origin
	// against it, so only locations the parent wrote itself count as hints.
	ClassifierTaskText string

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

	// PlanModeReadOnlyCommands is retained for old config/controller data. Main
	// Plan execution classifies bash through Permissions instead.
	PlanModeReadOnlyCommands []string

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

// New constructs an Agent. MaxSteps <= 0 means no cap — the run loop continues
// until the model gives a final answer, the context is cancelled, or the
// provider errors (compaction keeps the context bounded). A nil sink is replaced
// with event.Discard so the agent can always emit unconditionally.
func New(prov provider.Provider, tools *tool.Registry, session *Session, opts Options, sink event.Sink) *Agent {
	if opts.CompactRatio <= 0 {
		opts.CompactRatio = defaultCompactRatio
	}
	if opts.RecentKeep <= 0 {
		opts.RecentKeep = minRecentKeep
	}
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	gate := opts.Gate
	if nilutil.IsNil(gate) {
		gate = nil
	}
	planModeReadOnlyTrust := opts.PlanModeReadOnlyTrustGate
	if nilutil.IsNil(planModeReadOnlyTrust) {
		planModeReadOnlyTrust = nil
	}
	sandboxEscapeApprover := opts.SandboxEscapeApprover
	if nilutil.IsNil(sandboxEscapeApprover) {
		sandboxEscapeApprover = nil
	}
	configWriteApprover := opts.ConfigWriteApprover
	if nilutil.IsNil(configWriteApprover) {
		configWriteApprover = nil
	}
	hooks := opts.Hooks
	if nilutil.IsNil(hooks) {
		hooks = nil
	}
	maxStepsKey := opts.MaxStepsKey
	if strings.TrimSpace(maxStepsKey) == "" {
		maxStepsKey = "max_steps"
	}
	maxSubagentDepth := opts.MaxSubagentDepth
	if maxSubagentDepth == 0 {
		maxSubagentDepth = DefaultMaxSubagentDepth
	} else {
		maxSubagentDepth = NormalizeMaxSubagentDepth(maxSubagentDepth)
	}
	subagentDepth := max(opts.SubagentDepth, 0)
	reasoningByteLimit := opts.ReasoningByteLimit
	if reasoningByteLimit == 0 {
		reasoningByteLimit = defaultReasoningByteLimit
	}
	a := &Agent{
		prov:                      prov,
		tools:                     tools,
		session:                   session,
		taskBudget:                runBudget{limit: normalizeTaskBudget(opts.TaskBudget)},
		maxSteps:                  opts.MaxSteps,
		maxStepsKey:               maxStepsKey,
		reasoningByteLimit:        reasoningByteLimit,
		maxOutputTokens:           opts.MaxOutputTokens,
		temperature:               opts.Temperature,
		pricing:                   opts.Pricing,
		usageSource:               usageSourceOrDefault(opts.UsageSource, event.UsageSourceExecutor),
		modelRef:                  strings.TrimSpace(opts.ModelRef),
		sink:                      sink,
		requireVisibleFinal:       opts.RequireVisibleFinal,
		gate:                      gate,
		extensions:                opts.Extensions,
		recoveryGate:              opts.RecoveryGate,
		recoveryAgentID:           strings.TrimSpace(opts.RecoveryAgentID),
		recoveryTaskID:            strings.TrimSpace(opts.RecoveryTaskID),
		readOnlyExecution:         opts.ReadOnlyExecution,
		plannerMCPExecution:       opts.PlannerMCPExecution,
		planModeReadOnlyTrust:     planModeReadOnlyTrust,
		sandboxEscapeApprover:     sandboxEscapeApprover,
		configWriteApprover:       configWriteApprover,
		hooks:                     hooks,
		jobs:                      opts.Jobs,
		memQueue:                  opts.MemoryQueue,
		writeScheduler:            opts.WriteScheduler,
		writeWorkspaceRoot:        strings.TrimSpace(opts.WriteWorkspaceRoot),
		workspaceLease:            opts.WorkspaceLease,
		missingReasoningWarnState: missingReasoningWarnStateFor(opts.MissingReasoningWarnStateDir),
		evidence:                  evidence.NewLedger(),
		projectChecks:             append([]instruction.VerifyCheck(nil), opts.ProjectChecks...),
		deliveryProfile:           opts.DeliveryProfile,
		ablation:                  opts.Ablation,
		classifierTaskText:        opts.ClassifierTaskText,
		capabilityLedger:          opts.CapabilityLedger,
		capabilityAudit:           opts.CapabilityAudit,
		contextWindow:             opts.ContextWindow,
		compactRatio:              opts.CompactRatio,
		recentKeep:                opts.RecentKeep,
		archiveDir:                opts.ArchiveDir,
		keepPolicy:                opts.KeepPolicy,
		sessionPath:               strings.TrimSpace(opts.SessionPath),
		workspaceID:               strings.TrimSpace(opts.WorkspaceID),
		cacheState:                CacheStateUnknown,
		strictAlternatingRoles:    opts.StrictAlternatingRoles,
		subagentDepth:             subagentDepth,
		maxSubagentDepth:          maxSubagentDepth,
		mutationObserver:          opts.MutationObserver,
	}
	a.outputBudget = outputBudgetOf(prov)
	if a.sessionPath != "" {
		a.LoadProjectionSidecar(a.sessionPath)
	}
	a.SetResponseLanguage(opts.ResponseLanguage)
	a.SetReasoningLanguage(opts.ReasoningLanguage)
	a.maybeArmForkFromEnv()
	a.maybeWrapForkCaptureProvider()
	return a
}

func usageSourceOrDefault(source, fallback string) string {
	source = strings.TrimSpace(source)
	if source != "" {
		return source
	}
	return fallback
}

// missingReasoningWarnStateFor returns nil when no state dir is configured, so
// direct Agent construction keeps the historical once-per-session notice scope.
func missingReasoningWarnStateFor(dir string) *missingReasoningWarnState {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	return newMissingReasoningWarnState(dir)
}

// reserveParentWrite holds write claims for the duration of a parent-agent
// write tool call. Returns a no-op release when reservation is not needed
// (subagent, read-only, no scheduler, or non-write tool).
func (a *Agent) reserveParentWrite(runTool tool.Tool, args json.RawMessage, readOnly bool) (release func(), err error) {
	noop := func() {}
	if a == nil || a.writeScheduler == nil || a.subagentDepth > 0 || readOnly || runTool == nil {
		return noop, nil
	}
	name := runTool.Name()
	if !parentWriteGuardTarget(name) {
		return noop, nil
	}
	claim, err := parentWriteReservation(a.writeWorkspaceRoot, name, args)
	if err != nil {
		return noop, err
	}
	return a.writeScheduler.ReserveParentWrite(claim)
}

// Run appends the user input and drives the tool loop until the model returns a
// final answer, the context is cancelled, or the provider errors. maxSteps <= 0
// leaves the loop unbounded here: bounding it is the host's call, and the
// adaptive stop is the no-progress ladder rather than a round count. Turn policy
// lives in beginRunTurn / runToolLoop / handleFinalResponse / handleToolRound.
func (a *Agent) Run(ctx context.Context, input string) (runErr error) {
	runMaxSteps := a.maxSteps
	runMaxStepsKey := a.maxStepsKey
	runLimitHostOwned := false
	runPauseAfterFinal := false
	if limit, ok := runStepLimitFromContext(ctx); ok {
		if !limit.defaultOnly || a.maxSteps <= 0 {
			runMaxSteps = limit.steps
			runLimitHostOwned = true
			runPauseAfterFinal = limit.pauseAfterFinal
			if limit.key != "" {
				runMaxStepsKey = limit.key
			}
		}
	}
	a.recoveryRunSeq.Add(1)
	if a.deliveryProfile && a.workspaceLease != nil {
		a.workspaceLease.BeginRun()
		defer a.workspaceLease.EndRun()
	}
	turnStartedAt := time.Now()
	workDurationMs := func() int64 {
		if elapsed := time.Since(turnStartedAt).Milliseconds(); elapsed > 0 {
			return elapsed
		}
		return 1
	}
	defer a.flushSteerQueue()
	a.steerMu.Lock()
	a.steerConsumed = false
	a.steerRunActive = true
	a.steerMu.Unlock()

	// Commit background-job evidence leases only after this turn delivers.
	// wait/bash_output merge a finished background writer's receipts into the
	// ledger provisionally; if the turn reaches a final answer (runErr == nil)
	// the delivery gates have verified and reviewed those mutations, so the
	// job's evidence can be permanently drained. A failed or cancelled turn
	// leaves the lease uncommitted so the next turn re-collects it.
	defer func() {
		if runErr != nil || a.evidence == nil || a.jobs == nil {
			return
		}
		for _, lease := range a.evidence.BackgroundLeases() {
			a.jobs.CommitEvidenceForSession(lease.Session, lease.JobID)
		}
	}()
	if _, scoped := DeliveryExecutionScopeFromContext(ctx); scoped {
		defer func() { a.updateDeliveryCheckpoint(runErr) }()
	}
	defer a.activeTurnCreatedAt.Store(0)

	// agent.before_start: an extension may abort the run before the user turn
	// is appended. The redacted reason surfaces like a normal run error.
	if err := a.interceptAgentStart(ctx); err != nil {
		return err
	}

	_, state := a.beginRunTurn(ctx, input)
	if a.forkRestore != nil {
		a.forkRestore(state)
	}
	state.runMaxSteps = runMaxSteps
	state.runMaxStepsKey = runMaxStepsKey
	state.runLimitHostOwned = runLimitHostOwned
	state.runPauseAfterFinal = runPauseAfterFinal
	state.workDurationMs = workDurationMs
	return a.runToolLoop(ctx, state)
}

// observeMissingToolCallReasoning classifies a thinking-mode tool-call turn and
// claims the single silent retry allowed for its active compatibility incident.
// DeepSeek requires provider-issued thinking content to be replayed, so a
// missing value is retried once before tools execute. Persistent broken rounds
// use the existing exact-configuration cooldown; a healthy round resolves the
// incident after three consecutive healthy turns and re-arms a future isolated
// regression (#6259, #7059).
func (a *Agent) observeMissingToolCallReasoning(calls []provider.ToolCall, reasoning string) (missing, shouldRetry bool) {
	if len(calls) == 0 || !provider.WarnOnMissingToolCallReasoning(a.prov) {
		return false, false
	}
	fingerprint := provider.MissingToolCallReasoningWarningFingerprint(a.prov)
	observedAt := time.Now()
	if strings.TrimSpace(reasoning) != "" {
		if a.missingReasoningWarnState == nil {
			if a.warnedMissingToolCallReasoning {
				a.missingReasoningHealthyStreak++
				if a.missingReasoningHealthyStreak >= missingReasoningHealthyResolveStreak {
					a.warnedMissingToolCallReasoning = false
					a.missingReasoningHealthyStreak = 0
				}
			}
			return false, false
		}
		shouldResolve := !a.missingReasoningWarnStateChecked || a.warnedMissingToolCallReasoning
		if shouldResolve {
			result := missingReasoningResolveResult{Recorded: true, Resolved: true}
			if pending := a.missingReasoningWarnPendingResolveAt; !pending.IsZero() {
				result = a.missingReasoningWarnState.resolveAt(fingerprint, pending)
				if result.Recorded {
					a.missingReasoningWarnPendingResolveAt = time.Time{}
				}
			}
			if result.Recorded {
				result = a.missingReasoningWarnState.resolveAt(fingerprint, observedAt)
			}
			if !result.Recorded {
				if observedAt.After(a.missingReasoningWarnPendingResolveAt) {
					a.missingReasoningWarnPendingResolveAt = observedAt
				}
				a.warnedMissingToolCallReasoning = true
				a.missingReasoningWarnStateChecked = false
			} else if result.Resolved {
				a.warnedMissingToolCallReasoning = false
				a.missingReasoningWarnStateChecked = true
			} else {
				a.warnedMissingToolCallReasoning = true
				a.missingReasoningWarnStateChecked = false
			}
		}
		return false, false
	}
	a.missingReasoningHealthyStreak = 0
	if s := a.missingReasoningWarnState; s != nil {
		stateReady := true
		alreadyActive := a.warnedMissingToolCallReasoning
		if pending := a.missingReasoningWarnPendingResolveAt; !pending.IsZero() {
			result := s.resolveAt(fingerprint, pending)
			stateReady = result.Recorded
			if result.Recorded {
				a.missingReasoningWarnPendingResolveAt = time.Time{}
				if result.Resolved {
					alreadyActive = false
					a.warnedMissingToolCallReasoning = false
				}
			}
		}
		claimed := stateReady && s.claimAt(fingerprint, observedAt)
		if !claimed || alreadyActive {
			// This exact configuration already attempted recovery for the active
			// incident, so keep the empty-key fallback without doubling requests.
			a.warnedMissingToolCallReasoning = true
			a.missingReasoningWarnStateChecked = true
			return true, false
		}
		if !stateReady {
			a.missingReasoningWarnStateChecked = false
		}
	} else if a.warnedMissingToolCallReasoning {
		return true, false
	}
	a.warnedMissingToolCallReasoning = true
	if a.missingReasoningWarnPendingResolveAt.IsZero() {
		a.missingReasoningWarnStateChecked = true
	}
	return true, true
}

// ReadinessResult is the host-consumable outcome of the Delivery final-answer
// readiness check. The Controller reads it after each goal turn; plain turns
// receive the same outcome as a FinalReadinessError.
type ReadinessResult struct {
	// Ready is true when no missing requirement remains.
	Ready bool
	// Missing lists stable category ids of the missing requirements
	// (project_check, todo, criteria, verification, review, signoff, action,
	// mutation, capability). Empty when Ready.
	Missing []string
	// Reason is the user-facing summary of what is still missing.
	Reason string
	// ProgressKey is the host-verifiable progress signature of the current
	// evidence state. Identical ProgressKey across consecutive goal turns
	// means no host-observable progress was made.
	ProgressKey string
}

// ReadinessResult returns the current final-readiness outcome for the host.
func (a *Agent) ReadinessResult() ReadinessResult {
	check := a.finalReadinessCheckFor()
	if check.reason == "" {
		return ReadinessResult{Ready: true, ProgressKey: check.progressSignature()}
	}
	return ReadinessResult{
		Ready:       false,
		Missing:     check.missingIDs(),
		Reason:      check.reason,
		ProgressKey: check.progressSignature(),
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// DeliveryCheckpoint returns the compact Goal-scoped delivery state. It is safe
// to persist next to the Goal sidecar because it contains no raw arguments.
func (a *Agent) DeliveryCheckpoint() evidence.DeliveryCheckpoint {
	return a.deliveryCheckpoint
}

// RestoreDeliveryCheckpoint seeds a rebuilt controller before its next Goal
// run. A mismatched/empty scope is ignored conservatively.
func (a *Agent) RestoreDeliveryCheckpoint(checkpoint evidence.DeliveryCheckpoint) {
	checkpoint.ScopeID = strings.TrimSpace(checkpoint.ScopeID)
	if checkpoint.ScopeID == "" {
		return
	}
	a.deliveryCheckpoint = checkpoint
	a.deliveryScopeID = checkpoint.ScopeID
}

// PrepareDeliveryRecovery preserves the exhausted turn's evidence for exactly
// one explicit continuation. It returns false when there is no matching
// readiness failure, so normal follow-up turns cannot inherit stale mutations.
func (a *Agent) PrepareDeliveryRecovery() bool {
	if !a.deliveryProfile || !a.deliveryRecoveryPending {
		return false
	}
	a.preserveEvidenceOnce = true
	a.deliveryRecoveryPending = false
	return true
}

func (a *Agent) updateDeliveryCheckpoint(runErr error) {
	if !a.deliveryScopeActive || a.deliveryScopeID == "" || a.evidence == nil {
		return
	}
	cp := a.deliveryCheckpoint
	if cp.ScopeID != a.deliveryScopeID {
		cp = evidence.DeliveryCheckpoint{ScopeID: a.deliveryScopeID}
	}
	cp.CriteriaEstablished = cp.CriteriaEstablished || a.deliveryCriteriaEstablished || a.evidence.HasSuccessfulTodoWrite()
	cp.WorkObserved = cp.WorkObserved || a.evidence.HasSuccessfulWorkReceipt()
	persistentOnlyReady := a.deliveryPersistentExpected && !a.deliveryMutationExpected &&
		a.evidence.HasSuccessfulToolReceipt("remember") && !a.evidence.HasSuccessfulMutationOtherThan("remember")
	if _, ok := a.evidence.LatestSuccessfulMutationIndex(); ok && !persistentOnlyReady {
		cp.MutationObserved = true
		cp.PendingMutation = true
	}
	if persistentOnlyReady {
		cp.MutationObserved = true
	}
	if runErr == nil && cp.PendingMutation && a.deliveryMutationCheckpointReady() {
		cp.PendingMutation = false
	}
	a.deliveryCheckpoint = cp
}

func (a *Agent) deliveryMutationCheckpointReady() bool {
	if a.evidence == nil || !a.deliveryCriteriaEstablished {
		return false
	}
	mutation, ok := a.evidence.LatestSuccessfulMutationIndex()
	if !ok {
		mutation = -1
	}
	return a.evidence.HasSuccessfulCompleteStepAfter(mutation) &&
		a.evidence.HasSuccessfulDeliverySignoffAfter(mutation) &&
		a.evidence.HasSuccessfulReviewAfter(mutation) &&
		a.deliveryReviewGateFailure() == ""
}

func (a *Agent) setTodoState(todos []evidence.TodoItem) {
	a.todoMu.Lock()
	a.todoState = evidence.NormalizeSerialTodos(todos)
	a.todoMu.Unlock()
}

func (a *Agent) hasActiveCanonicalTodo() bool {
	a.todoMu.Lock()
	defer a.todoMu.Unlock()
	for _, todo := range a.todoState {
		if canonicalTodoStatus(todo.Status) == "in_progress" {
			return true
		}
	}
	return false
}

func (a *Agent) canonicalTodoProgress() (int, bool) {
	a.todoMu.Lock()
	defer a.todoMu.Unlock()
	completed := 0
	incomplete := false
	for _, todo := range a.todoState {
		status := canonicalTodoStatus(todo.Status)
		if status == "completed" {
			completed++
		} else {
			incomplete = true
		}
	}
	return completed, incomplete
}

// registryHasWriterTools reports whether any registered tool can mutate state.
// A strictly read-only registry (read_only_task / read_only_skill subagents)
// can never satisfy a "state change required" delivery expectation, so that
// expectation must not be armed for it.
func registryHasWriterTools(reg *tool.Registry) bool {
	if reg == nil {
		return false
	}
	for _, name := range reg.Names() {
		if t, ok := reg.Get(name); ok && !t.ReadOnly() {
			return true
		}
	}
	return false
}

// advanceCanonicalTodo flips the canonical todo matching a signed-off step to
// completed (promoting the next pending item to in_progress) and emits a
// synthetic todo_write so the task panel reflects it without the model
// re-sending the whole list. No-op when nothing matches or it is already done.
func (a *Agent) advanceCanonicalTodo(step string) {
	a.todoMu.Lock()
	if len(a.todoState) == 0 {
		a.todoMu.Unlock()
		return
	}
	m, ok := evidence.MatchStep(step, a.todoState)
	if !ok || !evidence.AdvanceSerialTodo(a.todoState, m.Index-1) {
		a.todoMu.Unlock()
		return
	}
	snapshot := append([]evidence.TodoItem(nil), a.todoState...)
	a.todoMu.Unlock()
	a.recordTodoState(snapshot)
	a.emitTodoState(snapshot, m.Index)
}

// emitTodoState emits a synthetic todo_write event so the frontend task panel
// reflects a host-advanced completion without the model re-sending the list.
// itemIndex is the 1-based position of the completed todo in the panel.
func (a *Agent) emitTodoState(todos []evidence.TodoItem, itemIndex int) {
	args, err := json.Marshal(map[string]any{"todos": todos})
	if err != nil {
		return
	}
	id := fmt.Sprintf("host-advance-%d-%d", a.hostAdvanceSeq.Add(1), itemIndex)
	t := event.Tool{ID: id, Name: "todo_write", Args: string(args), ReadOnly: true}
	a.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: t})
	t.Output = "task list advanced by complete_step"
	a.sink.Emit(event.Event{Kind: event.ToolResult, Tool: t})
}

// RebuildTodoState re-derives canonical task state from the current session
// transcript. Call after externally truncating the session (e.g. after a
// user-cancel strip) so Agent.todoState stays consistent with the messages.
func (a *Agent) RebuildTodoState() {
	a.rebuildTodoState(a.Session().Snapshot())
}

// rebuildTodoState reconstructs the canonical task list from a transcript: the
// latest successful todo_write is the base, then every complete_step after it
// advances an item. Deterministic from persisted messages, so it survives a
// fresh load or a rewind (the truncated history yields the historical state).
// Empty after compaction drops the todo_write — no worse than no canonical list.
func (a *Agent) rebuildTodoState(msgs []provider.Message) {
	successful := successfulToolCallIDs(msgs)
	var todos []evidence.TodoItem
	baseIdx := -1
	for i, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			if tc.Name != "todo_write" || !successful[tc.ID] {
				continue
			}
			rec := evidence.ReceiptFromToolCall(tc.Name, json.RawMessage(tc.Arguments), true, true)
			// A successful empty todo_write is an explicit clear. Preserve it as the
			// latest base so history reloads do not resurrect an older non-empty list.
			todos = evidence.NormalizeSerialTodos(rec.Todos)
			baseIdx = i
		}
	}
	if baseIdx < 0 {
		a.setTodoState(nil)
		return
	}
	for i := baseIdx; i < len(msgs); i++ {
		for _, tc := range msgs[i].ToolCalls {
			if tc.Name != "complete_step" || !successful[tc.ID] {
				continue
			}
			rec := evidence.ReceiptFromToolCall(tc.Name, json.RawMessage(tc.Arguments), true, true)
			if m, ok := evidence.MatchStep(rec.Step, todos); ok {
				evidence.AdvanceSerialTodo(todos, m.Index-1)
			}
		}
	}
	a.setTodoState(todos)
}

func successfulToolCallIDs(msgs []provider.Message) map[string]bool {
	successful := map[string]bool{}
	for _, msg := range msgs {
		if msg.Role != provider.RoleTool || msg.ToolCallID == "" {
			continue
		}
		if !toolResultFailed(msg.Content) {
			successful[msg.ToolCallID] = true
		}
	}
	return successful
}

func toolResultFailed(content string) bool {
	content = strings.TrimSpace(content)
	return strings.HasPrefix(content, "error:") ||
		strings.HasPrefix(content, "blocked:") ||
		strings.HasPrefix(content, "Error:") ||
		strings.HasPrefix(content, "[error")
}

func shouldNudgeExecutorHandoff(input, answer string) bool {
	return !executorHandoffAllowsTextOnly(input, answer)
}

func executorHandoffAllowsTextOnly(input, answer string) bool {
	if looksLikeExecutorHandoffDeferral(answer) {
		return false
	}
	task, plan, ok := parseExecutorHandoff(input)
	if !ok {
		return false
	}
	if handoffTaskLooksTextOnly(task) {
		return true
	}
	return handoffPlanLooksTextOnly(plan)
}

func parseExecutorHandoff(input string) (task, plan string, ok bool) {
	input = StripTransientUserBlocks(input)
	marker := "# " + executorHandoffMarker
	i := strings.Index(input, marker)
	if i < 0 {
		return "", "", false
	}
	input = input[i+len(marker):]
	_, input, ok = strings.Cut(input, "\n\nOriginal task:\n")
	if !ok {
		return "", "", false
	}
	task, input, ok = strings.Cut(input, "\n\nPlanner output:\n")
	if !ok {
		return "", "", false
	}
	plan, _, ok = strings.Cut(input, "\n\nExecutor instructions:")
	if !ok {
		return "", "", false
	}
	if beforeToolContext, _, found := strings.Cut(plan, "\n\nExecutor tool context:"); found {
		plan = beforeToolContext
	}
	return strings.TrimSpace(task), strings.TrimSpace(plan), true
}

func looksLikeExecutorHandoffDeferral(answer string) bool {
	lower := strings.ToLower(strings.TrimSpace(answer))
	if lower == "" {
		return true
	}
	if containsAnySubstring(lower, executorHandoffDeferralPhrases) {
		return true
	}
	switch strings.Trim(lower, " \t\r\n.!?。！？") {
	case "ok", "okay", "sounds good", "done", "好的", "可以", "没问题", "收到":
		return true
	default:
		return false
	}
}

func handoffTaskLooksTextOnly(task string) bool {
	lower := strings.ToLower(strings.TrimSpace(task))
	if lower == "" {
		return false
	}
	if containsAnySubstring(lower, executorHandoffWorkRequestTerms) {
		return false
	}
	return containsAnySubstring(lower, executorHandoffTextOnlyTaskTerms)
}

func handoffPlanLooksTextOnly(plan string) bool {
	lower := strings.ToLower(strings.TrimSpace(plan))
	if lower == "" {
		return false
	}
	if containsAnySubstring(lower, executorHandoffLocalActionTerms) {
		return false
	}
	if containsAnySubstring(lower, executorHandoffTextOnlyPlanTerms) {
		return true
	}
	return strings.Contains(lower, "?")
}

func containsAnySubstring(s string, terms []string) bool {
	for _, term := range terms {
		if strings.Contains(s, term) {
			return true
		}
	}
	return false
}

var executorHandoffDeferralPhrases = []string{
	"plan looks", "looks good", "should be easy", "should be straightforward",
	"i can implement", "i'll implement", "i will implement", "i'll get started",
	"let me ", "i will now", "i'll now", "i can do that",
	"计划看起来", "可以实现", "我会", "我将", "接下来我", "马上开始",
}

var executorHandoffWorkRequestTerms = []string{
	"implement", "fix", "refactor", "migrate", "edit", "write", "create", "delete",
	"update", "remove", "add ", "test", "build", "repair", "patch",
	"修改", "修复", "实现", "新增", "重构", "迁移", "补齐", "更新", "删除", "移除",
}

var executorHandoffTextOnlyTaskTerms = []string{
	"now what", "what next", "tl;dr", "tldr", "summarize", "summary", "explain",
	"i installed", "i just installed", "i turned on", "i enabled", "it's on", "it is on",
	"怎么办", "下一步", "然后呢", "总结", "解释", "说明", "装了", "装好了", "安装了", "开了", "开启了", "打开了",
}

var executorHandoffLocalActionTerms = []string{
	"write_file", "read_file", "apply_patch", "bash",
	"workspace", "repo", "repository", "codebase", "file", "path",
	"write ", "edit ", "modify ", "create ", "delete ", "remove ", "update ", "add ", "patch ", "refactor ", "implement ",
	"run ", "command", "test", "build",
	"文件", "路径", "仓库", "代码", "写入", "编辑", "修改", "创建", "删除", "移除", "更新", "新增", "运行", "命令", "测试", "构建",
}

var executorHandoffTextOnlyPlanTerms = []string{
	"tell the user", "ask the user", "guide the user", "explain to the user",
	"summarize", "summary", "tl;dr", "tldr", "answer the user", "respond to the user",
	"provide guidance", "walk the user", "instruct the user", "have the user",
	"user should", "the user should", "user can", "the user can", "manual", "manually",
	"no tools needed", "no tool calls needed", "does not need tools", "needs no tools",
	"listen", "play a song", "compare the difference", "checkbox",
	"告诉用户", "询问用户", "问用户", "让用户", "请用户", "指导用户", "解释", "总结", "回答",
	"手动", "无需工具", "不需要工具", "试听", "听歌", "对比", "勾选",
}

func executorHandoffRetryMessage() string {
	return `You are already in the executor phase. The planner's read-only limitations do not apply to you.

The tool schema is still attached to this executor request. Do not invent that MCP servers or tools are unavailable; only report an unavailable tool after a real tool call or host error proves it.

Do not answer as the planner and do not ask how to trigger the executor.
Use your available tools now to carry out the task. If carrying out the planner's instructions requires a user-owned choice or review, call the ask tool with concrete options and wait for its tool result; do not ask in prose, and do not claim the user answered unless an actual ask tool result or a new user message says so. If a write or command is blocked by permissions or workspace boundaries, state that specific blocker and ask for the needed approval/path.`
}

func hasVisibleFinalAnswer(text string) bool {
	return strings.TrimSpace(text) != ""
}

// reasoningOnlyFinishHonoured reports whether the model finished with a stop
// signal but placed its answer in the reasoning stream rather than the content
// block. DeepSeek thinking mode does this occasionally: it streams a long
// reasoning_content, then returns finish_reason="stop" with an empty content.
// The model has signalled completion, so the host accepts the turn instead of
// retrying and forcing another expensive thinking round.
//
// The accept is scoped to DeepSeek thinking mode (ToolCallReasoningPolicy):
// for other providers a reasoning-only turn keeps the empty-final retry
// safety net — local <think>-tag models often recover a visible answer on
// the second attempt, and a gateway that mislabels truncation as "stop"
// must not have a degenerate turn committed as the final answer.
func reasoningOnlyFinishHonoured(p provider.Provider, u *provider.Usage, reasoning string) bool {
	if !provider.RequiresToolCallReasoning(p) {
		return false
	}
	if u == nil || u.FinishReason != "stop" {
		return false
	}
	return strings.TrimSpace(reasoning) != ""
}

func emptyFinalRetryMessage() string {
	return "The previous assistant response finished without any visible answer text. Continue the same task now and provide a concise visible answer to the user. Do not send reasoning only."
}

func emptyFinalNotice() string {
	return "No visible answer was produced; asking the assistant to respond again."
}

func emptyFinalNoticeDetail(prov string, u *provider.Usage, reasoningLen int) string {
	finish := "unknown"
	if u != nil && u.FinishReason != "" {
		finish = u.FinishReason
	}
	return fmt.Sprintf("empty final answer blocked: %s returned no visible answer text (finish=%s, reasoning=%d chars); retrying", prov, finish, reasoningLen)
}

func executorHandoffNoticeText() string {
	return "The assistant answered before taking action; asking it to use the required tools."
}

func toolBudgetNoticeText() string {
	return "Tool round limit reached; asking the assistant to summarize progress."
}

// stream runs one completion, emitting reasoning and text deltas as typed
// events and collecting complete tool calls. A Message event closes the text
// stream so a sink can re-render the streamed raw text as styled markdown. The
// accumulated text and reasoning are also returned so the caller can round-trip
// reasoning on the next turn.
//
// When frozen is non-nil, the request is not rebuilt from session — retries
// must replay the same provider-visible body.
func (a *Agent) stream(ctx context.Context, turn int, sink event.Sink) streamedTurn {
	return a.streamWithFrozen(ctx, turn, sink, nil, "")
}

func (a *Agent) streamWithFrozen(ctx context.Context, turn int, sink event.Sink, frozen *samplingRequest, attemptID string) streamedTurn {
	ctx = provider.WithRetryNotify(ctx, func(info provider.RetryInfo) {
		sink.Emit(event.Event{Kind: event.Retrying, RetryAttempt: info.Attempt, RetryMax: info.Max, RetryScope: event.RetryScopeHeaders})
	})
	// Reuse a parent attempt counter when present so stream retries accumulate
	// into one RequestCount; otherwise install a fresh counter for this call.
	ctx = provider.WithRequestAttemptCounter(ctx)
	// A stream can terminate locally before the provider channel closes (for
	// example when the client-side reasoning guard fires). Own a child context
	// here so every return path aborts the HTTP request and releases the provider
	// reader instead of leaving generation and billing running in the background.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var req provider.Request
	var err error
	if frozen != nil {
		req = freezeProviderRequest(frozen.req)
	} else {
		prepared, perr := a.prepareSamplingRequest(ctx)
		if perr != nil {
			return streamedTurn{err: perr}
		}
		req = prepared.req
	}
	// Host stream cancels on generation drain (OpenAI/Anthropic HTTP reads).
	defer trackPublishedHostStream(ctx, cancel)()
	ch, err := a.streamProviderRequest(ctx, req)
	if err != nil {
		return streamedTurn{usage: provider.UsageWithRequestAttemptCount(ctx, nil), err: err}
	}

	// A PostLLMCall hook rewrites the whole reasoning block, so when one is wired
	// up we buffer reasoning silently and emit the transformed text once after the
	// stream. With no such hook the reasoning streams live, chunk by chunk, as
	// before — the common case must not lose its live "thinking…" display.
	transformReasoning := a.hooks != nil && a.hooks.HasPostLLMCall()

	var text, reasoning strings.Builder
	var signature string                    // provider-issued proof for the reasoning (Anthropic thinking)
	var reasoningID, reasoningStatus string // Responses reasoning item id/status (meta chunk)
	var calls []provider.ToolCall
	var responsesItems []json.RawMessage
	var partialCalls []provider.ToolCall
	var usage *provider.Usage
	var partialToolStarted bool
	var maxArgChars int
	var lastArgProgress time.Time
	// collect packages the stream state accumulated so far; stored is the
	// finishReasoning output that becomes the round-tripped reasoning.
	collect := func(stored string, err error) streamedTurn {
		return streamedTurn{
			text: text.String(), reasoning: stored, signature: signature,
			reasoningID: reasoningID, reasoningStatus: reasoningStatus,
			calls: calls, responsesItems: responsesItems, usage: usage,
			partialToolStarted: partialToolStarted, partialCalls: partialCalls,
			maxArgChars: maxArgChars, err: err,
		}
	}
	finishReasoning := func() (stored, display string) {
		original := reasoning.String()
		display = original
		if transformReasoning && original != "" {
			display = a.hooks.PostLLMCall(ctx, original, turn)
			if display != "" {
				sink.Emit(event.Event{Kind: event.Reasoning, Text: display})
			}
		}
		stored = display
		providerBound := signature != "" || reasoningID != "" || reasoningStatus != ""
		if providerBound || provider.RequiresReasoningRoundTrip(a.prov) || (len(calls) > 0 && provider.RequiresToolCallReasoning(a.prov)) {
			stored = original
		}
		return stored, display
	}
	for {
		var chunk provider.Chunk
		select {
		case <-ctx.Done():
			stored, _ := finishReasoning()
			usage = bestEffortStreamUsage(usage, text.Len(), reasoning.Len(), "interrupted")
			usage = provider.UsageWithRequestAttemptCount(ctx, usage)
			return collect(stored, ctx.Err())
		case c, ok := <-ch:
			if !ok {
				if err := ctx.Err(); err != nil {
					stored, _ := finishReasoning()
					usage = bestEffortStreamUsage(usage, text.Len(), reasoning.Len(), "interrupted")
					usage = provider.UsageWithRequestAttemptCount(ctx, usage)
					return collect(stored, err)
				}
				stored, display := finishReasoning()
				// provider.response: extensions rule on the assembled terminal
				// response before it is persisted. A replacement becomes the
				// visible assistant turn (the user's transcript); a block fails
				// the turn.
				providerSignature := signature
				finalText, finalReasoning, signature, calls, usage, err := a.interceptProviderResponse(
					ctx, text.String(), stored, signature, calls, usage)
				if err != nil {
					return streamedTurn{partialToolStarted: partialToolStarted, partialCalls: partialCalls, maxArgChars: maxArgChars, err: err}
				}
				// Responses reasoning IDs/status and Anthropic signatures are
				// provider-bound metadata. Never attach the provider's metadata
				// to reasoning that an extension replaced.
				if finalReasoning != stored || signature != providerSignature {
					reasoningID, reasoningStatus = "", ""
				}
				if finalReasoning != stored {
					// The extension replaced the reasoning: what is persisted
					// and what the closing Message event re-renders must agree.
					display = finalReasoning
				}
				if finalText != "" || display != "" {
					sink.Emit(event.Event{
						Kind:      event.Message,
						Text:      DisplayAssistantText(finalText),
						Reasoning: display,
					})
				}
				usage = provider.UsageWithRequestAttemptCount(ctx, usage)
				// A clean terminal never reports partialToolStarted: the calls
				// slice is now authoritative and the partial cards were merged.
				return streamedTurn{
					text: finalText, reasoning: finalReasoning, signature: signature,
					reasoningID: reasoningID, reasoningStatus: reasoningStatus,
					calls: calls, responsesItems: responsesItems, usage: usage,
					partialCalls: partialCalls, maxArgChars: maxArgChars,
				}
			}
			chunk = c
		}
		switch chunk.Type {
		case provider.ChunkReasoning:
			reasoning.WriteString(chunk.Text)
			if chunk.Signature != "" {
				signature = chunk.Signature
			}
			// 元数据 chunk（空 Text）：reasoning item id/status 贯通
			// SSE → session → 下一轮回传（评审 #7234 第 1 点）。
			if chunk.ReasoningID != "" {
				reasoningID = chunk.ReasoningID
			}
			if chunk.ReasoningStatus != "" {
				reasoningStatus = chunk.ReasoningStatus
			}
			if chunk.Text != "" && !transformReasoning {
				sink.Emit(event.Event{Kind: event.Reasoning, Text: chunk.Text})
			}
			if a.reasoningByteLimit > 0 && reasoning.Len() > a.reasoningByteLimit {
				stored, _ := finishReasoning()
				usage = bestEffortStreamUsage(usage, text.Len(), reasoning.Len(), finishReasonClientReasoningLimit)
				usage = provider.UsageWithRequestAttemptCount(ctx, usage)
				a.storeLatestRequestUsage(usage)
				return collect(stored, errReasoningByteLimitExceeded)
			}
		case provider.ChunkText:
			text.WriteString(chunk.Text)
			sink.Emit(event.Event{Kind: event.Text, Text: chunk.Text})
		case provider.ChunkToolCallStart:
			partialToolStarted = true
			// Surface the tool card as soon as the call begins — before its
			// (possibly large) arguments finish streaming — so the user sees it
			// working instead of a stall. executeBatch emits the full dispatch
			// (with args) once the call completes; the frontend merges by ID.
			if tc := chunk.ToolCall; tc != nil {
				partialCalls = upsertPartialToolCall(partialCalls, *tc)
				sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
					ID: tc.ID, Name: tc.Name, ReadOnly: a.toolReadOnly(tc.Name), Partial: true, AttemptID: attemptID,
				}})
			}
		case provider.ChunkToolCallArgsDelta:
			partialToolStarted = true
			// Liveness ticks while a large argument payload streams: re-emit the
			// partial dispatch with the cumulative size (time-throttled) so the
			// UI can show progress instead of a dead counter for the duration of
			// a 30KB write_file body.
			if chunk.ArgChars > maxArgChars {
				maxArgChars = chunk.ArgChars
			}
			if tc := chunk.ToolCall; tc != nil && time.Since(lastArgProgress) >= 250*time.Millisecond {
				partialCalls = upsertPartialToolCall(partialCalls, *tc)
				lastArgProgress = time.Now()
				sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
					ID: tc.ID, Name: tc.Name, ReadOnly: a.toolReadOnly(tc.Name), Partial: true, ArgChars: chunk.ArgChars, AttemptID: attemptID,
				}})
			}
		case provider.ChunkToolCall:
			partialToolStarted = true
			if chunk.ToolCall != nil {
				calls = append(calls, *chunk.ToolCall)
				partialCalls = upsertPartialToolCall(partialCalls, *chunk.ToolCall)
				if n := len(chunk.ToolCall.Arguments); n > maxArgChars {
					maxArgChars = n
				}
			}
		case provider.ChunkResponsesItem:
			if len(chunk.ResponsesItem) > 0 {
				responsesItems = append(responsesItems, append(json.RawMessage(nil), chunk.ResponsesItem...))
			}
		case provider.ChunkUsage:
			usage, a.lastReasoning = chunk.Usage, chunk.Usage.ReasoningTokens
			a.storeLatestRequestUsage(chunk.Usage)
			a.sessCacheHit.Add(int64(chunk.Usage.CacheHitTokens))
			a.sessCacheMiss.Add(int64(chunk.Usage.CacheMissTokens))
		case provider.ChunkError:
			if provider.IsStreamInterrupted(chunk.Err) {
				stored, _ := finishReasoning()
				usage = bestEffortStreamUsage(usage, text.Len(), reasoning.Len(), "interrupted")
				usage = provider.UsageWithRequestAttemptCount(ctx, usage)
				st := collect(stored, chunk.Err)
				st.interrupted = true
				return st
			}
			stored, _ := finishReasoning()
			if errors.Is(chunk.Err, context.Canceled) || errors.Is(chunk.Err, context.DeadlineExceeded) {
				usage = bestEffortStreamUsage(usage, text.Len(), reasoning.Len(), "interrupted")
			}
			usage = provider.UsageWithRequestAttemptCount(ctx, usage)
			return collect(stored, chunk.Err)
		}
	}
}

func bestEffortStreamUsage(current *provider.Usage, textBytes, reasoningBytes int, finishReason string) *provider.Usage {
	if current == nil && textBytes == 0 && reasoningBytes == 0 {
		return nil
	}
	var usage provider.Usage
	if current != nil {
		usage = *current
	}
	if finishReason != "" {
		usage.FinishReason = finishReason
	}
	reasoningTokens := estimateTokensFromBytes(reasoningBytes)
	textTokens := estimateTokensFromBytes(textBytes)
	completionTokens := reasoningTokens + textTokens
	if usage.ReasoningTokens < reasoningTokens {
		usage.ReasoningTokens = reasoningTokens
		usage.Estimated = true
	}
	if usage.CompletionTokens < completionTokens {
		usage.CompletionTokens = completionTokens
		usage.Estimated = true
	}
	if minTotal := usage.PromptTokens + usage.CompletionTokens; usage.TotalTokens < minTotal {
		usage.TotalTokens = minTotal
		usage.Estimated = true
	}
	return &usage
}

func estimateTokensFromBytes(n int) int {
	if n <= 0 {
		return 0
	}
	tokens := n / 4
	if n%4 != 0 {
		tokens++
	}
	if tokens <= 0 {
		return 1
	}
	return tokens
}

func upsertPartialToolCall(calls []provider.ToolCall, call provider.ToolCall) []provider.ToolCall {
	for i := range calls {
		if call.ID != "" && calls[i].ID == call.ID {
			calls[i] = call
			return calls
		}
	}
	return append(calls, call)
}

func (a *Agent) recordInterruptedDisplay(text, reasoning string, calls []provider.ToolCall, pending bool, workDurationMs int64) {
	displayCalls := make([]provider.ToolCall, 0, len(calls))
	interrupted := make([]string, 0, len(calls))
	seen := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Name)
		key := call.ID + "\x00" + name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		displayCalls = append(displayCalls, provider.ToolCall{ID: call.ID, Name: name})
		if name != "" {
			interrupted = append(interrupted, name)
		}
	}
	a.session.Add(provider.Message{
		Role:             provider.RoleTool,
		Content:          text,
		ReasoningContent: reasoning,
		ToolCalls:        displayCalls,
		ToolCallID:       provider.LocalOnlyToolID,
		Name:             provider.LocalOnlyToolName,
		WorkDurationMs:   workDurationMs,
		LocalOnly:        true,
		InterruptedTurn: &provider.InterruptedTurnRecovery{
			Pending:                 pending,
			InterruptedTools:        interrupted,
			DroppedPartialText:      strings.TrimSpace(text) != "",
			DroppedPartialReasoning: strings.TrimSpace(reasoning) != "",
		},
	})
}

func (a *Agent) capturePrefixShape(schemas []provider.ToolSchema) PrefixShape {
	return CaptureShape(a.systemPrompt(), schemas, a.session.RewriteVersion())
}

func (a *Agent) systemPrompt() string {
	var b strings.Builder
	for _, m := range a.session.Messages {
		if m.Role != provider.RoleSystem {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(m.Content)
	}
	return b.String()
}

func toEventShellExecution(in *tool.ShellExecution, durationMs int64) *event.ShellExecution {
	if in == nil {
		return nil
	}
	out := &event.ShellExecution{
		Kind:           in.Kind,
		Shell:          in.Shell,
		ShellVersion:   in.ShellVersion,
		Platform:       in.Platform,
		SupportsAndAnd: in.SupportsAndAnd,
		State:          in.State,
		FailurePhase:   in.FailurePhase,
		OutputTail:     in.OutputTail,
		MutationRisk:   in.MutationRisk,
		Verification:   in.Verification,
		DurationMs:     in.DurationMs,
	}
	if out.DurationMs == 0 && durationMs > 0 {
		out.DurationMs = durationMs
	}
	if in.ExitCode != nil {
		code := *in.ExitCode
		out.ExitCode = &code
	}
	return out
}

func toProviderToolExecution(in *tool.ShellExecution) *provider.ToolExecution {
	if in == nil {
		return nil
	}
	out := &provider.ToolExecution{
		Kind:           in.Kind,
		Shell:          in.Shell,
		ShellVersion:   in.ShellVersion,
		Platform:       in.Platform,
		SupportsAndAnd: in.SupportsAndAnd,
		State:          in.State,
		FailurePhase:   in.FailurePhase,
		OutputTail:     in.OutputTail,
		MutationRisk:   in.MutationRisk,
		Verification:   in.Verification,
		DurationMs:     in.DurationMs,
	}
	if in.ExitCode != nil {
		code := *in.ExitCode
		out.ExitCode = &code
	}
	return out
}

func (a *Agent) emitFullToolDispatch(ctx context.Context, c provider.ToolCall, refreshed bool) {
	t, _, ambiguous := a.tools.ResolveCall(c.Name)
	ok := t != nil && len(ambiguous) == 0
	ev := event.Tool{ID: c.ID, Name: c.Name, Args: c.Arguments, ReadOnly: ok && t.ReadOnly(), Refreshed: refreshed}
	ev.FileDiff = event.FileDiff{Diff: c.Diff, Added: c.Added, Removed: c.Removed}
	if ok && ev.Diff == "" && ev.Added == 0 && ev.Removed == 0 {
		if ch, ok := tool.PreviewChange(ctx, t, json.RawMessage(c.Arguments)); ok {
			ev.FileDiff = event.FileDiff{Diff: ch.Diff, Added: ch.Added, Removed: ch.Removed}
		}
	}
	if ok {
		if pr, ok := t.(interface {
			ResolveProfile(json.RawMessage) *event.Profile
		}); ok {
			ev.Profile = pr.ResolveProfile(json.RawMessage(c.Arguments))
		}
	}
	a.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: ev})
}

// emitResolvedToolDispatch upserts the real target classification of a stable
// proxy call without changing the provider-visible Name/Args. Append-only sinks
// ignore Refreshed events; stateful frontends replace the existing card by ID.
func (a *Agent) emitResolvedToolDispatch(c provider.ToolCall) {
	if c.ResolvedReadOnly == nil {
		return
	}
	if c.ResolvedName != "" && c.ResolvedName != c.Name {
		EmitProxyAudit(a.sink, tool.ResolvedCall{
			DisplayName:  c.Name,
			TargetName:   c.ResolvedName,
			CapabilityID: c.CapabilityID,
		})
	}
	a.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
		ID:           c.ID,
		Name:         c.Name,
		Args:         c.Arguments,
		ResolvedName: c.ResolvedName,
		CapabilityID: c.CapabilityID,
		ReadOnly:     *c.ResolvedReadOnly,
		Refreshed:    true,
		FileDiff: event.FileDiff{
			Diff: c.Diff, Added: c.Added, Removed: c.Removed,
		},
	}})
}

// refreshCurrentFileDiff recomputes a writer preview against the state left by
// earlier successful writers in the same provider batch. Preview failures clear
// any stale initial diff; a later Execute will then fail or ask for recovery
// without presenting the user with a preview that no longer describes disk.
func refreshCurrentFileDiff(ctx context.Context, t tool.Tool, call provider.ToolCall) (provider.ToolCall, bool) {
	pv, ok := t.(tool.Previewer)
	if !ok {
		return call, false
	}
	refreshed := call
	refreshed.Diff = ""
	refreshed.Added = 0
	refreshed.Removed = 0
	if change, err := pv.Preview(ctx, json.RawMessage(call.Arguments)); err == nil {
		refreshed.Diff = change.Diff
		refreshed.Added = change.Added
		refreshed.Removed = change.Removed
	}
	changed := refreshed.Diff != call.Diff || refreshed.Added != call.Added || refreshed.Removed != call.Removed
	return refreshed, changed
}

func (a *Agent) withPreviewFileDiffs(ctx context.Context, calls []provider.ToolCall) []provider.ToolCall {
	if len(calls) == 0 {
		return calls
	}
	out := make([]provider.ToolCall, len(calls))
	copy(out, calls)
	for i := range out {
		if out[i].Diff != "" || out[i].Added != 0 || out[i].Removed != 0 {
			continue
		}
		t, _, ambiguous := a.tools.ResolveCall(out[i].Name)
		ok := t != nil && len(ambiguous) == 0
		if !ok {
			continue
		}
		if ch, ok := tool.PreviewChange(ctx, t, json.RawMessage(out[i].Arguments)); ok {
			out[i].Diff = ch.Diff
			out[i].Added = ch.Added
			out[i].Removed = ch.Removed
		}
	}
	return out
}

// completedMCPConnect recognizes a synthetic cache-miss connect call whose
// background discovery finished after the provider request was serialized. The
// connect placeholder is intentionally absent once real tools replace it, but
// the already-advertised call still completed its only job and must not surface
// as an unknown tool.
func completedMCPConnect(reg *tool.Registry, name string) (string, bool) {
	server, rawName, ok := tool.SplitMCPName(name)
	if !ok || rawName != "connect" {
		return "", false
	}
	prefix := tool.MCPNamePrefix + server + "__"
	for _, current := range reg.Names() {
		if current != name && strings.HasPrefix(current, prefix) {
			return server, true
		}
	}
	return "", false
}

// recoveryPlanTransition detects structural rewrites of an active canonical
// task list. Initial plans and progress-only status updates stay on the fast
// path; changing step identity, order, or hierarchy while work remains is a
// semantic transition for the independent Auto reviewer.
func (a *Agent) recoveryPlanTransition(toolName string, args json.RawMessage) (bool, string, string, string) {
	if a == nil || toolName != "todo_write" || a.planMode.Load() {
		return false, "", "", ""
	}
	before := a.CanonicalTodoState()
	if len(before) == 0 || len(evidence.IncompleteTodos(before)) == 0 {
		return false, "", "", ""
	}
	after := evidence.ReceiptFromToolCall("todo_write", args, true, true).Todos
	if len(after) == 0 || evidence.ValidateSerialTodos(after) != nil || !evidence.PreservesCompletedTodoPositions(before, after) {
		// Let todo_write report malformed or invalid state directly; an invalid
		// task list is not a meaningful plan proposal for the reviewer.
		return false, "", "", ""
	}
	if samePlanStructure(before, after) {
		return false, "", "", ""
	}
	return true, planReviewText(before), planReviewText(after), planTransitionDiff(before, after)
}

func samePlanStructure(a, b []evidence.TodoItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Level != b[i].Level || normalizePlanStep(a[i].Content) != normalizePlanStep(b[i].Content) {
			return false
		}
	}
	return true
}

func normalizePlanStep(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func planReviewText(todos []evidence.TodoItem) string {
	var b strings.Builder
	for i, todo := range todos {
		indent := ""
		if todo.Level == 1 {
			indent = "  "
		}
		fmt.Fprintf(&b, "%s%d. %s [%s]", indent, i+1, normalizePlanStep(todo.Content), canonicalTodoStatus(todo.Status))
		if i+1 < len(todos) {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func recoveryTaskScopeID(deliveryScopeID string, runSeq uint64) string {
	if scope := strings.TrimSpace(deliveryScopeID); scope != "" {
		return "goal:" + scope
	}
	return fmt.Sprintf("turn:%d", runSeq)
}

func (a *Agent) readOnlyExecutionBlock(visible tool.Tool, resolved *tool.ResolvedCall) (toolOutcome, bool) {
	if a == nil || !a.readOnlyExecution {
		return toolOutcome{}, false
	}
	block := func(reason string) (toolOutcome, bool) {
		return toolOutcome{
			output:  "blocked: read-only agent cannot " + reason,
			blocked: true,
			errMsg:  "blocked by read-only execution boundary",
		}, true
	}
	// Destructive MCP is left for the Executor; Planner must not misread this
	// as missing configuration or an unavailable MCP server.
	blockDestructiveForExecutor := func(name string) (toolOutcome, bool) {
		msg := "blocked: MCP capability " + name + " is destructive and is reserved for the Executor. Write the required operation into the plan/handoff so the Coordinator can hand it to the Executor; do not treat this as missing MCP configuration or an unavailable capability."
		return toolOutcome{
			output:  msg,
			blocked: true,
			errMsg:  "blocked: destructive MCP reserved for executor",
		}, true
	}
	if resolved == nil {
		if a.plannerMCPExecution && isMCPExecutionTarget(visible, "") {
			if !mcpServerAuthorized(visible) {
				return block("execute an MCP capability from an unauthorized server")
			}
			if readOnlyExecutionMCPDestructive(visible) {
				return blockDestructiveForExecutor(visible.Name())
			}
			return toolOutcome{}, false
		}
		if visible == nil || !visible.ReadOnly() {
			if reasoner, ok := visible.(tool.ReadOnlyExecutionBlockReason); ok && strings.TrimSpace(reasoner.ReadOnlyExecutionBlockReason()) != "" {
				return block(reasoner.ReadOnlyExecutionBlockReason())
			}
			return block("execute a state-changing tool")
		}
		if isInstalledMCPTool(visible) && !mcpServerAuthorized(visible) {
			return block("execute a reader from an unauthorized MCP server")
		}
		if readOnlyExecutionMCPDestructive(visible) {
			return block("execute a destructive MCP capability")
		}
		if h, ok := visible.(tool.ReadOnlyExecutionHostMutation); ok && h.ReadOnlyExecutionHostMutation() && !readOnlyExecutionAllowsMCPStartup(visible) {
			return block("start or mutate a host capability")
		}
		return toolOutcome{}, false
	}

	switch resolved.ProxyAction {
	case "list", "inspect":
		if !resolved.SkipExecute || resolved.Target != nil || !resolved.ReadOnly {
			return block("execute a malformed dynamic inspection")
		}
		return toolOutcome{}, false
	case "decline":
		return block("decline a capability decision")
	case "call":
		if resolved.Target == nil {
			if a.plannerMCPExecution && resolved.HostCompleted && resolved.SkipExecute && resolved.ReadOnly && !resolved.Unavailable {
				if _, ok := parseMCPServerCapabilityID(resolved.CapabilityID); ok {
					return toolOutcome{}, false
				}
			}
			return block("execute an unresolved dynamic capability")
		}
		if a.plannerMCPExecution && plannerAllowsMCPTarget(resolved.Target, resolved.TargetName) {
			if isMCPLifecycleConnectTarget(resolved.Target) {
				if !plannerMCPConnectAllowed(resolved.Target) {
					return block("start an unauthorized MCP server")
				}
			} else if !mcpServerAuthorized(resolved.Target) {
				return block("execute an MCP capability from an unauthorized server")
			}
			if readOnlyExecutionMCPDestructive(resolved.Target) {
				name := resolved.TargetName
				if name == "" {
					name = resolved.CapabilityID
				}
				return blockDestructiveForExecutor(name)
			}
			return toolOutcome{}, false
		}
		if !resolved.ReadOnly {
			if reasoner, ok := resolved.Target.(tool.ReadOnlyExecutionBlockReason); ok && strings.TrimSpace(reasoner.ReadOnlyExecutionBlockReason()) != "" {
				return block(reasoner.ReadOnlyExecutionBlockReason())
			}
			return block("execute a state-changing dynamic capability")
		}
		if isInstalledMCPTool(resolved.Target) && !mcpServerAuthorized(resolved.Target) {
			return block("execute a dynamic reader from an unauthorized MCP server")
		}
		if readOnlyExecutionMCPDestructive(resolved.Target) {
			return block("execute a destructive MCP capability")
		}
		if h, ok := resolved.Target.(tool.ReadOnlyExecutionHostMutation); ok && h.ReadOnlyExecutionHostMutation() && !readOnlyExecutionAllowsMCPStartup(resolved.Target) {
			return block("start or mutate a host capability")
		}
		return toolOutcome{}, false
	default:
		return block("execute an unknown dynamic capability action")
	}
}

func readOnlyExecutionMCPDestructive(t tool.Tool) bool {
	return mcpDestructiveHint(t)
}

func readOnlyExecutionAllowsMCPStartup(t tool.Tool) bool {
	if t == nil || !t.ReadOnly() || readOnlyExecutionMCPDestructive(t) {
		return false
	}
	if !mcpServerAuthorized(t) {
		return false
	}
	meta, ok := t.(tool.MCPMetadata)
	if !ok || strings.TrimSpace(meta.MCPServerName()) == "" || strings.TrimSpace(meta.MCPRawToolName()) == "" {
		return false
	}
	return true
}

// plannerAllowsMCPTarget reports whether a resolved use_capability target is an
// MCP tool or lifecycle connect that Planner may consider under
// PlannerMCPExecution (authorization and destructive checks run separately).
func plannerAllowsMCPTarget(t tool.Tool, targetName string) bool {
	if t == nil {
		return false
	}
	if isInstalledMCPTool(t) || isMCPLifecycleConnectTarget(t) {
		return true
	}
	return isMCPExecutionTarget(t, targetName)
}

// isMCPLifecycleConnectTarget identifies on-demand MCP connect-and-list targets
// (mcp_connect__<server>) used by use_capability action=call on mcp-server ids.
func isMCPLifecycleConnectTarget(t tool.Tool) bool {
	if t == nil {
		return false
	}
	if _, ok := t.(mcpLifecycleConnect); ok {
		return true
	}
	name := strings.TrimSpace(t.Name())
	return strings.HasPrefix(name, "mcp_connect__")
}

// mcpLifecycleConnect is implemented by deferred connect targets so Planner
// can authorize lifecycle actions without relying on name prefixes alone.
type mcpLifecycleConnect interface {
	MCPLifecycleConnect() bool
	MCPServerAuthorized() bool
}

func plannerMCPConnectAllowed(t tool.Tool) bool {
	if life, ok := t.(mcpLifecycleConnect); ok {
		return life.MCPServerAuthorized()
	}
	return mcpServerAuthorized(t)
}

func isInstalledMCPTool(t tool.Tool) bool {
	meta, ok := t.(tool.MCPMetadata)
	return ok && strings.TrimSpace(meta.MCPServerName()) != "" && strings.TrimSpace(meta.MCPRawToolName()) != ""
}

func isMCPExecutionTarget(t tool.Tool, name string) bool {
	return isInstalledMCPTool(t) || strings.HasPrefix(strings.TrimSpace(name), "mcp__")
}

func mcpServerAuthorized(t tool.Tool) bool {
	authority, ok := t.(tool.MCPServerAuthorization)
	return ok && authority.MCPServerAuthorized()
}

func mcpDestructiveHint(t tool.Tool) bool {
	annotations, ok := t.(tool.MCPAnnotations)
	return ok && annotations.MCPDestructiveHint()
}

func (a *Agent) planModeDecision(toolName string, readOnly bool, safety planmode.PlanSafety, args json.RawMessage) planmode.Decision {
	return (planmode.Policy{}).Decide(planmode.Call{
		Name:     toolName,
		ReadOnly: readOnly,
		Safety:   safety,
		Args:     args,
	})
}

func (a *Agent) repeatedSuccessBlock(call provider.ToolCall, t tool.Tool) (string, bool) {
	sig, ok := repeatSuccessSignature(call, t)
	if !ok || a.repeatSuccessCounts == nil {
		return "", false
	}
	count := a.repeatSuccessCounts[sig]
	if count < repeatSuccessBreakThreshold {
		return "", false
	}
	return fmt.Sprintf(
		"blocked: [loop guard] %q has already succeeded %d times with the same write-like arguments in this user turn. Re-running it is unlikely to help and may burn tokens or repeat file writes. Change approach: use edit_file or multi_edit for file changes, verify with a read/test command, or explain the blocker in your final answer.",
		call.Name, count), true
}

func (a *Agent) staleAnchorEditBlock(call provider.ToolCall) (string, bool) {
	if a.evidence == nil || !anchorBasedEditTool(call.Name) {
		return "", false
	}
	rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), true, false)
	if len(rec.Paths) == 0 {
		return "", false
	}
	writeIndex, ok := a.evidence.LatestSuccessfulWriteIndex(rec.Paths)
	if !ok || a.evidence.HasSuccessfulAnchorRefreshReadAfter(rec.Paths, writeIndex) {
		return "", false
	}
	return fmt.Sprintf(
		"blocked: [fresh read required] %q targets %s, which was already modified earlier this turn. Re-read the current file with read_file without offset/limit before another range deletion, or use multi_edit with exact replacements when possible. This prevents stale start/end anchors from selecting an unintended destructive span.",
		call.Name, strings.Join(rec.Paths, ", ")), true
}

func anchorBasedEditTool(name string) bool {
	switch name {
	// edit_file synchronously reads the current file, requires a unique exact
	// or narrowly fuzzy match, and returns the actual applied diff. Let it try
	// optimistically; a stale old_string fails without writing and tells the
	// model to re-read. delete_range remains guarded because two independently
	// resolved anchors can otherwise select an unintended destructive span.
	case "delete_range":
		return true
	default:
		return false
	}
}

func (a *Agent) recordRepeatSuccess(call provider.ToolCall, t tool.Tool) {
	sig, ok := repeatSuccessSignature(call, t)
	if !ok {
		return
	}
	if a.repeatSuccessCounts == nil {
		a.repeatSuccessCounts = make(map[string]int)
	}
	a.repeatSuccessCounts[sig]++
}

func repeatSuccessSignature(call provider.ToolCall, t tool.Tool) (string, bool) {
	if t.ReadOnly() {
		return "", false
	}
	switch call.Name {
	case "write_file", "edit_file", "multi_edit", "move_file", "notebook_edit":
		return call.Name + "\x00" + canonicalToolArgs(call.Arguments), true
	case "bash":
		var p struct {
			Command         string `json:"command"`
			RunInBackground bool   `json:"run_in_background"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &p); err != nil {
			return "", false
		}
		if p.RunInBackground || !isShellFileWriteCommand(p.Command) {
			return "", false
		}
		return "bash\x00" + normalizeShellCommand(p.Command), true
	default:
		return "", false
	}
}

func canonicalToolArgs(raw string) string {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return strings.TrimSpace(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return strings.TrimSpace(raw)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, b); err != nil {
		return string(b)
	}
	return compact.String()
}

func normalizeShellCommand(command string) string {
	if fields, malformed := shellparse.StaticFields(command); malformed == "" && len(fields) > 0 {
		return strings.Join(fields, " ")
	}
	return strings.Join(strings.Fields(command), " ")
}

func isShellFileWriteCommand(command string) bool {
	lower := strings.ToLower(command)
	switch {
	case shellPythonOpenWrites(lower):
		return true
	case strings.Contains(lower, "set-content") || strings.Contains(lower, "add-content") || strings.Contains(lower, "out-file"):
		return true
	case strings.Contains(lower, "sed -i") || strings.Contains(lower, "perl -pi"):
		return true
	case hasShellWriteRedirect(command):
		return true
	default:
		return false
	}
}

func shellPythonOpenWrites(lower string) bool {
	if !strings.Contains(lower, "open(") {
		return false
	}
	if strings.Contains(lower, ".write(") {
		return true
	}
	for _, marker := range []string{", 'w", `, "w`, ", 'a", `, "a`, ", 'x", `, "x`, "mode='w", `mode="w`, "mode='a", `mode="a`, "mode='x", `mode="x`} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func hasShellWriteRedirect(command string) bool {
	file, err := shellparse.ParseBash(command)
	if err == nil {
		hasWrite := false
		syntax.Walk(file, func(node syntax.Node) bool {
			redir, ok := node.(*syntax.Redirect)
			if !ok {
				return true
			}
			if bashRedirectWritesFile(command, redir) {
				hasWrite = true
				return false
			}
			return true
		})
		return hasWrite
	}
	return hasShellWriteRedirectFallback(command)
}

func bashRedirectWritesFile(source string, redir *syntax.Redirect) bool {
	if redir == nil {
		return false
	}
	switch redir.Op {
	case syntax.RdrOut, syntax.AppOut, syntax.RdrClob, syntax.AppClob,
		syntax.RdrAll, syntax.RdrAllClob, syntax.AppAll, syntax.AppAllClob,
		syntax.RdrInOut:
		return !redirectWordIsNullSink(source, redir.Word)
	default:
		return false
	}
}

func redirectWordIsNullSink(source string, word *syntax.Word) bool {
	if word == nil {
		return false
	}
	if value, ok := shellparse.StaticWord(word); ok {
		if isNullSinkWord(strings.TrimSpace(value)) {
			return true
		}
	}
	value := strings.TrimSpace(redirectWordSource(source, word))
	if isNullSinkWord(value) {
		return true
	}
	if len(value) >= 2 && ((value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"')) {
		return isNullSinkWord(value[1 : len(value)-1])
	}
	return false
}

func isNullSinkWord(value string) bool {
	if value == "/dev/null" {
		return true
	}
	return strings.EqualFold(value, "$null") || strings.EqualFold(value, "nul")
}

func redirectWordSource(source string, word *syntax.Word) string {
	if word == nil || !word.Pos().IsValid() || !word.End().IsValid() {
		return ""
	}
	start := int(word.Pos().Offset())
	end := int(word.End().Offset())
	if start < 0 || end < start || end > len(source) {
		return ""
	}
	return source[start:end]
}

func hasShellWriteRedirectFallback(command string) bool {
	var quote rune
	var prev rune
	for _, r := range command {
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			prev = r
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			prev = r
			continue
		}
		if r == '>' {
			if prev == '2' {
				prev = r
				continue
			}
			return true
		}
		prev = r
	}
	return false
}

// isBackgroundTaskCall reports whether a `task` call set run_in_background, so a
// fire-and-return dispatch isn't mistaken for a sub-agent that has stopped.
func isBackgroundTaskCall(args string) bool {
	var p struct {
		RunInBackground bool `json:"run_in_background"`
	}
	_ = json.Unmarshal([]byte(args), &p)
	return p.RunInBackground
}

// toolReadOnly reports a tool's ReadOnly classification by name (false for an
// unknown tool), for stamping early ToolDispatch events.
func (a *Agent) toolReadOnly(name string) bool {
	t, _, ambiguous := a.tools.ResolveCall(name)
	return t != nil && len(ambiguous) == 0 && t.ReadOnly()
}

// firstLine returns s up to its first newline — a one-line failure summary for
// the display Err, while the full error stays in the model-facing output.
func firstLine(s string) string {
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before
	}
	return s
}

// truncateToolOutput is the first-visible hard cap for a tool result. Under-cap
// bodies are returned byte-identical. Over-cap bodies keep a tool-aware head and
// tail under maxToolOutputBytes; the full original is stored separately as
// RawContent by the session writer. The bounded form is stable for the message
// lifetime and is never re-truncated by later maintenance.
func truncateToolOutput(s string) (string, string) {
	return truncateToolOutputFor(s, "", "")
}

// truncateToolOutputFor is the tool-aware first-visible limiter. toolName and
// toolCallID populate the truncation marker so the model can re-fetch.
func truncateToolOutputFor(s, toolName, toolCallID string) (string, string) {
	if len(s) <= maxToolOutputBytes {
		return s, ""
	}
	strategy := snipStrategy{head: 40, tail: 40, headChars: 8000, tailChars: 8000}
	switch {
	case toolName == "bash" || toolName == "shell" || strings.Contains(toolName, "bash"):
		strategy = snipStrategy{head: 40, tail: 40, headChars: 8000, tailChars: 8000}
	case toolName == "read_file" || toolName == "web_fetch" || strings.Contains(toolName, "read"):
		strategy = snipStrategy{head: 120, tail: 12, headChars: 12000, tailChars: 2000}
	case toolName == "grep" || toolName == "glob" || toolName == "ls" || toolName == "list_dir":
		strategy = snipStrategy{head: 80, tail: 8, headChars: 10000, tailChars: 1000}
	}
	headKeep := strategy.headChars
	tailKeep := strategy.tailChars
	if headKeep+tailKeep > maxToolOutputBytes-512 {
		headKeep = maxToolOutputBytes * 2 / 3
		tailKeep = maxToolOutputBytes - headKeep - 512
	}
	if headKeep < 1024 {
		headKeep = maxToolOutputBytes / 2
		tailKeep = maxToolOutputBytes / 2
	}
	// Prefer more tail when the body looks like a failure.
	lower := strings.ToLower(s)
	if strings.Contains(lower, "error:") || strings.Contains(lower, "panic:") || strings.Contains(lower, "fatal:") {
		tailKeep = max(tailKeep, maxToolOutputBytes/3)
		if headKeep+tailKeep > maxToolOutputBytes-512 {
			headKeep = maxToolOutputBytes - 512 - tailKeep
		}
	}
	head := snapToRuneBoundary(s, 0, headKeep)
	tail := snapToRuneBoundary(s, len(s)-tailKeep, len(s))
	omitted := len(s) - len(head) - len(tail)
	namePart := toolName
	if namePart == "" {
		namePart = "tool"
	}
	idPart := toolCallID
	if idPart == "" {
		idPart = "-"
	}
	notice := fmt.Sprintf("tool output truncated: %d of %d bytes elided", omitted, len(s))
	marker := fmt.Sprintf(
		"\n\n…[truncated tool=%s call_id=%s original_bytes=%d kept_bytes=%d — full original retained in canonical transcript; re-read or retry with narrower args]…\n\n",
		namePart, idPart, len(s), len(head)+len(tail),
	)
	body := head + marker + tail
	if len(body) > maxToolOutputBytes {
		overflow := len(body) - maxToolOutputBytes
		trimHead := overflow / 2
		trimTail := overflow - trimHead
		if trimHead < len(head) {
			head = snapToRuneBoundary(head, 0, len(head)-trimHead)
		}
		if trimTail < len(tail) {
			tail = snapToRuneBoundary(tail, trimTail, len(tail))
		}
		body = head + marker + tail
	}
	return body, notice
}

// snapToRuneBoundary returns s[lo:hi] with the bounds nudged outward until
// both land on rune-start positions.
func snapToRuneBoundary(s string, lo, hi int) string {
	for lo > 0 && !utf8.RuneStart(s[lo]) {
		lo--
	}
	for hi < len(s) && !utf8.RuneStart(s[hi]) {
		hi++
	}
	return s[lo:hi]
}

// finishReasonMessage maps an abnormal finish_reason to a one-line warning,
// returning ok=false for the normal terminations ("stop", "tool_calls") and a
// nil usage. The sink renders the message; the "! " prefix is presentation.
func finishReasonMessage(u *provider.Usage) (string, bool) {
	if u == nil {
		return "", false
	}
	switch u.FinishReason {
	case "length":
		return "response truncated: hit max output tokens", true
	case finishReasonClientReasoningLimit:
		return "response stopped: hit the client reasoning safety limit", true
	case "content_filter":
		return "response blocked by content filter", true
	case "repetition_truncation":
		return "response truncated: model repetition detected", true
	default:
		return "", false
	}
}
