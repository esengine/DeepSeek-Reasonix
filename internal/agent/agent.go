package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"reasonix/internal/diff"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/instruction"
	"reasonix/internal/jobs"
	"reasonix/internal/memory"
	"reasonix/internal/nilutil"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// maxToolOutputBytes caps a single tool result before it goes into the model's
// context. ~32KB is roughly 8K tokens — enough for a full file read or a busy
// grep, while preventing one accidental "read this 5 MB log" from blowing the
// window before the next compaction runs.
const maxToolOutputBytes = 32 * 1024

const maxFinalReadinessBlocks = 3
const maxEmptyFinalBlocks = 3
const maxStreamRecoveries = 1
const maxExecutorHandoffNudges = 1

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
func withCallContext(ctx context.Context, parentID string, sink event.Sink, asker Asker, planMode bool) context.Context {
	return context.WithValue(ctx, callContextKey{}, callContext{parentID: parentID, sink: sink, asker: asker, planMode: planMode})
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

// PlanModeFromContext reports whether the tool call is executing under the
// agent's read-only planning gate. Tools that are themselves ReadOnly may use
// this to avoid enabling follow-up writer-only surfaces during planning.
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
// execute time (after the plan-mode gate). It is interface-shaped so the agent
// stays independent of the permission package and of how "ask" is resolved
// (silently in headless runs, interactively in the chat TUI). A nil gate means
// no gating — every call runs, preserving behaviour for callers that don't wire
// one in. reason is fed back to the model when allow is false; a non-nil err
// (e.g. ctx cancelled awaiting approval) is treated as a block for that call.
type Gate interface {
	Check(ctx context.Context, toolName string, args json.RawMessage, readOnly bool) (allow bool, reason string, err error)
}

// ToolHooks fires user-configured shell hooks around each tool call. PreToolUse
// runs before the call and may block it (block=true; message is the reason fed
// back to the model); PostToolUse runs after and only surfaces output to the
// user (it can't block). It is interface-shaped so the agent stays independent
// of the hook package — a nil hooks field disables hook firing entirely.
type ToolHooks interface {
	PreToolUse(ctx context.Context, name string, args json.RawMessage) (block bool, message string)
	PostToolUse(ctx context.Context, name string, args json.RawMessage, result string)
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

	// planMode, when true, refuses any tool call whose ReadOnly() is false.
	// The system prompt and tool list never change with the toggle so the
	// prompt-cache prefix stays valid; the gating happens at execute time
	// and the model sees a "blocked" result it can adapt to. Toggled from
	// the outside via SetPlanMode.
	planMode atomic.Bool

	// gate, when non-nil, is the per-call permission gate consulted after the
	// plan-mode check. nil disables gating entirely.
	gate Gate

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

	// steerQueue holds mid-turn user messages queued while the agent is
	// running. Each is consumed once per loop iteration, persisted to the
	// session for history replay, and sent to the model as guidance (not a
	// new task). Cache miss for the next API call is unavoidable but limited
	// to one call — the prefix stays stable otherwise.
	steerMu       sync.Mutex
	steerQueue    []string
	steerConsumed bool

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

	// memQueue, when non-nil, lets the remember/forget tools fold a turn-tail note
	// about a just-made memory change into the next turn, so it applies this
	// session without touching the cache-stable prefix. Set via SetMemoryQueue.
	memQueue memory.Queue

	// Context management: when a turn's prompt nears contextWindow, the older
	// middle of the session is summarized away, keeping a token-bounded recent
	// tail verbatim (recentKeep is the message floor) and archiving the originals
	// under archiveDir. compactStuck latches when compaction can't get the prompt
	// under the window (consecutiveCompacts crosses the limit), so auto-compaction
	// pauses instead of looping. softCompactNoticed gates the one-shot soft-ratio
	// notice so it fires once per approach, not every turn.
	contextWindow       int
	softCompactRatio    float64
	compactRatio        float64
	compactForceRatio   float64
	softCompactNoticed  bool
	recentKeep          int
	archiveDir          string
	compactStuck        bool
	consecutiveCompacts int

	// stormSig / stormCount track a run of turns that keep failing the same way so
	// the loop can break a death-spiral. The signature is each call's (tool, error)
	// in order, NOT (tool, args): a stuck model reliably reworks the arguments
	// cosmetically (a re-worded essay, a reordered object) while the call fails
	// identically every time — keying on args misses the loop entirely (observed
	// live against truncated tool-call arguments). Because errors that embed their
	// subject (e.g. "file not found: /x") differ per target, genuine varied probing
	// does not collapse to one signature. Reset whenever a turn does anything else
	// (a different failure shape, or any success). See applyStormBreaker.
	stormSig   string
	stormCount int

	// repeatSuccessCounts tracks write-like tool calls that have already
	// succeeded in this user turn. This catches the complementary loop shape to
	// stormSig: a model keeps doing the same successful write, so there is no
	// error for the failure-only storm breaker to see.
	repeatSuccessCounts map[string]int
}

// SetPlanMode flips the read-only gate. While true, executeOne refuses any
// non-ReadOnly tool the model calls and returns a "blocked" result instead of
// running it. The cache-friendly bits — system prompt, tools schema, message
// history — are left untouched, so the toggle costs nothing in cache hits.
func (a *Agent) SetPlanMode(v bool) { a.planMode.Store(v) }

// SetReasoningLanguage updates the visible reasoning language preference for
// subsequent user-role messages emitted by this agent.
func (a *Agent) SetReasoningLanguage(lang string) {
	if a == nil {
		return
	}
	a.reasoningLanguage.Store(NormalizeReasoningLanguage(lang))
}

// SetGate installs the per-call permission gate. Used by `reasonix chat` to swap the
// headless gate built in setup for an interactive one that prompts the user;
// nil disables gating. Safe to call before the run loop starts.
func (a *Agent) SetGate(g Gate) {
	if nilutil.IsNil(g) {
		g = nil
	}
	a.gate = g
}

func (a *Agent) withReasoningLanguage(input string) string {
	if a == nil {
		return input
	}
	lang := "auto"
	if v := a.reasoningLanguage.Load(); v != nil {
		if s, ok := v.(string); ok {
			lang = s
		}
	}
	return WithReasoningLanguage(input, lang)
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
// `reasonix chat --resume` to load a saved JSONL transcript before the first turn,
// so the model picks up exactly where it left off. Callers serialise it against a
// running turn (it only fires while idle); sessMu guards the pointer swap itself.
func (a *Agent) SetSession(s *Session) {
	a.sessMu.Lock()
	a.session = s
	a.sessMu.Unlock()
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
func SteerText(content string) (string, bool) {
	after, found := strings.CutPrefix(content, MidTurnSteerPrefix)
	if !found {
		return "", false
	}
	// Strip only the "\n" separator, preserving the user's original text.
	after = strings.TrimPrefix(after, "\n")
	return after, true
}

// Steer queues a message for mid-turn injection.
func (a *Agent) Steer(text string) {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	a.steerQueue = append(a.steerQueue, text)
	a.steerConsumed = false
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

func (a *Agent) clearSteerQueue() {
	a.steerMu.Lock()
	defer a.steerMu.Unlock()
	a.steerQueue = nil
	a.steerConsumed = false
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
	// MaxStepsKey names the configuration knob shown when the MaxSteps guard is
	// hit. Empty defaults to agent.max_steps.
	MaxStepsKey string
	Temperature float64
	Pricing     *provider.Pricing // optional, for per-turn cost display

	// Gate is the per-call permission gate. nil disables gating.
	Gate Gate

	// Context management. ContextWindow <= 0 disables compaction. Ratios and
	// RecentKeep fall back to defaults when unset.
	ContextWindow     int
	SoftCompactRatio  float64
	CompactRatio      float64
	CompactForceRatio float64
	RecentKeep        int
	ArchiveDir        string

	// Hooks fires PreToolUse / PostToolUse shell hooks around tool calls. nil
	// disables hook firing.
	Hooks ToolHooks

	// Jobs is the session's background-job manager (nil disables background tools).
	Jobs *jobs.Manager

	// ProjectChecks are host-observable structured checks extracted during boot.
	ProjectChecks []instruction.VerifyCheck

	// ReasoningLanguage controls visible reasoning language preference as transient
	// user-turn context. Empty/auto injects nothing.
	ReasoningLanguage string
}

// New constructs an Agent. MaxSteps <= 0 means no cap — the run loop continues
// until the model gives a final answer, the context is cancelled, or the
// provider errors (compaction keeps the context bounded). A nil sink is replaced
// with event.Discard so the agent can always emit unconditionally.
func New(prov provider.Provider, tools *tool.Registry, session *Session, opts Options, sink event.Sink) *Agent {
	if opts.SoftCompactRatio <= 0 {
		opts.SoftCompactRatio = defaultSoftCompactRatio
	}
	if opts.CompactRatio <= 0 {
		opts.CompactRatio = defaultCompactRatio
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
	hooks := opts.Hooks
	if nilutil.IsNil(hooks) {
		hooks = nil
	}
	maxStepsKey := opts.MaxStepsKey
	if strings.TrimSpace(maxStepsKey) == "" {
		maxStepsKey = "agent.max_steps"
	}
	a := &Agent{
		prov:              prov,
		tools:             tools,
		session:           session,
		maxSteps:          opts.MaxSteps,
		maxStepsKey:       maxStepsKey,
		temperature:       opts.Temperature,
		pricing:           opts.Pricing,
		sink:              sink,
		gate:              gate,
		hooks:             hooks,
		jobs:              opts.Jobs,
		evidence:          evidence.NewLedger(),
		projectChecks:     append([]instruction.VerifyCheck(nil), opts.ProjectChecks...),
		contextWindow:     opts.ContextWindow,
		softCompactRatio:  opts.SoftCompactRatio,
		compactRatio:      opts.CompactRatio,
		compactForceRatio: opts.CompactForceRatio,
		recentKeep:        opts.RecentKeep,
		archiveDir:        opts.ArchiveDir,
	}
	a.SetReasoningLanguage(opts.ReasoningLanguage)
	return a
}

// Run appends the user input and drives the tool loop until the model returns a
// final answer (no tool calls), the context is cancelled, or the provider errors.
// With maxSteps <= 0 the loop is unbounded — the natural termination is the model
// finishing, and the real safety bounds are user cancellation and compaction, not
// a round count. A positive maxSteps imposes an optional hard guard, surfaced as
// a resumable notice when hit.
func (a *Agent) Run(ctx context.Context, input string) error {
	defer a.clearSteerQueue()
	a.steerMu.Lock()
	a.steerConsumed = false
	a.steerMu.Unlock()
	if a.evidence != nil {
		a.evidence.Reset()
	}
	a.repeatSuccessCounts = nil
	a.sink.Emit(event.Event{Kind: event.TurnStarted})
	input = a.withReasoningLanguage(input)
	a.session.Add(provider.Message{Role: provider.RoleUser, Content: input, Images: userImages(ctx)})

	finalReadinessBlocks := 0
	emptyFinalBlocks := 0
	handoffNudges := 0
	usedAnyTool := false
	streamRecoveries := 0
	executorHandoff := a.executorHandoffGuard && strings.Contains(input, executorHandoffMarker)
	for step := 0; a.maxSteps <= 0 || step < a.maxSteps; step++ {
		// Consume a queued steer and persist it to the session so it
		// survives tab switches and history replay. The model sees it as
		// guidance (with a prefix), not a new task. One cache miss per
		// steer is unavoidable — the model must see the new instruction.
		if text, ok := a.consumeSteer(); ok {
			a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withReasoningLanguage(midTurnSteerMessage(text))})
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
					})
				}
				a.session.Add(provider.Message{
					Role:    provider.RoleUser,
					Content: a.withReasoningLanguage(streamRecoveryMessage(hasVisibleFinalAnswer(text), partialToolStarted)),
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
		a.session.Add(provider.Message{
			Role:               provider.RoleAssistant,
			Content:            text,
			ReasoningContent:   reasoning,
			ReasoningSignature: signature,
			ToolCalls:          calls,
		})

		if len(calls) == 0 {
			readiness := a.finalReadinessCheck()
			if readiness.reason != "" {
				finalReadinessBlocks++
				result := evidence.ReadinessBlocked
				if finalReadinessBlocks >= maxFinalReadinessBlocks {
					result = evidence.ReadinessErrored
					event.RecordReadinessAudit(a.sink, readiness.audit(result, false))
					return fmt.Errorf("final-answer readiness failed %d times: %s", finalReadinessBlocks, readiness.reason)
				}
				event.RecordReadinessAudit(a.sink, readiness.audit(result, false))
				a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "final-answer readiness blocked: " + readiness.reason})
				a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withReasoningLanguage(finalReadinessRetryMessage(readiness.reason))})
				a.maybeCompact(ctx, usage)
				continue
			}
			if !hasVisibleFinalAnswer(text) {
				emptyFinalBlocks++
				if emptyFinalBlocks >= maxEmptyFinalBlocks {
					return fmt.Errorf("model finished without a visible final answer %d times", emptyFinalBlocks)
				}
				a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: emptyFinalNotice(a.prov.Name(), usage, len(reasoning))})
				a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withReasoningLanguage(emptyFinalRetryMessage())})
				a.maybeCompact(ctx, usage)
				continue
			}
			if executorHandoff && !usedAnyTool && handoffNudges < maxExecutorHandoffNudges {
				handoffNudges++
				a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "executor answered without taking any action; nudging it to use its tools"})
				a.session.Add(provider.Message{Role: provider.RoleUser, Content: a.withReasoningLanguage(executorHandoffRetryMessage())})
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

		results := a.executeBatch(ctx, calls)
		for i, call := range calls {
			a.session.Add(provider.Message{
				Role:       provider.RoleTool,
				Content:    results[i],
				ToolCallID: call.ID,
				Name:       call.Name,
			})
		}

		// The prompt only grows from here; compact before the next turn so it
		// stays within the model's window.
		a.maybeCompact(ctx, usage)
	}
	// Only reached when a positive maxSteps guard is configured. The work so far
	// is already in the session, so the user can just send another message to pick
	// up where it left off.
	return fmt.Errorf("paused after %d tool-call rounds (%s) — the work so far is saved; send another message to continue, or set %s higher or to 0 for no limit", a.maxSteps, a.maxStepsKey, a.maxStepsKey)
}

func (a *Agent) finalReadinessFailure() string {
	return a.finalReadinessCheck().reason
}

type finalReadinessCheck struct {
	applies              bool
	reason               string
	missingProjectChecks int
	incompleteTodos      int
}

func (c finalReadinessCheck) audit(result evidence.ReadinessAuditResult, recovered bool) evidence.ReadinessAudit {
	return evidence.ReadinessAudit{
		Result:                 result,
		Recovered:              recovered,
		MissingProjectChecks:   c.missingProjectChecks,
		IncompleteTodos:        c.incompleteTodos,
		CommandMismatchMissing: c.missingProjectChecks,
	}
}

func (a *Agent) finalReadinessCheck() finalReadinessCheck {
	if a.evidence == nil {
		return finalReadinessCheck{}
	}
	var missing []string
	out := finalReadinessCheck{}
	if !a.planMode.Load() {
		incomplete, hasTodos := a.evidence.IncompleteLatestTodos()
		if !hasTodos && a.evidence.HasAnySuccessfulReceipt() {
			incomplete, hasTodos = a.incompleteCanonicalTodos()
		}
		if hasTodos && len(incomplete) > 0 {
			out.applies = true
			out.incompleteTodos = len(incomplete)
			missing = append(missing, finalReadinessIncompleteTodos(incomplete))
		}
	}
	writer, hasWriter := a.evidence.LatestSuccessfulWriterIndex()
	if !hasWriter {
		if len(missing) > 0 {
			out.reason = strings.Join(missing, "; ")
		}
		return out
	}
	hasProjectChecks := len(a.projectChecks) > 0
	hasTodoReceipt := a.evidence.HasSuccessfulTodoWrite()
	if !hasProjectChecks && !hasTodoReceipt && len(missing) == 0 {
		return finalReadinessCheck{}
	}
	out.applies = true
	for _, check := range a.projectChecks {
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
	out.reason = strings.Join(missing, "; ")
	return out
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

func (a *Agent) setTodoState(todos []evidence.TodoItem) {
	a.todoMu.Lock()
	a.todoState = append([]evidence.TodoItem(nil), todos...)
	a.todoMu.Unlock()
}

func (a *Agent) incompleteCanonicalTodos() ([]evidence.TodoStepMatch, bool) {
	a.todoMu.Lock()
	defer a.todoMu.Unlock()
	if len(a.todoState) == 0 {
		return nil, false
	}
	return evidence.IncompleteTodos(a.todoState), true
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
	if !ok || canonicalTodoStatus(a.todoState[m.Index-1].Status) == "completed" {
		a.todoMu.Unlock()
		return
	}
	a.todoState[m.Index-1].Status = "completed"
	promoteNextPendingTodo(a.todoState)
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

func promoteNextPendingTodo(todos []evidence.TodoItem) {
	for _, t := range todos {
		if canonicalTodoStatus(t.Status) == "in_progress" {
			return
		}
	}
	for i := range todos {
		if canonicalTodoStatus(todos[i].Status) == "pending" {
			todos[i].Status = "in_progress"
			return
		}
	}
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

// rebuildTodoState reconstructs the canonical task list from a transcript: the
// latest successful todo_write is the base, then every complete_step after it
// advances an item. Deterministic from persisted messages, so it survives a
// fresh load or a rewind (the truncated history yields the historical state).
// Empty after compaction drops the todo_write — no worse than no canonical list.
func (a *Agent) rebuildTodoState(msgs []provider.Message) {
	failed := failedToolCallIDs(msgs)
	var todos []evidence.TodoItem
	baseIdx := -1
	for i, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			if tc.Name != "todo_write" || failed[tc.ID] {
				continue
			}
			if rec := evidence.ReceiptFromToolCall(tc.Name, json.RawMessage(tc.Arguments), true, true); len(rec.Todos) > 0 {
				todos = append([]evidence.TodoItem(nil), rec.Todos...)
				baseIdx = i
			}
		}
	}
	if baseIdx < 0 {
		a.setTodoState(nil)
		return
	}
	for i := baseIdx; i < len(msgs); i++ {
		for _, tc := range msgs[i].ToolCalls {
			if tc.Name != "complete_step" || failed[tc.ID] {
				continue
			}
			rec := evidence.ReceiptFromToolCall(tc.Name, json.RawMessage(tc.Arguments), true, true)
			if m, ok := evidence.MatchStep(rec.Step, todos); ok && canonicalTodoStatus(todos[m.Index-1].Status) != "completed" {
				todos[m.Index-1].Status = "completed"
				promoteNextPendingTodo(todos)
			}
		}
	}
	a.setTodoState(todos)
}

func failedToolCallIDs(msgs []provider.Message) map[string]bool {
	failed := map[string]bool{}
	for _, msg := range msgs {
		if msg.Role != provider.RoleTool || msg.ToolCallID == "" {
			continue
		}
		if strings.HasPrefix(msg.Content, "error:") || strings.HasPrefix(msg.Content, "blocked:") {
			failed[msg.ToolCallID] = true
		}
	}
	return failed
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
	return "Host final-answer readiness check failed. Before giving a final answer, address the missing host-observable receipts: " + reason + ". Run the required tool calls, then answer when readiness is satisfied."
}

func executorHandoffRetryMessage() string {
	return `You are already in the executor phase. The planner's read-only limitations do not apply to you.

Do not answer as the planner and do not ask how to trigger the executor.
Use your available tools now to carry out the task. If a write or command is blocked by permissions or workspace boundaries, state that specific blocker and ask for the needed approval/path.`
}

func hasVisibleFinalAnswer(text string) bool {
	return strings.TrimSpace(text) != ""
}

func emptyFinalRetryMessage() string {
	return "The previous assistant response finished without any visible answer text. Continue the same task now and provide a concise visible answer to the user. Do not send reasoning only."
}

func emptyFinalNotice(prov string, u *provider.Usage, reasoningLen int) string {
	finish := "unknown"
	if u != nil && u.FinishReason != "" {
		finish = u.FinishReason
	}
	return fmt.Sprintf("empty final answer blocked: %s returned no visible answer text (finish=%s, reasoning=%d chars); retrying", prov, finish, reasoningLen)
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
	ch, err := a.prov.Stream(ctx, provider.Request{
		Messages:    a.session.Messages,
		Tools:       a.tools.Schemas(),
		Temperature: a.temperature,
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
		if signature != "" {
			stored = original
		}
		return stored, display
	}
	for chunk := range ch {
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
	// With a PostLLMCall hook, the live stream was suppressed above; transform the
	// full reasoning now and emit it once so the sink never sees the untranslated
	// text. Without a hook this is skipped — the chunk-by-chunk events already fired.
	stored, display := finishReasoning()
	// Store the transformed reasoning — except when a provider signature pins it to
	// the original text (Anthropic extended thinking). That signed thinking block is
	// replayed verbatim on the next tool-call turn; re-uploading transformed text
	// under the original signature is rejected, so keep the original for storage
	// while the user still sees the transformed version live. finishReasoning did
	// that choice above.
	// Close the text stream: a sink may re-render the streamed raw text as
	// styled markdown now that it is complete. Reasoning rides along so the sink
	// has the full chain if it wants it.
	if text.Len() > 0 || display != "" {
		a.sink.Emit(event.Event{Kind: event.Message, Text: text.String(), Reasoning: display})
	}
	return text.String(), stored, signature, calls, usage, false, false, nil
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
// parallelised.
func (a *Agent) executeBatch(ctx context.Context, calls []provider.ToolCall) []string {
	for _, c := range calls {
		t, ok := a.tools.Get(c.Name)
		ev := event.Tool{ID: c.ID, Name: c.Name, Args: c.Arguments, ReadOnly: ok && t.ReadOnly()}
		if ok {
			if ch, ok := tool.PreviewChange(t, json.RawMessage(c.Arguments)); ok {
				ev.FileDiff = event.FileDiff{Diff: ch.Diff, Added: ch.Added, Removed: ch.Removed}
			}
			if pr, ok := t.(interface {
				ResolveProfile(json.RawMessage) *event.Profile
			}); ok {
				ev.Profile = pr.ResolveProfile(json.RawMessage(c.Arguments))
			}
		}
		a.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: ev})
	}

	results := make([]string, len(calls))
	outcomes := make([]toolOutcome, len(calls))
	durations := make([]int64, len(calls))
	run := func(i int) {
		start := time.Now()
		outcomes[i] = a.executeOne(ctx, calls[i])
		durations[i] = time.Since(start).Milliseconds()
		results[i] = outcomes[i].output
	}

	for _, batch := range partitionToolCalls(a.tools, calls) {
		if batch.parallel && batch.end-batch.start > 1 {
			runParallel(batch.start, batch.end, run)
			continue
		}
		for i := batch.start; i < batch.end; i++ {
			run(i)
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
	a.applyStormBreaker(calls, outcomes, results)
	return results
}

type toolCallBatch struct {
	start    int
	end      int
	parallel bool
}

// partitionToolCalls keeps provider order while letting contiguous known
// read-only tools run together. Unknown and writer tools are single-call serial
// batches so they cannot reorder around reads or produce surprising errors.
// complete_step and todo_write are read-only but never join a parallel run: they
// read the turn's evidence ledger, so every prior call's receipt must be recorded
// before they run.
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
	if name == "complete_step" || name == "todo_write" {
		return false
	}
	t, ok := r.Get(name)
	return ok && t.ReadOnly()
}

func runParallel(start, end int, run func(int)) {
	const maxParallel = 8
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	for i := start; i < end; i++ {
		i := i
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			run(i)
		}()
	}
	wg.Wait()
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

// applyStormBreaker detects a run of identically-failing turns and, past the
// threshold, rewrites the model-facing result (results[0]) into a directive to
// change approach. It keys on each call's (tool, error) — not its args — because a
// stuck model reworks the arguments cosmetically while failing identically (see
// the stormSig field doc). A turn is a fixation candidate only when every one of
// its calls errored and none was merely blocked by plan mode / permissions (those
// carry a clear, distinct message the model can already act on). Any success, any
// block, or a different batch shape is varied work, so it resets the counter. This
// covers both the single-call spiral and a repeated multi-call batch. The hard
// maxSteps guard remains the ultimate backstop; this just keeps the loop from
// burning that whole budget bouncing off the same failure.
func (a *Agent) applyStormBreaker(calls []provider.ToolCall, outcomes []toolOutcome, results []string) {
	sig, ok := batchStormSignature(calls, outcomes)
	if !ok {
		a.stormSig, a.stormCount = "", 0
		return
	}
	if sig != a.stormSig {
		a.stormSig, a.stormCount = sig, 1
		return
	}
	a.stormCount++
	if a.stormCount < stormBreakThreshold {
		return
	}
	subject := fmt.Sprintf("%q", calls[0].Name)
	short := calls[0].Name
	if len(calls) > 1 {
		subject = fmt.Sprintf("this batch of %d tool calls", len(calls))
		short = fmt.Sprintf("a batch of %d calls", len(calls))
	}
	results[0] = outcomes[0].output + fmt.Sprintf(
		"\n\n[loop guard] %s has now failed %d times in a row with the same error. Re-sending it — even with the wording changed — will not help: the calls keep failing the same way. Change approach: if an argument is being truncated, write less in one call and split the work into several smaller calls; otherwise fix the arguments, use a different tool, or explain the blocker in your final answer.",
		subject, a.stormCount)
	a.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: fmt.Sprintf(
		"loop guard: %s failed %d× the same way — nudging the model to change approach",
		short, a.stormCount)})
}

// batchStormSignature returns a per-turn fixation signature — each call's
// (name, error) in order — and ok=true only when every call errored and none was
// merely blocked. ok=false (any success or block) means the turn made varied
// progress, so the caller resets the counter. Keying on the error rather than the
// args is deliberate: a stuck model reworks the arguments while failing the same
// way, so identical-args matching would miss the loop.
func batchStormSignature(calls []provider.ToolCall, outcomes []toolOutcome) (string, bool) {
	if len(calls) == 0 {
		return "", false
	}
	var sb strings.Builder
	for i := range calls {
		if outcomes[i].errMsg == "" || outcomes[i].blocked {
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
// (without the "· " prefix) when the output was head+tailed.
type toolOutcome struct {
	output    string
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
			errMsg:  "blocked by loop guard",
		}
	}
	if a.planMode.Load() && !t.ReadOnly() {
		return toolOutcome{
			output:  fmt.Sprintf("blocked: %q is a writer tool and plan mode is read-only. Keep exploring with read-only tools, then write your plan as your reply — the user will be asked to approve it before any changes are made.", call.Name),
			blocked: true,
			errMsg:  "blocked: plan mode is read-only",
		}
	}
	if a.gate != nil {
		allow, reason, err := a.gate.Check(ctx, call.Name, json.RawMessage(call.Arguments), t.ReadOnly())
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
	// PreToolUse hooks run after permission is granted but before the call: a
	// gating hook (exit 2) refuses it, surfaced to the model like a gate denial.
	if a.hooks != nil {
		if block, msg := a.hooks.PreToolUse(ctx, call.Name, json.RawMessage(call.Arguments)); block {
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
	if a.onPreEdit != nil && !t.ReadOnly() {
		if pv, ok := t.(tool.Previewer); ok {
			if change, perr := pv.Preview(json.RawMessage(call.Arguments)); perr == nil {
				a.onPreEdit(change)
			}
		}
	}
	cctx := withCallContext(ctx, call.ID, a.sink, a.asker, a.planMode.Load())
	if a.evidence != nil {
		cctx = evidence.WithLedger(cctx, a.evidence)
		cctx = evidence.WithSessionMessages(cctx, a.session.Snapshot())
	}
	if len(a.projectChecks) > 0 {
		cctx = instruction.WithChecks(cctx, a.projectChecks)
	}
	if a.jobs != nil {
		cctx = jobs.WithManager(cctx, a.jobs)
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
	result, err := t.Execute(cctx, json.RawMessage(call.Arguments))
	if a.evidence != nil {
		if call.Name == "complete_step" {
			if err == nil {
				rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), true, t.ReadOnly())
				a.evidence.Record(rec)
				a.advanceCanonicalTodo(rec.Step)
			}
		} else {
			rec := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), err == nil, t.ReadOnly())
			a.evidence.Record(rec)
			if err == nil && call.Name == "todo_write" {
				a.setTodoState(rec.Todos)
			}
		}
	}
	// PostToolUse hooks observe the result (they can't block); fired whether the
	// call succeeded or errored, since the tool did run.
	if a.hooks != nil {
		a.hooks.PostToolUse(ctx, call.Name, json.RawMessage(call.Arguments), result)
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
	return toolOutcome{output: body, truncated: truncMsg != "", truncMsg: truncMsg}
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
