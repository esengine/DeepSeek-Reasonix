// Package control is the transport-agnostic session driver. A Controller owns
// the agent run loop and session lifecycle, takes commands (Send/Cancel/Approve/
// SetPlanMode/Compact/NewSession/…), and emits everything that happens —
// reasoning, tool calls, approvals, turn completion — as a typed event stream to
// a single event.Sink.
//
// The point is one orchestration layer behind every frontend: a terminal TUI, a
// desktop webview, or an HTTP/SSE server each drive the Controller identically
// (issue commands, render events) and none of them re-implement turn lifecycle,
// cancellation, or approval. The Controller depends on no frontend.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/ablation"
	"reasonix/internal/agent"
	"reasonix/internal/billing"
	"reasonix/internal/capability"
	"reasonix/internal/checkpoint"
	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/extension/uihub"
	"reasonix/internal/goaleval"
	"reasonix/internal/guardian"
	"reasonix/internal/hook"
	"reasonix/internal/i18n"
	"reasonix/internal/jobs"
	"reasonix/internal/memory"
	"reasonix/internal/nilutil"
	"reasonix/internal/permission"
	"reasonix/internal/planmode"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/recovery"
	"reasonix/internal/sandbox"
	"reasonix/internal/sessioninbox"
	"reasonix/internal/sessiontemp"
	"reasonix/internal/shellrun"
	"reasonix/internal/skill"
	"reasonix/internal/store"
	"reasonix/internal/taskmonitor"
	"reasonix/internal/tool"
	"reasonix/internal/workspacelease"
)

// ErrTurnRunning reports that a caller tried to start a second foreground turn
// while one is already active in the same Controller.
var ErrTurnRunning = errors.New("turn already running")

// ErrRuntimeDraining reports that a caller targeted a controller generation
// superseded by a successful rebuild.
var ErrRuntimeDraining = errors.New("runtime is draining after rebuild")

// errTurnRunningRotation and errRotationInProgress are returned by the
// session-rotation gate (beginRotation) when a rotation cannot proceed: a turn
// is in flight, or another rotation already holds the gate.
var (
	errTurnRunningRotation = errors.New("cannot start a new session while a turn is running")
	errRotationInProgress  = errors.New("cannot start a new session while another session change is in progress")
)

// errNoSessionPath is returned by snapshot when a session has content to persist
// but no resolved session path — a misconfiguration (e.g. an unresolvable data
// dir in a bot deployment) that previously dropped conversations silently
// (#4414). Callers log it and continue; it must never be swallowed quietly.
var errNoSessionPath = errors.New("session has content but no session path; conversation cannot be persisted")

// Controller drives one chat session. Construct with New; drive with the command
// methods; observe through the Sink passed in Options.
type Controller struct {
	controllerDeps

	guardianPath string // persisted guardian session file ("" when disabled)
	systemPrompt string
	commands     atomic.Pointer[[]command.Command]
	// hookContexts carries one-shot lifecycle hook context into the next real
	// user turn without changing the cache-stable system prompt.
	hookContexts []string
	// testCacheColdAfter overrides cacheColdAfter() in tests. Zero uses the
	// vendor-aware resolution from config.
	testCacheColdAfter   time.Duration
	startedOnce          bool      // guards the one-shot SessionStart hook on first turn
	closeOnce            sync.Once // makes close idempotent under racing teardown paths
	onSessionRecovered   func(SessionRecoveryInfo) error
	onSessionPathChanged func(path string)

	runtimeGeneration  uint64 // PublishGate gen; 0 disables
	lastResumeDecision extension.ResumeDecision
	// extensions is the frozen extension dispatcher for this controller
	// generation, or nil when no v2 runtime packages are installed (the
	// universal pre-dispatch fast path). It is installed before the controller
	// starts serving (Options.Extensions or SetExtensions) and never swapped
	// afterwards, so wiring points read it without locking.
	extensions *dispatch.Dispatcher
	// extensionUI is the host extension UI hub for this controller generation
	// (stage 8a), or nil when no v2 runtime packages started. Installed via
	// SetExtensionUI before serving and never swapped; readers take c.mu.
	extensionUI *uihub.Hub
	// providerResolver is the build's merged provider catalog (extension
	// sidecar providers over the config/broker base), or nil when no sidecar
	// declared providers. Immutable after New; ProviderCatalog reads it.
	providerResolver provider.Resolver

	// Capability routing (Delivery hybrid route + dual-model Planner proxy).
	// Not part of the provider-visible prefix; only seeds the turn-scoped ledger
	// and optional semantic router.
	pluginCfg       []config.PluginEntry
	capCachedTools  map[string][]plugin.CachedTool
	capCacheKeyOK   map[string]bool
	semanticRouter  *capability.SemanticRouter
	capabilityAudit *capability.Audit
	// capabilityProxy directs unready MCP candidates to use_capability in the
	// transient route block (Delivery and dual-model Planner).
	capabilityProxy bool
	// proxyToolsFn returns live tools observed through use_capability without
	// entering the provider-visible registry (Balanced dual-model Planner).
	proxyToolsFn   func() map[string][]plugin.CachedTool
	runtimeProfile capability.Profile

	// externalFolderRefs maps session-generated @ tokens to user-dropped
	// directories outside workspaceRoot. It is intentionally per-controller:
	// dragging a folder authorizes that folder for this chat session only, without
	// widening scoped @ resolution to arbitrary absolute paths.
	externalFolderRefsMu   sync.RWMutex
	externalFolderRefs     map[string]string
	externalFolderToolRefs externalFolderToolRefs

	// checkpoints owns the snapshot-based rewind bookkeeping (the per-session
	// store, the monotonic turn counter, and the conversation-rewind boundary map)
	// behind its own lock, off c.mu — so a boundary read for a rewind never
	// contends on the run-state lock. The Controller keeps the rewind/summarize
	// orchestration (truncating the session, restoring code, emitting events). See
	// checkpoint.go.
	checkpoints checkpointManager
	// mutationObserver is the host-side file mutation observer for v2 checkpoints.
	mutationObserver *checkpoint.MutationObserver
	// sessionRevision increments on successful rewind/undo and is used as a
	// prepare/commit freshness token.
	sessionRevision int64

	// mu guards the run state; every critical section under it is short and
	// non-blocking.
	mu sync.Mutex
	// parkedTurns holds turn bodies that arrived during the finishing window,
	// FIFO. finishGuardedTurn starts the oldest one as it closes the window
	// (see runGuarded/finishGuardedTurn); close() discards any remainder.
	parkedTurns []func(ctx context.Context) error
	// gate is turn admission: running, finishing, canceling, rotating, closed.
	// Rotation matters here because checking running once and swapping later
	// leaves a TOCTOU window — a turn can start during the intervening
	// Snapshot() and then have its live session replaced. See turn_gate.go.
	gate        turnGate
	autosaveWG  sync.WaitGroup
	planRuntime atomic.Pointer[planmode.Runtime]
	sessionPath string
	// sessionTemp owns the logical-session private temporary directory shared
	// by Bash calls. Retained for this Controller's lifetime; rotated on
	// /new, /clear, resume of another session, and branch switches.
	sessionTemp *sessiontemp.Manager
	// recoveryDepthCapNotices records session paths that already surfaced the
	// depth-cap recovery warning. Repeated saves on the same conflict copy are
	// diagnostic noise for the UI; keep logging/diagnostics, but emit the user
	// notice once per controller/session path.
	recoveryDepthCapNotices map[string]bool
	// snapshotMu serializes the whole save/recovery handoff for this controller.
	// Agent-level path locks protect individual files, but recovery also moves
	// controller-owned state (sessionPath, guardianPath, checkpoints, rewrite
	// baseline). Letting a second snapshot observe that migration halfway through
	// can turn one conflict into a recovery cascade. Session/path swaps
	// (new/clear/branch/switch/resume/SetSessionPath) hold it for the same
	// reason: a save that reads the old path but the new session would write one
	// transcript's messages into another's file, or manufacture a bogus conflict.
	// Not reentrant — never call snapshot (or anything that snapshots, such as
	// recoverInterruptedTurn or maybeColdResumePrune) while holding it.
	snapshotMu sync.Mutex
	// turn counts model turns this session, passed to hooks in their payload.
	turn int

	displayRecorder func(content, display string)

	// inbox is the durable session-level instruction queue. Disk I/O never
	// runs under c.mu; the store owns its own lock.
	inbox inboxState
}

type approvalReply struct {
	allow   bool
	session bool
	persist bool // true = write "always allow" rule to config
}

type pendingApproval struct {
	id           string
	tool         string
	subject      string
	reason       string
	rawInput     json.RawMessage
	fresh        bool
	requireHuman bool
	autoDrain    bool
	kind         string // tool | plan | recovery; empty = tool
	recovery     *event.RecoveryApproval
	reply        chan approvalReply
}

// pendingAsk is an in-flight ask question batch. questions is retained so the
// AskRequest can be re-emitted to a frontend that reconnected after the original
// event (see ReplayPendingPrompts).
type pendingAsk struct {
	questions []event.AskQuestion
	reply     chan []event.AskAnswer
	queued    bool // registered but not yet shown; replay must skip it
}

type plannerSessionResetter interface {
	ResetPlannerSession()
}

// RuntimeStatus is the frontend-facing snapshot of foreground turn state. It is
// intentionally more explicit than the legacy Running bool so UI code can
// distinguish a cancellable foreground turn from pending prompts and background
// jobs.
type RuntimeStatus struct {
	Running         bool
	PendingPrompt   bool
	BackgroundJobs  int
	CancelRequested bool
	Cancellable     bool
}

const (
	ToolApprovalAsk     = "ask"
	ToolApprovalAuto    = "auto"
	ToolApprovalDontAsk = "dontAsk"
	ToolApprovalYolo    = "yolo"
)

const (
	memoryRememberTool = "remember"
	memoryForgetTool   = "forget"
)

// RememberResult describes what happened when an approval rule was persisted.
type RememberResult struct {
	Rule      string
	Path      string
	Saved     bool
	CoveredBy string
	Err       error
}

type SessionRecoveryRequest struct {
	OriginalPath string
	Reason       string
	Mode         string
}

type SessionRecoveryInfo struct {
	OriginalPath string
	RecoveryPath string
	Existing     bool
	Reason       string
	Meta         agent.BranchMeta
}

type externalFolderToolRefs interface {
	RegisterReadRoot(token, root string)
}

// Options carries the already-built pieces setup assembles. Lifecycle metadata
// lets the controller mint and rotate session files; Host/Commands are surfaced
// to frontends that resolve MCP prompts and slash commands.
type Options struct {
	Runner   agent.Runner
	Executor *agent.Agent
	Guardian *guardian.Session
	// RecoveryReviewer is the optional independent recovery reviewer (nil =
	// rule-only path with fail-closed human confirmation for ambiguous cases).
	RecoveryReviewer recovery.Reviewer
	// RecoveryHeadless blocks mutations that need confirmation instead of
	// waiting forever when no human decision channel exists.
	RecoveryHeadless bool
	// TaskBudget is the configured spend gate; unset leaves a turn unbounded.
	TaskBudget agent.TaskBudget
	// GoalTokenBudget bounds an unattended Goal loop by cumulative tokens.
	GoalTokenBudget int
	// GoalEvaluator is the optional bounded Goal completion evaluator consulted
	// when the working model submits no update_goal report. nil fails closed:
	// the goal pauses instead of defaulting to continue.
	GoalEvaluator goaleval.Evaluator
	Sink          event.Sink
	Policy        permission.Policy
	// SubagentGate is the shared, mutable gate every headless-only sub-agent
	// surface (task, writer-capable skill sub-agents, planner) reads from. Nil
	// disables gating for those surfaces same as before this field existed.
	// SetToolApprovalMode and ApplyHeadlessApprovalMode call Update on it so a
	// runtime approval-mode switch reaches sub-agents, not just the parent
	// executor's own gate.
	SubagentGate  *SharedHeadlessGate
	Label         string
	ModelRef      string
	SystemPrompt  string
	SessionDir    string
	SessionPath   string
	Host          *plugin.Host
	Commands      []command.Command
	Skills        []skill.Skill
	AllSkills     []skill.Skill
	SkillStore    *skill.Store
	AllSkillStore *skill.Store
	// DisableImplicitSkillInvocation controls model-facing discovery only;
	// explicit /skill commands and management remain host-side capabilities.
	DisableImplicitSkillInvocation bool
	// SkillRunner executes a runAs=subagent skill in an isolated child loop.
	// ReadOnlySkillRunner is reserved for explicitly read-only entry points;
	// Plan itself is a workflow instruction and uses SkillRunner with the shared
	// Permissions/Sandbox gate. SkillProfile supplies model/effort display
	// metadata for the synthetic top-level run_skill event.
	SkillRunner         skill.SubagentRunner
	ReadOnlySkillRunner skill.SubagentRunner
	SkillProfile        skill.ProfileResolver
	Hooks               *hook.Runner
	Memory              *memory.Set
	Cleanup             func()
	// Balance reads the active provider's optional wallet endpoint. Nil, or a
	// cache built on an empty URL, means the provider declares none. Hosts that
	// build several runtimes hand every pane the same cache.
	Balance *billing.Cache
	// Jobs is the session-scoped background-job manager (nil disables background jobs).
	Jobs *jobs.Manager
	// TaskStore remains a FileStore-compatible authority. Desktop injects one
	// observed instance so recorder and task-control APIs share post-commit
	// projection hints; nil preserves the ordinary FileStore.
	TaskStore taskmonitor.WriteStore
	// WorkspaceLease is the Delivery writer owner shared with the executor.
	WorkspaceLease *workspacelease.Owner
	// Registry is the executor's live tool set, and PluginCtx the session-scoped
	// context; both are needed for hot-adding MCP servers via AddMCPServer.
	Registry  *tool.Registry
	PluginCtx context.Context
	// MCPDefaultCallTimeout is the global MCP call cap used by hot-connected
	// servers when they do not declare a server- or tool-specific override.
	MCPDefaultCallTimeout time.Duration
	// MCPConfigureSpec injects host-local launch and isolation policy into every
	// hot-connected server without persisting that state in project config.
	MCPConfigureSpec func(*plugin.Spec)
	// CapabilityRuntime is the controller-local authoritative MCP inventory used
	// by stable use_capability frontends. It shares Host processes with sibling
	// tabs but never shares their enabled/disabled state.
	CapabilityRuntime *agent.MCPCapabilityRuntime
	RuntimeGeneration uint64 // PublishGate generation for admission
	// RuntimeOwner isolates publish/drain gates and receipts to one
	// controller/session rebuild lineage. Nil preserves compatibility behavior.
	RuntimeOwner *extension.RuntimeOwner
	// WorkspaceRoot is the project root checkpoint restores are confined to ("" =
	// no confinement). Frontends pass the cwd they launched the session in.
	WorkspaceRoot          string
	ExternalFolderToolRefs externalFolderToolRefs
	ShowTurnReceipt        bool // attach the end-of-turn verification report; see display_prefs.go
	// ResponseLanguage controls final-answer language preference. Empty/auto
	// means no transient injection because the stable language policy follows the
	// current user turn.
	ResponseLanguage string
	// ReasoningLanguage controls visible reasoning language preference. Empty/auto
	// means no transient injection because the stable language policy already
	// follows the conversation language.
	ReasoningLanguage string
	// DisableColdResumePrune suppresses the cold-resume cache-state notice.
	// Resume never rewrites history regardless of this flag.
	DisableColdResumePrune bool
	// Shell is the interpreter user-invoked "!" commands run under, so /shell
	// matches the agent's configured [tools.shell] choice. Zero value = auto.
	Shell sandbox.Shell
	// OnRemember, when set, is invoked with a new allow rule the user chose to
	// persist to disk (e.g. "Bash(go test:*)"). The callback is wired into the
	// permission Gate on EnableInteractiveApproval.
	OnRemember func(rule string) RememberResult
	// SessionRecoveryMeta lets a frontend attach scope/topic/profile metadata to
	// an automatic recovery branch before it is written.
	SessionRecoveryMeta func(SessionRecoveryRequest) agent.BranchMeta
	// OnSessionRecovered is called after a stale runtime's transcript has been
	// saved as a recovery branch, before the controller commits to that branch.
	OnSessionRecovered func(SessionRecoveryInfo) error
	// ApprovalTimeout bounds how long a tool-approval or ask prompt blocks waiting
	// for a user decision. Zero (default) waits forever — right for an interactive
	// terminal. Bot/headless frontends set a positive value so an unanswered
	// prompt can't wedge the session indefinitely (#4626, #4402).
	ApprovalTimeout time.Duration
	// RuntimeProfile selects capability routing/filtering behavior. Empty keeps
	// the backward-compatible Balanced profile.
	RuntimeProfile capability.Profile
	// Extensions is the frozen extension dispatcher for this controller
	// generation (Extension Protocol v2, stage 6b1). Nil means no v2 runtime
	// packages are installed: every extension wiring point takes an untouched
	// fast path. Boot installs it through SetExtensions because sidecars (and
	// therefore the dispatcher) only exist after snapshot assembly, which runs
	// after New.
	Extensions *dispatch.Dispatcher
	// ProviderResolver is the build's merged provider catalog — extension
	// sidecar providers folded over the config/broker base (stage 7). Nil when
	// no v2 runtime sidecar declared providers; ProviderCatalog then returns
	// nil and frontends enumerate providers from config alone, as before.
	ProviderResolver provider.Resolver
	// Ablation switches subsystems off for a benchmark arm. The zero value runs
	// everything.
	Ablation ablation.Set
	// SessionTemp is the logical-session private temporary directory manager
	// shared by sandboxed Bash calls. Nil creates a fresh Manager owned by this
	// Controller. Hot rebuilds pass the previous Controller's Manager so the
	// temporary directory survives model/settings swaps.
	SessionTemp *sessiontemp.Manager
}

// New builds a Controller. A nil Sink becomes event.Discard; unless the caller
// already provided a goalUsageTee (NewGoalUsageTee), the sink is wrapped in one
// so billable usage can be accounted to Goal budgets.
func New(opts Options) *Controller {
	sink := opts.Sink
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	usageTee, ok := sink.(*goalUsageTee)
	if !ok {
		usageTee = NewGoalUsageTee(sink).(*goalUsageTee)
		sink = usageTee
	}
	pluginCtx := opts.PluginCtx
	if pluginCtx == nil {
		pluginCtx = context.Background()
	}
	runtimeOwner := runtimeOwnerOrDefault(opts.RuntimeOwner)
	pluginCtx = extension.ContextWithRuntimeOwner(pluginCtx, runtimeOwner)
	runtimeProfile := opts.RuntimeProfile
	if runtimeProfile == "" {
		runtimeProfile = capability.ProfileBalanced
	}
	if opts.Hooks != nil {
		opts.Hooks.SetSessionID(agent.BranchID(opts.SessionPath))
	}
	c := &Controller{
		controllerDeps:         newControllerDeps(opts, sink, usageTee, runtimeOwner, pluginCtx),
		guardianPath:           guardian.PathFor(opts.SessionPath),
		systemPrompt:           opts.SystemPrompt,
		sessionPath:            opts.SessionPath,
		commands:               atomic.Pointer[[]command.Command]{},
		onSessionRecovered:     opts.OnSessionRecovered,
		runtimeProfile:         runtimeProfile,
		externalFolderToolRefs: opts.ExternalFolderToolRefs,
		providerResolver:       opts.ProviderResolver,
		runtimeGeneration:      opts.RuntimeGeneration,
	}
	// Session-private temporary directory: reuse a shared Manager on hot
	// rebuild, otherwise create one. Retain so ReleaseResources/Close drop the
	// owner reference without racing a replacement Controller.
	if opts.SessionTemp != nil {
		c.sessionTemp = opts.SessionTemp
	} else {
		c.sessionTemp = sessiontemp.New()
	}
	c.sessionTemp.Retain()

	c.publishPerProjectContext(opts)
	if opts.Extensions != nil {
		c.extensions = opts.Extensions
		c.sink = newFrontendEventSink(c.sink, opts.Extensions)
		if c.executor != nil {
			c.executor.SetExtensions(opts.Extensions)
		}
	}
	// Checkpoints: bind a store to the session and route writer pre-edits into it.
	c.rebindCheckpoints(opts.SessionPath)
	c.setActiveJobSession(opts.SessionPath)
	c.rebindInbox()
	// Observe Steer / unapplied-steer for durable inbox state transitions.
	// Must wrap both the controller sink and the executor sink: agent.Steer
	// emits on the executor path, TurnDone on the controller path.
	c.sink = &inboxEventSink{AuditForwarder: event.AuditForwarder{Inner: c.sink}, c: c}
	if c.executor != nil {
		c.executor.SetSink(c.sink)
		c.executor.SetHostContext(c)
	}
	cmdsInit := opts.Commands
	c.commands.Store(&cmdsInit)
	if c.executor != nil {
		c.wireMutationObserver()
		c.executor.SetMemoryQueue(c)
	}
	// Auto Guard is built into Auto. Ask and YOLO bypass it through the mode
	// provider, so no separate enablement state is needed.
	c.initRecoveryGate(opts.RecoveryReviewer, opts.RecoveryHeadless)

	c.observeJobs(opts.TaskStore)
	return c
}

// SetDisplayRecorder installs an optional hook used by frontends that persist a
// shorter user-facing transcript than the fully composed model prompt.
func (c *Controller) SetDisplayRecorder(fn func(content, display string)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.displayRecorder = fn
}

// SetExtensions installs the extension dispatcher after construction. Boot
// uses it because sidecars — and therefore the dispatcher — only exist after
// snapshot assembly, which runs after New. First non-nil install wins for the
// cold-start path; use ReplaceExtensions for generation-safe rebuild swaps.
// Nil is a no-op. The executor agent receives the same dispatcher (stage 6b2).
func (c *Controller) SetExtensions(d *dispatch.Dispatcher) {
	if d == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.extensions != nil {
		return
	}
	c.installExtensionsLocked(d)
}

// ReplaceExtensions atomically swaps the dispatcher for a reused controller
// after a narrow rebuild. Updates sink strategy owner and executor together.
func (c *Controller) ReplaceExtensions(d *dispatch.Dispatcher) {
	if c == nil || d == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.installExtensionsLocked(d)
}

func (c *Controller) installExtensionsLocked(d *dispatch.Dispatcher) {
	c.extensions = d
	// Keep the inbox observer as the outermost sink so Steer/unapplied events
	// always update durable state, while still installing/updating the
	// frontendEventSink wrapper underneath for extension rulings.
	switch sink := c.sink.(type) {
	case *inboxEventSink:
		if existing, ok := sink.Inner.(*frontendEventSink); ok {
			existing.setDispatcher(d)
		} else {
			sink.Inner = newFrontendEventSink(sink.Inner, d)
		}
	case *frontendEventSink:
		sink.setDispatcher(d)
		// Ensure inbox observer stays outer.
		c.sink = &inboxEventSink{AuditForwarder: event.AuditForwarder{Inner: sink}, c: c}
	default:
		c.sink = &inboxEventSink{AuditForwarder: event.AuditForwarder{Inner: newFrontendEventSink(c.sink, d)}, c: c}
	}
	if c.executor != nil {
		c.executor.SetExtensions(d)
		c.executor.SetSink(c.sink)
	}
}

// SetProviderResolver replaces the session's merged provider catalog (narrow
// rebuild after sidecar Manager roll). Nil clears extension-hosted providers.
func (c *Controller) SetProviderResolver(r provider.Resolver) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.providerResolver = r
	c.mu.Unlock()
}

// ApplyExtensionSystemPrompt swaps the executor to a fresh session carrying
// the extension strategy's final system prompt and makes it the controller's
// rotation prompt, so /new and /clear keep the strategy-composed prompt too.
// Boot calls it when a system_prompt.build replacement changed the prompt
// after the controller (and its session) was built with the host-composed
// one. It must run before any turn or history resume: the fresh session holds
// only the system message, so a later resume cleanly layers history on top.
func (c *Controller) ApplyExtensionSystemPrompt(prompt string) {
	if c == nil || c.executor == nil {
		return
	}
	c.mu.Lock()
	c.systemPrompt = prompt
	c.mu.Unlock()
	c.executor.SetSession(agent.NewSession(prompt))
}

func (c *Controller) recordDisplay(content, display string) {
	if strings.TrimSpace(display) == "" || content == display {
		return
	}
	c.mu.Lock()
	record := c.displayRecorder
	c.mu.Unlock()
	if record != nil {
		record(content, display)
	}
}

// ToolContractEntries returns a stable snapshot of the executor's live tool
// contract: provider-visible names, descriptions, canonical schemas, and
// read-only flags. It is intended for diagnostics and regression tests.
func (c *Controller) ToolContractEntries() []tool.ContractEntry {
	if c == nil {
		return nil
	}
	reg := c.mcp.registry()
	if reg == nil {
		return nil
	}
	return reg.ContractEntries()
}

// AllToolContractEntries returns every registered tool, including those hidden
// from the provider-visible schema and only reachable via use_capability.
func (c *Controller) AllToolContractEntries() []tool.ContractEntry {
	if c == nil {
		return nil
	}
	reg := c.mcp.registry()
	if reg == nil {
		return nil
	}
	return reg.AllContractEntries()
}

// ProviderCatalog returns the session's merged provider catalog: the config
// (or broker) base plus every provider a live extension sidecar declared,
// keyed by ref — extension refs carry their plugin/<plugin>/<provider>/<model>
// namespace. Nil when no sidecar declared providers, so frontends can tell
// "enumerate config only" apart from "the extension catalog is empty".
func (c *Controller) ProviderCatalog() []provider.Descriptor {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	r := c.providerResolver
	c.mu.Unlock()
	if r == nil {
		return nil
	}
	return r.Catalog()
}

func (c *Controller) recordDisplayForNewUser(startMessages int, display string) {
	if strings.TrimSpace(display) == "" {
		return
	}
	msgs := c.History()
	if startMessages > len(msgs) {
		startMessages = len(msgs)
	}
	for _, m := range msgs[startMessages:] {
		if m.Role == provider.RoleUser {
			c.recordDisplay(m.Content, display)
			return
		}
	}
}

func (c *Controller) markEditedForNewUser(startMessages int, original string) {
	if strings.TrimSpace(original) == "" || c.executor == nil {
		return
	}
	s := c.executor.Session()
	msgs := s.Snapshot()
	if startMessages > len(msgs) {
		startMessages = len(msgs)
	}
	for i := startMessages; i < len(msgs); i++ {
		if msgs[i].Role != provider.RoleUser {
			continue
		}
		if agent.UserMessageText(msgs[i]) == original {
			return
		}
		msgs[i].Edited = true
		msgs[i].Original = original
		// A periodic autosave may already contain this user message without its
		// local edit metadata. Classify the mutation atomically so the turn-end
		// save performs an owned rewrite instead of forking a bogus
		// same-revision recovery branch. Edited/Original are local-only display
		// metadata (provider requests ignore them), so this must not report a
		// cache-prefix change — ReplaceLocalMetadata, not Rewrite.
		s.ReplaceLocalMetadata(msgs)
		return
	}
}

// ckptDir derives a session's checkpoint directory from its file path
// (…/<id>.jsonl → …/<id>.ckpt). Empty path → empty (in-memory checkpoints).
func ckptDir(sessionPath string) string {
	return store.SessionCheckpointDir(sessionPath)
}

// rebindCheckpoints points the store at the (possibly new) session, loading any
// checkpoints already on disk, and resets the turn boundaries. Called on
// construction and whenever the session path changes (NewSession/Resume/SetSessionPath).
// Also re-wires the mutation observer so capture targets the new store.
func (c *Controller) rebindCheckpoints(sessionPath string) {
	c.goals.setStatePath(goalStatePath(sessionPath))
	c.checkpoints.rebind(ckptDir(sessionPath), c.workspaceRoot)
	if c.executor != nil {
		c.wireMutationObserver()
	}
}

// commands (frontend → controller)

func turnOutcome(err error) string {
	var readinessErr *agent.FinalReadinessError
	if errors.As(err, &readinessErr) {
		return event.TurnOutcomeFinalReadiness
	}
	return ""
}

// Send starts a turn with an uncomposed message. The controller applies
// plan-mode, memory, and background-job framing inside the async turn path.
func (c *Controller) Send(input string) {
	c.SendWithRaw(input, input)
}

// SendWithRaw starts a turn with separate model input and raw prompt text.
func (c *Controller) SendWithRaw(input, raw string) {
	c.runGuarded(func(ctx context.Context) error {
		return c.runTurnLoop(ctx, orchestratedTurn{input: input, raw: raw})
	})
}

// planApprovalTool is the Tool name on the ApprovalRequest the controller emits
// to gate a proposed plan. Frontends key their plan-approval UI on it (the
// desktop renders a plan card; the chat TUI a plan banner).
const planApprovalTool = "exit_plan_mode"

// PlanDecisionAction preserves the three user-owned meanings of the Plan card.
// Revise and exit both deny execution at the approval gate, but they are not the
// same product decision and must remain distinguishable in durable receipts.
type PlanDecisionAction string

const (
	PlanDecisionStartExecution PlanDecisionAction = "start_execution"
	PlanDecisionRevisePlan     PlanDecisionAction = "revise_plan"
	PlanDecisionExitPlan       PlanDecisionAction = "exit_plan"
)

// SandboxEscapeApprovalTool is the internal Tool name used for one-shot approval
// to rerun a shell command without the OS sandbox after the sandbox failed.
const SandboxEscapeApprovalTool = "sandbox_escape"

// ManagedConfigWriteApprovalTool is the internal Tool name used for per-write
// approval when a file tool targets a Reasonix-managed config file outside the
// workspace write roots. It is a fresh human decision: config files control
// providers, sandbox rules, permissions, and MCP servers for future sessions,
// so YOLO/auto approval must never answer it.
const ManagedConfigWriteApprovalTool = "config_write"

// planApprovedMessage is the follow-up turn sent once the user approves a plan —
// the in-context nudge to execute and keep the (already-seeded) task list honest.
const planApprovedMessage = "Plan approved — plan mode is off. Implement the plan now. The ordinary writer fallback is approved for this execution turn; explicit ask/deny rules and forced fresh reviews still apply. Use this serial workflow: 1) mark the first sub-step in_progress with todo_write (this establishes the task list); 2) execute the sub-step; 3) call complete_step with evidence — the host then marks that sub-step completed and moves the next one to in_progress for you. Repeat 2–3 for each remaining sub-step. You don’t need another todo_write to mark steps completed; each complete_step advances the list. Sign off one sub-step at a time — never batch multiple completions."

// runTurnLoop runs one model turn under the plan-approval gate, then keeps
// pursuing an active Goal with it — with no goal the loop is what a single turn
// looks like, plus whatever that turn still owes. In Plan the model writes its
// plan as an ordinary answer; approving it exits plan mode and continues into
// execution, rejecting it leaves the next turn free to revise.
func (c *Controller) runTurnLoop(ctx context.Context, turn orchestratedTurn) error {
	return newTurnOrchestrator(c).runTurnLoop(ctx, turn)
}

// runOneTurn runs a single model turn with no Goal loop behind it.
func (c *Controller) runOneTurn(ctx context.Context, turn orchestratedTurn) error {
	return newTurnOrchestrator(c).runOrchestratedTurn(ctx, turn)
}

// RunTurn executes one foreground turn synchronously through the same lifecycle
// used by interactive frontends: transient memory/background-job
// composition, checkpoints, hooks, and plan approval. It is for transports that
// need a blocking request/response boundary, such as ACP session/prompt.
func (c *Controller) RunTurn(ctx context.Context, input string) error {
	return c.runSynchronousTurn(ctx, nil, func(runCtx context.Context) error {
		return c.runTurnLoop(runCtx, orchestratedTurn{input: input, raw: input})
	})
}

// withTurnFormat binds a structured-output format to the turn context
// (empty is a no-op). Extracted from the runTurnLoop closure so tests can
// assert the format actually reaches the agent request path.
func (c *Controller) withTurnFormat(ctx context.Context, format string) context.Context {
	if format == "" {
		return ctx
	}
	return agent.WithResponseFormat(ctx, format)
}

func (c *Controller) runSubagentSkillSlash(sk skill.Skill, task, raw, display string) {
	sk = c.skills.prepare(sk)
	c.runGuarded(func(ctx context.Context) error {
		planMode := c.PlanMode()
		runner := c.skillRunner
		if runner == nil {
			return fmt.Errorf("subagent skill runner is unavailable for /%s", sk.Name)
		}
		return newTurnOrchestrator(c).runSubagentSkillGoalLoop(ctx, sk, task, raw, display, runner, planMode)
	})
}

func (c *Controller) stopGoal(status string) {
	path, data, ok := c.goals.stop(status, c.goalTodos())
	c.persistGoalState(path, data, ok)
}

// lastAssistantText returns the content of the most recent assistant message with
// non-empty text — the model's final answer for the turn (its plan, in plan mode).
func lastAssistantText(msgs []provider.Message) string {
	for _, msg := range slices.Backward(msgs) {
		if msg.Role == provider.RoleAssistant && strings.TrimSpace(msg.Content) != "" {
			return msg.Content
		}
	}
	return ""
}

// IsNonTurnInput reports input that has no turn to start: a management verb, a
// memory note, a shell shortcut. A frontend that judges a submission by whether
// a turn began has to ask this first — /compact does its work and emits a
// notice without ever running one.
func IsNonTurnInput(input string) bool { return isNonTurnHTTPInput(input) }

// isNonTurnHTTPInput reports inputs that never reach the agent turn loop, so a
// structured-output request attached to them would otherwise leak into the
// next real turn (the format slot is consumed only by runTurnLoopWithRawDisplay).
func isNonTurnHTTPInput(input string) bool {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return true
	}
	// Memory quick-add / remember shortcuts and goal commands bypass turns.
	if _, ok := MemoryQuickAddNote(trimmed); ok {
		return true
	}
	if _, ok := RememberCommandNote(trimmed); ok {
		return true
	}
	// "!" shell commands are rejected by submitHTTP before the turn loop
	// (403 over HTTP); a format attached to them would never be consumed.
	if strings.HasPrefix(trimmed, "!") {
		return true
	}
	// Slash commands are management verbs (/compact /new /clear /model ...)
	// or notices, not completion turns.
	if strings.HasPrefix(trimmed, "/") {
		return true
	}
	return false
}

type preparedInvocationTurn struct {
	composed  string
	subagents []skill.Skill
}

// compactAndReport folds the context and says what happened. A fold the kernel
// declined is an answer about this transcript — nothing left worth folding —
// and reporting it as a failure sent people looking for a broken kernel.
func (c *Controller) compactAndReport(focus string) {
	err := c.Compact(context.Background(), focus)
	switch {
	case err == nil:
		c.notice("compacted")
		if err := c.SnapshotRewrite(); err != nil {
			slog.Warn("controller: snapshot after compact", "err", err)
		}
	case agent.IsCompactionDeclined(err):
		c.notice("nothing to compact — " + agent.CompactionDeclineReason(err))
	default:
		c.notice("compaction failed: " + err.Error())
	}
}

func (c *Controller) rememberProjectNote(note string) {
	if note == "" {
		c.notice("nothing to remember")
		return
	}
	if path, err := c.QuickAdd(memory.ScopeProject, note); err != nil {
		c.notice("memory: " + err.Error())
	} else {
		c.notice("remembered → " + path)
	}
}

// prometheusPrompt is the strategic planner system prompt.
const prometheusPrompt = "You are Prometheus, a strategic planner. Interview the user one question at a time. Cover: scope, modules, files, constraints, tests. When ready, output a numbered plan with each step tagged by module. End by calling update_goal with status complete. Do not implement.\n\nFor independent research directions, use parallel_tasks before planning."

// applyPrometheus starts an interactive planning interview, inspired by OMO's
// Prometheus agent. It enters goal mode with a structured interview prompt.
func (c *Controller) applyPrometheus(input, display string) {
	args := strings.TrimSpace(strings.TrimPrefix(input, "/prometheus"))
	if args == "" || args == "--strict" {
		c.notice("usage: /prometheus <your task description>")
		return
	}
	strict := false
	if strings.HasPrefix(args, "--strict ") {
		strict = true
		args = strings.TrimPrefix(args, "--strict ")
	}
	prompt := prometheusPrompt + "\n\n## User request\n\n" + args + "\n\nBegin the interview by asking your first clarifying question."
	c.SetPlanMode(false)
	c.SetGoal("plan: " + ShortGoalForNotice(args))
	c.GoalStrict(strict)
	c.notice("prometheus: starting planning interview")
	if c.runner != nil {
		c.runGuarded(func(ctx context.Context) error {
			return c.runTurnLoop(ctx, orchestratedTurn{input: prompt, raw: prompt, display: display})
		})
	}
}

// shellTimeout is the maximum time a user-invoked "!command" may run. Matches
// the bash tool's timeout so behaviour is consistent across invocation paths.
const shellTimeout = 120 * time.Second

// shellWaitDelay bounds how long cmd.Run() waits after context cancellation for
// the child's pipes to drain, matching the bash tool's WaitDelay.
const shellWaitDelay = 5 * time.Second

func shellCommandPreview(command string) string {
	command = strings.TrimSpace(strings.ReplaceAll(command, "\n", " "))
	const max = 48
	r := []rune(command)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return command
}

// RunShell executes a shell command directly (bypassing the model) and streams
// the output as ToolDispatch/ToolProgress/ToolResult events. It uses the same
// bash-tool infrastructure (shell resolution, timeout) and shares the runGuarded
// lock with model turns — only one can run at a time. User-invoked "!" commands
// run without the OS sandbox (the user typed the command explicitly).
func (c *Controller) RunShell(command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		c.notice(i18n.M.ShellExecEmpty)
		return
	}
	c.runGuarded(func(ctx context.Context) error {
		sh := c.shell
		if sh.Path == "" {
			sh = sandbox.ResolveShell("", "", nil)
		}
		argv, _ := sandbox.Command(sandbox.Spec{}, sh, command) // false = unsandboxed (user invoked)

		preview := []rune(command)
		if len(preview) > 32 {
			preview = preview[:32]
		}
		id := "shell-" + string(preview)
		diagnosticPreview := shellCommandPreview(command)
		desc := shellrun.DescriptorFromShell(sh)

		c.sink.Emit(event.Event{
			Kind: event.ToolDispatch,
			Tool: event.Tool{
				ID:   id,
				Name: "bash",
				Args: fmt.Sprintf(`{"command":%q}`, command),
				Execution: &event.ShellExecution{
					Kind: desc.Kind, Shell: desc.Shell, ShellVersion: desc.ShellVersion,
					Platform: desc.Platform, SupportsAndAnd: desc.SupportsAndAnd,
					State: tool.ShellStateRunning,
				},
			},
		})

		start := time.Now()
		res := shellrun.RunForeground(ctx, shellrun.Request{
			Argv:           argv,
			Dir:            c.workspaceRoot,
			Timeout:        shellTimeout,
			WaitDelay:      shellWaitDelay,
			CommandPreview: diagnosticPreview,
			ShellKind:      sh.Kind.String(),
			ShellPath:      sh.Path,
			Source:         "user_shell",
			Track:          true,
			Progress: func(chunk string) {
				c.sink.Emit(event.Event{
					Kind: event.ToolProgress,
					Tool: event.Tool{ID: id, Output: chunk},
				})
			},
		})
		durationMs := time.Since(start).Milliseconds()
		ex := &event.ShellExecution{
			Kind: desc.Kind, Shell: desc.Shell, ShellVersion: desc.ShellVersion,
			Platform: desc.Platform, SupportsAndAnd: desc.SupportsAndAnd,
			State: res.State, FailurePhase: res.FailurePhase,
			OutputTail: res.OutputTail, DurationMs: durationMs,
			MutationRisk: tool.ShellMutationNone,
			Verification: tool.ShellVerificationNotVerification,
		}
		if res.ExitCode != nil {
			code := *res.ExitCode
			ex.ExitCode = &code
		}
		switch res.State {
		case tool.ShellStateCompleted:
			ex.MutationRisk = tool.ShellMutationNone
		case tool.ShellStateNotRun:
			ex.MutationRisk = tool.ShellMutationNotStarted
		case tool.ShellStateFailed:
			if res.FailurePhase == tool.ShellPhaseLaunch {
				ex.MutationRisk = tool.ShellMutationNotStarted
			} else {
				ex.MutationRisk = tool.ShellMutationMayBePartial
			}
		case tool.ShellStateTimedOut, tool.ShellStateCancelled:
			ex.MutationRisk = tool.ShellMutationMayBePartial
		}

		errText := ""
		switch res.State {
		case tool.ShellStateCancelled:
			errText = i18n.M.TurnCancelled
		case tool.ShellStateTimedOut:
			errText = fmt.Sprintf(i18n.M.ShellExecTimeoutFmt, shellTimeout)
		case tool.ShellStateFailed, tool.ShellStateNotRun:
			if res.Err != nil {
				errText = fmt.Sprintf(i18n.M.ShellExecFailedFmt, res.Err)
			}
		}
		c.sink.Emit(event.Event{
			Kind: event.ToolResult,
			Tool: event.Tool{
				ID: id, Name: "bash", Output: res.Combined, Err: errText,
				DurationMs: durationMs, Execution: ex,
			},
		})
		return nil
	})
}

// refTurn is one @-ref-resolving turn: what to send, which line carries the
// refs, what the transcript shows, and how the refs resolve. The zero value
// resolves input against the workspace with no structured-output format.
type refTurn struct {
	input string
	// refLine carries the refs when they are not in input, so a compiler
	// diagnostic like "/path/File.kt:12: error" attaches @/path/File.kt without
	// rewriting the error text the user sees. Empty reads them from input.
	refLine string
	display string
	// original is a resubmitted turn's pre-edit text; non-empty routes it
	// through the edited-goal loop.
	original string
	format   string
	// resolve reads the refs out of refLine. nil resolves against the whole
	// workspace; a frontend that must not widen a ref to an arbitrary absolute
	// path passes ResolveScopedRefs.
	resolve func(context.Context, string) (string, []string)
}

// runRefTurn resolves the turn's @references into a context block and starts a
// turn with it prepended (or the raw line when nothing resolved), under turn
// admission.
func (c *Controller) runRefTurn(r refTurn) {
	c.runGuarded(func(ctx context.Context) error { return c.runRefTurnSync(ctx, r) })
}

// runRefTurnSync is runRefTurn on the caller's goroutine, for a caller that
// already holds turn admission.
func (c *Controller) runRefTurnSync(ctx context.Context, r refTurn) error {
	ctx = c.withTurnFormat(ctx, r.format)
	resolve := r.resolve
	if resolve == nil {
		resolve = c.ResolveRefs
	}
	refLine := r.refLine
	if refLine == "" {
		refLine = r.input
	}
	block, errs := resolve(ctx, refLine)
	for _, e := range errs {
		c.notice(e)
	}
	sent := r.input
	if block != "" {
		sent = "Referenced context:\n\n" + block + "\n\n" + r.input
	}
	return c.runTurnLoop(ctx, orchestratedTurn{
		input: sent, raw: r.input, imageRefs: refLine, display: r.display, editedOriginal: r.original,
	})
}

// notice emits an informational Notice event.
func (c *Controller) notice(text string) {
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: text})
}

func (c *Controller) noticeDetail(text, detail string) {
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: text, Detail: detail})
}

// Run executes a turn synchronously, returning the agent's error. Used by the
// headless `reasonix run` path, where the Sink renders to stdout and the caller
// just needs the exit status — no TurnDone event, no cancel bookkeeping.
func (c *Controller) runReady(ctx context.Context, input string) (err error) {
	ctx = extension.ContextWithRuntimeOwner(ctx, c.RuntimeOwner())
	if c.RuntimePhase() == RuntimePhaseDraining {
		c.emitDrainingNotice()
		return ErrRuntimeDraining
	}
	defer event.RecordTurnCompletion(c.sink)
	c.maybeSessionStart(ctx)
	parentSession := c.parentSessionID()
	ctx = agent.WithParentSession(ctx, parentSession)
	ctx = jobs.WithSession(ctx, parentSession)
	rawInput := input
	ctx, turnImgs := c.withTurnImages(ctx, rawInput)
	ctx = agent.WithRawUserInput(ctx, rawInput)
	input = c.imageRoutingPrefix(turnImgs) + c.Compose(input)
	// input.receive: same interception seam as the orchestrated turn — the
	// composed headless input crosses the extension chain before it enters
	// the session.
	input, blocked, interceptErr := c.interceptInputReceive(ctx, input)
	if interceptErr != nil {
		return interceptErr
	}
	if blocked {
		return nil
	}
	startMessages := c.messageCount()
	var marker agent.InFlightTurnMeta
	defer func() { c.finishInFlightTurn(startMessages, marker) }()
	c.beginCheckpoint(ctx, input)
	if c.hooks.Enabled() {
		c.mu.Lock()
		c.turn++
		turn := c.turn
		c.mu.Unlock()
		if block, _ := c.hooks.PromptSubmit(ctx, input, turn); block {
			return nil
		}
		defer func() { c.hooks.StopResult(context.Background(), lastAssistantText(c.History()), turn, err) }()
	}
	ctx, marker = c.beginTurn(ctx, startMessages, true)
	ctx = c.withPlannerTurnMetadata(ctx, rawInput, false)
	err = c.runner.Run(ctx, c.withCapabilityRoute(ctx, input, rawInput))
	return err
}

// RunSubagentProfile executes one named runAs=subagent skill synchronously and
// returns only its final answer. It is the headless CLI counterpart to explicit
// slash invocation: the child keeps an isolated session, while the caller owns
// stdout rendering and exit status. readOnly selects the preview-safe runner
// used by `reasonix subagent try`.
func (c *Controller) RunSubagentProfile(ctx context.Context, name, task string, readOnly bool) (string, error) {
	name = strings.TrimSpace(name)
	task = strings.TrimSpace(task)
	if name == "" {
		return "", fmt.Errorf("subagent name is required")
	}
	if task == "" {
		return "", fmt.Errorf("subagent task is required")
	}
	sk, ok := c.skills.bySlashName(name)
	if !ok {
		return "", fmt.Errorf("unknown or disabled subagent profile %q", name)
	}
	if sk.RunAs != skill.RunSubagent {
		return "", fmt.Errorf("skill %q is not runAs=subagent", name)
	}
	sk = c.skills.prepare(sk)
	runner := c.skillRunner
	if readOnly {
		runner = c.readOnlySkillRunner
	}
	if runner == nil {
		return "", fmt.Errorf("subagent skill runner is unavailable for %q", name)
	}

	c.maybeSessionStart(ctx)
	parentSession := c.parentSessionID()
	ctx = agent.WithParentSession(ctx, parentSession)
	ctx = jobs.WithSession(ctx, parentSession)
	ctx, turnImgs := c.withTurnImages(ctx, task)
	ctx = agent.WithResponseLanguagePreference(ctx, c.display.responseLanguage)
	ctx = agent.WithReasoningLanguagePreference(ctx, c.display.reasoningLanguage)
	ctx = agent.WithSubagentDepth(ctx, 0)
	answer, err := runner(ctx, sk, c.imageRoutingPrefix(turnImgs)+task, skill.SubagentRunOptions{HostInitiated: true})
	if err != nil {
		return "", err
	}
	return tool.GuardSubagentHostDecisionText(answer), nil
}

// Running reports whether a turn is currently in flight.
func (c *Controller) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gate.active()
}

// beginRotation claims the session-rotation gate. It fails if a turn is running
// or another rotation is already in progress, so the caller holds exclusive
// rights to swap the executor session from the check here through endRotation.
// This closes the TOCTOU window that a bare `if c.gate.running` check left open:
// between that check and the actual SetSession, a turn could start and then be
// yanked out from under the run loop.
func (c *Controller) beginRotation() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gate.active() {
		return errTurnRunningRotation
	}
	if c.gate.rotating {
		return errRotationInProgress
	}
	c.gate.rotating = true
	return nil
}

// RuntimeStatus reports the active work owned by the foreground controller.
func (c *Controller) RuntimeStatus() RuntimeStatus {
	c.mu.Lock()
	running := c.gate.running
	active := running || c.gate.finishing
	canceling := c.gate.canceling
	c.mu.Unlock()
	pending := c.approval.hasPending()
	backgroundJobs := len(c.Jobs())
	return RuntimeStatus{
		Running:         active,
		PendingPrompt:   pending,
		BackgroundJobs:  backgroundJobs,
		CancelRequested: canceling,
		Cancellable:     running || pending,
	}
}

// Turn returns the current turn number (0 before the first submit).
func (c *Controller) Turn() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turn
}

type plannerPlanApprover struct {
	c *Controller
}

func (p plannerPlanApprover) RunWithPlannerApproval(ctx context.Context, plan string, run func(context.Context) error) error {
	c := p.c
	allow, _, err := c.requestApproval(ctx, approvalRequest{tool: planApprovalTool, reason: "Planner requested host approval before execution."})
	if err != nil {
		return err
	}
	if !allow {
		return nil
	}
	todoArgs := c.seedPlanTodos(plan)
	execStart := c.sessionMessageCount()
	c.approval.setPlanAutoApprove(true)
	defer c.approval.setPlanAutoApprove(false)
	if err := run(ctx); err != nil {
		return err
	}
	if todoArgs != "" && !c.hasTodoUpdateSince(execStart) {
		c.completePlanTodos(todoArgs)
	}
	return nil
}

func (c *Controller) allowLowRiskRemember(args json.RawMessage) bool {
	mem := c.Memory()
	if mem != nil {
		if assessment := memory.AssessRememberWrite(mem.Store, args); assessment.AutoAllow {
			c.memory.authorizeAutoRemember(args)
			return true
		}
	}
	c.memory.revokeAutoRemember(args)
	return false
}

// denyPermissionApprover answers for a session nobody is watching: a headless
// run has no prompt to show, so a call that needs approval can only be refused.
type denyPermissionApprover struct{}

func (denyPermissionApprover) Approve(context.Context, string, string, json.RawMessage) (bool, bool, error) {
	return false, false, nil
}

// ApproveWithReason says which of the two refusals this is. Without it the gate
// falls back to "the user declined this tool call", which is untrue when there
// was no user, and it sends the model to ask someone who was never there: one
// headless run rewrote its file, was refused again, and closed by offering the
// user three choices about the file's contents.
func (denyPermissionApprover) ApproveWithReason(context.Context, string, string, json.RawMessage) (bool, bool, string, error) {
	return false, false, "this session has no interactive approver, so any call that needs approval is refused — nobody declined it, and neither retrying nor rewriting it can change that. If this work is meant to run unattended, it needs a permission mode that does not ask (or an explicit allow rule for this tool). Otherwise do the part that needs no approval and call conclude_blocked naming what was refused.", nil
}

// rulesWithoutFreshHumanApproval drops any session-allow rule that targets a
// tool requiring fresh human approval, so an explicit allowlist cannot bypass
// the always-prompt contract for those tools.
func rulesWithoutFreshHumanApproval(rules []permission.Rule) []permission.Rule {
	if len(rules) == 0 {
		return rules
	}
	filtered := make([]permission.Rule, 0, len(rules))
	for _, r := range rules {
		if RequiresFreshHumanApprovalTool(r.Tool) {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

// TrySteer queues mid-turn guidance only when the active agent turn accepts it.
func (c *Controller) TrySteer(text string) bool {
	c.mu.Lock()
	exec := c.executor
	running := c.gate.running
	c.mu.Unlock()
	return running && exec != nil && exec.Steer(text)
}

// Steer is the compatibility path for callers that cannot observe admission.
// Interactive hosts should call TrySteer so a rejected steer remains in their
// draft/queue and can be retried as a regular follow-up.
func (c *Controller) Steer(text string) {
	if c.TrySteer(text) {
		return
	}
	// No active turn accepted the steer: the frontend's runningRef was stale,
	// the turn exited between our running check and the enqueue, or no
	// executor is bound yet. Deliver it as a regular turn instead.
	c.submitSteerFallback(text)
}

// submitSteerFallback records steer text that no active turn accepted as
// unapplied guidance, not as a new task. This compatibility path deliberately
// never opens a provider turn: replaying stale historical guidance as the
// user's current request caused unintended code changes (#7045).
func (c *Controller) submitSteerFallback(text string) admissionResult {
	return c.runGuardedOrPark(func(context.Context) error {
		if c.executor != nil {
			c.executor.RecordUnappliedSteer(text)
		}
		return nil
	})
}

// SteerConsumed returns true when the steer queue is empty after the last consume.
func (c *Controller) SteerConsumed() bool {
	c.mu.Lock()
	exec := c.executor
	c.mu.Unlock()
	if exec != nil {
		return exec.SteerConsumed()
	}
	return true
}

// promptQueueNoticeDelay is how long a prompt may wait behind another before
// the user is told why nothing has appeared. Short enough to beat "it's stuck",
// long enough that an approval answered promptly never emits a notice.
var promptQueueNoticeDelay = 3 * time.Second

// lockPromptFor acquires the prompt lock, emitting one notice if the wait is
// long enough to look like a hang. It reports false only when ctx ended first;
// the lock is held on true.
func (c *Controller) lockPromptFor(ctx context.Context, kind string) bool {
	acquired := make(chan struct{})
	go func() {
		c.approval.promptMu.Lock()
		close(acquired)
	}()
	select {
	case <-acquired:
		return true
	case <-ctx.Done():
	case <-time.After(promptQueueNoticeDelay):
	}
	if ctx.Err() == nil {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodePromptQueued,
			Text:   "A " + kind + " is waiting for you to answer the prompt ahead of it.",
			Detail: "the assistant asked something while an earlier approval or question was still open; it appears once that one is answered"})
	}
	select {
	case <-acquired:
		return true
	case <-ctx.Done():
		// The lock may still be handed to the goroutine above; release it so the
		// next prompt is not blocked by this abandoned wait.
		go func() {
			<-acquired
			c.approval.promptMu.Unlock()
		}()
		return false
	}
}

func askAnswersHaveSelection(answers []event.AskAnswer) bool {
	for _, answer := range answers {
		if len(answer.Selected) > 0 {
			return true
		}
	}
	return false
}

// SetPlanMode flips the executor's plan-first workflow flag without touching the
// cache-stable system/tool prefix, and remembers the state so Compose can prepend
// the plan-mode marker to outgoing user turns.
func (c *Controller) SetPlanMode(v bool) {
	c.applyPlanMode(v)
}

// SetAgentPreset updates the session role setting for subsequent turns without
// rebuilding the controller, provider, or tool schemas. Callers must already
// hold active-work guards (no foreground turn, background jobs, or pending
// approvals/asks).
func (c *Controller) SetAgentPreset(preset string) {
	if c == nil {
		return
	}
	preset = strings.TrimSpace(preset)
	if preset == "" {
		preset = "balanced"
	}
	// Map legacy economy/full names through the dual-write helper if available.
	if normalized := strings.ToLower(preset); normalized == "economy" || normalized == "full" {
		switch normalized {
		case "economy":
			preset = "light"
		case "full":
			preset = "balanced"
		}
	}
	if setter, ok := c.runner.(interface{ SetAgentPreset(string) }); ok {
		setter.SetAgentPreset(preset)
	}
	if c.executor != nil {
		c.executor.SetAgentPreset(preset)
	}
	// Keep capability runtimeProfile labels coherent for diagnostics.
	c.mu.Lock()
	switch strings.ToLower(preset) {
	case "light", "economy":
		c.runtimeProfile = capability.ProfileEconomy
	case "delivery":
		c.runtimeProfile = capability.ProfileDelivery
	default:
		c.runtimeProfile = capability.ProfileBalanced
	}
	c.mu.Unlock()
}

// AgentPreset returns the current session role setting.
func (c *Controller) AgentPreset() string {
	if c == nil {
		return "balanced"
	}
	if c.executor != nil {
		return c.executor.AgentPreset()
	}
	if getter, ok := c.runner.(interface{ AgentPreset() string }); ok {
		return getter.AgentPreset()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch c.runtimeProfile {
	case capability.ProfileEconomy:
		return "light"
	case capability.ProfileDelivery:
		return "delivery"
	default:
		return "balanced"
	}
}

func (c *Controller) applyPlanMode(v bool) {
	c.plan().SetActive(v)
	c.sharePlanRuntime()
}

// SetResponseLanguage updates the final-answer language preference for
// subsequent turns.
func (c *Controller) SetResponseLanguage(lang string) {
	mode := config.NormalizeLanguage(lang)
	c.mu.Lock()
	c.display.responseLanguage = mode
	c.mu.Unlock()
	if setter, ok := c.runner.(interface{ SetResponseLanguage(string) }); ok {
		setter.SetResponseLanguage(mode)
	} else if c.executor != nil {
		c.executor.SetResponseLanguage(mode)
	}
}

// SetReasoningLanguage updates the visible reasoning language preference for
// subsequent turns.
func (c *Controller) SetReasoningLanguage(lang string) {
	mode := config.NormalizeReasoningLanguage(lang)
	c.mu.Lock()
	c.display.reasoningLanguage = mode
	c.mu.Unlock()
	if setter, ok := c.runner.(interface{ SetReasoningLanguage(string) }); ok {
		setter.SetReasoningLanguage(mode)
	} else if c.executor != nil {
		c.executor.SetReasoningLanguage(mode)
	}
}

// PlanPhase reports where the run sits in the plan lifecycle.
func (c *Controller) PlanPhase() planmode.Phase { return c.plan().State().Phase }

// GoalStrict enables or disables strict goal mode. Since the structured
// protocol, every complete claim is validated against host readiness and an
// incomplete-todo intercept can never be overridden, so the flag is persisted
// for compatibility with older frontends but no longer changes FSM behavior.
func (c *Controller) GoalStrict(strict bool) {
	path, data, ok := c.goals.setStrict(strict, c.goalTodos())
	c.persistGoalState(path, data, ok)
}

// SetGoal stores a session-scoped active goal. Compose injects it into outgoing
// user turns, not the system prompt or tool schema, so it does not disturb the
// cache-stable prefix.
func (c *Controller) SetGoal(goal string) {
	c.SetGoalWithResearchMode(goal, GoalResearchAuto)
}

// SetGoalDurable updates the Goal only when its sidecar can be replaced
// atomically.
func (c *Controller) SetGoalDurable(goal string) error {
	snapshot := c.goals.capture()
	path, data, persist := c.goals.set(goal, budgetClassForLegacyMode(goal, GoalResearchAuto), c.frozenVerificationContract(), c.goalTodos())
	if persist {
		if err := c.goals.writeStateErr(path, data); err != nil {
			c.goals.restore(snapshot)
			return err
		}
	}
	return nil
}

func (c *Controller) SetGoalWithResearchMode(goal string, researchMode GoalResearchMode) {
	path, data, ok := c.goals.set(goal, budgetClassForLegacyMode(goal, researchMode), c.frozenVerificationContract(), c.goalTodos())
	c.persistGoalState(path, data, ok)
}

// ResumeGoal re-enters a recoverable blocked/stopped Goal without resetting its
// delivery evidence scope or accumulated usage statistics.
func (c *Controller) ResumeGoal() bool {
	spentBudget := c.goals.runtimeView().StopCause == stopCauseBudgetSpend
	path, data, persist, resumed := c.goals.resume(c.goalTodos())
	if !resumed {
		return false
	}
	c.persistGoalState(path, data, persist)
	if c.executor != nil {
		if spentBudget {
			c.executor.ResetTaskBudget()
		}
		c.executor.RestoreDeliveryCheckpoint(c.goals.deliveryState())
	}
	return true
}

// PauseGoal suspends a running Goal without losing its todo list, Delivery
// checkpoint, or runtime history; ResumeGoal restores it. Returns false when no
// running Goal exists.
func (c *Controller) PauseGoal() bool {
	if !c.goals.active() {
		return false
	}
	path, data, ok := c.goals.pauseFor(stopCauseManual, i18n.M.GoalPausedReason, c.goalTodos())
	c.persistGoalState(path, data, ok)
	c.notice(i18n.M.GoalPaused)
	return true
}

// GoalRuntime returns the active Goal's usage/runtime summary for frontends.
func (c *Controller) GoalRuntime() GoalRuntimeView {
	return c.goals.runtimeView()
}

// goalEvaluatorEvidence assembles the bounded evaluator's evidence: the goal
// contract, the current assistant final, a todo/readiness summary,
// turn/budget state, and the last
// continuation reason. Every field is treated as untrusted by the evaluator.
func (c *Controller) goalEvaluatorEvidence() goaleval.GoalEvidence {
	goal, _ := c.goals.snapshot()
	ev := goaleval.GoalEvidence{
		GoalContract:           goal,
		LastContinuationReason: c.goals.lastContinuationReasonText(),
	}
	if c.executor != nil {
		ev.AssistantFinal = lastAssistantText(c.History())
		todos := c.goalTodos()
		incomplete := 0
		for _, t := range todos {
			if t.Status != "completed" {
				incomplete++
			}
		}
		rr := c.executor.ReadinessResult()
		readinessText := "ready"
		if rr.Reason != "" {
			readinessText = rr.Reason
		}
		ev.TodoSummary = fmt.Sprintf("todos: %d total, %d incomplete; delivery readiness: %s", len(todos), incomplete, readinessText)
	}
	ev.TurnStatus = c.goals.budgetStatusText()
	return ev
}

func (c *Controller) persistGoalDeliveryCheckpoint() {
	if c.executor == nil {
		return
	}
	checkpoint := c.executor.DeliveryCheckpoint()
	path, data, ok := c.goals.setDeliveryCheckpoint(checkpoint, c.goalTodos())
	c.persistGoalState(path, data, ok)
}

func (c *Controller) ClearGoal() {
	c.SetGoal("")
}

func (c *Controller) Goal() string {
	return c.goals.goalText()
}

func (c *Controller) GoalStatus() string {
	return c.goals.statusForDisplay()
}

// Compact runs one compaction pass on the executor's session on demand.
// instructions is optional `/compact <focus>` guidance steering what to keep.
func (c *Controller) Compact(ctx context.Context, instructions string) error {
	if c.executor == nil {
		return nil
	}
	// The rotation gate keeps a turn from starting while a manual compaction is
	// building and installing a new model-visible projection.
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return fmt.Errorf("cannot compact while a turn is running")
		}
		return err
	}
	defer c.endRotation()
	return c.executor.CompactNow(ctx, instructions)
}

func removeSessionArtifacts(path string) error {
	if path == "" {
		return nil
	}
	if err := jobs.RemoveArtifacts(path); err != nil {
		return err
	}
	// Artifacts include the event log — the authoritative transcript. Leaving it
	// behind would both leak the cleared conversation and let LoadSession
	// resurrect it on the recycled path. The guardian transcript saves through
	// the same session layer, so its own artifacts go the same way.
	if err := store.RemoveSessionArtifacts(path); err != nil {
		return err
	}
	if err := store.RemoveSessionArtifacts(guardian.PathFor(path), guardian.CursorPathFor(path)); err != nil {
		return err
	}
	if err := sessioninbox.RemoveDir(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if dir := ckptDir(path); dir != "" {
		if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := agent.DeleteSubagentsByParent(filepath.Dir(path), agent.BranchID(path)); err != nil {
		return err
	}
	if err := agent.ClearCleanupPending(path); err != nil {
		return err
	}
	return nil
}

// RemoveSessionArtifacts removes a transcript and every durable artifact owned
// by it. Remote runtimes use this when a newly-created fork fails before it can
// be registered as a live session.
func RemoveSessionArtifacts(path string) error {
	return removeSessionArtifacts(path)
}

// ReconcileCleanupPending retries physical cleanup for logically removed
// sessions that were left behind by a previous process.
func ReconcileCleanupPending(dir string) error {
	return agent.ReconcileCleanupPending(dir, func(item agent.CleanupPendingInfo) error {
		return removeSessionArtifacts(item.SessionPath)
	})
}

// RewindScope selects what a Rewind restores.
type RewindScope int

const (
	RewindCode         RewindScope = iota // files only
	RewindConversation                    // message log only
	RewindBoth                            // both
)

// Checkpoints lists the session's rewind points (one per user turn), oldest first.
//
// Each Meta.Prompt is reduced to what the user typed. A checkpoint opens with
// the composed turn, so the stored prompt can carry the plan-mode marker and
// transient blocks; every consumer of this list is a label (the rewind picker,
// the desktop change list, the workbench projection) and the picker also
// restores the prompt into the composer, so composed text must not reach them.
// Stripping on read rather than only on write keeps checkpoints already on disk
// readable — they were recorded composed.
func (c *Controller) Checkpoints() []checkpoint.Meta {
	metas := c.checkpoints.list()
	for i := range metas {
		metas[i].Prompt = StripComposePrefixes(metas[i].Prompt)
	}
	return metas
}

func (c *Controller) CheckpointFileState(path string) (checkpoint.FileState, bool) {
	return c.checkpoints.fileState(path)
}

func (c *Controller) CheckpointTurnsByMessageIndex() map[int]int {
	return c.checkpoints.turnsByMessageIndex()
}

// rewindFail emits the error as a Warn notice (so a frontend that swallows the
// returned error — e.g. the desktop bridge's .catch — still shows the user why
// the rewind did nothing) and returns it.
func (c *Controller) rewindFail(err error) error {
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: err.Error()})
	return err
}

// Rewind is implemented in rewind.go (transactional conversation+file restore).

func (c *Controller) CheckpointHasBoundary(turn int) bool {
	boundary, ok := c.checkpoints.boundary(turn)
	if !ok {
		return false
	}
	// After compaction the key may still exist but the boundary value is
	// stale (it points past the truncated message log).  Treat those
	// turns the same as "no boundary" so the UI can disable the button.
	// Len is lock-guarded: this runs on frontend goroutines while a turn appends.
	return boundary <= c.executor.Session().Len()
}

// ResolveBranchRef resolves a /switch-style branch reference (id, unique
// prefix, name, or path) against a branch listing, using the same matching
// rules as SwitchBranch. Frontends use it to learn the target session path
// before switching — e.g. to move their session lease first.
func ResolveBranchRef(branches []agent.BranchInfo, ref string) (agent.BranchInfo, error) {
	return resolveBranch(branches, strings.TrimSpace(ref))
}

func resolveBranch(branches []agent.BranchInfo, ref string) (agent.BranchInfo, error) {
	refLower := strings.ToLower(ref)
	var matches []agent.BranchInfo
	for _, b := range branches {
		nameLower := strings.ToLower(strings.TrimSpace(b.Name))
		switch {
		case b.ID == ref || strings.EqualFold(b.ID, ref):
			return b, nil
		case b.Name != "" && nameLower == refLower:
			matches = append(matches, b)
		case strings.HasPrefix(strings.ToLower(b.ID), refLower):
			matches = append(matches, b)
		case strings.HasPrefix(strings.ToLower(shortBranchID(b.ID)), refLower):
			matches = append(matches, b)
		case b.Path == ref:
			return b, nil
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return agent.BranchInfo{}, fmt.Errorf("branch %q is ambiguous", ref)
	}
	return agent.BranchInfo{}, fmt.Errorf("branch %q not found", ref)
}

func branchDisplayName(b agent.BranchInfo) string {
	if strings.TrimSpace(b.Name) != "" {
		return fmt.Sprintf("%s (%s)", b.Name, b.ID)
	}
	return b.ID
}

// SummarizeFrom and SummarizeUpTo preserve the historical turn-index API while
// changing only the model-visible context projection. The canonical transcript
// and checkpoint boundaries remain available for rewind and undo.
func (c *Controller) SummarizeFrom(ctx context.Context, turn int) error {
	return c.summarizeAt(ctx, turn, true)
}

func (c *Controller) SummarizeUpTo(ctx context.Context, turn int) error {
	return c.summarizeAt(ctx, turn, false)
}

func (c *Controller) summarizeAt(ctx context.Context, turn int, from bool) error {
	if c.executor == nil {
		return c.rewindFail(fmt.Errorf("checkpoints unavailable"))
	}
	// Hold the rotation gate from the checkpoint-boundary lookup through
	// projection installation so a turn cannot start against an intermediate
	// context view.
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return c.rewindFail(fmt.Errorf("cannot summarize while a turn is running"))
		}
		return c.rewindFail(err)
	}
	defer c.endRotation()
	boundary, hasBound := c.checkpoints.boundary(turn)
	if !hasBound {
		return c.rewindFail(fmt.Errorf("summarize unavailable for turn %d (resumed session)", turn))
	}
	var err error
	if from {
		err = c.executor.SummarizeFrom(ctx, boundary)
	} else {
		err = c.executor.SummarizeUpTo(ctx, boundary)
	}
	if err != nil {
		return c.rewindFail(err)
	}
	return nil
}

func shouldRotateSessionTempOnResume(prevPath, nextPath string) bool {
	prevPath = strings.TrimSpace(prevPath)
	nextPath = strings.TrimSpace(nextPath)
	if prevPath == "" || nextPath == "" {
		return false
	}
	return filepath.Clean(prevPath) != filepath.Clean(nextPath)
}

func (c *Controller) loadGuardianSession() {
	if c.guardianSess == nil {
		return
	}
	c.guardianSess.Reset()
	path := c.guardianPath
	if path == "" {
		return
	}
	if err := c.guardianSess.Load(path); err != nil && !os.IsNotExist(err) {
		slog.Warn("controller: load guardian session", "err", err)
	}
}

// ResetPlannerSession clears the planner's conversation history so the next
// plan starts fresh. In dual-model (Plan+Execute) mode, this prevents stale
// planner output from a previous session or tab from contaminating the current
// executor's handoff. Safe to call on a single-model controller (no-op).
func (c *Controller) ResetPlannerSession() {
	runner, ok := c.runner.(plannerSessionResetter)
	if ok {
		runner.ResetPlannerSession()
	}
}

// cacheColdAfter resolves how long the active provider keeps a prompt prefix
// cached. A session idle longer than this resumes against a cold cache, so a
// history rewrite at that moment costs no extra cache misses — it only shrinks
// the full-price first request. The TTL is vendor-aware: DeepSeek/unknown
// 24h (legacy default deliberately preserved), DashScope 5m, Anthropic 5m.
// Users can override per-provider
// with cache_ttl_minutes in config.toml.
func (c *Controller) cacheColdAfter() time.Duration {
	if c.testCacheColdAfter != 0 {
		if c.testCacheColdAfter == -1 {
			return 0
		}
		return c.testCacheColdAfter
	}
	// 查询路径只读：LoadForRootReadOnly 不触发配置迁移写盘（评审 #7168
	// 第 4 点）；失败时保守回退 24h（DeepSeek/未知 vendor 默认），避免
	// 把 cache TTL 过期误当成历史改写信号（resume 只记录 warm/cold/unknown）。
	cfg, err := config.LoadForRootReadOnly(c.workspaceRoot)
	if err != nil {
		return 24 * time.Hour
	}
	ref := c.modelRef
	if ref == "" {
		ref = cfg.DefaultModel
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		return 24 * time.Hour
	}
	return entry.EffectiveCacheTTL()
}

// snapshotConflictLogAttrs flattens a snapshot-conflict error into slog attrs.
// Field reports of #6069-class "session changed on disk" spam are only
// diagnosable when the logs say which trigger fired and what the revision
// ledger looked like, so every recoverSnapshotConflict outcome logs these.
func snapshotConflictLogAttrs(saveErr error, path, mode string) []any {
	attrs := []any{"path", path, "mode", mode}
	var conflict *agent.SessionSnapshotConflictError
	if errors.As(saveErr, &conflict) && conflict != nil {
		attrs = append(attrs,
			"kind", string(conflict.Kind),
			"disk_messages", conflict.ExistingMessages,
			"snapshot_messages", conflict.SnapshotMessages,
			"base_revision", conflict.BaseRevision,
			"disk_revision", conflict.DiskRevision,
		)
	}
	return attrs
}

// conflictOutcome is recoverSnapshotConflict's declared result. Callers act
// on it directly instead of re-deriving what happened from path or session
// pointer comparisons — the misclassification that broke the depth-cap
// rewrite baseline (#6120) hid in exactly that inference.
type conflictOutcome int

const (
	// conflictDropped: nothing was recovered and the disk transcript could
	// not be adopted; this snapshot was deliberately dropped.
	conflictDropped conflictOutcome = iota
	// conflictAdoptedDisk: the executor session object was replaced by the
	// newer disk transcript; adoptDiskSession already reset its baselines.
	conflictAdoptedDisk
	// conflictForkedBranch: the same in-memory session moved to a freshly
	// forked recovery branch path.
	conflictForkedBranch
)

const recoveryDepthCapNoticeText = "repeated save conflicts were detected; saved the current conflict copy in an isolated recovery branch"

func sessionRecoveryNotice(code, text string) event.Event {
	return event.Event{
		Kind:     event.Notice,
		Level:    event.LevelWarn,
		Audience: event.NoticeAudienceOperator,
		Code:     code,
		Text:     text,
	}
}

func (c *Controller) messageCount() int {
	if c.executor == nil {
		return 0
	}
	return c.executor.Session().Len()
}

func (c *Controller) markInFlightTurn(startMessageIndex int, preserveUser bool) agent.InFlightTurnMeta {
	path := c.SessionPath()
	if path == "" {
		return agent.InFlightTurnMeta{}
	}
	marker, err := agent.BeginSessionInFlightTurn(path, startMessageIndex, preserveUser)
	if err != nil {
		slog.Warn("controller: mark in-flight turn", "err", err)
		return agent.InFlightTurnMeta{}
	}
	return c.withInheritedInterruptions(marker)
}

// finishInFlightTurn persists the completed transcript before removing the
// crash marker. A crash can therefore leave either a recoverable marker or a
// durable completed transcript, never an unmarked in-memory-only suffix.
func (c *Controller) finishInFlightTurn(startMessages int, marker agent.InFlightTurnMeta) {
	commitPrepared := marker.ID == ""
	if marker.ID != "" && c.executor != nil {
		digest, digestErr := c.executor.Session().ContentDigest()
		if digestErr != nil {
			slog.Warn("controller: compute completed turn digest", "err", digestErr)
		} else if prepared, matched, prepareErr := agent.PrepareSessionInFlightTurnCommit(c.SessionPath(), marker, digest); prepareErr != nil {
			slog.Warn("controller: prepare in-flight turn commit", "err", prepareErr)
		} else if matched {
			marker = prepared
			commitPrepared = true
		}
	}
	durable, err := c.snapshotActivityIfChanged(startMessages)
	if err != nil && !durable {
		// Keep the marker when the transcript did not become durable. Resume can
		// then retry recovery instead of treating an in-memory-only tail as done.
		slog.Warn("controller: keeping in-flight marker after failed turn snapshot", "err", err)
		return
	}
	if err != nil {
		slog.Warn("controller: turn transcript saved before metadata update failed", "err", err)
	}
	if !commitPrepared {
		// Do not clear an unprepared marker: a crash between the snapshot and this
		// point would otherwise leave recovery without exact commit evidence.
		slog.Warn("controller: keeping in-flight marker without commit digest", "marker_id", marker.ID)
		return
	}
	c.clearInFlightTurn(marker)
}

// transplantInFlightTurnMarker moves a pending in-flight-turn marker from the
// session path a recovery fork abandoned onto the branch the turn continues
// on. Left behind, the stale marker would fire recoverInterruptedTurn on the
// next open of the original branch and strip messages from a turn that in
// fact kept running on the recovery branch; missing from the recovery branch,
// a crash before turn end would leave its partial tail unmarked.
func (c *Controller) transplantInFlightTurnMarker(fromPath, toPath string) {
	if strings.TrimSpace(fromPath) == "" || strings.TrimSpace(toPath) == "" || fromPath == toPath {
		return
	}
	meta, ok, err := agent.LoadBranchMeta(fromPath)
	if err != nil || !ok || meta.InFlightTurn == nil {
		if err != nil {
			slog.Warn("controller: load in-flight turn marker for transplant", "path", fromPath, "err", err)
		}
		return
	}
	marker := meta.InFlightTurn
	if err := agent.SetSessionInFlightTurn(toPath, *marker); err != nil {
		// Keep the original marker: a turn boundary on the wrong branch beats
		// no boundary anywhere if the runtime dies before the turn completes.
		slog.Warn("controller: transplant in-flight turn marker", "path", toPath, "err", err)
		return
	}
	if _, err := agent.ClearSessionInFlightTurnIfMatch(fromPath, *marker); err != nil {
		slog.Warn("controller: clear in-flight turn marker on forked-from branch", "path", fromPath, "err", err)
	}
}

// interruptedTurnCrossesLaterTurn detects the data-loss shape where an old
// marker survived while one or more later turns were durably appended. A
// compaction summary and mid-turn steer are not new foreground turn boundaries.
func interruptedTurnCrossesLaterTurn(msgs []provider.Message, start int) bool {
	if start < 0 || start >= len(msgs) {
		return false
	}
	turns := 0
	for _, msg := range msgs[start:] {
		if msg.Role != provider.RoleUser || agent.IsCompactionSummary(msg) {
			continue
		}
		if _, ok := agent.SteerText(msg.Content); ok {
			continue
		}
		turns++
		if turns > 1 {
			return true
		}
	}
	return false
}

// interruptedTurnContinuedOnRecoveryBranch reports whether a recovery branch
// forked off path after its in-flight-turn marker was set. Markers only exist
// while a turn runs and recovery forks happen on saves, so a child recovery
// branch younger than the marker means the marked turn itself moved there —
// the marker is a leftover from a runtime that switched paths mid-turn, not a
// crashed turn whose partial tail needs stripping. A marker without a start
// time is treated as continued whenever any recovery child exists: erring
// toward keeping messages is the data-safe direction.
func interruptedTurnContinuedOnRecoveryBranch(path string, marker *agent.InFlightTurnMeta) bool {
	if marker == nil {
		return false
	}
	branches, err := agent.ListBranches(filepath.Dir(path))
	if err != nil {
		return false
	}
	id := agent.BranchID(path)
	for _, b := range branches {
		if b.Recovered && b.ParentID == id && b.CreatedAt.After(marker.StartedAt) {
			return true
		}
	}
	return false
}

// stripTurnMessagesAfter truncates the executor's session to keep only messages
// before the given index, discarding an incomplete synthetic turn (the synthetic
// user prompt plus every assistant/tool message that followed).
func (c *Controller) stripTurnMessagesAfter(idx int) {
	if c.executor == nil {
		return
	}
	msgs := c.executor.Session().Snapshot()
	if len(msgs) <= idx {
		return
	}
	c.replaceSessionAfterCancel(msgs[:idx])
}

func (c *Controller) inFlightTurnStartedAt() time.Time {
	path := c.SessionPath()
	if path == "" {
		return time.Time{}
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok || meta.InFlightTurn == nil {
		return time.Time{}
	}
	return meta.InFlightTurn.StartedAt
}

// resolveInterruptedTurnStart turns the pre-run array index into a stable
// boundary after compaction. New user messages carry a creation timestamp set
// after the marker, and graceful cleanup also has the exact composed prompt as
// a fallback. We only fall back to the legacy index when it still points at a
// plausible turn-start user message, keeping recovery data-safe for older
// sidecars without timestamps.
func resolveInterruptedTurnStart(msgs []provider.Message, idx int, preserveUser bool, startedAt time.Time, fallback provider.Message) (int, bool) {
	fallbackContent := ""
	if fallback.Role == provider.RoleUser {
		fallbackContent = StripComposePrefixes(fallback.Content)
	}
	matchesKind := func(m provider.Message) bool {
		if m.Role != provider.RoleUser {
			return false
		}
		if preserveUser {
			if IsSyntheticUserMessage(m.Content) {
				return false
			}
			if _, ok := agent.SteerText(m.Content); ok {
				return false
			}
			if fallbackContent != "" && StripComposePrefixes(m.Content) != fallbackContent {
				return false
			}
		}
		return true
	}
	startedMillis := startedAt.UnixMilli()
	if !startedAt.IsZero() {
		for i, m := range msgs {
			if matchesKind(m) && m.CreatedAt >= startedMillis {
				return i, true
			}
		}
	}
	// Tests/headless runners may not persist an in-flight sidecar. The exact
	// graceful fallback still distinguishes the current visible turn; search
	// backward so a repeated prompt selects the newest occurrence.
	if fallbackContent != "" {
		for i, msg := range slices.Backward(msgs) {
			if matchesKind(msg) {
				return i, true
			}
		}
	}
	if idx >= 0 && idx < len(msgs) && matchesKind(msgs[idx]) {
		return idx, true
	}
	return 0, false
}

func completeToolTurnEnd(msgs []provider.Message, i int) (int, bool) {
	if i < 0 || i >= len(msgs) {
		return i, false
	}
	m := msgs[i]
	if m.LocalOnly || m.Role != provider.RoleAssistant || len(m.ToolCalls) == 0 {
		return i, false
	}
	end := i + 1
	for end < len(msgs) && msgs[end].Role == provider.RoleTool && !msgs[end].LocalOnly {
		end++
	}
	results := msgs[i+1 : end]
	if len(results) != len(m.ToolCalls) {
		return i, false
	}
	for k, call := range m.ToolCalls {
		if strings.TrimSpace(call.Name) == "" || (call.Arguments != "" && !json.Valid([]byte(call.Arguments))) {
			return i, false
		}
		if results[k].ToolCallID != call.ID || results[k].Name != call.Name {
			return i, false
		}
	}
	return end, true
}

func toolResultWasInterrupted(content string) bool {
	content = strings.ToLower(strings.TrimSpace(content))
	return strings.HasPrefix(content, "cancelled:") || strings.Contains(content, "context canceled") || strings.Contains(content, "context cancelled")
}

func displayOnlyToolCalls(calls []provider.ToolCall) []provider.ToolCall {
	out := make([]provider.ToolCall, 0, len(calls))
	for _, call := range calls {
		out = append(out, provider.ToolCall{ID: call.ID, Name: strings.TrimSpace(call.Name)})
	}
	return out
}

func appendUniqueString(dst []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return dst
	}
	if slices.Contains(dst, value) {
		return dst
	}
	return append(dst, value)
}

func interruptedToolSummary(call provider.ToolCall) provider.InterruptedToolSummary {
	summary := provider.InterruptedToolSummary{
		ID: call.ID, Name: strings.TrimSpace(call.Name), Added: call.Added, Removed: call.Removed,
	}
	addFile := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || path == "/dev/null" || len(summary.Files) >= 8 {
			return
		}
		if slices.Contains(summary.Files, path) {
			return
		}
		summary.Files = append(summary.Files, path)
	}
	var args map[string]any
	if json.Unmarshal([]byte(call.Arguments), &args) == nil {
		for _, key := range []string{"path", "file", "file_path", "filename"} {
			if value, ok := args[key].(string); ok && strings.TrimSpace(value) != "" {
				addFile(value)
			}
		}
	}
	for line := range strings.SplitSeq(call.Diff, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			addFile(strings.TrimPrefix(line, "+++ b/"))
		case strings.HasPrefix(line, "--- a/"):
			addFile(strings.TrimPrefix(line, "--- a/"))
		case strings.HasPrefix(line, "*** Update File: "):
			addFile(strings.TrimPrefix(line, "*** Update File: "))
		case strings.HasPrefix(line, "*** Add File: "):
			addFile(strings.TrimPrefix(line, "*** Add File: "))
		case strings.HasPrefix(line, "*** Delete File: "):
			addFile(strings.TrimPrefix(line, "*** Delete File: "))
		}
	}
	return summary
}

// SessionDir reports the directory new session files land in ("" disables
// persistence), so the caller can decide whether to mint a path.
func (c *Controller) SessionDir() string { return c.sessionDir }

// History returns the executor's current message log (for repopulating a
// resumed frontend's view).
func (c *Controller) History() []provider.Message {
	if c.executor == nil {
		return nil
	}
	return c.executor.Session().Snapshot() // copy — a turn may be appending concurrently
}

// HistoryLen returns the number of messages in the live log.
func (c *Controller) HistoryLen() int {
	if c.executor == nil {
		return 0
	}
	return c.executor.Session().Len()
}

// HistoryWindow returns a copy of the messages in [start, end) of the live
// log. Paging frontends use it to convert a display window without copying
// the whole history.
func (c *Controller) HistoryWindow(start, end int) []provider.Message {
	if c.executor == nil {
		return []provider.Message{}
	}
	return c.executor.Session().MessageRange(start, end)
}

// ContextSnapshot returns (usedTokens, contextWindow) for the gauge. usedTokens
// is what the next request will send, measured the way the compaction trigger
// measures it, so the gauge and the trigger can never disagree. Both zero means
// no data yet — a gauge hides itself.
func (c *Controller) ContextSnapshot() (int, int) {
	if c.executor == nil {
		return 0, 0
	}
	return c.executor.ContextUsedTokens(), c.executor.ContextWindow()
}

// CompactRatio returns the auto-compaction threshold as a fraction of the window
// (0 when the executor is unset). The status line shows headroom against it.
func (c *Controller) CompactRatio() float64 {
	if c.executor == nil {
		return 0
	}
	return c.executor.CompactRatio()
}

// LastUsage returns the most recent turn's token telemetry (nil before the first
// turn), so frontends can derive the prompt cache-hit rate for the status line.
func (c *Controller) LastUsage() *provider.Usage {
	if c.executor == nil {
		return nil
	}
	return c.executor.LastUsage()
}

// SessionCache returns cumulative cache hit/miss prompt tokens for the session,
// so a frontend can render the aggregate (session-wide) cache-hit rate — steadier
// than the single-turn rate and unaffected by compaction.
func (c *Controller) SessionCache() (hit, miss int) {
	if c.executor == nil {
		return 0, 0
	}
	return c.executor.SessionCache()
}

// Todos returns a copy of the canonical task list (the latest todo_write state
// merged with complete_step advances) so frontends can render a live task panel.
func (c *Controller) Todos() []evidence.TodoItem {
	if c.executor == nil {
		return nil
	}
	return c.executor.CanonicalTodoState()
}

// ToolResultData holds the full arguments and output for one tool call, loaded
// on demand when a frontend expands a collapsed tool card.
type ToolResultData struct {
	Args      string                  `json:"args"`
	Output    string                  `json:"output"`
	Execution *provider.ToolExecution `json:"execution,omitempty"`
}

// ToolResult looks up a tool call by its ID in the session history and returns
// the full arguments + output that were elided from the frontend's items[].
// Returns nil when the tool ID isn't found (e.g. a sub-agent's tool call that
// lives in a different session).
func (c *Controller) ToolResult(toolID string) *ToolResultData {
	if c.executor == nil {
		return nil
	}
	msgs := c.executor.Session().Snapshot()
	// Search backwards: tool result first (most recent), then find the args
	// from the preceding assistant turn.
	for i, msg := range slices.Backward(msgs) {
		if msg.Role != provider.RoleTool || msg.ToolCallID != toolID {
			continue
		}
		out := &ToolResultData{
			Args:      "",
			Output:    msg.Content,
			Execution: msg.ToolExecution,
		}
		// Walk back to find the assistant turn that issued this call.
		for j := i; j >= 0; j-- {
			if msgs[j].Role != provider.RoleAssistant {
				continue
			}
			for _, tc := range msgs[j].ToolCalls {
				if tc.ID == toolID {
					out.Args = tc.Arguments
					return out
				}
			}
		}
		return out
	}
	return nil
}

// Balance reads the active provider's wallet. One declaring no balance_url
// reads back unconfigured rather than failed: "there is no wallet here" and
// "the wallet did not answer" are opposite answers to why there is no number.
func (c *Controller) Balance(ctx context.Context) billing.Reading {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return c.balance.Read(ctx)
}

// Host returns the running MCP host (nil when no plugins), for frontends that
// list servers / resolve MCP prompts.
func (c *Controller) Host() *plugin.Host { return c.mcp.hostRef() }

// Commands returns the loaded custom slash commands.
func (c *Controller) Commands() []command.Command {
	if p := c.commands.Load(); p != nil {
		return *p
	}
	return nil
}

// ReloadCommands rescans all command directories and hot-swaps the slash_command
// tool and the internal command slice — no MCP restart, no hook rerun.
func (c *Controller) ReloadCommands(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	cmds, loadErr := command.LoadRoots(config.CommandRootsForRoot(c.workspaceRoot)...)
	var cmdSkills []skill.Skill
	if !c.skills.noImplicitInvocation {
		cmdSkills = c.SlashSkills()
	}

	entries := make([]command.SlashEntry, 0, len(cmdSkills)+len(cmds))
	for _, sk := range cmdSkills {

		entries = append(entries, command.SlashEntry{
			Name:        sk.SlashName(),
			Description: sk.Description,
			Render:      func(args []string) string { return c.skills.render(sk, strings.Join(args, " ")) },
		})
	}
	for _, cmd := range cmds {
		if cmd.Hidden {
			continue
		}

		entries = append(entries, command.SlashEntry{
			Name:        cmd.Name,
			Description: cmd.Description,
			ArgHint:     cmd.ArgHint,
			Render:      func(args []string) string { return cmd.Render(args) },
		})
	}
	c.mcp.registerTool(command.NewSlashCommandTool(entries))
	cmdSlice := cmds
	c.commands.Store(&cmdSlice)
	return loadErr
}

// Executor returns the underlying agent when present (nil for pure runners).
func (c *Controller) Executor() *agent.Agent {
	if c == nil {
		return nil
	}
	return c.executor
}

// Skills scans the live Store, so a skill installed this session is listed.
func (c *Controller) Skills() []skill.Skill {
	return c.skills.list()
}

// ImplicitSkillInvocationEnabled reports whether skills are exposed to the
// model for automatic discovery and invocation. Explicit /skill handling is
// independent of this model-facing capability.
func (c *Controller) ImplicitSkillInvocationEnabled() bool {
	return c != nil && !c.skills.noImplicitInvocation
}

// SlashSkills returns the user-visible skill directory. Plugin skills use
// package-qualified names while Skills keeps bare model/run_skill identifiers.
func (c *Controller) SlashSkills() []skill.Skill {
	return c.skills.slashList()
}

// AllSkills returns every discoverable skill, including disabled ones, for
// management surfaces that need to re-enable a hidden skill.
func (c *Controller) AllSkills() []skill.Skill {
	return c.skills.listAll()
}

// DisabledSkills returns all discoverable skills that are off in this project.
func (c *Controller) DisabledSkills() []skill.Skill {
	resolve := c.skillActivation()
	var out []skill.Skill
	for _, sk := range c.AllSkills() {
		if !resolve(sk.Name) {
			out = append(out, sk)
		}
	}
	return out
}

// SkillEnabled reports whether a skill is on in this project.
func (c *Controller) SkillEnabled(name string) bool {
	return c.skillActivation()(name)
}

// skillActivation resolves several names against one config and one store read.
// skills.disabled_skills stays readable as the declared default, so a
// hand-written config keeps working even though the switch no longer writes it.
func (c *Controller) skillActivation() func(string) bool {
	declared := func(string) bool { return true }
	if cfg, err := config.Load(); err == nil {
		declared = func(name string) bool { return !cfg.IsSkillDisabled(name) }
	}
	resolver, err := config.DefaultActivationStore().SkillResolverFor(c.workspaceRoot)
	if err != nil {
		return declared
	}
	return func(name string) bool { return resolver.Enabled(name, declared(name)) }
}

// SkillOverrideScope reports where the decision governing name lives, so the
// settings surface can show whether this project set it for itself.
func (c *Controller) SkillOverrideScope(name string) (config.ActivationScope, bool) {
	scope, found, err := config.DefaultActivationStore().SkillOverrideScope(name, c.workspaceRoot)
	if err != nil {
		return config.ActivationGlobal, false
	}
	return scope, found
}

// SetSkillEnabled persists a skill's switch at scope. The caller should rebuild
// the controller for the prompt/tool registry to reflect it immediately.
// Flipping a skill the project inherits writes a project row: the user answered
// for this folder, and two projects may hold different skills of one name.
func (c *Controller) SetSkillEnabled(name string, scope config.ActivationScope, enabled bool) error {
	canonical, err := c.canonicalSkillName(name)
	if err != nil {
		return err
	}
	return config.DefaultActivationStore().SetSkillEnabled(canonical, c.workspaceRoot, scope, enabled)
}

// ClearSkillOverride drops this project's exception for name.
func (c *Controller) ClearSkillOverride(name string, scope config.ActivationScope) error {
	canonical, err := c.canonicalSkillName(name)
	if err != nil {
		return err
	}
	return config.DefaultActivationStore().ClearSkill(canonical, c.workspaceRoot, scope)
}

func (c *Controller) canonicalSkillName(name string) (string, error) {
	for _, sk := range c.AllSkills() {
		if config.SkillNameKey(sk.Name) == config.SkillNameKey(name) {
			return sk.Name, nil
		}
	}
	return "", fmt.Errorf("unknown skill: %s", name)
}

// HookRunner returns the session's hook runner (nil-safe; may hold zero hooks),
// so a frontend can list the active hooks via `/hooks`.
func (c *Controller) HookRunner() *hook.Runner { return c.hooks }

func controllerMCPTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func controllerMCPToolTimeouts(values map[string]int) map[string]time.Duration {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]time.Duration, len(values))
	for name, seconds := range values {
		if name = strings.TrimSpace(name); name != "" && seconds > 0 {
			out[name] = time.Duration(seconds) * time.Second
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Label returns the human-readable model label, e.g. "deepseek-flash".
func (c *Controller) Label() string { return c.label }

// ModelRef returns the canonical provider/model reference for the session.
func (c *Controller) ModelRef() string { return c.modelRef }

// WorkspaceRoot returns the workspace root for this controller's session
// (the directory that file-writers and @-references are scoped to).
// Empty means no scoping is in effect.
func (c *Controller) WorkspaceRoot() string { return c.workspaceRoot }

func (c *Controller) imageInputEnabled() bool {
	ref := c.modelRef
	cfg, err := config.LoadForRoot(c.workspaceRoot)
	if err == nil && ref == "" {
		ref = cfg.DefaultModel
	}
	if err != nil || ref == "" {
		return false
	}
	entry, ok := cfg.ResolveModel(ref)
	return ok && config.EffectiveVision(entry)
}

// ImageInputEnabled reports whether the current model accepts direct image
// inputs, so frontends can gate image-only UX before a turn starts.
func (c *Controller) ImageInputEnabled() bool { return c.imageInputEnabled() }

// InheritLifecycleFrom carries same-session lifecycle state across controller
// rebuilds, such as model switches that preserve the conversation.
func (c *Controller) InheritLifecycleFrom(prev *Controller) {
	if prev == nil {
		return
	}
	prev.mu.Lock()
	started := prev.startedOnce
	turn := prev.turn
	prev.mu.Unlock()

	c.mu.Lock()
	c.startedOnce = started
	if c.turn < turn {
		c.turn = turn
	}
	c.mu.Unlock()
}

// SessionAuthorizations snapshots this controller's same-session tool
// grants ("Allow for this session") and Plan-mode read-only command trust,
// for carrying into a replacement controller across a rebuild — see
// RestoreSessionAuthorizations.
func (c *Controller) SessionAuthorizations() SessionAuthorizations {
	return c.approval.snapshotSessionAuthorizations()
}

// RestoreSessionAuthorizations re-applies session authorizations captured
// from a prior controller in the same session (see SessionAuthorizations). A
// model/effort/profile switch rebuilds the controller, and without this the
// replacement forgets every grant the user already made this session.
func (c *Controller) RestoreSessionAuthorizations(auth SessionAuthorizations) {
	c.approval.restoreSessionAuthorizations(auth)
}

// ReleaseResources stops plugin subprocesses and releases resources without
// firing SessionEnd. Use it only when replacing the controller for the same
// logical session.
func (c *Controller) ReleaseResources() {
	c.close(false, closeJobsWithGrace)
}

// Close stops plugin subprocesses and releases resources. A session that ever
// started fires SessionEnd so a teardown hook runs.
func (c *Controller) Close() {
	c.close(true, closeJobsWithGrace)
}

// CloseAfterDestroy releases controller resources after the caller has already
// begun session-specific job teardown. It avoids a second synchronous job grace
// wait while still cancelling the manager root and reaping temporary artifacts
// once every job goroutine finally exits.
func (c *Controller) CloseAfterDestroy() {
	c.close(true, closeJobsAsync)
}

type closeJobsMode int

const (
	closeJobsWithGrace closeJobsMode = iota
	closeJobsAsync
)

func (c *Controller) close(fireSessionEnd bool, jobsMode closeJobsMode) {
	// Desktop tab lifecycles can race a rebind/model-switch/close on the same
	// controller; make teardown idempotent so a duplicate Close cannot re-fire
	// SessionEnd hooks or re-run cleanup. The first caller's jobsMode wins.
	c.closeOnce.Do(func() {
		c.mu.Lock()
		started := c.startedOnce
		cancel := c.gate.cancel
		// Seal turn admission and drop anything already parked: a parked turn
		// must not start against a controller that is being torn down, and
		// without the closed flag a submit landing after this critical
		// section (while a running turn's TurnDone delivery is still in
		// flight) would park again and start after teardown.
		c.gate.closed = true
		c.parkedTurns = nil
		// A finishing-only controller no longer needs the delivery gate because
		// closed seals every admission path. Keep running truthful until the
		// foreground goroutine actually exits; clearing it here would report idle
		// while tools and prompt waiters were still live.
		c.gate.finishing = false
		if cancel != nil {
			c.gate.canceling = true
		}
		c.mu.Unlock()
		if cancel != nil {
			// clearAll deliberately does not signal waiters. Pair it with the
			// foreground cancellation so approval/ask waits always unblock.
			c.approval.clearAll()
			cancel()
		}
		if fireSessionEnd && started {
			c.hooks.SessionEnd(context.Background(), "other")
			c.extensionSessionEvent(extension.PointSessionEnd, dispatch.PhaseEnd, c.SessionPath())
		}
		if c.jobs != nil {
			switch jobsMode {
			case closeJobsAsync:
				c.jobs.CloseAsync()
			default:
				c.jobs.Close() // cancel any still-running background jobs
			}
		}
		if c.cleanup != nil {
			c.cleanup()
		}
		// Drop the Controller owner reference last so background job leases
		// that outlive close still pin retired generations until they exit.
		if c.sessionTemp != nil {
			c.sessionTemp.Release()
		}
	})
}

// SessionTemp returns the logical-session private temporary directory manager.
// Hot rebuilds pass this to the replacement Controller so the directory survives
// model/settings swaps. Nil only when the Controller was constructed without one
// (should not happen after New).
func (c *Controller) SessionTemp() *sessiontemp.Manager {
	if c == nil {
		return nil
	}
	return c.sessionTemp
}

// rotateSessionTemp advances the private temporary generation so a new logical
// session cannot see the previous session's temporary files. In-flight command
// leases keep the old generation alive until they release.
func (c *Controller) rotateSessionTemp() {
	if c == nil || c.sessionTemp == nil {
		return
	}
	c.sessionTemp.Rotate()
}

// Jobs returns the still-running background jobs for the status bar (nil when
// background jobs are disabled).
func (c *Controller) Jobs() []jobs.View {
	if c.jobs == nil {
		return nil
	}
	return c.jobs.RunningForSession(c.parentSessionID())
}

// KillJob cancels a running background job by ID.
func (c *Controller) KillJob(id string) bool {
	if c.jobs == nil {
		return false
	}
	return c.jobs.Kill(id)
}

// CancelJob stops one background job owned by this controller's session.
func (c *Controller) CancelJob(id string) bool {
	if c.jobs == nil {
		return false
	}
	return c.jobs.KillForSession(c.parentSessionID(), id)
}

// WorkspaceLeaseState reports only whether this controller owns or is waiting
// for the Delivery workspace writer lease. It never exposes filesystem or
// process identity.
func (c *Controller) WorkspaceLeaseState() workspacelease.State {
	return c.workspaceLease.State()
}

// SetBypass is the legacy name for SetAutoApproveTools. Keep it for existing
// desktop/serve bindings and CLI code that still uses the bypass wording.
func (c *Controller) SetBypass(on bool) {
	c.SetAutoApproveTools(on)
}

// SetMode applies the Plan workflow flag and tool auto-approval together so a turn
// submitted right after a composer mode switch can't observe a half-applied
// gate. Turning tool auto-approval on drains any pending tool approval.
func (c *Controller) SetMode(plan, autoApproveTools bool) {
	c.ApplyMode(plan, autoApproveTools)
}

// ApplyMode is SetMode reporting which pending approval prompt ids the tool
// approval switch auto-allowed (see ApplyToolApprovalMode).
func (c *Controller) ApplyMode(plan, autoApproveTools bool) []string {
	c.applyPlanMode(plan)
	if autoApproveTools {
		return c.ApplyToolApprovalMode(ToolApprovalYolo)
	}
	return c.ApplyToolApprovalMode(ToolApprovalAsk)
}

// Bypass is the legacy name for AutoApproveTools.
func (c *Controller) Bypass() bool {
	return c.AutoApproveTools()
}

// memory
//
// The memory snapshot, the pending turn-tail notes queue, and write serialization
// live in c.memory (a memoryManager) behind its own locks, off c.mu — so a
// memory-panel save never stalls an approval or status poll. These methods are
// the SessionAPI surface; each is a thin delegation. See memory.go.

// QuickAdd appends a one-line note to the doc-memory file for scope (project
// REASONIX.md by default) — the write side of "#<note>". Returns the file written.
func (c *Controller) QuickAdd(scope memory.Scope, note string) (string, error) {
	return c.memory.quickAdd(scope, note)
}

// SaveDoc overwrites a recognized memory doc with body — the save side of the
// desktop panel's in-place editor. Returns the file written.
func (c *Controller) SaveDoc(path, body string) (string, error) {
	return c.memory.saveDoc(path, body)
}

// SaveMemory writes an active auto-memory fact and refreshes the in-session
// snapshot. It is the explicit user-confirmed counterpart to the model-owned
// remember tool, used by management surfaces that preview a candidate first.
func (c *Controller) SaveMemory(m memory.Memory) (string, error) {
	return c.memory.saveMemory(m)
}

// ForgetMemory removes a saved auto-memory by name — the panel/TUI forget action,
// the manual counterpart to the model's `forget` tool.
func (c *Controller) ForgetMemory(name string) error {
	return c.memory.forget(name)
}

// QueueMemory implements memory.Queue: when the model runs the remember/forget
// tool, the tool calls this with a note that rides the next turn so the change
// applies this session without touching the cache-stable prefix. It also
// refreshes the snapshot a memory panel reads.
func (c *Controller) QueueMemory(note string) {
	c.memory.queue(note)
}

// ClaimAutoMemoryWrite consumes the one-shot create-only authorization issued
// by gateApprover for a low-risk project fact.
func (c *Controller) ClaimAutoMemoryWrite(args json.RawMessage) bool {
	return c.memory.claimAutoRemember(args)
}

func (c *Controller) MemoryRevisions(ref string) []memory.Memory {
	return c.memory.revisions(ref)
}

// RestoreMemory restores an older active-memory revision as a new audited
// revision and applies it to the next user turn.
func (c *Controller) RestoreMemory(ref string, revision int) (memory.Memory, error) {
	return c.memory.restore(ref, revision)
}

// RestoreArchivedMemory recovers an archived fact as a new audited revision and
// applies it to the next user turn.
func (c *Controller) RestoreArchivedMemory(archivePath string) (memory.Memory, error) {
	return c.memory.restoreArchived(archivePath)
}

// Memory returns the loaded memory snapshot (nil when memory is disabled), for
// frontends that surface a memory panel or the /memory command. The returned
// *Set is immutable — mutations go through QuickAdd / SaveDoc.
func (c *Controller) Memory() *memory.Set {
	return c.memory.current()
}

// approval bridge (agent gate → events)

// gateApprover adapts the Controller to permission.Approver. It is distinct
// from the public Approve command (different signature, different direction).
type gateApprover struct{ c *Controller }

const dynamicBashApprovalReason = "This command uses nested or indirect shell execution. Auto and broad allow rules cannot verify the inner command; approve this exact command or use YOLO."

func (g gateApprover) Approve(ctx context.Context, tool, subject string, args json.RawMessage) (bool, bool, error) {
	allow, remember, _, err := g.ApproveWithReason(ctx, tool, subject, args)
	return allow, remember, err
}

func (g gateApprover) ApproveWithReason(ctx context.Context, tool, subject string, args json.RawMessage) (bool, bool, string, error) {
	return g.approveWithPolicyReason(ctx, tool, subject, args, "")
}

func (g gateApprover) ApproveWithPolicyReason(ctx context.Context, tool, subject string, args json.RawMessage, policyReason string) (bool, bool, string, error) {
	return g.approveWithPolicyReason(ctx, tool, subject, args, policyReason)
}

func combineApprovalReasons(reasons ...string) string {
	var kept []string
	for _, reason := range reasons {
		if reason = strings.TrimSpace(reason); reason != "" {
			kept = append(kept, reason)
		}
	}
	return strings.Join(kept, "\n")
}

func (g gateApprover) approveWithPolicyReason(ctx context.Context, tool, subject string, args json.RawMessage, policyReason string) (bool, bool, string, error) {
	if tool == memoryRememberTool && g.c.allowLowRiskRemember(args) {
		return true, false, "", nil
	}
	subject = approvalDisplaySubject(tool, subject, args)
	requireHuman := strings.EqualFold(tool, "bash") && permission.BashSubjectRequiresExplicitApproval(subject)
	// Check pre-approval first, before any prompt or Guardian review. Dynamic
	// Bash accepts only YOLO or an exact session grant here; ordinary calls also
	// accept the just-approved-plan window. Deny rules already bit at the policy
	// level before this point.
	if requireHuman && g.c.approval.preApprovedForRequiredHuman(tool, subject) {
		return true, false, "", nil
	}
	if !requireHuman && g.c.approval.preApproved(tool, subject, args) {
		return true, false, "", nil
	}
	if g.c.guardianSess != nil && !requireHuman {
		allow, reason, reviewErr := g.c.guardianSess.Review(ctx, tool, args, g.c.executor.Session())
		if reviewErr != nil {
			return false, false, "", reviewErr
		}
		if allow && !requiresFreshApprovalTool(tool) {
			return true, false, "", nil
		}
		reason = combineApprovalReasons(policyReason, reason)
		humanAllow, remember, err := g.c.requestApproval(ctx, approvalRequest{tool: tool, subject: subject, args: args, reason: reason})
		if err != nil {
			return false, false, reason, err
		}
		if !humanAllow {
			return false, false, reason, nil
		}
		return true, remember, "", nil
	}
	if requireHuman {
		reason := combineApprovalReasons(policyReason, dynamicBashApprovalReason)
		allow, remember, err := g.c.requestApproval(ctx, approvalRequest{tool: tool, subject: subject, args: args, reason: reason, requireHuman: true})
		return allow, remember, "", err
	}
	allow, remember, err := g.c.requestApproval(ctx, approvalRequest{tool: tool, subject: subject, args: args, reason: policyReason})
	return allow, remember, "", err
}

type sandboxEscapeApprover struct{ c *Controller }

func (s sandboxEscapeApprover) ApproveSandboxEscape(ctx context.Context, req sandbox.EscapeRequest) (bool, string, error) {
	subject := sandboxEscapeApprovalSubject(req.Command)
	reason := sandboxEscapeApprovalReason(req.Reason)
	reply, err := s.c.requestApprovalDecision(ctx, approvalRequest{tool: SandboxEscapeApprovalTool, subject: subject, args: req.Args, reason: reason, fresh: true})
	if err != nil {
		return false, "approval aborted", err
	}
	if !reply.allow {
		return false, i18n.M.SandboxEscapeDeclined, nil
	}
	if reply.session {
		s.c.approval.grantSession(SandboxEscapeApprovalTool, subject)
	}
	return true, "", nil
}

func (s sandboxEscapeApprover) SandboxEscapeSessionAllowed(_ context.Context, req sandbox.EscapeRequest) bool {
	return s.c.approval.preApprovedForDecision(SandboxEscapeApprovalTool, sandboxEscapeApprovalSubject(req.Command), nil, true)
}

func sandboxEscapeApprovalSubject(command string) string {
	subject := strings.TrimSpace(command)
	if subject == "" {
		return i18n.M.SandboxEscapeSubjectFallback
	}
	return i18n.M.SandboxEscapeSubjectPrefix + subject
}

func sandboxEscapeApprovalReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return i18n.M.SandboxEscapeRuntimeReason
	}
	return reason
}

// managedConfigWriteApprover routes a file tool's Reasonix-managed config write
// through the fresh-human approval prompt (see ManagedConfigWriteApprovalTool).
// A session grant is tool-wide (mirroring sandbox_escape): one "allow for this
// session" covers the rest of the repair flow across the handful of managed
// config files without re-prompting on every incremental edit.
type managedConfigWriteApprover struct{ c *Controller }

func (m managedConfigWriteApprover) ApproveManagedConfigWrite(ctx context.Context, req tool.ConfigWriteRequest) (bool, string, error) {
	subject := managedConfigWriteApprovalSubject(req.Path)
	args, _ := json.Marshal(map[string]string{"path": req.Path})
	reply, err := m.c.requestApprovalDecision(ctx, approvalRequest{tool: ManagedConfigWriteApprovalTool, subject: subject, args: args, reason: i18n.M.ConfigWriteReason, fresh: true})
	if err != nil {
		return false, "approval aborted", err
	}
	if !reply.allow {
		return false, i18n.M.ConfigWriteDeclined, nil
	}
	if reply.session {
		m.c.approval.grantSession(ManagedConfigWriteApprovalTool, subject)
	}
	return true, "", nil
}

func (m managedConfigWriteApprover) ManagedConfigWriteSessionAllowed(_ context.Context, req tool.ConfigWriteRequest) bool {
	return m.c.approval.preApprovedForDecision(ManagedConfigWriteApprovalTool, managedConfigWriteApprovalSubject(req.Path), nil, true)
}

func managedConfigWriteApprovalSubject(path string) string {
	return i18n.M.ConfigWriteSubjectPrefix + strings.TrimSpace(path)
}

func approvalDisplaySubject(tool, subject string, args json.RawMessage) string {
	switch tool {
	case memoryRememberTool:
		return rememberApprovalSubject(subject, args)
	case memoryForgetTool:
		return forgetApprovalSubject(subject, args)
	case "move_file":
		return moveApprovalSubject(subject, args)
	default:
		return subject
	}
}

func moveApprovalSubject(fallback string, args json.RawMessage) string {
	if len(args) == 0 {
		return fallback
	}
	var in struct {
		SourcePath      string `json:"source_path"`
		DestinationPath string `json:"destination_path"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return fallback
	}
	if in.SourcePath == "" || in.DestinationPath == "" {
		return fallback
	}
	return in.SourcePath + " -> " + in.DestinationPath
}

func rememberApprovalSubject(fallback string, args json.RawMessage) string {
	if len(args) == 0 {
		return fallback
	}
	var in struct {
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Type        string `json:"type"`
		Body        string `json:"body"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return fallback
	}
	name := approvalCompactText(firstNonEmpty(in.Name, in.Title))
	desc := approvalTruncate(approvalCompactText(in.Description), 180)
	body := approvalTruncate(approvalCompactText(in.Body), 240)
	typ := string(memory.NormalizeType(in.Type))

	var b strings.Builder
	b.WriteString(i18n.M.MemoryApprovalSaveUpdate)
	baseLen := b.Len()
	if name != "" {
		fmt.Fprintf(&b, " %q", name)
	}
	if typ != "" {
		fmt.Fprintf(&b, " [%s]", typ)
	}
	if desc != "" {
		b.WriteString(": ")
		b.WriteString(desc)
	}
	if body != "" {
		if desc == "" {
			b.WriteString(": ")
		} else {
			b.WriteString(" | ")
		}
		b.WriteString(i18n.M.MemoryApprovalBodyLabel)
		b.WriteString(": ")
		b.WriteString(body)
	}
	if b.Len() == baseLen && fallback != "" {
		return fallback
	}
	return b.String()
}

func forgetApprovalSubject(fallback string, args json.RawMessage) string {
	if len(args) == 0 {
		return fallback
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return fallback
	}
	name := approvalCompactText(in.Name)
	if name == "" {
		return fallback
	}
	return fmt.Sprintf(i18n.M.MemoryApprovalArchiveFmt, name)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func approvalCompactText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func approvalTruncate(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

func (c *Controller) sessionMessageCount() int {
	if c.executor == nil {
		return 0
	}
	return c.executor.Session().Len()
}

// parseRewind parses the arguments after "/rewind". The user may provide:
//
//	/rewind              → latest checkpoint, both
//	/rewind <turn>       → that turn, both
//	/rewind <turn> <scope> → that turn, code|conversation|both
//
// If no turn is given, the latest checkpoint is used. If no scope is given, Both is assumed.
func parseRewind(args string, cps []checkpoint.Meta) (int, RewindScope, error) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		if len(cps) == 0 {
			return 0, RewindBoth, fmt.Errorf("no checkpoints available")
		}
		return cps[len(cps)-1].Turn, RewindBoth, nil
	}
	turn, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, RewindBoth, fmt.Errorf("invalid turn: %w", err)
	}
	scope := RewindBoth
	if len(fields) >= 2 {
		switch strings.ToLower(fields[1]) {
		case "code":
			scope = RewindCode
		case "conversation":
			scope = RewindConversation
		case "both":
			scope = RewindBoth
		default:
			return 0, RewindBoth, fmt.Errorf("unknown scope %q", fields[1])
		}
	}
	return turn, scope, nil
}

// approvalRequest is one ask: what is being approved, why, and which postures
// may answer it instead of a human. The zero value is an ordinary tool
// permission with no stated reason.
type approvalRequest struct {
	tool    string
	subject string
	args    json.RawMessage
	reason  string
	// fresh marks a user trust/business decision rather than an ordinary tool
	// permission. It may reuse an explicit session grant, but YOLO/auto approval
	// must not answer or drain the prompt.
	fresh bool
	// requireHuman marks an ordinary tool approval that Auto, an approved-plan
	// window, Guardian, or an allowing hook must not answer. Unlike fresh it
	// retains the ordinary four-choice UI and YOLO remains an explicit bypass.
	requireHuman bool
}

func (c *Controller) emitRememberResult(r RememberResult) {
	if r.Err != nil {
		c.sink.Emit(event.Event{
			Kind:  event.Notice,
			Level: event.LevelWarn,
			Text:  fmt.Sprintf(i18n.M.PermissionSaveFailedFmt, r.Rule, r.Err),
		})
		return
	}
	switch {
	case r.Saved:
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf(i18n.M.PermissionSavedFmt, r.Path, r.Rule)})
	case strings.TrimSpace(r.CoveredBy) != "":
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf(i18n.M.PermissionAlreadyAllowedFmt, r.Path, r.CoveredBy)})
	}
}
