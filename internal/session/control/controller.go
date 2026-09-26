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
	"reasonix/internal/state/sessionstore"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/base/i18n"
	"reasonix/internal/base/nilutil"
	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/command"
	"reasonix/internal/ext/extension"
	"reasonix/internal/ext/extension/dispatch"
	"reasonix/internal/ext/extension/uihub"
	"reasonix/internal/ext/hook"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/ext/skill"
	"reasonix/internal/model/billing"
	"reasonix/internal/platform/browser"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/capability"
	"reasonix/internal/runtime/goaleval"
	"reasonix/internal/runtime/guardian"
	"reasonix/internal/runtime/promptrefine"
	"reasonix/internal/runtime/recovery"
	"reasonix/internal/runtime/taskmonitor"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/safety/permission"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/memory"
	"reasonix/internal/state/sessiontemp"
	"reasonix/internal/state/workspacelease"
	"reasonix/internal/tools/jobs"
	"reasonix/internal/tools/shellrun"
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
// dir in a headless deployment) that previously dropped conversations silently
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
	//, or nil when no v2 runtime packages started. Installed via
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

	// externalFolders owns the @ tokens for user-dropped directories outside
	// workspaceRoot, behind its own lock. See external_folders.go.
	externalFolders externalFolders

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
	browser     *browser.Session
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
	Meta         sessionstore.BranchMeta
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
	// PromptRefiner rewrites a draft before it is sent; nil refuses the ask.
	PromptRefiner *promptrefine.Refiner
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
	CapabilityRuntime *usecap.MCPCapabilityRuntime
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
	SessionRecoveryMeta func(SessionRecoveryRequest) sessionstore.BranchMeta
	// OnSessionRecovered is called after a stale runtime's transcript has been
	// saved as a recovery branch, before the controller commits to that branch.
	OnSessionRecovered func(SessionRecoveryInfo) error
	// ApprovalTimeout bounds how long a tool-approval or ask prompt blocks waiting
	// for a user decision. Zero (default) waits forever — right for an interactive
	// terminal. Headless frontends set a positive value so an unanswered
	// prompt can't wedge the session indefinitely (#4626, #4402).
	ApprovalTimeout time.Duration
	// RuntimeProfile selects capability routing/filtering behavior. Empty keeps
	// the backward-compatible Balanced profile.
	RuntimeProfile capability.Profile
	// Extensions is the frozen extension dispatcher for this controller
	// generation (Extension Protocol v2). Nil means no v2 runtime
	// packages are installed: every extension wiring point takes an untouched
	// fast path. Boot installs it through SetExtensions because sidecars (and
	// therefore the dispatcher) only exist after snapshot assembly, which runs
	// after New.
	Extensions *dispatch.Dispatcher
	// ProviderResolver is the build's merged provider catalog — extension
	// sidecar providers folded over the config/broker base. Nil when
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
	SessionTemp    *sessiontemp.Manager
	BrowserSession *browser.Session // the agent's browser; a rebuild hands on the same one
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
		opts.Hooks.SetSessionID(sessionstore.BranchID(opts.SessionPath))
	}
	c := &Controller{
		controllerDeps:     newControllerDeps(opts, sink, usageTee, runtimeOwner, pluginCtx),
		guardianPath:       guardian.PathFor(opts.SessionPath),
		systemPrompt:       opts.SystemPrompt,
		sessionPath:        opts.SessionPath,
		commands:           atomic.Pointer[[]command.Command]{},
		onSessionRecovered: opts.OnSessionRecovered,
		runtimeProfile:     runtimeProfile,
		externalFolders:    externalFolders{toolRefs: opts.ExternalFolderToolRefs},
		providerResolver:   opts.ProviderResolver,
		runtimeGeneration:  opts.RuntimeGeneration,
	}
	c.adoptSessionResources(opts)

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
		c.executor.SetAdmissionSaver(c)
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
		if sessionstore.UserMessageText(msgs[i]) == original {
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

// commands (frontend → controller)

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

// NetworkEgressApprovalTool is the internal Tool name used to ask whether bash
// may reach a host outside [sandbox] allowed_domains; the subject is the host.
const NetworkEgressApprovalTool = permission.NetworkEgress

// ManagedConfigWriteApprovalTool is the internal Tool name used for per-write
// approval when a file tool targets a Reasonix-managed config file outside the
// workspace write roots. It is a fresh human decision: config files control
// providers, sandbox rules, permissions, and MCP servers for future sessions,
// so YOLO/auto approval must never answer it.
const ManagedConfigWriteApprovalTool = "config_write"

// planApprovedMessage is the follow-up turn sent once the user approves a plan —
// the in-context nudge to execute and keep the (already-seeded) task list honest.
const planApprovedMessage = "Plan approved — plan mode is off. Implement the plan now. The ordinary writer fallback is approved for this execution turn; explicit ask/deny rules and forced fresh reviews still apply. Use this serial workflow: 1) mark the first sub-step in_progress with todo_write (this establishes the task list); 2) execute the sub-step; 3) call complete_step with evidence — the host then marks that sub-step completed and moves the next one to in_progress for you. Repeat 2–3 for each remaining sub-step. You don’t need another todo_write to mark steps completed; each complete_step advances the list. Sign off one sub-step at a time — never batch multiple completions."

type preparedInvocationTurn struct {
	composed  string
	subagents []skill.Skill
}

// compactAndReport folds the context and says what happened. A fold the kernel
// declined is an answer about this transcript — nothing left worth folding —
// and reporting it as a failure sent people looking for a broken kernel.
func (c *Controller) compactAndReport(focus string) {
	verdict, err := c.Compact(context.Background(), agent.CompactRequest{Instructions: focus})
	switch {
	case err == nil && verdict.Compacted():
		c.notice("compacted")
		if err := c.SnapshotRewrite(); err != nil {
			slog.Warn("controller: snapshot after compact", "err", err)
		}
	case err == nil:
		// The host settled which economics declined; saying it in the kernel's
		// own words beats a frontend inferring one from an empty result.
		c.notice("nothing to compact — " + agent.CompactDeclineText(verdict.Reason))
	case agent.IsCompactionDeclined(err):
		c.notice("nothing to compact — " + agent.CompactionDeclineReason(err))
	default:
		c.notice("compaction failed: " + err.Error())
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
				ID:     id,
				Name:   "bash",
				Args:   fmt.Sprintf(`{"command":%q}`, command),
				Issuer: event.IssuedByUser,
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
				DurationMs: durationMs, Execution: ex, Issuer: event.IssuedByUser,
			},
		})
		return c.answerShell(ctx, command, res.State, ex.ExitCode, res.Combined, errText)
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
	tags     turnTags
	// resolve reads the refs out of refLine. nil resolves against the whole
	// workspace; a frontend that must not widen a ref to an arbitrary absolute
	// path passes ResolveScopedRefs.
	resolve func(context.Context, string) (string, []string)
}

// notice emits an informational Notice event.
func (c *Controller) notice(text string) {
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: text})
}

func (c *Controller) noticeDetail(text, detail string) {
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: text, Detail: detail})
}

// Running reports whether a turn is currently in flight.
func (c *Controller) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gate.active()
}

// RuntimeStatus reports the active work owned by the foreground controller.
func (c *Controller) RuntimeStatus() RuntimeStatus {
	c.mu.Lock()
	running := c.gate.running
	// A rotation (compact, rewind, new session) is rewriting the history a
	// rebuild would carry over, so it is active work to every busy guard.
	active := running || c.gate.finishing || c.gate.rotating
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

// promptQueueNoticeDelay is how long a prompt may wait behind another before
// the user is told why nothing has appeared. Short enough to beat "it's stuck",
// long enough that an approval answered promptly never emits a notice.
var promptQueueNoticeDelay = 3 * time.Second

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

// Compact runs one compaction pass on the executor's session on demand.
// instructions is optional `/compact <focus>` guidance steering what to keep.
func (c *Controller) Compact(ctx context.Context, req agent.CompactRequest) (agent.CompactVerdict, error) {
	if c.executor == nil {
		return agent.CompactVerdict{}, nil
	}
	// The rotation gate keeps a turn from starting while a manual compaction is
	// building and installing a new model-visible projection.
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return agent.CompactVerdict{}, fmt.Errorf("cannot compact while a turn is running")
		}
		return agent.CompactVerdict{}, err
	}
	defer c.endRotation()
	return c.executor.CompactNow(ctx, req)
}

// RewindScope selects what a Rewind restores.
type RewindScope int

const (
	RewindCode         RewindScope = iota // files only
	RewindConversation                    // message log only
	RewindBoth                            // both
)

// Rewind is implemented in rewind.go (transactional conversation+file restore).

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

// SessionDir reports the directory new session files land in ("" disables
// persistence), so the caller can decide whether to mint a path.
func (c *Controller) SessionDir() string { return c.sessionDir }

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

type closeJobsMode int

const (
	closeJobsWithGrace closeJobsMode = iota
	closeJobsAsync
)

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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
