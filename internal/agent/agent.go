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

	"reasonix/internal/capability"
	"reasonix/internal/diff"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
	"reasonix/internal/jobs"
	"reasonix/internal/memory"
	"reasonix/internal/nilutil"
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

const maxFinalReadinessBlocks = 3

// maxFinalReadinessBlocksWithProgress is the hard cap on readiness retries when
// the model keeps producing new host-observable receipts between blocks. A
// converging turn (edit → verify → review still catching up to the latest
// mutation) deserves more nudges than a stuck one; a turn that stalls with no
// new receipts still fails at maxFinalReadinessBlocks.
const maxFinalReadinessBlocksWithProgress = 6
const maxEmptyFinalBlocks = 3
const maxStreamRecoveries = 3
const maxExecutorHandoffNudges = 1

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

// FreshApprovalGate is an optional Gate extension for calls that must be
// answered by a current human decision even when Auto/YOLO or an allow rule
// would normally bypass approval. MCP destructiveHint uses it as a conservative
// fallback when the gate does not implement MCP-local approval policy.
type FreshApprovalGate interface {
	CheckFresh(ctx context.Context, toolName, subject string, args json.RawMessage, readOnly bool) (allow bool, reason string, err error)
}

// MCPApprovalGate applies local per-server/per-tool MCP policy without changing
// provider-visible tool metadata. Destructive calls are identified separately
// so the gate can apply the effective approval mode before deciding whether a
// fresh review is required.
type MCPApprovalGate interface {
	CheckMCP(ctx context.Context, toolName, subject string, args json.RawMessage, readOnly, destructive bool, mode, reviewer string) (allow bool, reason string, err error)
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
	prov        provider.Provider
	tools       *tool.Registry
	session     *Session
	sessMu      sync.Mutex // guards the session pointer for external Session()/SetSession
	maxSteps    int
	maxStepsKey string
	// executorHandoffGuard is enabled by Coordinator for the executor agent. The
	// per-turn marker check in Run keeps ordinary single-model turns unaffected.
	executorHandoffGuard bool
	temperature          float64
	pricing              *provider.Pricing
	usageSource          string
	responseLanguage     atomic.Value // string: auto|zh|en
	reasoningLanguage    atomic.Value // string: auto|zh|en

	// sink receives the turn's typed event stream (reasoning/text deltas, tool
	// dispatch/results, usage, notices). The agent no longer formats output
	// itself — a frontend's Sink decides how to render. Never nil; New defaults
	// it to event.Discard.
	sink event.Sink

	// lastUsage caches the most recent per-turn telemetry the provider reported so
	// the CLI can expose a context gauge without re-scraping the usage line. The
	// run loop writes it while a frontend's status line reads it, so it is atomic.
	lastUsage atomic.Pointer[provider.Usage]

	// sessCacheHit/sessCacheMiss accumulate cache tokens across every API call
	// this session, so frontends can show the aggregate hit-rate (Σhit/Σ(hit+miss))
	// — a steadier, cost-oriented number than the single-turn rate. They are NOT
	// reset on compaction (compaction only rewrites session.Messages), so the
	// aggregate never craters when the prefix is summarized away. Atomic: the run
	// loop accumulates them while the status line reads them.
	sessCacheHit  atomic.Int64
	sessCacheMiss atomic.Int64

	// lastPrefixShape records the previous provider request's cacheable prefix
	// so usage events can explain prefix churn on the next request.
	lastPrefixShape     PrefixShape
	haveLastPrefixShape bool

	// warnedMissingToolCallReasoning dedupes the missing tool-call reasoning
	// notice: when an endpoint stops emitting reasoning it tends to do so for
	// every following round, so the first notice carries the signal and
	// per-round repeats only flood the transcript. Loop-owned; reset by
	// SetSession so a swapped-in conversation warns anew.
	warnedMissingToolCallReasoning bool

	// planMode enables planning workflow instructions and explicit phase opt-outs.
	// It does not replace the permission or sandbox boundary. The system prompt and
	// tool list never change with the toggle, preserving the provider-cache prefix.
	planMode atomic.Bool

	// readOnlyExecution is a construction-time defense for planner/research
	// agents. Unlike planMode it is not a collaboration toggle: it remains on
	// for the agent's lifetime and validates proxy calls after resolution.
	readOnlyExecution bool

	// gate, when non-nil, is the per-call permission gate for both standard and
	// Plan workflows. nil disables gating entirely.
	gate Gate

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
	// Set via SetPreEditHook.
	onPreEdit func(diff.Change)

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

	// steerQueue holds mid-turn user messages queued while the agent is
	// running. Each is consumed once per loop iteration, persisted to the
	// session for history replay, and sent to the model as guidance (not a
	// new task). Cache miss for the next API call is unavoidable but limited
	// to one call — the prefix stays stable otherwise.
	steerMu       sync.Mutex
	steerQueue    []string
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
	todoMu    sync.Mutex
	todoState []evidence.TodoItem

	// hostAdvanceSeq guarantees unique tool IDs across turns: every
	// emitTodoState call increments it so the frontend always sees a fresh
	// dispatch even when the same panel index is signed off in different turns.
	hostAdvanceSeq atomic.Int64

	// projectChecks are structured project instructions that complete_step can
	// verify against same-turn bash receipts after a write-backed completion.
	projectChecks []instruction.VerifyCheck

	// deliveryProfile enables the runtime-enforced delivery contract. The stable
	// profile prompt explains intent; these fields are host state and never enter
	// the provider-cached prefix. deliveryCriteriaEstablished resets per user turn
	// but may inherit an unfinished canonical task list on continuation.
	deliveryProfile             bool
	deliveryCriteriaEstablished bool
	deliveryTaskExpected        bool
	deliveryMutationExpected    bool
	deliveryScopeID             string
	deliveryScopeActive         bool
	deliveryCheckpoint          evidence.DeliveryCheckpoint

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

	// Context management: when a turn's prompt nears contextWindow, the older
	// middle of the session is summarized away, keeping a token-bounded recent
	// tail verbatim (recentKeep is the message floor) and archiving the originals
	// under archiveDir. compactStuck latches when compaction can't get the prompt
	// under the window (consecutiveCompacts crosses the limit), so auto-compaction
	// pauses instead of looping. softCompactNoticed gates the one-shot soft-ratio
	// notice so it fires once per approach, not every turn.
	contextWindow       int
	softCompactRatio    float64
	toolResultSnipRatio float64
	compactRatio        float64
	compactForceRatio   float64
	softCompactNoticed  bool
	recentKeep          int
	archiveDir          string
	keepPolicy          KeepPolicy
	compactStuck        bool
	consecutiveCompacts int

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

	// blockedTurnStreak counts consecutive turns in which every tool call was
	// blocked by the host (permission, plan mode, hook, or loop guard).
	// stormSig catches a model fixated on one call shape; this catches a model
	// rotating between blocked shapes — alternating tools, reordering a batch,
	// or blockers whose text varies per attempt — which is zero progress all
	// the same. Reset by any turn containing a non-blocked outcome and at the
	// start of each user turn. See applyStormBreaker.
	blockedTurnStreak int

	// loopGuardArmed / loopGuardReceiptMark let final readiness stand down
	// after a loop guard fired this user turn: once the host has told the model
	// to stop retrying and report the blocker, demanding the receipts that the
	// blocker prevents would restart the loop the guard just broke. The mark is
	// the evidence-ledger receipt count from just before the guarded batch, so
	// real progress — a successful write or command receipt landing after it —
	// revokes the pass, while the bookkeeping the guard itself recommends
	// (ask, todo_write, complete_step) keeps it. Host state, not message text:
	// tool output that merely quotes "[loop guard]" must not unlock readiness.
	// Reset at the start of each user turn. See loopGuardAllowsFinal.
	loopGuardArmed       bool
	loopGuardReceiptMark int

	// repeatSuccessCounts tracks write-like tool calls that have already
	// succeeded in this user turn. This catches the complementary loop shape to
	// stormSig: a model keeps doing the same successful write, so there is no
	// error for the failure-only storm breaker to see.
	repeatSuccessCounts map[string]int
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
func (a *Agent) SetPreEditHook(fn func(diff.Change)) { a.onPreEdit = fn }

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
	a.warnedMissingToolCallReasoning = false
	if s != nil {
		a.rebuildTodoState(s.Snapshot())
	}
}

// LastUsage returns the most recent per-turn token telemetry the provider
// reported (nil if no turn has run yet). The TUI uses it to show a context
// gauge alongside the prompt; the actual cache decisions still live inside
// maybeCompact.
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

// Steer queues a message for mid-turn injection. It reports whether an active
// turn accepted the text; on false nothing was queued and the caller must
// deliver it another way (typically as a new turn). Without the active check,
// a steer landing in the window between the turn's exit flush and the
// controller observing running=false would sit in the queue unconsumed and
// unpersisted — invisible to both the model and history.
func (a *Agent) Steer(text string) bool {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	if !a.steerRunActive {
		return false
	}
	a.steerQueue = append(a.steerQueue, text)
	a.steerConsumed = false
	return true
}

// SteerConsumed returns true when the steer queue became empty after the last consume.
func (a *Agent) SteerConsumed() bool {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	return a.steerConsumed
}

func (a *Agent) consumeSteer() (string, bool) {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	if len(a.steerQueue) == 0 {
		return "", false
	}
	t := a.steerQueue[0]
	a.steerQueue = a.steerQueue[1:]
	a.steerConsumed = len(a.steerQueue) == 0
	return t, true
}

// flushSteerQueue ends the turn's steer intake: it drains whatever is still
// queued and persists each entry to the session, exactly as the in-loop
// consume would have (#6238 — a dropped steer vanished from both the model's
// context and history). The flushed steers reach the model on the next turn;
// the Steer event keeps the transcript honest about when they arrived.
func (a *Agent) flushSteerQueue() {
	a.steerMu.Lock()
	pending := a.steerQueue
	a.steerQueue = nil
	if len(pending) > 0 {
		a.steerConsumed = true
	}
	a.steerRunActive = false
	a.steerMu.Unlock()
	for _, text := range pending {
		a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(midTurnSteerMessage(text))})
		a.sink.Emit(event.Event{Kind: event.Steer, Text: text})
	}
}

func (a *Agent) steerQueueLen() int {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	return len(a.steerQueue)
}

// CompactRatio returns the fraction of the window at which auto-compaction
// fires (e.g. 0.8). The status line uses it to show headroom to the next compact.
func (a *Agent) CompactRatio() float64 { return a.compactRatio }

// CompactNow runs one compaction pass immediately, regardless of the
// usage-ratio threshold maybeCompact normally honours. Used by the chat
// TUI's `/compact` command so the user can reset the prefix before it
// naturally fills up.
func (a *Agent) CompactNow(ctx context.Context, instructions string) error {
	return a.compact(ctx, "manual", instructions, true)
}

// Options configures an Agent.
type Options struct {
	MaxSteps int
	// MaxStepsKey names the explicit runtime control shown when the MaxSteps guard
	// is hit. Empty defaults to the generic max_steps tool/runtime parameter.
	MaxStepsKey string
	Temperature float64
	Pricing     *provider.Pricing // optional, for per-turn cost display
	UsageSource string            // optional billable usage source; default executor

	// Gate is the per-call permission gate. nil disables gating.
	Gate Gate

	// ReadOnlyExecution enables a permanent host-side read-only boundary for
	// planner and research agents. It is intentionally independent of Plan mode
	// so a stale collaboration flag cannot authorize a dynamic writer target.
	ReadOnlyExecution bool

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
	ContextWindow       int
	SoftCompactRatio    float64
	ToolResultSnipRatio float64
	CompactRatio        float64
	CompactForceRatio   float64
	RecentKeep          int
	ArchiveDir          string
	KeepPolicy          KeepPolicy

	// Hooks fires PreToolUse / PostToolUse shell hooks around tool calls. nil
	// disables hook firing.
	Hooks ToolHooks

	// Jobs is the session's background-job manager (nil disables background tools).
	Jobs *jobs.Manager

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

	// ClassifierTaskText, when non-empty, is the pristine task text delivery
	// intent classification should judge instead of the raw Run input. Sub-agent
	// spawners set it before prepending host framing (subagent/workspace context,
	// review contracts) so framing verbs cannot arm expectations and user input
	// dressed up as framing cannot disarm them.
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

	// PlanModeAllowedTools is retained for source compatibility. MCP assembly may
	// still translate legacy entries into local read-only trust, but it does not
	// grant or revoke calls in the main Plan workflow.
	PlanModeAllowedTools []string
	// PlanModeReadOnlyCommands is retained for old config/controller data. Main
	// Plan execution classifies bash through Permissions instead.
	PlanModeReadOnlyCommands []string

	// SubagentDepth is the current nesting depth for this agent. Root sessions are
	// depth 0; child subagents are depth 1. MaxSubagentDepth caps delegation.
	SubagentDepth    int
	MaxSubagentDepth int
}

// New constructs an Agent. MaxSteps <= 0 means no cap — the run loop continues
// until the model gives a final answer, the context is cancelled, or the
// provider errors (compaction keeps the context bounded). A nil sink is replaced
// with event.Discard so the agent can always emit unconditionally.
func New(prov provider.Provider, tools *tool.Registry, session *Session, opts Options, sink event.Sink) *Agent {
	if opts.SoftCompactRatio <= 0 {
		opts.SoftCompactRatio = defaultSoftCompactRatio
	}
	if opts.ToolResultSnipRatio <= 0 {
		opts.ToolResultSnipRatio = defaultToolResultSnipRatio
	}
	if opts.CompactRatio <= 0 {
		opts.CompactRatio = defaultCompactRatio
	}
	if opts.ToolResultSnipRatio >= opts.CompactRatio {
		opts.ToolResultSnipRatio = opts.CompactRatio
	}
	if opts.CompactForceRatio <= 0 {
		opts.CompactForceRatio = defaultCompactForceRatio
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
	subagentDepth := opts.SubagentDepth
	if subagentDepth < 0 {
		subagentDepth = 0
	}
	a := &Agent{
		prov:                  prov,
		tools:                 tools,
		session:               session,
		maxSteps:              opts.MaxSteps,
		maxStepsKey:           maxStepsKey,
		temperature:           opts.Temperature,
		pricing:               opts.Pricing,
		usageSource:           usageSourceOrDefault(opts.UsageSource, event.UsageSourceExecutor),
		sink:                  sink,
		gate:                  gate,
		readOnlyExecution:     opts.ReadOnlyExecution,
		planModeReadOnlyTrust: planModeReadOnlyTrust,
		sandboxEscapeApprover: sandboxEscapeApprover,
		configWriteApprover:   configWriteApprover,
		hooks:                 hooks,
		jobs:                  opts.Jobs,
		writeScheduler:        opts.WriteScheduler,
		writeWorkspaceRoot:    strings.TrimSpace(opts.WriteWorkspaceRoot),
		workspaceLease:        opts.WorkspaceLease,
		evidence:              evidence.NewLedger(),
		projectChecks:         append([]instruction.VerifyCheck(nil), opts.ProjectChecks...),
		deliveryProfile:       opts.DeliveryProfile,
		classifierTaskText:    opts.ClassifierTaskText,
		capabilityLedger:      opts.CapabilityLedger,
		capabilityAudit:       opts.CapabilityAudit,
		contextWindow:         opts.ContextWindow,
		softCompactRatio:      opts.SoftCompactRatio,
		toolResultSnipRatio:   opts.ToolResultSnipRatio,
		compactRatio:          opts.CompactRatio,
		compactForceRatio:     opts.CompactForceRatio,
		recentKeep:            opts.RecentKeep,
		archiveDir:            opts.ArchiveDir,
		keepPolicy:            opts.KeepPolicy,
		subagentDepth:         subagentDepth,
		maxSubagentDepth:      maxSubagentDepth,
	}
	a.SetResponseLanguage(opts.ResponseLanguage)
	a.SetReasoningLanguage(opts.ReasoningLanguage)
	return a
}

func usageSourceOrDefault(source, fallback string) string {
	source = strings.TrimSpace(source)
	if source != "" {
		return source
	}
	return fallback
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
// final answer (no tool calls), the context is cancelled, or the provider errors.
// With maxSteps <= 0 the loop is unbounded — the natural termination is the model
// finishing, and the real safety bounds are user cancellation and compaction, not
// a round count. A positive maxSteps imposes an optional hard guard, surfaced as
// a resumable notice when hit.
func (a *Agent) Run(ctx context.Context, input string) (runErr error) {
	if a.deliveryProfile && a.workspaceLease != nil {
		a.workspaceLease.BeginRun()
		defer a.workspaceLease.EndRun()
	}
	rawInput := input
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
	scope, scoped := DeliveryExecutionScopeFromContext(ctx)
	preserveEvidence := a.preserveEvidenceOnce
	if a.evidence != nil {
		switch {
		case preserveEvidence:
			a.evidence.ResetBackgroundLeases()
		case scoped && a.deliveryScopeID == scope.ID:
			a.evidence.ResetBackgroundLeases()
		default:
			a.evidence.Reset()
		}
	}
	a.preserveEvidenceOnce = false
	if !preserveEvidence {
		a.deliveryRecoveryPending = false
	}
	if scoped {
		a.deliveryScopeID = scope.ID
	} else if !preserveEvidence {
		a.deliveryScopeID = ""
	}
	a.deliveryScopeActive = scoped
	if scoped && a.deliveryCheckpoint.ScopeID != scope.ID {
		a.deliveryCheckpoint = evidence.DeliveryCheckpoint{ScopeID: scope.ID}
	}
	// Re-lease this session's background-job mutations that no turn has
	// committed yet. The Reset above just wiped any lease a failed or
	// cancelled turn held (its ledger is gone), and a process restart starts
	// from an empty ledger too — in both cases the job manager still marks the
	// job's evidence uncommitted. Without re-injecting it here, a turn that
	// never re-issues wait/bash_output (the model has no reason to if it
	// doesn't know a mutation is still pending) would ship the background
	// change without the final-readiness gate ever seeing it. Plan turns defer
	// this lease like collectBackgroundEvidence does so execution evidence is
	// consumed and audited only after plan approval.
	if a.evidence != nil && a.jobs != nil && !a.planMode.Load() {
		session := jobs.SessionFromContext(ctx)
		for _, jobID := range a.jobs.PendingEvidenceJobIDsForSession(session) {
			summary, ready := a.jobs.TryLeaseEvidenceForSession(session, jobID)
			if !ready {
				continue
			}
			if !a.evidence.NoteBackgroundLease(session, jobID) {
				continue
			}
			a.evidence.MergeChild(summary)
		}
	}
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
	a.deliveryCriteriaEstablished = a.hasIncompleteCanonicalCriteria() ||
		(a.evidence != nil && a.evidence.HasSuccessfulTodoWrite()) ||
		(scoped && a.deliveryCheckpoint.CriteriaEstablished)
	if scoped {
		defer func() { a.updateDeliveryCheckpoint(runErr) }()
	}
	// Classify delivery expectations from the task text. Sub-agent spawners
	// pass the pristine task through Options.ClassifierTaskText (a trusted
	// host channel) because their Run input carries host framing whose
	// incidental verbs — "file tools resolve relative paths" — once classified
	// every workspace-wrapped subagent prompt as a mutation request and
	// deadlocked read-only subagents. Without the override the raw input is
	// classified verbatim: stripping user-controllable markup here would let
	// input dressed up as host framing disarm the delivery gates.
	classifierInput := a.classifierTaskText
	if scoped && strings.TrimSpace(scope.TaskText) != "" {
		classifierInput = scope.TaskText
	} else if strings.TrimSpace(classifierInput) == "" {
		classifierInput = rawInput
	}
	a.deliveryTaskExpected = deliveryTaskNeedsEvidence(classifierInput)
	a.deliveryMutationExpected = deliveryTaskNeedsMutation(classifierInput) && registryHasWriterTools(a.tools)
	a.repeatSuccessCounts = nil
	a.blockedTurnStreak = 0
	a.loopGuardArmed = false
	a.loopGuardReceiptMark = 0
	a.sink.Emit(event.Event{Kind: event.TurnStarted})
	input = a.withTurnPreferences(rawInput)
	a.session.Add(provider.Message{Role: provider.RoleUser, Content: input, Images: userImages(ctx), CreatedAt: time.Now().UnixMilli()})

	finalReadinessBlocks := 0
	seenReadinessStates := make(map[string]struct{})
	emptyFinalBlocks := 0
	handoffNudges := 0
	usedAnyTool := false
	streamRecoveries := 0
	graceRound := false
	todoProgress, trackingTodoProgress := a.canonicalTodoProgress()
	todoStallRounds := 0
	seenTodoProgress := make(map[string]struct{})
	if a.evidence != nil {
		for _, sig := range a.evidence.SuccessfulProgressSignaturesSince(0) {
			seenTodoProgress[sig] = struct{}{}
		}
	}
	executorHandoff := a.executorHandoffGuard && strings.Contains(input, executorHandoffMarker)
	for step := 0; a.maxSteps <= 0 || step < a.maxSteps || graceRound; step++ {
		// Consume a queued steer and persist it to the session so it
		// survives tab switches and history replay. The model sees it as
		// guidance (with a prefix), not a new task. One cache miss per
		// steer is unavoidable — the model must see the new instruction.
		if text, ok := a.consumeSteer(); ok {
			a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(midTurnSteerMessage(text))})
			a.sink.Emit(event.Event{Kind: event.Steer, Text: text})
		}
		schemas := a.tools.Schemas()
		prefixShape := a.capturePrefixShape(schemas)
		prevPrefixShape := a.lastPrefixShape
		if !a.haveLastPrefixShape {
			prevPrefixShape = prefixShape
		}

		text, reasoning, signature, calls, usage, interrupted, partialToolStarted, err := a.stream(ctx, step+1)
		if err != nil {
			if interrupted && streamRecoveries < maxStreamRecoveries {
				streamRecoveries++
				if hasVisibleFinalAnswer(text) {
					a.session.Add(provider.Message{
						Role:               provider.RoleAssistant,
						Content:            text,
						ReasoningContent:   reasoning,
						ReasoningSignature: signature,
						WorkDurationMs:     workDurationMs(),
					})
				}
				a.session.Add(provider.Message{
					Role:    provider.RoleUser,
					Content: a.withTurnPreferences(streamRecoveryMessage(hasVisibleFinalAnswer(text), partialToolStarted)),
				})
				a.sink.Emit(event.Event{Kind: event.Retrying, RetryAttempt: streamRecoveries, RetryMax: maxStreamRecoveries})
				step-- // recovery retries do not consume the tool-round maxSteps budget
				continue
			}
			return err
		}
		streamRecoveries = 0
		cacheDiagnostics := CompareShape(prevPrefixShape, prefixShape, usage)
		a.lastPrefixShape = prefixShape
		a.haveLastPrefixShape = true
		if usage != nil && usage.TotalTokens > 0 {
			a.sink.Emit(event.Event{Kind: event.Usage, Usage: usage, Pricing: a.pricing,
				UsageSource:      a.usageSource,
				CacheDiagnostics: &cacheDiagnostics,
				SessionHit:       int(a.sessCacheHit.Load()), SessionMiss: int(a.sessCacheMiss.Load())})
		}
		if msg, ok := finishReasonMessage(usage); ok {
			a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: msg})
		}

		// Keep reasoning_content on the assistant turn for display and session
		// archive. It is NOT re-uploaded to the API: the openai provider drops it
		// when building the request, since re-sent reasoning is billable prompt
		// input for no cache or coherence gain.
		calls = a.withPreviewFileDiffs(calls)
		a.warnMissingToolCallReasoning(calls, reasoning)
		a.session.Add(provider.Message{
			Role:               provider.RoleAssistant,
			Content:            text,
			ReasoningContent:   reasoning,
			ReasoningSignature: signature,
			ToolCalls:          calls,
			WorkDurationMs:     workDurationMs(),
		})

		if len(calls) == 0 {
			finalizeTask := !a.deliveryScopeActive || deliveryDisposition(text) == deliveryGoalFinal
			readiness := a.finalReadinessCheckFor(finalizeTask)
			if graceRound && (readiness.reason != "" || !hasVisibleFinalAnswer(text)) {
				a.maybeCompact(ctx, usage)
				return &maxStepsPause{steps: a.maxSteps, key: a.maxStepsKey}
			}
			if readiness.reason != "" {
				// Extend the base retry budget only when the missing-requirement
				// state actually changes. Counting ledger length let repeated reads,
				// failed bookkeeping, or other irrelevant receipts masquerade as
				// convergence and burn through six expensive model calls.
				state := readiness.progressSignature()
				_, repeatedState := seenReadinessStates[state]
				progressed := finalReadinessBlocks > 0 && !repeatedState
				seenReadinessStates[state] = struct{}{}
				finalReadinessBlocks++
				exhausted := finalReadinessBlocks >= maxFinalReadinessBlocksWithProgress ||
					(finalReadinessBlocks >= maxFinalReadinessBlocks && !progressed)
				result := evidence.ReadinessBlocked
				if exhausted {
					result = evidence.ReadinessErrored
					event.RecordReadinessAudit(a.sink, readiness.audit(result, false))
					a.deliveryRecoveryPending = true
					return &FinalReadinessError{Attempts: finalReadinessBlocks, Reason: readiness.reason, Missing: readiness.missingIDs()}
				}
				event.RecordReadinessAudit(a.sink, readiness.audit(result, false))
				a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeFinalReadiness, Text: finalReadinessNoticeText(), Detail: readiness.reason})
				a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(finalReadinessRetryMessage(readiness.reason))})
				a.maybeCompact(ctx, usage)
				continue
			}
			if !hasVisibleFinalAnswer(text) {
				// DeepSeek thinking mode can stream a long reasoning_content and
				// then finish with finish_reason="stop" but an empty content
				// block: the model has explicitly signalled completion and its
				// reasoning was already streamed to the user. Retrying here overrides
				// that stop signal and forces another expensive thinking round (the
				// "still thinking after the task is done" symptom), so honour the
				// stop when reasoning carried the substance of the answer and treat
				// the turn as a final answer instead of retrying.
				if !reasoningOnlyFinishHonoured(a.prov, usage, reasoning) {
					emptyFinalBlocks++
					if emptyFinalBlocks >= maxEmptyFinalBlocks {
						return fmt.Errorf("model finished without a visible final answer %d times", emptyFinalBlocks)
					}
					a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeEmptyFinal, Text: emptyFinalNotice(), Detail: emptyFinalNoticeDetail(a.prov.Name(), usage, len(reasoning))})
					a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(emptyFinalRetryMessage())})
					a.maybeCompact(ctx, usage)
					continue
				}
			}
			if executorHandoff && !usedAnyTool && handoffNudges < maxExecutorHandoffNudges && shouldNudgeExecutorHandoff(input, text) {
				handoffNudges++
				a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeExecutorHandoff, Text: executorHandoffNoticeText(), Detail: "executor answered without taking any action; nudging it to use its tools"})
				a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(executorHandoffRetryMessage())})
				a.maybeCompact(ctx, usage)
				continue
			}
			if readiness.applies {
				event.RecordReadinessAudit(a.sink, readiness.audit(evidence.ReadinessAllowed, finalReadinessBlocks > 0))
			}
			if a.steerQueueLen() > 0 {
				continue
			}
			// A final-answer turn otherwise skips compaction, so a large context
			// carries into the next turn un-folded and can overflow the model window.
			// No-op below the trigger, so normal turns keep their warm cache.
			a.maybeCompact(ctx, usage)
			return nil // model gave a final answer
		}
		emptyFinalBlocks = 0
		usedAnyTool = true

		// Grace round guard: if we already gave the model one extra response
		// and it still wants to call tools, stop here.
		if graceRound {
			return &maxStepsPause{steps: a.maxSteps, key: a.maxStepsKey}
		}

		receiptMark := 0
		if a.evidence != nil {
			receiptMark = a.evidence.Len()
		}
		results, images := a.executeBatch(ctx, calls)
		for i, call := range calls {
			a.session.Add(provider.Message{
				Role:       provider.RoleTool,
				Content:    results[i],
				Images:     images[i],
				ToolCallID: call.ID,
				Name:       call.Name,
			})
		}
		// If the context was cancelled during tool execution, return after storing
		// the batch results so the session keeps paired tool-call history.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !a.planMode.Load() {
			nextProgress, nextTracking := a.canonicalTodoProgress()
			hostProgress := false
			if a.evidence != nil {
				for _, sig := range a.evidence.SuccessfulProgressSignaturesSince(receiptMark) {
					if _, seen := seenTodoProgress[sig]; !seen {
						hostProgress = true
						seenTodoProgress[sig] = struct{}{}
					}
				}
			}
			switch {
			case !nextTracking:
				todoStallRounds = 0
			case !trackingTodoProgress || nextProgress > todoProgress || hostProgress:
				todoStallRounds = 0
			default:
				todoStallRounds++
			}
			todoProgress, trackingTodoProgress = nextProgress, nextTracking
			if todoStallRounds == todoProgressNudgeRounds {
				nudge := todoProgressNudgeMessage(todoStallRounds)
				a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(nudge)})
				a.sink.Emit(event.Event{
					Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeLoopGuard,
					Text:   loopGuardNoticeText(),
					Detail: fmt.Sprintf("the current todo has no new completion, unique read, command, or mutation for %d consecutive tool-call rounds; asking the assistant to reassess", todoStallRounds),
				})
			}
			if todoStallRounds >= maxTodoStallRounds {
				a.sink.Emit(event.Event{
					Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeLoopGuard,
					Text:   "Task progress stalled; pausing before more tools are called.",
					Detail: fmt.Sprintf("the current todo has no new completion, unique read, command, or mutation for %d consecutive tool-call rounds after a host reassessment; work is saved and can be resumed", todoStallRounds),
				})
				return &todoStallPause{rounds: todoStallRounds}
			}
		}

		// The prompt only grows from here; compact before the next turn so it
		// stays within the model's window.
		a.maybeCompact(ctx, usage)

		// When the tool-call budget runs out this round, give the model
		// one grace round to produce a final answer from completed work.
		if a.maxSteps > 0 && step+1 >= a.maxSteps {
			graceRound = true
			nudge := fmt.Sprintf("Do not call any more tools — your tool-call round limit (%s) has been reached. Instead, synthesize a final answer from all the work already completed: summarize what was accomplished, what remains to be done, and any decisions the user should make. The user can increase %s or continue in the next turn if more work is needed.", a.maxStepsKey, a.maxStepsKey)
			a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withTurnPreferences(nudge)})
			a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeToolBudget, Text: toolBudgetNoticeText(), Detail: fmt.Sprintf("budget (%s=%d) exhausted: one grace round to finalize", a.maxStepsKey, a.maxSteps)})
		}
	}
	// Only reached when a positive maxSteps guard is configured. The work so far
	// is already in the session, so the user can just send another message to pick
	// up where it left off.
	return &maxStepsPause{steps: a.maxSteps, key: a.maxStepsKey}
}

// warnMissingToolCallReasoning surfaces a thinking-mode tool_calls turn that
// arrived without reasoning text only when the provider/model is expected to
// emit it. The turn is still saved and the replay still succeeds (the wire
// layer always emits the reasoning_content key on such turns), but models that
// rely on tool-call reasoning continue without their chain-of-thought context,
// so that degradation is worth one visible warning. Exactly one per session:
// the shape is endpoint-conditional (observed on the official DeepSeek API as
// well as behind gateways) and tends to repeat for every round once it starts,
// so per-round notices bury the transcript without adding signal (#6259).
func (a *Agent) warnMissingToolCallReasoning(calls []provider.ToolCall, reasoning string) {
	if len(calls) == 0 || !provider.WarnOnMissingToolCallReasoning(a.prov) {
		return
	}
	if strings.TrimSpace(reasoning) != "" {
		return
	}
	if a.warnedMissingToolCallReasoning {
		return
	}
	a.warnedMissingToolCallReasoning = true
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
		Text:   fmt.Sprintf("%s returned tool calls without reasoning_content; continuing, but thinking context is lost on such turns (shown once per session)", a.prov.Name()),
		Detail: fmt.Sprintf("this round carried %d tool call(s) and no reasoning. Whether reasoning accompanies tool calls is endpoint-side behavior; the turn is saved and replayed with an empty reasoning_content key, which the API accepts. Later rounds with the same shape stay silent for the rest of the session.", len(calls))})
}

// maxStepsPause is the deliberate stop when a positive tool-call budget runs
// out: the session already holds the completed work and the user is asked to
// continue. It is a control-flow signal, not a provider failure — Coordinator
// matches on it to surface the pause instead of degrading the turn to
// executor-only.
type maxStepsPause struct {
	steps int
	key   string
}

func (e *maxStepsPause) Error() string {
	return fmt.Sprintf("paused after %d tool-call rounds (%s) — the work so far is saved; send another message to continue, or set %s higher or to 0 for no limit", e.steps, e.key, e.key)
}

type todoStallPause struct {
	rounds int
}

func (e *todoStallPause) Error() string {
	return fmt.Sprintf("paused after %d tool-call rounds without advancing the current todo — the work so far is saved; inspect the blocker or send another message to continue", e.rounds)
}

func isToolLoopPause(err error) bool {
	var maxPause *maxStepsPause
	var stallPause *todoStallPause
	return errors.As(err, &maxPause) || errors.As(err, &stallPause)
}

func (a *Agent) finalReadinessFailure() string {
	return a.finalReadinessCheckFor(true).reason
}

// GoalReadinessFailure returns the final-readiness failure reason — a summary of
// incomplete todos and unverified project checks — or empty string if none.
// Exported so the Controller can gate [goal:complete] on evidence.
func (a *Agent) GoalReadinessFailure() string {
	return a.finalReadinessFailure()
}

type finalReadinessCheck struct {
	applies                   bool
	reason                    string
	missingProjectChecks      int
	incompleteTodos           int
	missingAcceptanceCriteria int
	missingVerification       int
	missingReview             int
	missingSignoff            int
	missingActionEvidence     int
	missingMutation           int
	missingCapabilities       int
}

func (c finalReadinessCheck) progressSignature() string {
	return fmt.Sprintf("%d/%d/%d/%d/%d/%d/%d/%d/%d/%d\x00%s",
		c.missingProjectChecks,
		c.incompleteTodos,
		c.missingAcceptanceCriteria,
		c.missingVerification,
		c.missingReview,
		c.missingSignoff,
		c.missingActionEvidence,
		c.missingMutation,
		c.missingCapabilities,
		boolInt(c.applies),
		c.reason,
	)
}

func (c finalReadinessCheck) missingIDs() []string {
	missing := make([]string, 0, 9)
	add := func(id string, count int) {
		if count > 0 {
			missing = append(missing, id)
		}
	}
	add("project_check", c.missingProjectChecks)
	add("todo", c.incompleteTodos)
	add("criteria", c.missingAcceptanceCriteria)
	add("verification", c.missingVerification)
	add("review", c.missingReview)
	add("signoff", c.missingSignoff)
	add("action", c.missingActionEvidence)
	add("mutation", c.missingMutation)
	add("capability", c.missingCapabilities)
	return missing
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (c finalReadinessCheck) audit(result evidence.ReadinessAuditResult, recovered bool) evidence.ReadinessAudit {
	return evidence.ReadinessAudit{
		Result:                    result,
		Recovered:                 recovered,
		MissingProjectChecks:      c.missingProjectChecks,
		IncompleteTodos:           c.incompleteTodos,
		CommandMismatchMissing:    c.missingProjectChecks,
		MissingAcceptanceCriteria: c.missingAcceptanceCriteria,
		MissingVerification:       c.missingVerification,
		MissingReview:             c.missingReview,
		MissingSignoff:            c.missingSignoff,
		MissingActionEvidence:     c.missingActionEvidence,
		MissingMutation:           c.missingMutation,
		MissingCapabilities:       c.missingCapabilities,
	}
}

func (a *Agent) finalReadinessCheck() finalReadinessCheck {
	return a.finalReadinessCheckFor(true)
}

func (a *Agent) finalReadinessCheckFor(finalizeTask bool) finalReadinessCheck {
	if a.evidence == nil {
		return finalReadinessCheck{}
	}
	var missing []string
	out := finalReadinessCheck{}
	// Planning returns a proposal to the controller, which owns the approval gate
	// and starts a fresh execution turn after Plan is disabled. Delivery completion
	// requirements, including required capabilities, wait for that execution turn:
	// forcing them here could make a writer requirement contradict the plan-first
	// workflow. This is a workflow boundary only; model-initiated tool calls above
	// still use the normal Permissions/Sandbox path.
	if a.planMode.Load() {
		return out
	}
	if finalizeTask {
		incomplete, hasTodos := a.evidence.IncompleteLatestTodos()
		if !hasTodos && a.evidence.HasAnySuccessfulReceipt() {
			incomplete, hasTodos = a.incompleteCanonicalTodos()
		}
		if hasTodos && len(incomplete) > 0 && a.evidence.HasSuccessfulTodoProgressReceipt() {
			out.applies = true
			out.incompleteTodos = len(incomplete)
			missing = append(missing, finalReadinessIncompleteTodos(incomplete))
		}
	}
	writer, hasWriter := a.evidence.LatestSuccessfulWriterIndex()
	deliveryMutation := false
	deliveryVerificationOnly := false
	checkpoint := a.deliveryCheckpoint
	checkpointApplies := a.deliveryScopeActive && checkpoint.ScopeID == a.deliveryScopeID
	if a.deliveryProfile {
		if mutation, ok := a.evidence.LatestSuccessfulMutationIndex(); ok {
			writer, hasWriter = mutation, true
			deliveryMutation = true
		} else if checkpointApplies && checkpoint.PendingMutation {
			// The mutation happened before a controller rebuild/restart. Treat it as
			// the baseline so this run can satisfy verification/review/sign-off
			// without manufacturing another write.
			writer, hasWriter = -1, true
			deliveryMutation = true
		} else if checkpointApplies && checkpoint.MutationObserved {
			deliveryMutation = true
		}
		workObserved := a.evidence.HasSuccessfulWorkReceipt() || (checkpointApplies && checkpoint.WorkObserved)
		if finalizeTask && a.deliveryTaskExpected && !workObserved {
			out.missingActionEvidence++
			missing = append(missing, "perform host-observable work for this technical task before answering")
		}
		if finalizeTask && a.deliveryMutationExpected && !deliveryMutation {
			out.missingMutation++
			missing = append(missing, "the request requires a state change, but no successful mutation was observed")
		}
		if !hasWriter && a.evidence.HasSuccessfulVerificationCommand() {
			writer, hasWriter = -1, true
			deliveryVerificationOnly = true
		}
		// Required/preferred capability gates apply before the no-writer fast
		// path below: a user-required Skill/MCP must not be skippable by
		// answering from ordinary reads alone.
		if finalizeTask {
			if msg := a.capabilityGateFailure(); msg != "" {
				out.applies = true
				out.missingCapabilities++
				missing = append(missing, msg)
			}
		}
	}
	if !hasWriter {
		if len(missing) > 0 {
			if a.loopGuardAllowsFinal() {
				return out
			}
			out.reason = strings.Join(missing, "; ")
		}
		return out
	}
	hasProjectChecks := len(a.projectChecks) > 0
	hasTodoReceipt := a.evidence.HasSuccessfulTodoWrite()
	if !a.deliveryProfile && !hasProjectChecks && !hasTodoReceipt && len(missing) == 0 {
		return finalReadinessCheck{}
	}
	out.applies = true
	if a.deliveryProfile {
		criteriaEstablished := a.deliveryCriteriaEstablished || (checkpointApplies && checkpoint.CriteriaEstablished)
		if !criteriaEstablished {
			out.missingAcceptanceCriteria++
			missing = append(missing, "establish concrete acceptance criteria with todo_write before changing state")
		}
		hasCompleteStep := a.evidence.HasSuccessfulCompleteStepAfter(writer)
		if !hasCompleteStep {
			out.missingSignoff++
			missing = append(missing, "call complete_step after the latest mutation")
		}
		if !a.evidence.HasSuccessfulDeliverySignoffAfter(writer) {
			out.missingVerification++
			missing = append(missing, "run relevant verification after the latest mutation and cite that successful command in complete_step")
		}
		if deliveryMutation && !a.evidence.HasSuccessfulReviewAfter(writer) {
			out.missingReview++
			missing = append(missing, "inspect the changed result after the latest mutation (read the touched file or run git diff/status)")
		}
		if msg := a.deliveryReviewGateFailure(); msg != "" {
			out.missingReview++
			missing = append(missing, msg)
		}
		// The capability gate already ran before the no-writer fast path above.
	}
	for _, check := range a.projectChecks {
		if deliveryVerificationOnly {
			break
		}
		command := strings.TrimSpace(check.Command)
		if command == "" {
			continue
		}
		if !a.evidence.HasSuccessfulCommandAfter(command, writer) {
			out.missingProjectChecks++
			missing = append(missing, fmt.Sprintf("run %q from %s after the latest write", command, finalReadinessCheckSource(check)))
		}
	}

	if len(missing) == 0 {
		return out
	}
	if a.loopGuardAllowsFinal() {
		return out
	}
	out.reason = strings.Join(missing, "; ")
	return out
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
	if _, ok := a.evidence.LatestSuccessfulMutationIndex(); ok {
		cp.MutationObserved = true
		cp.PendingMutation = true
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

// armLoopGuardPass records that a loop guard fired this user turn.
// receiptMark is the evidence-ledger receipt count from just before the
// guarded batch ran, so a successful write or command receipt recorded after
// it counts as real progress and revokes the pass (see loopGuardAllowsFinal).
func (a *Agent) armLoopGuardPass(receiptMark int) {
	a.loopGuardArmed = true
	a.loopGuardReceiptMark = receiptMark
}

// loopGuardAllowsFinal reports whether final readiness should stand down: a
// loop guard fired this user turn and no host-observable progress — a
// successful write or command receipt — has landed since. In that state the
// missing receipts are exactly what the blocker prevents, so demanding them
// would restart the retry loop the guard just broke; the model must be free to
// report the blocker instead. The bookkeeping the guard recommends (ask,
// todo_write, complete_step) produces neither write nor command receipts, so
// it keeps the pass; real progress revokes it because receipts are obtainable
// again and readiness should resume enforcing them.
func (a *Agent) loopGuardAllowsFinal() bool {
	if a == nil || !a.loopGuardArmed {
		return false
	}
	if a.evidence == nil {
		return true
	}
	return !a.evidence.HasWriteOrCommandSince(a.loopGuardReceiptMark)
}

func finalReadinessIncompleteTodos(items []evidence.TodoStepMatch) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		label := strings.TrimSpace(item.Content)
		if label == "" {
			label = fmt.Sprintf("todo %d", item.Index)
		}
		parts = append(parts, fmt.Sprintf("%s: %s", label, item.Status))
	}
	return "latest successful todo_write still has incomplete items: " + strings.Join(parts, ", ")
}

func finalReadinessNoticeText() string {
	return "Task status needs one more check; asking the assistant to finish or explain what is blocking it."
}

func (a *Agent) setTodoState(todos []evidence.TodoItem) {
	a.todoMu.Lock()
	a.todoState = evidence.NormalizeSerialTodos(todos)
	a.todoMu.Unlock()
}

// SeedTodoState initializes the canonical task list from a host-generated
// starter list, such as an approved plan. A new host seed replaces stale state
// from earlier work so complete_step matches the plan the UI just displayed.
func (a *Agent) SeedTodoState(todos []evidence.TodoItem) {
	if len(todos) == 0 {
		return
	}
	a.setTodoState(todos)
}

// ReplaceTodoState mirrors a host-generated todo list into the canonical state.
// It is used when the host, rather than the model, owns the full state transition.
func (a *Agent) ReplaceTodoState(todos []evidence.TodoItem) {
	a.setTodoState(todos)
	a.recordTodoState(a.CanonicalTodoState())
}

// CanonicalTodoState returns a copy of the host-reconstructed task list.
func (a *Agent) CanonicalTodoState() []evidence.TodoItem {
	a.todoMu.Lock()
	defer a.todoMu.Unlock()
	return append([]evidence.TodoItem(nil), a.todoState...)
}

func (a *Agent) incompleteCanonicalTodos() ([]evidence.TodoStepMatch, bool) {
	a.todoMu.Lock()
	defer a.todoMu.Unlock()
	if len(a.todoState) == 0 {
		return nil, false
	}
	return evidence.IncompleteTodos(a.todoState), true
}

func (a *Agent) hasIncompleteCanonicalCriteria() bool {
	a.todoMu.Lock()
	defer a.todoMu.Unlock()
	return len(a.todoState) > 0 && len(evidence.IncompleteTodos(a.todoState)) > 0
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

func deliveryTaskNeedsEvidence(input string) bool {
	return heuristicInputIsTask(input)
}

func deliveryTaskNeedsMutation(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	for _, phrase := range []string{
		"do not fix", "don't fix", "without changing", "without modifying", "analysis only", "review only",
		"不要修复", "不要修改", "不要改动", "只分析", "仅分析", "只检查", "仅检查", "只评审", "仅评审",
	} {
		if strings.Contains(normalized, phrase) {
			return false
		}
	}
	for _, needle := range []string{
		"fix", "repair", "resolve", "create", "add", "write", "edit", "update", "change", "delete", "remove", "rename",
		"implement", "refactor", "apply", "install", "publish", "commit", "push", "continue work",
		"修复", "解决", "创建", "新建", "添加", "编写", "编辑", "修改", "更新", "删除", "移除", "重命名", "实现", "重构",
		"实施", "落地", "安装", "发布", "提交", "继续处理",
	} {
		if containsTaskNeedle(normalized, needle) {
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

// recordTodoState logs the host-advanced list as a synthetic todo_write receipt
// so the per-turn final gate (which reads the ledger's latest todo_write) sees
// the advance — the model no longer has to re-send a todo_write to mark the
// completion. It bypasses the todo_write tool, so the completion-transition
// guard never runs on it.
func (a *Agent) recordTodoState(todos []evidence.TodoItem) {
	if a.evidence == nil {
		return
	}
	args, err := json.Marshal(map[string]any{"todos": todos})
	if err != nil {
		return
	}
	a.evidence.Record(evidence.ReceiptFromToolCall("todo_write", json.RawMessage(args), true, true))
}

func canonicalTodoStatus(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "pending"
	}
	return s
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

func finalReadinessCheckSource(check instruction.VerifyCheck) string {
	source := strings.TrimSpace(check.SourcePath)
	if source == "" {
		source = "project memory"
	}
	if check.Line > 0 {
		return fmt.Sprintf("%s:%d", source, check.Line)
	}
	return source
}

func finalReadinessRetryMessage(reason string) string {
	return "Host final-answer readiness check failed. Before giving a final answer, address the missing host-observable receipts: " + reason + ". Run only the required tool calls, then answer when readiness is satisfied. Prefer signing off completed work with complete_step and updating todo_write from existing receipts; do not run exploratory bash commands just to satisfy readiness. If every todo is already completed and fresh review or verification makes the prior sign-off stale, renew the sign-off by calling complete_step with the final existing todo's exact text or 1-based step_index; do not invent a new step or rewrite the completed list. If a permission, plan-mode, hook, or loop-guard block prevents the required receipt, do not keep retrying the blocked command with different wording. If the blocked item needs user input, a user-owned choice, or manual review, call the ask tool with concrete options and wait for its tool result; do not ask in prose, and do not claim the user answered unless an actual ask tool result or a new user message says so."
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

func streamRecoveryMessage(hasPartialText, hadPartialTool bool) string {
	switch {
	case hadPartialTool:
		return "The previous assistant response was interrupted while a tool call was streaming. Continue the same task now. If a tool is still needed, issue a fresh complete tool call from scratch; do not rely on any partial tool-call arguments from the interrupted stream."
	case hasPartialText:
		return "The previous assistant response was interrupted during streaming. Continue the same task from immediately after the partial assistant message above. Do not repeat text that is already visible."
	default:
		return "The previous assistant response was interrupted during streaming before visible answer text was completed. Continue the same task now and provide the next useful response."
	}
}

// stream runs one completion, emitting reasoning and text deltas as typed
// events and collecting complete tool calls. A Message event closes the text
// stream so a sink can re-render the streamed raw text as styled markdown. The
// accumulated text and reasoning are also returned so the caller can round-trip
// reasoning on the next turn.
func (a *Agent) stream(ctx context.Context, turn int) (string, string, string, []provider.ToolCall, *provider.Usage, bool, bool, error) {
	ctx = provider.WithRetryNotify(ctx, func(info provider.RetryInfo) {
		a.sink.Emit(event.Event{Kind: event.Retrying, RetryAttempt: info.Attempt, RetryMax: info.Max})
	})
	// CreatedAt is durable UI metadata, not model input. Strip it from the
	// transport copy so wall-clock differences never invalidate the provider's
	// prompt-cache prefix (and custom providers cannot accidentally send it).
	requestMessages := append([]provider.Message(nil), a.session.Messages...)
	for i := range requestMessages {
		requestMessages[i].CreatedAt = 0
	}
	ch, err := a.prov.Stream(ctx, provider.Request{
		Messages:    requestMessages,
		Tools:       a.tools.Schemas(),
		Temperature: provider.OptionalTemperature(a.temperature),
	})
	if err != nil {
		return "", "", "", nil, nil, false, false, err
	}

	// A PostLLMCall hook rewrites the whole reasoning block, so when one is wired
	// up we buffer reasoning silently and emit the transformed text once after the
	// stream. With no such hook the reasoning streams live, chunk by chunk, as
	// before — the common case must not lose its live "thinking…" display.
	transformReasoning := a.hooks != nil && a.hooks.HasPostLLMCall()

	var text, reasoning strings.Builder
	var signature string // provider-issued proof for the reasoning (Anthropic thinking)
	var calls []provider.ToolCall
	var usage *provider.Usage
	var partialToolStarted bool
	var lastArgProgress time.Time
	finishReasoning := func() (stored, display string) {
		original := reasoning.String()
		display = original
		if transformReasoning && original != "" {
			display = a.hooks.PostLLMCall(ctx, original, turn)
			if display != "" {
				a.sink.Emit(event.Event{Kind: event.Reasoning, Text: display})
			}
		}
		stored = display
		if signature != "" || (len(calls) > 0 && provider.RequiresToolCallReasoning(a.prov)) {
			stored = original
		}
		return stored, display
	}
	for {
		var chunk provider.Chunk
		select {
		case <-ctx.Done():
			stored, _ := finishReasoning()
			return text.String(), stored, signature, calls, usage, false, partialToolStarted, ctx.Err()
		case c, ok := <-ch:
			if !ok {
				if err := ctx.Err(); err != nil {
					stored, _ := finishReasoning()
					return text.String(), stored, signature, calls, usage, false, partialToolStarted, err
				}
				stored, display := finishReasoning()
				if text.Len() > 0 || display != "" {
					a.sink.Emit(event.Event{
						Kind:      event.Message,
						Text:      StripGoalMarkers(text.String()),
						Reasoning: display,
					})
				}
				return text.String(), stored, signature, calls, usage, false, false, nil
			}
			chunk = c
		}
		switch chunk.Type {
		case provider.ChunkReasoning:
			reasoning.WriteString(chunk.Text)
			if chunk.Signature != "" {
				signature = chunk.Signature
			}
			if chunk.Text != "" && !transformReasoning {
				a.sink.Emit(event.Event{Kind: event.Reasoning, Text: chunk.Text})
			}
		case provider.ChunkText:
			text.WriteString(chunk.Text)
			a.sink.Emit(event.Event{Kind: event.Text, Text: chunk.Text})
		case provider.ChunkToolCallStart:
			partialToolStarted = true
			// Surface the tool card as soon as the call begins — before its
			// (possibly large) arguments finish streaming — so the user sees it
			// working instead of a stall. executeBatch emits the full dispatch
			// (with args) once the call completes; the frontend merges by ID.
			if tc := chunk.ToolCall; tc != nil {
				a.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
					ID: tc.ID, Name: tc.Name, ReadOnly: a.toolReadOnly(tc.Name), Partial: true,
				}})
			}
		case provider.ChunkToolCallArgsDelta:
			partialToolStarted = true
			// Liveness ticks while a large argument payload streams: re-emit the
			// partial dispatch with the cumulative size (time-throttled) so the
			// UI can show progress instead of a dead counter for the duration of
			// a 30KB write_file body.
			if tc := chunk.ToolCall; tc != nil && time.Since(lastArgProgress) >= 250*time.Millisecond {
				lastArgProgress = time.Now()
				a.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
					ID: tc.ID, Name: tc.Name, ReadOnly: a.toolReadOnly(tc.Name), Partial: true, ArgChars: chunk.ArgChars,
				}})
			}
		case provider.ChunkToolCall:
			partialToolStarted = true
			calls = append(calls, *chunk.ToolCall)
		case provider.ChunkUsage:
			usage = chunk.Usage
			a.lastUsage.Store(chunk.Usage)
			a.sessCacheHit.Add(int64(chunk.Usage.CacheHitTokens))
			a.sessCacheMiss.Add(int64(chunk.Usage.CacheMissTokens))
		case provider.ChunkError:
			if provider.IsStreamInterrupted(chunk.Err) {
				stored, _ := finishReasoning()
				return text.String(), stored, signature, calls, usage, true, partialToolStarted, chunk.Err
			}
			return "", "", "", nil, nil, false, false, chunk.Err
		}
	}
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

// executeBatch dispatches one model turn's tool calls. A ToolDispatch event is
// emitted for every call up front, in call order, so a frontend can show the
// timeline chronologically. Contiguous known ReadOnly calls fan out across
// goroutines; unknown and writer calls run as single-call serial segments so
// write/read ordering stays provider-ordered. ToolResult events are emitted
// after the batch in call order, so emission stays serial even when execution
// parallelised. The second return carries each call's tool-result images (nil
// for most calls), aligned by index with the first.
func (a *Agent) executeBatch(ctx context.Context, calls []provider.ToolCall) ([]string, [][]string) {
	// The assistant message already stored this slice in Session. Keep execution
	// state separate so refreshing a dependent preview never mutates shared
	// session memory outside Session's lock.
	calls = append([]provider.ToolCall(nil), calls...)
	for _, c := range calls {
		a.emitFullToolDispatch(c, false)
	}

	results := make([]string, len(calls))
	outcomes := make([]toolOutcome, len(calls))
	durations := make([]int64, len(calls))
	completedStepInBatch := false
	// Snapshot the receipt count before the batch runs: if a loop guard fires
	// for this batch, successes recorded during it (a mixed batch where only one
	// call was guard-blocked) must already count as progress against the pass.
	receiptMark := 0
	if a.evidence != nil {
		receiptMark = a.evidence.Len()
	}
	// Full dispatches are prepared against the batch's initial file state. After
	// one writer runs, a dependent later writer may only become previewable (or
	// its original preview may become stale). Refresh even after a failed writer:
	// commands and filesystem calls can mutate disk before reporting an error.
	// The first writer stays on the single-preview fast path.
	earlierWriterRan := false
	run := func(i int) {
		t, known := a.tools.Get(calls[i].Name)
		writer := known && !t.ReadOnly()
		if earlierWriterRan && writer {
			if refreshed, changed := refreshCurrentFileDiff(t, calls[i]); changed {
				calls[i] = refreshed
				a.session.UpdateToolCallPreview(refreshed)
				a.emitFullToolDispatch(refreshed, true)
			}
		}
		start := time.Now()
		if calls[i].Name == "complete_step" && completedStepInBatch {
			output := "blocked: only one successful complete_step is allowed per tool-call round. Continue from the newly promoted in_progress todo in the next round instead of batching sign-offs."
			outcomes[i] = toolOutcome{output: output, blocked: true, errMsg: "blocked: complete_step sign-offs must be serial"}
			if a.evidence != nil {
				a.evidence.Record(evidence.ReceiptFromToolCall(calls[i].Name, json.RawMessage(calls[i].Arguments), false, true))
			}
			durations[i] = time.Since(start).Milliseconds()
			results[i] = output
			return
		}
		outcomes[i] = a.executeOne(ctx, calls[i])
		if calls[i].Name == "complete_step" && outcomes[i].errMsg == "" {
			completedStepInBatch = true
		}
		durations[i] = time.Since(start).Milliseconds()
		results[i] = outcomes[i].output
		if writer {
			earlierWriterRan = true
		}
	}
	cancelled := false
	markCancelled := func(start int) {
		errMsg := context.Canceled.Error()
		if err := ctx.Err(); err != nil {
			errMsg = err.Error()
		}
		output := "cancelled: context cancelled before execution"
		for j := start; j < len(calls); j++ {
			results[j] = output
			outcomes[j] = toolOutcome{output: output, errMsg: errMsg}
		}
		cancelled = true
	}

	for _, batch := range partitionToolCalls(a.tools, calls) {
		if ctx.Err() != nil {
			markCancelled(batch.start)
			break
		}
		if batch.parallel && batch.end-batch.start > 1 {
			ranUntil := runParallel(ctx, batch.start, batch.end, run)
			// After parallel execution completes, check if context was cancelled.
			// The individual tool executions should have detected ctx.Done(), but
			// we verify here to ensure we don't continue to subsequent batches.
			if ctx.Err() != nil {
				markCancelled(ranUntil)
				break
			}
			continue
		}
		for i := batch.start; i < batch.end; i++ {
			// Before executing the next tool, check if context was cancelled.
			// This prevents starting new tools when a previous tool's execution
			// triggered cancellation.
			if ctx.Err() != nil {
				markCancelled(i)
				break
			}
			run(i)
			// After each tool execution, also check if the context was cancelled.
			// If so, stop executing remaining tools and return immediately so
			// the agent loop can detect the cancellation and exit.
			if ctx.Err() != nil {
				markCancelled(i + 1)
				break
			}
		}
		if cancelled {
			break
		}
	}

	for i, c := range calls {
		o := outcomes[i]
		t, ok := a.tools.Get(c.Name)
		a.sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
			ID:         c.ID,
			Name:       c.Name,
			Args:       c.Arguments,
			Output:     o.output,
			Err:        o.errMsg,
			ReadOnly:   ok && t.ReadOnly(),
			Truncated:  o.truncated,
			DurationMs: durations[i],
		}})
		if o.truncated && o.truncMsg != "" {
			a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: o.truncMsg})
		}
	}
	if !cancelled {
		a.applyStormBreaker(calls, outcomes, results, receiptMark)
	}
	images := make([][]string, len(calls))
	for i := range outcomes {
		images[i] = outcomes[i].images
	}
	return results, images
}

func (a *Agent) emitFullToolDispatch(c provider.ToolCall, refreshed bool) {
	t, ok := a.tools.Get(c.Name)
	ev := event.Tool{ID: c.ID, Name: c.Name, Args: c.Arguments, ReadOnly: ok && t.ReadOnly(), Refreshed: refreshed}
	ev.FileDiff = event.FileDiff{Diff: c.Diff, Added: c.Added, Removed: c.Removed}
	if ok && ev.Diff == "" && ev.Added == 0 && ev.Removed == 0 {
		if ch, ok := tool.PreviewChange(t, json.RawMessage(c.Arguments)); ok {
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

// refreshCurrentFileDiff recomputes a writer preview against the state left by
// earlier successful writers in the same provider batch. Preview failures clear
// any stale initial diff; a later Execute will then fail or ask for recovery
// without presenting the user with a preview that no longer describes disk.
func refreshCurrentFileDiff(t tool.Tool, call provider.ToolCall) (provider.ToolCall, bool) {
	pv, ok := t.(tool.Previewer)
	if !ok {
		return call, false
	}
	refreshed := call
	refreshed.Diff = ""
	refreshed.Added = 0
	refreshed.Removed = 0
	if change, err := pv.Preview(json.RawMessage(call.Arguments)); err == nil {
		refreshed.Diff = change.Diff
		refreshed.Added = change.Added
		refreshed.Removed = change.Removed
	}
	changed := refreshed.Diff != call.Diff || refreshed.Added != call.Added || refreshed.Removed != call.Removed
	return refreshed, changed
}

func (a *Agent) withPreviewFileDiffs(calls []provider.ToolCall) []provider.ToolCall {
	if len(calls) == 0 {
		return calls
	}
	out := make([]provider.ToolCall, len(calls))
	copy(out, calls)
	for i := range out {
		if out[i].Diff != "" || out[i].Added != 0 || out[i].Removed != 0 {
			continue
		}
		t, ok := a.tools.Get(out[i].Name)
		if !ok {
			continue
		}
		if ch, ok := tool.PreviewChange(t, json.RawMessage(out[i].Arguments)); ok {
			out[i].Diff = ch.Diff
			out[i].Added = ch.Added
			out[i].Removed = ch.Removed
		}
	}
	return out
}

type toolCallBatch struct {
	start    int
	end      int
	parallel bool
}

// partitionToolCalls keeps provider order while letting contiguous known
// read-only tools run together. Unknown and writer tools are single-call serial
// batches so they cannot reorder around reads or produce surprising errors.
// complete_step and todo_write read the turn's evidence ledger. wait and
// bash_output can merge a background task's receipts into that ledger. These
// evidence-sensitive tools never join a parallel run, so provider order stays
// receipt order.
func partitionToolCalls(r *tool.Registry, calls []provider.ToolCall) []toolCallBatch {
	var batches []toolCallBatch
	for i := 0; i < len(calls); {
		if parallelisable(r, calls[i].Name) {
			start := i
			i++
			for i < len(calls) && parallelisable(r, calls[i].Name) {
				i++
			}
			batches = append(batches, toolCallBatch{start: start, end: i, parallel: true})
			continue
		}
		batches = append(batches, toolCallBatch{start: i, end: i + 1})
		i++
	}
	return batches
}

func parallelisable(r *tool.Registry, name string) bool {
	switch name {
	case "complete_step", "todo_write", "wait", "bash_output":
		return false
	}
	t, ok := r.Get(name)
	return ok && t.ReadOnly()
}

func runParallel(ctx context.Context, start, end int, run func(int)) int {
	const maxParallel = 8
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	ranUntil := start
launch:
	for i := start; i < end; i++ {
		if ctx.Err() != nil {
			break
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break launch
		}
		if ctx.Err() != nil {
			<-sem
			break
		}
		i := i
		wg.Add(1)
		ranUntil = i + 1
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			run(i)
		}()
	}
	wg.Wait()
	return ranUntil
}

// stormBreakThreshold is how many times in a row the same tool may fail the same
// way before the loop stops echoing the raw error back and instead returns a
// directive to change approach. Two natural self-corrections are healthy; the
// third identical failure is a death-spiral — the dominant case being a tool call
// whose arguments are truncated at the output-token ceiling, which the model then
// re-emits (re-worded but still over-long), truncating the same way again.
const stormBreakThreshold = 3

// repeatSuccessBreakThreshold is how many identical write-like successes the
// agent allows before refusing another copy in the same user turn. Two gives the
// model room for a natural self-correction; the third repeat is usually a
// no-op/write loop and should be redirected to a different tool or final answer.
const repeatSuccessBreakThreshold = 2

const (
	// todoProgressNudgeRounds is the first adaptive checkpoint. The host asks
	// the model to reassess, but keeps the turn alive so it can recover.
	todoProgressNudgeRounds = 8
	// maxTodoStallRounds pauses only after the reassessment also failed to
	// produce a new completion or unique host-observed work receipt.
	maxTodoStallRounds = 16
)

func todoProgressNudgeMessage(rounds int) string {
	return fmt.Sprintf("Host progress check: the current todo has produced no new completion, unique read, command, or mutation for %d tool-call rounds. Reassess before using more tools: sign off the current item if it is done, narrow the remaining work without replacing the active item, or explain/ask about a real blocker. Do not repeat reads, commands, or writes just to reset this guard.", rounds)
}

// loopGuardBlockErrMsg is the errMsg carried by a repeat-success loop-guard
// block. applyStormBreaker matches it to arm the final-readiness loop-guard
// pass, since that guard also invites the model to report the blocker.
const loopGuardBlockErrMsg = "blocked by loop guard"

// applyStormBreaker detects a run of zero-progress turns and, past the
// threshold, rewrites the model-facing result (results[0]) into a directive to
// change approach. Two detectors, because a stuck model varies its retries two
// ways. The signature detector keys on each call's (tool, error/blocker) — not
// its args — since a stuck model reworks the arguments cosmetically while
// hitting the same host refusal or failure (see the stormSig field doc). The
// streak detector counts consecutive turns in which every call was blocked,
// regardless of shape: rotating tools, reordering a batch, or a blocker whose
// text varies per attempt escapes the signature but is still zero progress —
// only a host refusal (not a plain error) proves that, so the streak requires
// blocked outcomes. Any success resets both. When a guard fires — or when a
// call in the batch was already blocked by the per-call repeat-success guard —
// the final-readiness loop-guard pass is armed so the model may report the
// blocker (see loopGuardAllowsFinal). The hard maxSteps guard remains the
// ultimate backstop; this just keeps the loop from burning that whole budget
// bouncing off the same host refusals.
func (a *Agent) applyStormBreaker(calls []provider.ToolCall, outcomes []toolOutcome, results []string, receiptMark int) {
	allBlocked := len(outcomes) > 0
	for _, outcome := range outcomes {
		if !outcome.blocked {
			allBlocked = false
			break
		}
	}
	if allBlocked {
		a.blockedTurnStreak++
	} else {
		a.blockedTurnStreak = 0
	}
	for _, outcome := range outcomes {
		if outcome.blocked && outcome.errMsg == loopGuardBlockErrMsg {
			a.armLoopGuardPass(receiptMark)
			break
		}
	}

	sig, ok := batchStormSignature(calls, outcomes)
	switch {
	case !ok:
		a.stormSig, a.stormCount = "", 0
	case sig != a.stormSig:
		a.stormSig, a.stormCount = sig, 1
	default:
		a.stormCount++
	}
	stormHit := ok && a.stormCount >= stormBreakThreshold
	streakHit := allBlocked && a.blockedTurnStreak >= stormBreakThreshold
	if !stormHit && !streakHit {
		return
	}

	const blockedAdvice = "Change approach: do not keep retrying a blocked tool by changing the tool, command, or arguments. Respect the permission, plan-mode, hook, or loop-guard blocker; use an already-allowed tool, ask the user for the specific approval or choice if appropriate, or explain the blocker in your final answer."
	var guard, detail string
	if stormHit {
		subject := fmt.Sprintf("%q", calls[0].Name)
		short := calls[0].Name
		if len(calls) > 1 {
			subject = fmt.Sprintf("this batch of %d tool calls", len(calls))
			short = fmt.Sprintf("a batch of %d calls", len(calls))
		}
		anyBlocked := false
		for _, outcome := range outcomes {
			if outcome.blocked {
				anyBlocked = true
				break
			}
		}
		action := "failed"
		advice := "Change approach: if an argument is being truncated, write less in one call and split the work into several smaller calls; otherwise fix the arguments, use a different tool, or explain the blocker in your final answer."
		if anyBlocked {
			action = "been blocked or failed"
			advice = blockedAdvice
		}
		guard = fmt.Sprintf(
			"[loop guard] %s has now %s %d times in a row with the same host response. Re-sending it — even with the wording changed — will not help: the calls keep hitting the same outcome. %s",
			subject, action, a.stormCount, advice)
		detail = fmt.Sprintf(
			"loop guard: %s hit the same host response %d× — nudging the model to change approach",
			short, a.stormCount)
	} else {
		guard = fmt.Sprintf(
			"[loop guard] every tool call in the last %d turns has been blocked by the host (permission, plan mode, hook, or loop guard). Switching tools, reordering calls, or rewording arguments will not help while the blockers stand. %s",
			a.blockedTurnStreak, blockedAdvice)
		detail = fmt.Sprintf(
			"loop guard: every tool call blocked %d turns in a row — nudging the model to change approach",
			a.blockedTurnStreak)
	}
	results[0] = outcomes[0].output + "\n\n" + guard
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeLoopGuard, Text: loopGuardNoticeText(), Detail: detail})
	a.armLoopGuardPass(receiptMark)
}

func loopGuardNoticeText() string {
	return "The assistant is not making progress; asking it to change approach."
}

// batchStormSignature returns a per-turn fixation signature — each call's
// (name, error/blocker) in order — and ok=true only when every call errored or
// was blocked. ok=false (any success) means the turn made progress, so the
// caller resets the counter. Keying on the host response rather than the args is
// deliberate: a stuck model reworks the arguments while hitting the same
// response, so identical-args matching would miss the loop.
func batchStormSignature(calls []provider.ToolCall, outcomes []toolOutcome) (string, bool) {
	if len(calls) == 0 {
		return "", false
	}
	var sb strings.Builder
	for i := range calls {
		if outcomes[i].errMsg == "" {
			return "", false
		}
		sb.WriteString(calls[i].Name)
		sb.WriteByte(0)
		sb.WriteString(outcomes[i].errMsg)
		sb.WriteByte(0)
	}
	return sb.String(), true
}

// toolOutcome is one tool call's result, split into the model-facing output and
// the display-facing notice bits. errMsg is the short failure reason (empty on
// success) — a refused call, an unknown tool, or an execution error — so a sink
// renders the result as failed ("⊘ name <errMsg>" / a red card) instead of OK;
// blocked narrows that to a refusal (plan mode / permission). truncMsg is set
// (without the "· " prefix) when the output was head+tailed. images carries
// data URLs from a tool.ImageTool result; they ride outside output so text
// truncation can never corrupt an image payload.
type toolOutcome struct {
	output    string
	images    []string
	blocked   bool
	errMsg    string
	truncated bool
	truncMsg  string
}

// executeOne runs a single tool call. It is pure with respect to the event sink
// — the caller emits ToolDispatch/ToolResult — so it is safe to invoke from
// parallel goroutines.
func (a *Agent) executeOne(ctx context.Context, call provider.ToolCall) toolOutcome {
	t, ok := a.tools.Get(call.Name)
	if !ok {
		return toolOutcome{
			output: fmt.Sprintf("error: unknown tool %q", call.Name),
			errMsg: fmt.Sprintf("unknown tool %q", call.Name),
		}
	}
	if out, blocked := a.repeatedSuccessBlock(call, t); blocked {
		return toolOutcome{
			output:  out,
			blocked: true,
			errMsg:  loopGuardBlockErrMsg,
		}
	}
	if out, blocked := a.staleAnchorEditBlock(call); blocked {
		return toolOutcome{
			output:  out,
			blocked: true,
			errMsg:  "blocked: fresh read required",
		}
	}
	if a.planMode.Load() {
		// Translate the tool's optional plan-mode self-report into the policy's
		// tri-state. Mirrors the t.(tool.Previewer) assertion precedent below.
		safety := planmode.PlanSafetyUnknown
		if c, ok := t.(tool.PlanModeClassifier); ok {
			if c.PlanModeSafe() {
				safety = planmode.PlanSafetySafe
			} else {
				safety = planmode.PlanSafetyUnsafe
			}
		}
		if decision := a.planModeDecision(call.Name, t.ReadOnly(), planModeUntrustedReadOnly(t), safety, json.RawMessage(call.Arguments)); decision.Blocked {
			return toolOutcome{
				output:  decision.Message,
				blocked: true,
				errMsg:  "blocked: tool is unavailable during planning",
			}
		}
	}
	// Resolve proxy tools (use_capability) to the real MCP target before
	// permission, hooks, and evidence. Provider transcript keeps call.Name.
	permName := call.Name
	permArgs := json.RawMessage(call.Arguments)
	execTool := t
	execArgs := json.RawMessage(call.Arguments)
	evidenceName := call.Name
	evidenceArgs := json.RawMessage(call.Arguments)
	readOnly := t.ReadOnly()
	var resolved tool.ResolvedCall
	if resolver, ok := t.(tool.CallResolver); ok {
		rc, rerr := resolver.ResolveCall(ctx, json.RawMessage(call.Arguments))
		if rerr != nil {
			return toolOutcome{
				output: fmt.Sprintf("error: %v", rerr),
				errMsg: firstLine(rerr.Error()),
			}
		}
		resolved = rc
		if rc.TargetName != "" {
			permName = rc.TargetName
			evidenceName = rc.TargetName
		}
		if len(rc.Args) > 0 {
			permArgs = rc.Args
			evidenceArgs = rc.Args
			execArgs = rc.Args
		}
		if rc.Target != nil {
			execTool = rc.Target
		}
		readOnly = rc.ReadOnly
		if rc.TargetName != "" && rc.TargetName != call.Name {
			EmitProxyAudit(a.sink, rc)
		}
		if outcome, blocked := a.readOnlyExecutionBlock(t, &rc); blocked {
			return outcome
		}
		if rc.Commit != nil {
			if err := rc.Commit(); err != nil {
				return toolOutcome{
					output: fmt.Sprintf("error: %v", err),
					errMsg: firstLine(err.Error()),
				}
			}
		}
		if rc.SkipExecute {
			// Resolution completed without target execution; still record a meta receipt.
			// A connected mcp-server call completes during resolution by listing
			// its live tools, so account for that successful call here too.
			if rc.ProxyAction == "call" && !rc.Unavailable {
				a.noteCapabilityInvocation(call.Name, json.RawMessage(call.Arguments), nil)
			}
			result := rc.Result
			if a.evidence != nil {
				// inspect/decline are not mutations; unavailable call targets are not success.
				success := !rc.Unavailable
				rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), success, true)
				a.evidence.Record(rec)
			}
			if rc.Unavailable {
				return toolOutcome{output: result, errMsg: firstLine(rc.UnavailableReason)}
			}
			body, truncMsg := truncateToolOutput(result)
			return toolOutcome{output: body, truncated: truncMsg != "", truncMsg: truncMsg}
		}
	} else if outcome, blocked := a.readOnlyExecutionBlock(t, nil); blocked {
		return outcome
	}

	// A proxy resolution can point at a target with an explicit planning-phase
	// opt-out even though the proxy itself has none. Re-check the resolved target
	// before its ordinary permission and sandbox path.
	if resolved.TargetName != "" && a.planMode.Load() {
		safety := planmode.PlanSafetyUnknown
		if c, ok := execTool.(tool.PlanModeClassifier); ok {
			if c.PlanModeSafe() {
				safety = planmode.PlanSafetySafe
			} else {
				safety = planmode.PlanSafetyUnsafe
			}
		}
		if decision := a.planModeDecision(permName, resolved.ReadOnly, planModeUntrustedReadOnly(execTool), safety, permArgs); decision.Blocked {
			return toolOutcome{
				output:  decision.Message,
				blocked: true,
				errMsg:  "blocked: tool is unavailable during planning",
			}
		}
	}
	if a.planMode.Load() && isMCPExecutionTarget(execTool, permName) && (!readOnly || planModeUntrustedReadOnly(execTool) || mcpDestructiveHint(execTool)) {
		return toolOutcome{
			output:  fmt.Sprintf("blocked: MCP writer/destructive target %q is unavailable during Plan mode; finish or exit Plan mode before requesting this call", permName),
			blocked: true,
			errMsg:  "blocked: MCP writer is unavailable during planning",
		}
	}
	if a.deliveryProfile && evidenceName == "bash" && evidence.BashToolCallMasksVerificationExit(evidenceArgs) {
		return toolOutcome{
			output:  "blocked: the trailing echo/printf of $? masks the verifier's exit status, so this command would look successful even when the check failed. Run the verifier or read-only extraction pipeline by itself and let its exit status be the tool result; for example: tail ... | head ... | node --check -",
			blocked: true,
			errMsg:  "blocked: verification exit status masked",
		}
	}
	if a.deliveryProfile && evidenceName == "bash" && evidence.BashToolCallMixesMutationAndVerification(evidenceArgs) {
		return toolOutcome{
			output:  "blocked: this command mixes a verification check with a segment that may write state. Run the state-changing preparation separately while a todo is in_progress, then run a read-only verification command. For generated input, prefer a host-recognized read-only pipeline into the verifier (for example: tail ... | head ... | node --check -) instead of writing a temporary file.",
			blocked: true,
			errMsg:  "blocked: mixed mutation and verification command",
		}
	}
	if a.deliveryProfile && evidenceName == "bash" && evidence.BashToolCallUsesOpaqueInlineInterpreter(evidenceArgs) {
		return toolOutcome{
			output:  "blocked: delivery mode cannot audit inline interpreter source such as node -e or python -c, so executing it would become an opaque mutation and invalidate prior verification. For inspection, use read_file/grep or another host-proven read-only command. For validation, use a conventional verifier such as node --check, a project test/check/lint command, or a read-only extraction pipeline into the verifier. For an intentional state change, use a file tool or a script file under the current in_progress todo.",
			blocked: true,
			errMsg:  "blocked: opaque inline interpreter command",
		}
	}

	mutates := evidence.ToolCallMutates(evidenceName, evidenceArgs, readOnly)
	if a.deliveryProfile && evidence.ToolCallRequiresDeliveryCriteria(evidenceName, evidenceArgs, readOnly) && !a.deliveryCriteriaEstablished {
		return toolOutcome{
			output:  "blocked: delivery-first mode requires acceptance criteria before state-changing work. Call todo_write with a concrete, verifiable task list, then retry this tool call.",
			blocked: true,
			errMsg:  "blocked: delivery acceptance criteria required",
		}
	}
	if a.deliveryProfile && mutates && !a.hasActiveCanonicalTodo() {
		return toolOutcome{
			output:  "blocked: delivery-first mode requires every state change to belong to the current in_progress todo. Preserve the completed todo prefix, append a concrete new item if more work was discovered, mark that item in_progress with todo_write, then retry this mutation.",
			blocked: true,
			errMsg:  "blocked: active delivery todo required",
		}
	}
	destructive := mcpDestructiveHint(execTool)
	if isInstalledMCPTool(execTool) {
		mode, reviewer := mcpApprovalPolicy(execTool)
		var allow bool
		var reason string
		var err error
		if mcpGate, ok := a.gate.(MCPApprovalGate); ok {
			allow, reason, err = mcpGate.CheckMCP(ctx, permName, mcpApprovalSubject(execTool, permName), permArgs, readOnly, destructive, mode, reviewer)
		} else if destructive {
			freshGate, ok := a.gate.(FreshApprovalGate)
			if !ok {
				return toolOutcome{
					output:  "blocked: this destructive MCP tool requires fresh human approval or a configured automatic approval reviewer before execution.",
					blocked: true,
					errMsg:  "blocked: destructive MCP approval required",
				}
			}
			allow, reason, err = freshGate.CheckFresh(ctx, permName, mcpApprovalSubject(execTool, permName), permArgs, readOnly)
		} else if a.gate != nil {
			allow, reason, err = a.gate.Check(ctx, permName, permArgs, readOnly)
		} else {
			allow = true
		}
		if err != nil {
			return toolOutcome{
				output:  fmt.Sprintf("blocked: %s (%v)", reason, err),
				blocked: true,
				errMsg:  fmt.Sprintf("blocked: %v", err),
			}
		}
		if !allow {
			if strings.TrimSpace(reason) == "" {
				reason = "the MCP tool call was not approved"
			}
			return toolOutcome{
				output:  "blocked: " + reason,
				blocked: true,
				errMsg:  "blocked by MCP approval policy",
			}
		}
	} else if a.gate != nil {
		allow, reason, err := a.gate.Check(ctx, permName, permArgs, readOnly)
		if err != nil {
			return toolOutcome{
				output:  fmt.Sprintf("blocked: %s (%v)", reason, err),
				blocked: true,
				errMsg:  fmt.Sprintf("blocked: %v", err),
			}
		}
		if !allow {
			return toolOutcome{
				output:  "blocked: " + reason,
				blocked: true,
				errMsg:  "blocked by permission policy",
			}
		}
	}
	// Acquire after permission is granted but before PreToolUse: hooks are user
	// shell code and can themselves change the workspace. This keeps readers
	// concurrent and avoids holding the workspace during an approval prompt while
	// still covering every write-side action that follows authorization.
	if a.deliveryProfile && mutates && a.workspaceLease != nil {
		if err := a.workspaceLease.AcquireWrite(ctx); err != nil {
			return toolOutcome{
				output:  fmt.Sprintf("blocked: the workspace did not become available for Delivery writing: %v", err),
				blocked: true,
				errMsg:  "blocked: workspace write lease unavailable",
			}
		}
	}
	// Resolve the concrete execution target before hooks. A proxy may carry a
	// different target/name/argument set than the provider-visible call.
	runTool := execTool
	runArgs := execArgs
	if resolved.Target != nil {
		runTool = resolved.Target
		runArgs = resolved.Args
		if len(runArgs) == 0 {
			runArgs = json.RawMessage(`{}`)
		}
	}
	// Hold the parent claim before PreToolUse: hooks are user shell code and may
	// mutate the same workspace. The reservation remains live through hooks,
	// checkpointing, and the concrete Execute call, closing both hook-side and
	// check-before-write TOCTOU windows. Dynamic Economy/MCP tools are covered
	// here after registry lookup without schema-changing wrappers.
	if releaseParentWrite, perr := a.reserveParentWrite(runTool, runArgs, readOnly); perr != nil {
		return toolOutcome{
			output:  "blocked: " + perr.Error(),
			blocked: true,
			errMsg:  "blocked: write path claimed by background subagent",
		}
	} else if releaseParentWrite != nil {
		defer releaseParentWrite()
	}
	// PreToolUse hooks run after permission is granted but before the call: a
	// gating hook (exit 2) refuses it, surfaced to the model like a gate denial.
	// Proxy tools fire hooks against the real MCP target name and arguments.
	if a.hooks != nil {
		if block, msg := a.hooks.PreToolUse(ctx, permName, permArgs); block {
			if msg == "" {
				msg = "blocked by a PreToolUse hook"
			}
			return toolOutcome{
				output:  "blocked: " + msg,
				blocked: true,
				errMsg:  "blocked by PreToolUse hook",
			}
		}
	}
	// Checkpoint the file this writer is about to change, so the turn can be
	// rewound. Fires after all gating (the edit is cleared to run) and only for
	// tools that can describe their change; a Preview error means the edit will
	// likely fail anyway, so we skip rather than snapshot a stale state.
	if a.onPreEdit != nil && !readOnly {
		if pv, ok := execTool.(tool.Previewer); ok {
			if change, perr := pv.Preview(execArgs); perr == nil {
				a.onPreEdit(change)
			}
		}
	}
	cctx := withCallContext(ctx, call.ID, a.sink, a.asker, a.planMode.Load())
	cctx = WithSubagentDepth(cctx, a.subagentDepth)
	if a.evidence != nil {
		cctx = evidence.WithLedger(cctx, a.evidence)
		cctx = evidence.WithSessionMessages(cctx, a.session.Snapshot())
		if a.deliveryProfile {
			cctx = evidence.WithDeliveryProfile(cctx)
		}
	}
	if !a.planMode.Load() {
		cctx = evidence.WithTodoState(cctx, a.CanonicalTodoState())
	}
	if len(a.projectChecks) > 0 {
		cctx = instruction.WithChecks(cctx, a.projectChecks)
	}
	if a.jobs != nil {
		cctx = jobs.WithManager(cctx, a.jobs)
	}
	if a.sandboxEscapeApprover != nil {
		cctx = sandbox.WithEscapeApprover(cctx, a.sandboxEscapeApprover)
	}
	if a.configWriteApprover != nil {
		cctx = tool.WithConfigWriteApprover(cctx, a.configWriteApprover)
	}
	if v := a.responseLanguage.Load(); v != nil {
		if lang, ok := v.(string); ok {
			cctx = WithResponseLanguagePreference(cctx, lang)
		}
	}
	if v := a.reasoningLanguage.Load(); v != nil {
		if lang, ok := v.(string); ok {
			cctx = WithReasoningLanguagePreference(cctx, lang)
		}
	}
	if a.memQueue != nil {
		cctx = memory.WithQueue(cctx, a.memQueue)
	}
	callID := call.ID
	cctx = tool.WithProgress(cctx, func(chunk string) {
		a.sink.Emit(event.Event{Kind: event.ToolProgress, Tool: event.Tool{ID: callID, Output: chunk}})
	})
	var result string
	var images []string
	var err error
	// A call that was authorized under reader classification carries that
	// basis into dispatch: the MCP execution layer re-verifies it linearizably
	// against live trust state and refuses to promote it into a writer lane if
	// a concurrent revocation or reclassification landed after the gate.
	if readOnly && isInstalledMCPTool(runTool) && !mcpDestructiveHint(runTool) {
		fingerprint := ""
		if fp, ok := runTool.(tool.MCPCapabilityFingerprint); ok {
			fingerprint = fp.MCPCapabilityFingerprint()
		}
		cctx = tool.WithReaderExecutionIntent(cctx, fingerprint)
	}
	if it, ok := runTool.(tool.ImageTool); ok {
		result, images, err = it.ExecuteWithImages(cctx, runArgs)
	} else {
		result, err = runTool.Execute(cctx, runArgs)
	}
	if a.evidence != nil {
		// Always record the model-visible call for audit, then the real target
		// attributes for mutation/read classification when they differ.
		if call.Name == "complete_step" {
			rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), err == nil, t.ReadOnly())
			a.evidence.Record(rec)
			if err == nil {
				a.advanceCanonicalTodo(rec.Step)
			}
		} else if evidenceName != call.Name {
			// Proxy: meta receipt (non-mutation) + real target receipt.
			a.evidence.Record(evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), err == nil, true))
			rec := evidence.ReceiptFromToolCall(evidenceName, evidenceArgs, err == nil, readOnly)
			rec.OutputBytes = len(strings.TrimSpace(result))
			a.evidence.Record(rec)
		} else {
			rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), err == nil, t.ReadOnly())
			rec.OutputBytes = len(strings.TrimSpace(result))
			a.evidence.Record(rec)
			if err == nil && call.Name == "todo_write" {
				a.setTodoState(rec.Todos)
				if len(rec.Todos) > 0 {
					a.deliveryCriteriaEstablished = true
				}
			}
		}
	}
	// Track skill/capability outcomes for Delivery gates.
	a.noteCapabilityInvocation(call.Name, json.RawMessage(call.Arguments), err)
	// Success and failure hooks observe the result after the tool ran. Use the
	// real target name for proxied tools.
	if a.hooks != nil {
		if err != nil {
			a.hooks.PostToolUseFailure(ctx, permName, permArgs, result, err)
		} else {
			a.hooks.PostToolUse(ctx, permName, permArgs, result)
		}
	}
	if err != nil {
		detail := result
		// Malformed-args failures are a transient model JSON glitch (e.g. options
		// written as ["a":"b"] → "invalid character ':' after array element"). The
		// args can't be safely re-parsed, but echoing the tool's schema makes the
		// retry land valid instead of repeating the same broken shape.
		if !json.Valid([]byte(call.Arguments)) {
			detail = strings.TrimRight(detail, "\n") + "\nThe arguments were not valid JSON. Re-emit them exactly per this schema:\n" + string(t.Schema())
		}
		body, truncMsg := truncateToolOutput(fmt.Sprintf("error: %v\n%s", err, detail))
		return toolOutcome{output: body, errMsg: firstLine(err.Error()), truncated: truncMsg != "", truncMsg: truncMsg}
	}
	a.recordRepeatSuccess(call, t)
	// A foreground `task` sub-agent just finished — its result is the final answer.
	// (A backgrounded one returns a "Started…" string and stops later in a job, so
	// it doesn't fire here.) SubagentStop lets a hook react to delegated work.
	if a.hooks != nil && call.Name == "task" && !isBackgroundTaskCall(call.Arguments) {
		a.hooks.SubagentStop(ctx, result)
	}
	body, truncMsg := truncateToolOutput(result)
	return toolOutcome{output: body, images: images, truncated: truncMsg != "", truncMsg: truncMsg}
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
	if resolved == nil {
		if visible == nil || !visible.ReadOnly() {
			if reasoner, ok := visible.(tool.ReadOnlyExecutionBlockReason); ok && strings.TrimSpace(reasoner.ReadOnlyExecutionBlockReason()) != "" {
				return block(reasoner.ReadOnlyExecutionBlockReason())
			}
			return block("execute a state-changing tool")
		}
		if u, ok := visible.(tool.PlanModeUntrustedReadOnly); ok && u.PlanModeUntrustedReadOnly() {
			return block("trust an externally asserted read-only target")
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
	case "inspect":
		if !resolved.SkipExecute || resolved.Target != nil || !resolved.ReadOnly {
			return block("execute a malformed dynamic inspection")
		}
		return toolOutcome{}, false
	case "decline":
		return block("decline a capability decision")
	case "call":
		if resolved.Target == nil {
			return block("execute an unresolved dynamic capability")
		}
		if !resolved.ReadOnly {
			if reasoner, ok := resolved.Target.(tool.ReadOnlyExecutionBlockReason); ok && strings.TrimSpace(reasoner.ReadOnlyExecutionBlockReason()) != "" {
				return block(reasoner.ReadOnlyExecutionBlockReason())
			}
			return block("execute a state-changing dynamic capability")
		}
		if u, ok := resolved.Target.(tool.PlanModeUntrustedReadOnly); ok && u.PlanModeUntrustedReadOnly() {
			return block("trust an externally asserted read-only dynamic capability")
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
	if untrusted, ok := t.(tool.PlanModeUntrustedReadOnly); ok && untrusted.PlanModeUntrustedReadOnly() {
		return false
	}
	meta, ok := t.(tool.MCPMetadata)
	if !ok || strings.TrimSpace(meta.MCPServerName()) == "" || strings.TrimSpace(meta.MCPRawToolName()) == "" {
		return false
	}
	// A reader classification only admits a host start when explicit local or
	// signed package policy backs it. A server hint alone never satisfies the
	// strict boundary.
	if authority, ok := t.(tool.ReadOnlyExecutionAuthority); !ok || !authority.ReadOnlyExecutionAuthority() {
		return false
	}
	_, governed := t.(tool.MCPApprovalPolicy)
	return governed
}

func isInstalledMCPTool(t tool.Tool) bool {
	meta, ok := t.(tool.MCPMetadata)
	return ok && strings.TrimSpace(meta.MCPServerName()) != "" && strings.TrimSpace(meta.MCPRawToolName()) != ""
}

func isMCPExecutionTarget(t tool.Tool, name string) bool {
	return isInstalledMCPTool(t) || strings.HasPrefix(strings.TrimSpace(name), "mcp__")
}

func planModeUntrustedReadOnly(t tool.Tool) bool {
	untrusted, ok := t.(tool.PlanModeUntrustedReadOnly)
	return ok && untrusted.PlanModeUntrustedReadOnly()
}

func mcpDestructiveHint(t tool.Tool) bool {
	annotations, ok := t.(tool.MCPAnnotations)
	return ok && annotations.MCPDestructiveHint()
}

func mcpApprovalPolicy(t tool.Tool) (mode, reviewer string) {
	policy, ok := t.(tool.MCPApprovalPolicy)
	if !ok {
		return tool.MCPApprovalAuto, ""
	}
	return tool.NormalizeMCPApprovalMode(policy.MCPApprovalMode()), tool.NormalizeMCPApprovalReviewer(policy.MCPApprovalReviewer())
}

func mcpApprovalSubject(t tool.Tool, fallback string) string {
	if meta, ok := t.(tool.MCPMetadata); ok {
		server := strings.TrimSpace(meta.MCPServerName())
		raw := strings.TrimSpace(meta.MCPRawToolName())
		if server != "" && raw != "" {
			return server + "/" + raw
		}
	}
	return strings.TrimSpace(fallback)
}

func (a *Agent) planModeDecision(toolName string, readOnly, untrustedReadOnly bool, safety planmode.PlanSafety, args json.RawMessage) planmode.Decision {
	return (planmode.Policy{}).Decide(planmode.Call{
		Name:              toolName,
		ReadOnly:          readOnly,
		UntrustedReadOnly: untrustedReadOnly,
		Safety:            safety,
		Args:              args,
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
	t, ok := a.tools.Get(name)
	return ok && t.ReadOnly()
}

// firstLine returns s up to its first newline — a one-line failure summary for
// the display Err, while the full error stays in the model-facing output.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// truncateToolOutput head+tails s when it exceeds maxToolOutputBytes, slicing
// on rune boundaries so we never split a multibyte glyph. Returns the possibly
// trimmed body plus a one-line user-facing notice when truncation happened
// (empty when it didn't, without the "· " display prefix).
func truncateToolOutput(s string) (string, string) {
	if len(s) <= maxToolOutputBytes {
		return s, ""
	}
	keep := maxToolOutputBytes / 2
	head := snapToRuneBoundary(s, 0, keep)
	tail := snapToRuneBoundary(s, len(s)-keep, len(s))
	omitted := len(s) - len(head) - len(tail)
	notice := fmt.Sprintf("tool output truncated: %d of %d bytes elided", omitted, len(s))
	body := head + fmt.Sprintf("\n\n…[truncated %d of %d bytes — rerun with narrower args to see the middle]…\n\n", omitted, len(s)) + tail
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
	case "content_filter":
		return "response blocked by content filter", true
	case "repetition_truncation":
		return "response truncated: model repetition detected", true
	default:
		return "", false
	}
}
