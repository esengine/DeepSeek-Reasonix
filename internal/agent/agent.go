package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"reasonix/internal/ablation"
	"reasonix/internal/agentpreset"
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
	"reasonix/internal/taskpolicy"
	"reasonix/internal/tool"
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

// DeliveryRuntimeMarker is the retired delivery contract block. Nothing
// appends it any more; it survives byte-exact so a session recorded before
// the retirement still strips it out of previews and steer replay.
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
	if a.svc.jobs != nil {
		ctx = jobs.WithManager(ctx, a.svc.jobs)
	} else {
		ctx = jobs.WithoutManager(ctx)
	}
	if a.svc.memQueue != nil {
		ctx = memory.WithQueue(ctx, a.svc.memQueue)
	} else {
		ctx = memory.WithoutQueue(ctx)
	}
	return planmode.WithActive(ctx, a.planningPhase())
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
	agentConfig
	// svc are the collaborators this agent talks to; see services.go.
	svc agentServices
	// role is what this agent is; see agent_role.go.
	role agentRole
	// sess is the state one conversation owns; SetSession restarts it. See
	// sessionstate.go.
	sess              sessionRuntime
	responseLanguage  atomic.Value // string: auto|zh|en
	reasoningLanguage atomic.Value // string: auto|zh|en

	// unwrittenResolve is the resolve watermark a failed state write still owes.
	// It outlives the conversation, which is why it is not in sessionRuntime.
	unwrittenResolve unwrittenResolve

	// planRuntime is the plan lifecycle: which phase, under which authority
	// epoch. Replaceable, so a controller can hand several agents the one
	// lifecycle they share. Never a permission or sandbox boundary.
	planRuntime atomic.Pointer[planmode.Runtime]

	// mutationDependencyBarrier is how much of the rest of a provider tool batch
	// an earlier failure takes with it (see barrierLevel). executeOne re-checks
	// it after proxy resolution so use_capability cannot bypass it by
	// advertising schema-level ReadOnly()==true. Parallel read-only segments
	// never set it. Cleared at the start of each executeBatch.
	mutationDependencyBarrier atomic.Int32

	// recovery is who this agent is to the shared gate above.
	recovery recoveryIdentity

	// writeWorkspaceRoot is the workspace used to normalize parent write
	// reservations when writeScheduler is set.

	// steer holds mid-turn guidance admitted while the agent is running.
	// Entries keep a durable inbox item ID plus a loader so full bodies are not
	// retained in the agent heap beyond need. Cache miss for the next API call
	// is unavoidable but limited to one call — the prefix stays stable otherwise.
	steer steerInbox

	// task is the state shared by every Run continuing one delivery scope: the
	// receipt ledger complete_step validates citations against, the spend that
	// outlives a single Run, and the guards keyed to the task rather than the
	// turn. See taskstate.go.
	task taskRuntime

	planContract *plancontract.Plan // approved plan this turn executes, if any

	// hostAdvanceSeq guarantees unique tool IDs across turns: every
	// emitTodoState call increments it so the frontend always sees a fresh
	// dispatch even when the same panel index is signed off in different turns.
	hostAdvanceSeq atomic.Int64

	// projectChecks are structured project instructions that complete_step can
	// verify against same-turn bash receipts after a write-backed completion.
	projectChecks []instruction.VerifyCheck
	// projectSensitivePaths are the globs the project declared as needing the
	// hardest review. Empty means undeclared, and the host says so rather than
	// guessing sensitivity from how a path is spelled.
	projectSensitivePaths []string

	// deliveryProfile enables the runtime-enforced delivery contract. The stable
	// profile prompt explains intent; this is host state and never enters the
	// provider-cached prefix. The scope ID and checkpoint it works against live
	// in task; the per-turn expectations live in turn.
	// When agentPreset is set, deliveryProfile is derived for baseline Delivery
	// and may be elevated per-turn by TaskPolicy (e.g. Light high-risk).
	deliveryProfile bool

	// agentPreset is the session role setting. Atomic so SetAgentPreset can
	// update subsequent turns without rebuilding the agent.
	agentPreset atomic.Value // string light|balanced|delivery

	// turn is the state of the Run currently executing; beginRunTurn replaces
	// it wholesale. See turnruntime.go.
	turn turnRuntime

	// ablation names the subsystems a benchmark arm switched off. The zero value
	// is the control arm.
	ablation ablation.Set

	// pending is what an external caller arms before the next Run; see
	// turnruntime.go.
	pending pendingTurn

	// capabilityLedger tracks require/prefer outcomes for this user turn only.
	// Never serialized into prompts or session state.
	capabilityLedger *capability.Ledger
	// capabilityAudit accumulates non-persisted routing/proxy counters.
	capabilityAudit *capability.Audit
	// capabilityGate is the turn's gate memory across final-answer retries.
	capabilityGate capabilityGateState

	// subagentDepth tracks the current agent's nesting depth. maxSubagentDepth
	// caps delegation; when reached, recursive agent/skill tools are excluded.

	// windowProbe holds what a provider that declares no context window has
	// revealed about it by rejecting or accepting requests of a known size.
	windowProbe contextWindowProbe

	// Context management keeps the canonical transcript immutable and installs
	// at most one provider-visible checkpoint each time compactRatio is crossed.
	keepPolicy             KeepPolicy
	strictAlternatingRoles bool // coalesce adjacent user turns on provider request copies
	// activeTurnCreatedAt identifies the real/synthetic user message that began
	// the currently running turn. Compaction may rewrite older history while a
	// tool loop is active, but it must keep this message and everything after it
	// verbatim so cancellation/crash recovery can retain completed tool pairs.
	activeTurnCreatedAt atomic.Int64
}

// KeepPolicy is a bitmask controlling which messages are preserved beyond the
// recent tail during compaction.
type KeepPolicy int

const (
	KeepErrors KeepPolicy = 1 << iota
	KeepUserMarked
)

// SetPlanMode enters or leaves the plan-first workflow. Ordinary calls still
// use Permissions/Sandbox; only explicit phase opt-outs are refused. The system
// prompt and tool schemas stay untouched, while the caller supplies the
// model-facing Marker in a user turn.
func (a *Agent) SetPlanMode(v bool) { a.plan().SetActive(v) }

// SetTools replaces the agent's tool registry. The next API call picks up the
// new tool schema; tools already cached in the provider prefix are unaffected
// until the prefix is invalidated. Safe to call between turns.
func (a *Agent) SetTools(tools *tool.Registry) {
	if a == nil {
		return
	}
	a.svc.tools = tools
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
	a.svc.gate = g
}

// SetExtensions installs the extension dispatcher after construction. Boot
// uses it because sidecars — and therefore the dispatcher — only exist after
// snapshot assembly, which runs after the agent is built. Safe to call before
// the run loop starts; nil disables interception.
func (a *Agent) SetExtensions(d *dispatch.Dispatcher) {
	if a == nil {
		return
	}
	a.svc.extensions = d
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
	a.svc.recoveryGate = g
}

// SetRecoveryIdentity sets the agent/task labels used on recovery cards.
func (a *Agent) SetRecoveryIdentity(agentID, taskID string) {
	if a == nil {
		return
	}
	a.recovery.agentID = strings.TrimSpace(agentID)
	a.recovery.taskID = strings.TrimSpace(taskID)
}

// RecoveryGate returns the attached Auto Guard (may be nil).
func (a *Agent) RecoveryGate() RecoveryGate {
	if a == nil {
		return nil
	}
	return a.svc.recoveryGate
}

// SetSandboxEscapeApprover installs the optional one-shot approval path used by
// the bash tool when an enforced OS sandbox fails to start.
func (a *Agent) SetSandboxEscapeApprover(g sandbox.EscapeApprover) {
	if nilutil.IsNil(g) {
		g = nil
	}
	a.svc.sandboxEscape = g
}

// SetConfigWriteApprover installs the optional per-write approval path used by
// the file tools when a target is a Reasonix-managed config file outside the
// workspace write roots.
func (a *Agent) SetConfigWriteApprover(g tool.ConfigWriteApprover) {
	if nilutil.IsNil(g) {
		g = nil
	}
	a.svc.configWrite = g
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
	input = WithWorkspace(input, a.writeWorkspaceRoot, a.workspaceVCS)
	// The frozen role setting rides the per-turn <execution-policy>, not a marker.
	return input
}

// SetAsker installs the asker the `ask` tool uses to question the user.
// Interactive frontends wire one in; headless runs leave it nil.
func (a *Agent) SetAsker(as Asker) { a.svc.asker = as }

// SetMemoryQueue installs the sink the remember/forget tools use to apply a
// memory change in the current session. The controller wires itself in.
func (a *Agent) SetMemoryQueue(q memory.Queue) { a.svc.memQueue = q }

// SetPreEditHook installs the pre-edit snapshot hook (see onPreEdit). The
// controller wires it to its per-session checkpoint store; nil disables capture.
// Prefer SetMutationObserver for v2 capture (before+after fingerprints).
func (a *Agent) SetPreEditHook(fn func(diff.Change)) { a.svc.preEdit = fn }

// SetMutationObserver installs the unified mutation observer. When set, it
// supersedes onPreEdit for capture and also records after-mutation fingerprints.
// When a task tool is already registered it inherits the observer for sub-agents.
func (a *Agent) SetMutationObserver(obs *checkpoint.MutationObserver) {
	a.svc.mutationObserver = obs
	if a.svc.tools == nil || obs == nil {
		return
	}
	if t, ok := a.svc.tools.Get("task"); ok {
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
	return a.svc.mutationObserver
}

// Session returns the agent's current conversation, useful for persistence
// hooks that need to read the message log between turns. sessMu serialises this
// pointer read against SetSession, so a frontend (serve's concurrent /history and
// /new handlers) can't race the swap. The run loop touches a.session directly and
// only swaps it via SetSession while idle, so its reads need no lock.
func (a *Agent) Session() *Session {
	a.sess.mu.Lock()
	defer a.sess.mu.Unlock()
	return a.sess.conversation
}

// SetSession replaces the agent's conversation wholesale. Used by
// `reasonix --resume` to load a saved JSONL transcript before the first turn,
// so the model picks up exactly where it left off. Callers serialise it against a
// running turn (it only fires while idle); sessMu guards the pointer swap itself.
func (a *Agent) SetSession(s *Session) {
	a.sess.reset(s)
	// The replaced conversation's task is over, but the ledger and the bill
	// answer to beginRunTurn's scope check rather than to this seam.
	if s != nil {
		a.rebuildTodoState(s.Snapshot())
	}
}

// LastUsage returns the most recent per-turn token telemetry the provider
// reported (nil if no turn has run yet). The TUI uses it to show a context
// gauge alongside the prompt; ContextManager.Prepare owns cache-breaking
// maintenance decisions.
func (a *Agent) LastUsage() *provider.Usage { return a.sess.output.lastUsage.Load() }

// SessionCache returns the cumulative cache hit/miss prompt tokens across every
// API call this session — the basis for the status line's aggregate hit-rate.
func (a *Agent) SessionCache() (hit, miss int) {
	return int(a.sess.cacheHit.Load()), int(a.sess.cacheMiss.Load())
}

// ContextWindow returns the configured context-window size in tokens. 0
// means compaction is disabled for this agent.
func (a *Agent) ContextWindow() int { return a.effectiveContextWindow() }

// mid-turn steer marker.
// MidTurnSteerPrefix marks user messages that were injected mid-turn as
// guidance (via Steer). The model sees them as instructions; frontends
// display them as a notice, not a regular user bubble.
const MidTurnSteerPrefix = "[Mid-turn steer queued by the user. Do not treat this as a new task; use it only as additional guidance for the current task after completing the current step.]"

// HostNoticePrefix marks guidance the host runtime queued for itself, such as
// recovery advice after a tool failed. It rides the steer path but must not
// claim the user spoke: a model told the user interrupted starts answering a
// person who said nothing, and cannot tell a real interruption from this one.
const HostNoticePrefix = "[Mid-turn notice from the host runtime; the user did not send this. Do not treat it as a new task or as a message to answer; use it only as additional guidance for the current task after completing the current step.]"

func midTurnSteerMessage(text string, host bool) string {
	if host {
		return HostNoticePrefix + "\n" + text
	}
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
		for _, prefix := range []string{MidTurnSteerPrefix, HostNoticePrefix} {
			after, found := strings.CutPrefix(s, prefix)
			if !found {
				continue
			}
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

// steerEntry is one mid-turn guidance admission. host marks the ones the
// runtime queued for itself, which the model must not read as the user
// speaking.
// Steer queues a message for mid-turn injection. It reports whether an active
// turn accepted the text; on false nothing was queued and the caller must
// deliver it another way (typically as a new turn). Without the active check,
// a steer landing in the window between the turn's exit flush and the
// controller observing running=false would sit in the queue unconsumed and
// unpersisted — invisible to both the model and history.
func (a *Agent) Steer(text string) bool {
	return a.SteerItem("", func() (string, error) { return text, nil })
}

// SteerHostNotice queues runtime-authored guidance on the same path as Steer,
// attributed to the host instead of the user.
func (a *Agent) SteerHostNotice(text string) bool {
	return a.queueSteer(steerEntry{load: func() (string, error) { return text, nil }, host: true})
}

// SteerItem queues durable-inbox guidance identified by itemID. load is called
// only when the entry is consumed so the agent does not retain every body.
func (a *Agent) SteerItem(itemID string, load func() (string, error)) bool {
	return a.queueSteer(steerEntry{itemID: itemID, load: load})
}

func (a *Agent) queueSteer(e steerEntry) bool {
	return a.steer.admit(e)
}

// SteerConsumed returns true when the steer queue became empty after the last consume.
func (a *Agent) SteerConsumed() bool {
	return a.steer.drained()
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
	a.svc.sink = sink
}

func (a *Agent) consumeSteer() (text, itemID string, host, ok bool) {
	e, ok := a.steer.take()
	if !ok {
		return "", "", false, false
	}
	if e.load != nil {
		t, err := e.load()
		if err != nil {
			return "", e.itemID, e.host, false
		}
		return t, e.itemID, e.host, true
	}
	return e.text, e.itemID, e.host, true
}

// closeSteerIntakeIfIdle atomically closes the normal-completion race between
// the final queue check and Run returning. A steer accepted before this check
// keeps the loop alive; one arriving after it is rejected so the host can keep
// the user's draft and retry it as a regular follow-up.
func (a *Agent) closeSteerIntakeIfIdle() bool {
	return a.steer.closeIfIdle()
}

// flushSteerQueue ends the turn's steer intake. Guidance that arrived too late
// to be consumed is persisted for transcript visibility but marked local-only:
// replaying it to the model on the next unrelated user turn can execute a stale
// historical task (#7045). An explicit warning keeps the transcript honest
// without presenting the text as successfully applied guidance (#6238).
func (a *Agent) flushSteerQueue() {
	for _, e := range a.steer.close() {
		text := e.text
		if e.load != nil {
			if t, err := e.load(); err == nil {
				text = t
			}
		}
		a.recordUnappliedSteer(text, e.host, e.itemID)
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
	a.recordUnappliedSteer(text, false, itemID...)
}

func (a *Agent) steerQueueLen() int {
	return a.steer.pending()
}

// CompactRatio returns the fraction of the window at which auto-compaction
// fires (e.g. 0.8). The status line uses it to show headroom to the next compact.
func (a *Agent) CompactRatio() float64 { return a.compactRatio }

// CompactNow forces one projection compaction (canonical transcript untouched).
func (a *Agent) CompactNow(ctx context.Context, instructions string) error {
	_, err := a.contextManager().Prepare(ctx, ContextPreparePolicy{Trigger: CompactionTriggerManual, Instructions: instructions, Force: true})
	return err
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
		svc: newAgentServices(prov, tools, sink, gate,
			sandboxEscapeApprover, configWriteApprover, hooks, opts),
		agentConfig: agentConfig{
			maxSteps:           opts.MaxSteps,
			maxStepsKey:        maxStepsKey,
			reasoningByteLimit: reasoningByteLimit,
			maxOutputTokens:    opts.MaxOutputTokens,
			temperature:        opts.Temperature,
			usageSource:        usageSourceOrDefault(opts.UsageSource, event.UsageSourceExecutor),
			modelRef:           strings.TrimSpace(opts.ModelRef),
			workspaceID:        strings.TrimSpace(opts.WorkspaceID),
			classifierTaskText: opts.ClassifierTaskText,
			writeWorkspaceRoot: strings.TrimSpace(opts.WriteWorkspaceRoot),
			workspaceVCS:       strings.TrimSpace(opts.WorkspaceVCS),
			subagentDepth:      subagentDepth,
			maxSubagentDepth:   maxSubagentDepth,
			contextWindow:      opts.ContextWindow,
			compactRatio:       opts.CompactRatio,
			recentKeep:         opts.RecentKeep,
			budgets:            opts.CompactionBudgets,
			archiveDir:         opts.ArchiveDir,
		},
		sess: sessionRuntime{
			conversation: session,
			path:         strings.TrimSpace(opts.SessionPath),
			cacheState:   CacheStateUnknown,
		},
		task: taskRuntime{
			ledger: evidence.NewLedger(),
			budget: runBudget{limit: normalizeTaskBudget(opts.TaskBudget)},
		},
		role: agentRole{
			requireVisibleFinal: opts.RequireVisibleFinal,
			readOnlyExecution:   opts.ReadOnlyExecution,
			plannerMCPExecution: opts.PlannerMCPExecution,
		},
		recovery: recoveryIdentity{
			agentID: strings.TrimSpace(opts.RecoveryAgentID),
			taskID:  strings.TrimSpace(opts.RecoveryTaskID),
		},
		projectChecks:          append([]instruction.VerifyCheck(nil), opts.ProjectChecks...),
		projectSensitivePaths:  append([]string(nil), opts.ProjectSensitivePaths...),
		deliveryProfile:        opts.DeliveryProfile || agentpreset.Normalize(opts.AgentPreset) == agentpreset.Delivery,
		ablation:               opts.Ablation,
		capabilityLedger:       opts.CapabilityLedger,
		capabilityAudit:        opts.CapabilityAudit,
		keepPolicy:             opts.KeepPolicy,
		strictAlternatingRoles: opts.StrictAlternatingRoles,
	}
	a.sess.output.outputBudget = outputBudgetOf(prov)
	if a.sess.path != "" {
		a.LoadProjectionSidecar(a.sess.path)
	}
	preset := strings.TrimSpace(opts.AgentPreset)
	if preset == "" && opts.DeliveryProfile {
		preset = string(agentpreset.Delivery)
	}
	a.SetAgentPreset(preset)
	a.SetResponseLanguage(opts.ResponseLanguage)
	a.SetReasoningLanguage(opts.ReasoningLanguage)
	return a
}

// SetAgentPreset updates the role setting used by subsequent turns. It does not
// rebuild providers, tools, or history. The in-flight turn keeps its frozen
// TaskPolicy.
func (a *Agent) SetAgentPreset(preset string) {
	if a == nil {
		return
	}
	p := agentpreset.Normalize(preset)
	a.agentPreset.Store(string(p))
	// Keep baseline deliveryProfile aligned with Delivery role setting so
	// legacy gates that still read the bool stay coherent until fully migrated.
	switch p {
	case agentpreset.Delivery:
		a.deliveryProfile = true
	case agentpreset.Balanced:
		// A turn may still elevate; the baseline stays non-delivery.
		a.deliveryProfile = false
	}
}

// AgentPreset returns the current session role setting (never empty).
func (a *Agent) AgentPreset() string {
	if a == nil {
		return string(agentpreset.Balanced)
	}
	if v := a.agentPreset.Load(); v != nil {
		if s, ok := v.(string); ok && s != "" {
			return string(agentpreset.Normalize(s))
		}
	}
	if a.deliveryProfile {
		return string(agentpreset.Delivery)
	}
	return string(agentpreset.Balanced)
}

// TurnPolicy returns the frozen TaskPolicy for the active turn, if any.
func (a *Agent) TurnPolicy() (taskpolicy.TaskPolicy, bool) {
	if a == nil || !a.turn.policySet {
		return taskpolicy.TaskPolicy{}, false
	}
	return a.turn.policy, true
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
	if a == nil || a.svc.writeScheduler == nil || a.subagentDepth > 0 || readOnly || runTool == nil {
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
	return a.svc.writeScheduler.ReserveParentWrite(claim)
}

// Run appends the user input and drives the tool loop until the model returns a
// final answer, the context is cancelled, or the provider errors. maxSteps <= 0
// leaves the loop unbounded here: bounding it is the host's call, as a TaskBudget
// in hours or tokens rather than a round count, and every such budget ships off.
// Turn policy lives in beginRunTurn / runToolLoop / handleFinalResponse.
func (a *Agent) Run(ctx context.Context, input string) (runErr error) {
	runMaxSteps := a.maxSteps
	runMaxStepsKey := a.maxStepsKey
	runLimitHostOwned := false
	if limit, ok := runStepLimitFromContext(ctx); ok {
		runMaxSteps = limit.steps
		runLimitHostOwned = true
		if limit.key != "" {
			runMaxStepsKey = limit.key
		}
	}
	a.recovery.runSeq.Add(1)
	// All role settings participate in the workspace lease for the run; the
	// exclusive write lock is still acquired lazily on the first real writer.
	if a.svc.workspaceLease != nil {
		a.svc.workspaceLease.BeginRun()
		defer a.svc.workspaceLease.EndRun()
	}
	turnStartedAt := time.Now()
	workDurationMs := func() int64 {
		if elapsed := time.Since(turnStartedAt).Milliseconds(); elapsed > 0 {
			return elapsed
		}
		return 1
	}
	defer a.flushSteerQueue()
	a.steer.open()

	// Commit background-job evidence leases only after this turn delivers.
	// wait/bash_output merge a finished background writer's receipts into the
	// ledger provisionally; if the turn reaches a final answer (runErr == nil)
	// the delivery gates have verified and reviewed those mutations, so the
	// job's evidence can be permanently drained. A failed or cancelled turn
	// leaves the lease uncommitted so the next turn re-collects it.
	defer func() {
		if runErr != nil || a.task.ledger == nil || a.svc.jobs == nil {
			return
		}
		for _, lease := range a.task.ledger.BackgroundLeases() {
			a.svc.jobs.CommitEvidenceForSession(lease.Session, lease.JobID)
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
	state.runMaxSteps = runMaxSteps
	state.runMaxStepsKey = runMaxStepsKey
	state.runLimitHostOwned = runLimitHostOwned
	state.workDurationMs = workDurationMs
	return a.runToolLoop(ctx, state)
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
	return a.task.checkpoint
}

// RestoreDeliveryCheckpoint seeds a rebuilt controller before its next Goal
// run. A mismatched/empty scope is ignored conservatively.
func (a *Agent) RestoreDeliveryCheckpoint(checkpoint evidence.DeliveryCheckpoint) {
	checkpoint.ScopeID = strings.TrimSpace(checkpoint.ScopeID)
	if checkpoint.ScopeID == "" {
		return
	}
	a.task.checkpoint = checkpoint
	a.task.scopeID = checkpoint.ScopeID
}

// PrepareDeliveryRecovery preserves the exhausted turn's evidence for exactly
// one explicit continuation. It returns false when there is no matching
// readiness failure, so normal follow-up turns cannot inherit stale mutations.
func (a *Agent) PrepareDeliveryRecovery() bool {
	if !a.deliveryProfile {
		return false
	}
	return a.prepareEvidenceContinuation()
}

func (a *Agent) updateDeliveryCheckpoint(runErr error) {
	if !a.turn.deliveryScopeActive || a.task.scopeID == "" || a.task.ledger == nil {
		return
	}
	cp := a.task.checkpoint
	if cp.ScopeID != a.task.scopeID {
		cp = evidence.DeliveryCheckpoint{ScopeID: a.task.scopeID}
	}
	cp.CriteriaEstablished = cp.CriteriaEstablished || a.turn.deliveryCriteriaEstablished || a.task.ledger.HasSuccessfulTodoWrite()
	cp.WorkObserved = cp.WorkObserved || a.task.ledger.HasSuccessfulWorkReceipt()
	persistentOnlyReady := a.task.ledger.HasSuccessfulToolReceipt("remember") &&
		!a.task.ledger.HasSuccessfulMutationOtherThan("remember")
	if _, ok := a.task.ledger.LatestSuccessfulMutationIndex(); ok && !persistentOnlyReady {
		cp.MutationObserved = true
		cp.PendingMutation = true
	}
	if persistentOnlyReady {
		cp.MutationObserved = true
	}
	if runErr == nil && cp.PendingMutation && a.deliveryMutationCheckpointReady() {
		cp.PendingMutation = false
	}
	a.task.checkpoint = cp
}

func (a *Agent) deliveryMutationCheckpointReady() bool {
	if a.task.ledger == nil || !a.turn.deliveryCriteriaEstablished {
		return false
	}
	mutation, ok := a.task.ledger.LatestSuccessfulMutationIndex()
	if !ok {
		mutation = -1
	}
	return a.task.ledger.HasSuccessfulCompleteStepAfter(mutation) &&
		a.task.ledger.HasSuccessfulDeliverySignoffAfter(mutation) &&
		a.task.ledger.HasSuccessfulReviewAfter(mutation) &&
		a.reviewGateFailure() == ""
}

func (a *Agent) setTodoState(todos []evidence.TodoItem) {
	a.sess.todoMu.Lock()
	a.sess.todoState = evidence.NormalizeSerialTodos(todos)
	a.sess.todoMu.Unlock()
}

func (a *Agent) hasActiveCanonicalTodo() bool {
	a.sess.todoMu.Lock()
	defer a.sess.todoMu.Unlock()
	for _, todo := range a.sess.todoState {
		if canonicalTodoStatus(todo.Status) == "in_progress" {
			return true
		}
	}
	return false
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
	a.svc.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: t})
	t.Output = "task list advanced by complete_step"
	a.svc.sink.Emit(event.Event{Kind: event.ToolResult, Tool: t})
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
			rec := evidence.ReceiptFromToolCall(tc.Name, json.RawMessage(tc.Arguments), true, evidence.ToolFacts{ReadOnly: true})
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
			rec := evidence.ReceiptFromToolCall(tc.Name, json.RawMessage(tc.Arguments), true, evidence.ToolFacts{ReadOnly: true})
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

// shouldNudgeExecutorHandoff reports whether an executor that finished a
// planner handoff without touching a tool owes another attempt. The caller has
// already established the hard part — no tool ran — so the only question left
// is whether this plan called for one, which the plan contract states outright.
func (a *Agent) shouldNudgeExecutorHandoff() bool {
	plan := a.planContractSnapshot()
	if plan == nil {
		// No contract, no basis: nudging would interrupt an answer that may be
		// exactly right, on nothing but a guess about what the plan wanted.
		return false
	}
	return planContractExpectsAction(*plan)
}

// planContractExpectsAction reports whether carrying out the plan requires
// touching the workspace: a step that names files or a verification command
// cannot be delivered as prose alone.
func planContractExpectsAction(plan plancontract.Plan) bool {
	for _, step := range plan.Steps {
		if len(step.VerifiedFiles) > 0 || len(step.CandidateFiles) > 0 || len(step.Verification) > 0 {
			return true
		}
	}
	return false
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
	transformReasoning := a.svc.hooks != nil && a.svc.hooks.HasPostLLMCall()

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
			display = a.svc.hooks.PostLLMCall(ctx, original, turn)
			if display != "" {
				sink.Emit(event.Event{Kind: event.Reasoning, Text: display})
			}
		}
		stored = display
		providerBound := signature != "" || reasoningID != "" || reasoningStatus != ""
		if providerBound || provider.RequiresReasoningRoundTrip(a.svc.prov) || (len(calls) > 0 && provider.RequiresToolCallReasoning(a.svc.prov)) {
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
			responsesItems = appendProviderItem(responsesItems, chunk.ResponsesItem)
		case provider.ChunkProviderTool:
			a.absorbProviderRunCall(&text, sink, chunk, attemptID)
		case provider.ChunkUsage:
			usage = chunk.Usage
			a.storeLatestRequestUsage(chunk.Usage)
			a.sess.cacheHit.Add(int64(chunk.Usage.CacheHitTokens))
			a.sess.cacheMiss.Add(int64(chunk.Usage.CacheMissTokens))
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
	a.sess.conversation.Add(provider.Message{
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
	return CaptureShape(a.systemPrompt(), schemas, a.sess.conversation.RewriteVersion())
}

func (a *Agent) systemPrompt() string {
	var b strings.Builder
	for _, m := range a.sess.conversation.Messages {
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
	t, _, ambiguous := a.svc.tools.ResolveCall(c.Name)
	ok := t != nil && len(ambiguous) == 0
	ev := event.Tool{ID: c.ID, Name: c.Name, Args: c.Arguments, ReadOnly: ok && t.ReadOnly(), Refreshed: refreshed}
	ev.FileDiff = event.FileDiff{Diff: c.Diff, Added: c.Added, Removed: c.Removed}
	if ok && ev.Diff == "" && ev.Added == 0 && ev.Removed == 0 {
		if ch, ok := tool.PreviewChange(ctx, t, json.RawMessage(c.Arguments)); ok {
			ev.FileDiff = event.FileDiff{Diff: ch.Diff, Added: ch.Added, Removed: ch.Removed}
		}
	}
	if ok {
		ev.Profile = delegationProfile(t, json.RawMessage(c.Arguments))
	}
	a.svc.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: ev})
}

// emitResolvedToolDispatch upserts the real target classification of a stable
// proxy call without changing the provider-visible Name/Args. Append-only sinks
// ignore Refreshed events; stateful frontends replace the existing card by ID.
func (a *Agent) emitResolvedToolDispatch(c provider.ToolCall, profile *event.Profile) {
	if c.ResolvedReadOnly == nil {
		return
	}
	if c.ResolvedName != "" && c.ResolvedName != c.Name {
		EmitProxyAudit(a.svc.sink, tool.ResolvedCall{
			DisplayName:  c.Name,
			TargetName:   c.ResolvedName,
			CapabilityID: c.CapabilityID,
		})
	}
	a.svc.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
		ID:           c.ID,
		Name:         c.Name,
		Args:         c.Arguments,
		ResolvedName: c.ResolvedName,
		CapabilityID: c.CapabilityID,
		ReadOnly:     *c.ResolvedReadOnly,
		Refreshed:    true,
		Profile:      profile,
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
		t, _, ambiguous := a.svc.tools.ResolveCall(out[i].Name)
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
	if a == nil || toolName != "todo_write" || a.planningPhase() {
		return false, "", "", ""
	}
	before := a.CanonicalTodoState()
	if len(before) == 0 || len(evidence.IncompleteTodos(before)) == 0 {
		return false, "", "", ""
	}
	after := evidence.ReceiptFromToolCall("todo_write", args, true, evidence.ToolFacts{ReadOnly: true}).Todos
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
	if a == nil || !a.role.readOnlyExecution {
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
		if a.role.plannerMCPExecution && isMCPExecutionTarget(visible, "") {
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
			if a.role.plannerMCPExecution && resolved.HostCompleted && resolved.SkipExecute && resolved.ReadOnly && !resolved.Unavailable {
				if _, ok := parseMCPServerCapabilityID(resolved.CapabilityID); ok {
					return toolOutcome{}, false
				}
			}
			return block("execute an unresolved dynamic capability")
		}
		if a.role.plannerMCPExecution && plannerAllowsMCPTarget(resolved.Target, resolved.TargetName) {
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

func (a *Agent) staleAnchorEditBlock(call provider.ToolCall) (string, bool) {
	if a.task.ledger == nil || !anchorBasedEditTool(call.Name) {
		return "", false
	}
	rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), true, a.toolFactsFor(call.Name))
	if len(rec.Paths) == 0 {
		return "", false
	}
	writeIndex, ok := a.task.ledger.LatestSuccessfulWriteIndex(rec.Paths)
	if !ok || a.task.ledger.HasSuccessfulAnchorRefreshReadAfter(rec.Paths, writeIndex) {
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
	t, _, ambiguous := a.svc.tools.ResolveCall(name)
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
	return truncateToolOutputFor(s, "", "", maxToolOutputBytes, defaultSideEffectingSnip)
}

// truncateToolOutputFor is the first-visible limiter. The geometry comes from
// the caller, which reads it off the tool's own SnipHint contract; toolName and
// toolCallID only populate the marker so the model can re-fetch.
func truncateToolOutputFor(s, toolName, toolCallID string, cap int, strategy snipStrategy) (string, string) {
	if cap <= 0 {
		cap = maxToolOutputBytes
	}
	if len(s) <= cap {
		return s, ""
	}
	headKeep := strategy.headChars
	tailKeep := strategy.tailChars
	if headKeep+tailKeep > cap-512 {
		headKeep = cap * 2 / 3
		tailKeep = cap - headKeep - 512
	}
	if headKeep < 1024 {
		headKeep = cap / 2
		tailKeep = cap / 2
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
	if len(body) > cap {
		overflow := len(body) - cap
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
