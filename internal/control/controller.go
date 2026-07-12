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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/autoresearch"
	"reasonix/internal/billing"
	"reasonix/internal/capability"
	"reasonix/internal/checkpoint"
	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/diff"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/guardian"
	"reasonix/internal/hook"
	"reasonix/internal/i18n"
	"reasonix/internal/jobs"
	"reasonix/internal/memory"
	"reasonix/internal/memorycompiler"
	"reasonix/internal/nilutil"
	"reasonix/internal/permission"
	"reasonix/internal/plugin"
	"reasonix/internal/proc"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/skill"
	"reasonix/internal/store"
	"reasonix/internal/tool"
)

// ErrTurnRunning reports that a caller tried to start a second foreground turn
// while one is already active in the same Controller.
var ErrTurnRunning = errors.New("turn already running")

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
	runner       agent.Runner
	executor     *agent.Agent
	guardianSess *guardian.Session // nil when guardian is disabled
	guardianPath string            // persisted guardian session file ("" when disabled)
	sink         event.Sink
	policy       permission.Policy

	label        string
	modelRef     string
	systemPrompt string
	sessionDir   string
	commands     atomic.Pointer[[]command.Command]
	// skills owns the session's discovered skills (enabled subset, full set, and
	// the reloadable stores) — the skills slice of the Capabilities concern. See
	// skill.go.
	skills              skillSet
	skillRunner         skill.SubagentRunner
	readOnlySkillRunner skill.SubagentRunner
	skillProfile        skill.ProfileResolver
	slashSkillSeq       atomic.Uint64
	hooks               *hook.Runner // session hook runner; nil-safe (no hooks configured)
	// hookContexts carries one-shot lifecycle hook context into the next real
	// user turn without changing the cache-stable system prompt.
	hookContexts []string
	// memory owns the loaded memory snapshot, the pending turn-tail notes queue,
	// and write serialization behind its own locks, off c.mu — so a memory-panel
	// save never stalls an approval or status poll. See memory.go.
	memory            memoryManager
	cleanup           func()
	autoPlan          string
	responseLanguage  string
	reasoningLanguage string
	// disableColdResumePrune skips stale-tool-result elision on cold resume.
	// Zero value keeps the prune on (the cheaper default).
	disableColdResumePrune            bool
	shell                             sandbox.Shell // interpreter for user-invoked "!" commands; zero = auto
	classifier                        autoPlanClassifier
	startedOnce                       bool                             // guards the one-shot SessionStart hook on first turn
	closeOnce                         sync.Once                        // makes close idempotent under racing teardown paths
	onRemember                        func(rule string) RememberResult // set via Options; invoked when user picks "always allow"
	onRememberMCPReadOnlyTrust        func(serverName, rawToolName string) MCPReadOnlyTrustResult
	onRememberPlanModeReadOnlyCommand func(prefix string) PlanModeReadOnlyCommandTrustResult
	sessionRecoveryMeta               func(SessionRecoveryRequest) agent.BranchMeta
	onSessionRecovered                func(SessionRecoveryInfo) error

	// balanceURL/balanceKey target the active provider's optional wallet-balance
	// endpoint (empty when the provider declares none). Captured at build so a
	// model/key switch — which rebuilds the controller — refreshes them.
	balanceURL    string
	balanceKey    string
	balanceClient *http.Client

	// jobs is the session-scoped background-job manager. The agent's background
	// tools spawn into it; Compose drains its completion notes into the next turn;
	// Close cancels its still-running jobs.
	jobs *jobs.Manager

	// mcp owns the session's live tool/plugin surface — the MCP plugin Host, the
	// tool registry the executor reads each turn, and the session-scoped context a
	// hot-added stdio server binds its subprocess to — behind its own lock, off
	// c.mu. The Controller keeps the config-facing orchestration (persisting
	// reasonix.toml on add/remove, building specs from entries). See mcp.go.
	mcp mcpManager

	// Capability routing (Delivery hybrid route). Not part of the provider-visible
	// prefix; only seeds the turn-scoped ledger and optional semantic router.
	pluginCfg       []config.PluginEntry
	capCachedTools  map[string][]plugin.CachedTool
	capCacheHashOK  map[string]bool
	semanticRouter  *capability.SemanticRouter
	capabilityAudit *capability.Audit
	runtimeProfile  capability.Profile

	// goals owns the active goal's FSM (status, intercepts, idle/turn counters)
	// and its persistence, behind its own mutex so a per-turn goal save never
	// stalls an approval or status poll on c.mu. See goal.go.
	goals        goalMachine
	autoResearch *autoresearch.Store

	// workspaceRoot is the workspace root: the base for resolving @-refs and slash
	// path refs, the working directory for user "!" shell commands and custom
	// command discovery, and the guard root for checkpoint restore writes. It is
	// surfaced to frontends via WorkspaceRoot().
	workspaceRoot string

	// externalFolderRefs maps session-generated @ tokens to user-dropped
	// directories outside workspaceRoot. It is intentionally per-controller:
	// dragging a folder authorizes that folder for this chat session only, without
	// widening scoped @ resolution to arbitrary absolute paths.
	externalFolderRefsMu   sync.RWMutex
	externalFolderRefs     map[string]string
	externalFolderToolRefs externalFolderToolRefs

	// checkpoints owns the snapshot-based rewind bookkeeping (the per-session
	// store, the monotonic turn counter, and the conversation-rewind boundary map)
	// behind its own lock, off c.mu — so a boundary read for a rewind/fork never
	// contends on the run-state lock. The Controller keeps the rewind/fork/summarize
	// orchestration (truncating the session, restoring code, emitting events). See
	// checkpoint.go.
	checkpoints checkpointManager

	// approval owns the approval/ask prompt bookkeeping and the runtime approval
	// posture (ask/auto/yolo, session grants, the just-approved-plan window)
	// behind its own locks, off c.mu. The Controller keeps the I/O orchestration
	// (requestApproval/Ask emit events + fire hooks + rebuild the executor gate).
	// See approval.go.
	approval approvalManager

	// mu guards the run state; every critical section under it is short and
	// non-blocking.
	mu        sync.Mutex
	cancel    context.CancelFunc
	running   bool
	finishing bool // TurnDone is still being delivered; park a replacement turn
	canceling bool
	// closed marks the controller as terminally torn down (close() ran). It
	// seals turn admission: without it, a submit arriving AFTER close cleared
	// the parked queue — but while a still-running turn's TurnDone delivery
	// was in flight — would park again and then start against freed resources
	// when the window closed.
	closed bool
	// parkedTurns holds turn bodies that arrived during the finishing window,
	// FIFO. finishGuardedTurn starts the oldest one as it closes the window
	// (see runGuarded/finishGuardedTurn); close() discards any remainder.
	parkedTurns []func(ctx context.Context) error
	// rotating is set under mu while NewSession/ClearSession swap the executor
	// session out. Checking running once and then swapping later leaves a
	// TOCTOU window: a turn can start (running=false at check time) during the
	// intervening Snapshot() and then have its live session replaced. running
	// and rotating are mutually exclusive gates — a turn refuses to start while
	// a rotation is in progress, and a rotation refuses to start while a turn
	// runs — so the run loop's session reference cannot change under it.
	rotating    bool
	autosaveWG  sync.WaitGroup
	planMode    bool
	sessionPath string
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
	// (new/clear/fork/branch/switch/resume/SetSessionPath) hold it for the same
	// reason: a save that reads the old path but the new session would write one
	// transcript's messages into another's file, or manufacture a bogus conflict.
	// Not reentrant — never call snapshot (or anything that snapshots, such as
	// recoverInterruptedTurn or maybeColdResumePrune) while holding it.
	snapshotMu sync.Mutex
	// turn counts model turns this session, passed to hooks in their payload.
	turn int

	displayRecorder func(content, display string)
}

type approvalReply struct {
	allow   bool
	session bool
	persist bool // true = write "always allow" rule to config
}

type pendingApproval struct {
	tool      string
	subject   string
	reason    string
	fresh     bool
	autoDrain bool
	reply     chan approvalReply
}

// pendingAsk is an in-flight ask question batch. questions is retained so the
// AskRequest can be re-emitted to a frontend that reconnected after the original
// event (see ReplayPendingPrompts).
type pendingAsk struct {
	questions []event.AskQuestion
	reply     chan []event.AskAnswer
}

type AutoResearchEvidenceInput struct {
	ID       string
	Kind     string
	Summary  string
	Source   string
	Command  string
	Paths    []string
	Accepted bool
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
	ToolApprovalAsk  = "ask"
	ToolApprovalAuto = "auto"
	ToolApprovalYolo = "yolo"
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

// MCPReadOnlyTrustResult describes what happened when a trusted MCP read-only
// tool was persisted.
type MCPReadOnlyTrustResult struct {
	Server    string
	Tool      string
	Path      string
	Saved     bool
	CoveredBy string
	Err       error
}

// PlanModeReadOnlyCommandTrustResult describes what happened when a trusted bash
// command prefix was persisted for plan-mode research.
type PlanModeReadOnlyCommandTrustResult struct {
	Prefix    string
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
	Runner        agent.Runner
	Executor      *agent.Agent
	Guardian      *guardian.Session
	Sink          event.Sink
	Policy        permission.Policy
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
	// SkillRunner executes a runAs=subagent skill in an isolated child loop.
	// ReadOnlySkillRunner is the plan-mode-safe variant used by direct slash
	// invocation while planning; SkillProfile supplies model/effort display
	// metadata for the synthetic top-level run_skill event.
	SkillRunner         skill.SubagentRunner
	ReadOnlySkillRunner skill.SubagentRunner
	SkillProfile        skill.ProfileResolver
	Hooks               *hook.Runner
	Memory              *memory.Set
	Cleanup             func()
	// BalanceURL/BalanceKey wire the active provider's optional wallet-balance
	// endpoint and bearer key; empty when the provider declares no balance_url.
	BalanceURL    string
	BalanceKey    string
	BalanceClient *http.Client
	// Jobs is the session-scoped background-job manager (nil disables background jobs).
	Jobs *jobs.Manager
	// Registry is the executor's live tool set, and PluginCtx the session-scoped
	// context; both are needed for hot-adding MCP servers via AddMCPServer.
	Registry  *tool.Registry
	PluginCtx context.Context
	// WorkspaceRoot is the project root checkpoint restores are confined to ("" =
	// no confinement). Frontends pass the cwd they launched the session in.
	WorkspaceRoot          string
	ExternalFolderToolRefs externalFolderToolRefs
	AutoPlan               string
	// ResponseLanguage controls final-answer language preference. Empty/auto
	// means no transient injection because the stable language policy follows the
	// current user turn.
	ResponseLanguage string
	// ReasoningLanguage controls visible reasoning language preference. Empty/auto
	// means no transient injection because the stable language policy already
	// follows the conversation language.
	ReasoningLanguage string
	// DisableColdResumePrune skips the stale-tool-result elision that otherwise
	// runs when a session resumes past the provider cache window. Zero value
	// keeps the prune on (the cheaper default).
	DisableColdResumePrune bool
	// Shell is the interpreter user-invoked "!" commands run under, so /shell
	// matches the agent's configured [tools.shell] choice. Zero value = auto.
	Shell      sandbox.Shell
	Classifier autoPlanClassifier
	// OnRemember, when set, is invoked with a new allow rule the user chose to
	// persist to disk (e.g. "Bash(go test:*)"). The callback is wired into the
	// permission Gate on EnableInteractiveApproval.
	OnRemember func(rule string) RememberResult
	// OnRememberMCPReadOnlyTrust persists a raw MCP tool name as trusted
	// read-only when the user chooses "always allow" from the plan-mode trust
	// prompt.
	OnRememberMCPReadOnlyTrust func(serverName, rawToolName string) MCPReadOnlyTrustResult
	// OnRememberPlanModeReadOnlyCommand persists a bash command prefix as trusted
	// read-only when the user chooses "always allow" from the plan-mode trust
	// prompt.
	OnRememberPlanModeReadOnlyCommand func(prefix string) PlanModeReadOnlyCommandTrustResult
	// SessionRecoveryMeta lets a frontend attach scope/topic/profile metadata to
	// an automatic recovery branch before it is written.
	SessionRecoveryMeta func(SessionRecoveryRequest) agent.BranchMeta
	// OnSessionRecovered is called after a stale runtime's transcript has been
	// saved as a recovery branch, before the controller commits to that branch.
	OnSessionRecovered func(SessionRecoveryInfo) error
	// PlanModeAllowedTools names extra custom tools the plan-mode policy may treat
	// as read-only. Known blocked tools and unsafe bash still lose.
	PlanModeAllowedTools []string
	// ApprovalTimeout bounds how long a tool-approval or ask prompt blocks waiting
	// for a user decision. Zero (default) waits forever — right for an interactive
	// terminal. Bot/headless frontends set a positive value so an unanswered
	// prompt can't wedge the session indefinitely (#4626, #4402).
	ApprovalTimeout time.Duration
	// RuntimeProfile selects capability routing/filtering behavior. Empty keeps
	// the backward-compatible Balanced profile.
	RuntimeProfile capability.Profile
}

// New builds a Controller. A nil Sink is replaced with event.Discard.
func New(opts Options) *Controller {
	sink := opts.Sink
	if nilutil.IsNil(sink) {
		sink = event.Discard
	}
	classifier := opts.Classifier
	if nilutil.IsNil(classifier) {
		classifier = nil
	}
	pluginCtx := opts.PluginCtx
	if pluginCtx == nil {
		pluginCtx = context.Background()
	}
	runtimeProfile := opts.RuntimeProfile
	if runtimeProfile == "" {
		runtimeProfile = capability.ProfileBalanced
	}
	c := &Controller{
		runner:                            opts.Runner,
		executor:                          opts.Executor,
		guardianSess:                      opts.Guardian,
		guardianPath:                      guardian.PathFor(opts.SessionPath),
		sink:                              sink,
		policy:                            opts.Policy,
		label:                             opts.Label,
		modelRef:                          opts.ModelRef,
		systemPrompt:                      opts.SystemPrompt,
		sessionDir:                        opts.SessionDir,
		sessionPath:                       opts.SessionPath,
		commands:                          atomic.Pointer[[]command.Command]{},
		skills:                            newSkillSet(opts.Skills, opts.AllSkills, opts.SkillStore, opts.AllSkillStore),
		skillRunner:                       opts.SkillRunner,
		readOnlySkillRunner:               opts.ReadOnlySkillRunner,
		skillProfile:                      opts.SkillProfile,
		hooks:                             opts.Hooks,
		memory:                            newMemoryManager(opts.Memory),
		cleanup:                           opts.Cleanup,
		autoPlan:                          normalizeAutoPlan(opts.AutoPlan),
		responseLanguage:                  config.NormalizeLanguage(opts.ResponseLanguage),
		reasoningLanguage:                 config.NormalizeReasoningLanguage(opts.ReasoningLanguage),
		disableColdResumePrune:            opts.DisableColdResumePrune,
		shell:                             opts.Shell,
		classifier:                        classifier,
		onRemember:                        opts.OnRemember,
		onRememberMCPReadOnlyTrust:        opts.OnRememberMCPReadOnlyTrust,
		onRememberPlanModeReadOnlyCommand: opts.OnRememberPlanModeReadOnlyCommand,
		sessionRecoveryMeta:               opts.SessionRecoveryMeta,
		onSessionRecovered:                opts.OnSessionRecovered,
		balanceURL:                        opts.BalanceURL,
		balanceKey:                        opts.BalanceKey,
		balanceClient:                     opts.BalanceClient,
		jobs:                              opts.Jobs,
		mcp:                               newMcpManager(opts.Host, opts.Registry, pluginCtx),
		runtimeProfile:                    runtimeProfile,
		workspaceRoot:                     opts.WorkspaceRoot,
		externalFolderToolRefs:            opts.ExternalFolderToolRefs,
		approval:                          newApprovalManager(opts.Policy, ToolApprovalAsk, opts.ApprovalTimeout),
	}
	if strings.TrimSpace(opts.WorkspaceRoot) != "" {
		c.autoResearch = autoresearch.NewStore(opts.WorkspaceRoot)
	}
	// Checkpoints: bind a store to the session and route writer pre-edits into it.
	c.rebindCheckpoints(opts.SessionPath)
	c.setActiveJobSession(opts.SessionPath)
	cmdsInit := opts.Commands
	c.commands.Store(&cmdsInit)
	if c.executor != nil {
		c.executor.SetPreEditHook(func(ch diff.Change) {
			c.checkpoints.snapshot(ch)
		})
		c.executor.SetMemoryQueue(c)
	}
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
		if msgs[i].Content == original {
			return
		}
		msgs[i].Edited = true
		msgs[i].Original = original
		s.Replace(msgs)
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
func (c *Controller) rebindCheckpoints(sessionPath string) {
	c.goals.setStatePath(goalStatePath(sessionPath))
	c.checkpoints.rebind(ckptDir(sessionPath), c.workspaceRoot)
}

// beginCheckpoint opens a checkpoint for the turn about to run, recording the
// current message count as the conversation-rewind boundary. Called at the top of
// runTurn, before the user message is appended.
func (c *Controller) beginCheckpoint(input string) {
	if c.executor == nil {
		return
	}
	c.checkpoints.begin(input, len(c.executor.Session().Messages))
}

// --- commands (frontend → controller) ---

// admissionResult classifies what runGuarded did with a turn body.
type admissionResult int

const (
	// turnStarted: admission was open; the turn is running now.
	turnStarted admissionResult = iota
	// turnParked: the body landed inside the finishing window (TurnDone was
	// being delivered) and will start the moment the window closes. From the
	// caller's perspective the turn WILL run — nothing was lost.
	turnParked
	// turnDroppedRunning: a turn is genuinely in flight. Deliberately silent,
	// as before: interactive frontends prevent this with their own
	// steer/queue UX, and internal opportunistic callers (goal-loop
	// continuations, replays) rely on a quiet no-op.
	turnDroppedRunning
	// turnDroppedRotating: the executor session is being swapped out
	// (NewSession/ClearSession). The input's intended session is ambiguous,
	// so it is refused with a user-visible Notice asking to resend rather
	// than silently running against a session the user didn't see.
	turnDroppedRotating
	// turnDroppedClosed: the controller has been closed. Deliberately silent:
	// this controller's transports are being (or have been) torn down and the
	// input's home is the replacement controller the host swaps in — a Notice
	// here would go to a dead surface.
	turnDroppedClosed
)

// runGuarded runs body on a background goroutine under a fresh cancellable
// context, guarding against concurrent turns and emitting a TurnDone event when
// it finishes (Err set on failure; nil also for a user Cancel).
//
// Admission is NOT first-come-first-served across all states — see
// admissionResult. In particular, a body arriving during the finishing window
// is parked, not dropped: TurnDone is emitted inside that window, so every
// caller that reacts to TurnDone by submitting again (a frontend's queued
// auto-send, a bot, a fast Enter) would otherwise race a silent drop. That
// exact loss was observed in CI and reproduced on a clean main-v2 worktree,
// and the desktop composer already carries a workaround gating its auto-send
// on submitDisabled rather than turn_done (Composer.tsx).
func (c *Controller) runGuarded(body func(ctx context.Context) error) admissionResult {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return turnDroppedClosed
	}
	if c.rotating {
		c.mu.Unlock()
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "input was not accepted: the session is being switched — please resend"})
		return turnDroppedRotating
	}
	if c.running {
		c.mu.Unlock()
		return turnDroppedRunning
	}
	if c.finishing {
		c.parkedTurns = append(c.parkedTurns, body)
		c.mu.Unlock()
		return turnParked
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.running = true
	c.canceling = false
	c.mu.Unlock()
	c.spawnGuardedTurn(ctx, cancel, body)
	return turnStarted
}

// spawnGuardedTurn launches an admitted turn body plus its autosave companion.
// The caller must already have claimed admission (running=true) under c.mu.
func (c *Controller) spawnGuardedTurn(ctx context.Context, cancel context.CancelFunc, body func(ctx context.Context) error) {
	c.autosaveWG.Add(1)
	go func() {
		defer c.autosaveWG.Done()
		c.autosaveWhileRunning(ctx)
	}()
	go func() {
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				c.finishGuardedTurn(fmt.Errorf("internal error: %v", r))
			}
		}()
		err := body(ctx)
		c.finishGuardedTurn(explainError(err))
	}()
}

// finishGuardedTurn keeps admission closed while TurnDone is delivered. The
// sink fan-out may detach per-turn transports; allowing a replacement turn in
// after running=false but before that fan-out completed let the old completion
// clear or inherit the replacement turn's transport.
//
// When the window closes, the oldest parked turn (if any) is started under the
// SAME critical section that clears finishing: opening the gate first and then
// re-admitting would let an unrelated submit slip in ahead and bounce the
// parked turn back to a drop. Remaining parked turns drain one per
// finishGuardedTurn, preserving FIFO order. Rotation cannot interleave here:
// beginRotation refuses while running or finishing, and the drain flips
// finishing directly into running.
func (c *Controller) finishGuardedTurn(err error) {
	c.mu.Lock()
	c.running = false
	c.finishing = true
	c.cancel = nil
	c.canceling = false
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.finishing = false
		if c.closed || len(c.parkedTurns) == 0 {
			// A closed controller must not start a parked turn against freed
			// resources; close() also cleared the queue, this guards the
			// close-raced-with-delivery ordering.
			c.mu.Unlock()
			return
		}
		next := c.parkedTurns[0]
		c.parkedTurns = c.parkedTurns[1:]
		ctx, cancel := context.WithCancel(context.Background())
		c.cancel = cancel
		c.running = true
		c.canceling = false
		c.mu.Unlock()
		c.spawnGuardedTurn(ctx, cancel, next)
	}()
	c.sink.Emit(event.Event{Kind: event.TurnDone, Err: err})
}

// Send starts a turn with an uncomposed message. The controller applies
// auto-plan, plan-mode, memory, and background-job framing inside the async turn
// path so frontends do not block on classifier I/O.
func (c *Controller) Send(input string) {
	c.SendWithRaw(input, input)
}

// SendWithRaw starts a turn with separate model input and raw prompt text. The
// raw prompt is used only for auto-plan scoring; it deliberately excludes
// resolved @-reference payloads so referenced file contents cannot inflate the
// complexity score.
func (c *Controller) SendWithRaw(input, raw string) {
	c.runGuarded(func(ctx context.Context) error { return c.runGoalLoopWithRaw(ctx, input, raw) })
}

// planApprovalTool is the Tool name on the ApprovalRequest the controller emits
// to gate a proposed plan. Frontends key their plan-approval UI on it (the
// desktop renders a plan card; the chat TUI a plan banner).
const planApprovalTool = "exit_plan_mode"

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
const planApprovedMessage = "Plan approved — plan mode is off; you’re cleared to make the changes without asking again. Implement the plan now. Use this serial workflow: 1) mark the first sub-step in_progress with todo_write (this establishes the task list); 2) execute the sub-step; 3) call complete_step with evidence — the host then marks that sub-step completed and moves the next one to in_progress for you. Repeat 2–3 for each remaining sub-step. You don’t need another todo_write to mark steps completed; each complete_step advances the list. Sign off one sub-step at a time — never batch multiple completions."

// runTurn runs one model turn, then applies the plan-approval gate. This is the
// single, frontend-agnostic plan flow: in plan mode the model just researches
// (writers are blocked) and writes its plan as a normal answer — no special tool.
// When the turn ends with a text proposal, the controller asks the user to
// approve (reusing the ApprovalRequest channel both frontends already render);
// on approval it exits plan mode, seeds the task list from the plan, and
// continues straight into execution; on rejection it stays in plan mode so the
// next turn can revise. Plan mode is only ever set interactively, so the headless
// `Run` path (which doesn't call this) never blocks on a prompt.
func (c *Controller) runTurn(ctx context.Context, input string) error {
	return c.runGoalLoopWithRaw(ctx, input, input)
}

// RunTurn executes one foreground turn synchronously through the same lifecycle
// used by interactive frontends: auto-plan, transient memory/background-job
// composition, checkpoints, hooks, and plan approval. It is for transports that
// need a blocking request/response boundary, such as ACP session/prompt.
func (c *Controller) RunTurn(ctx context.Context, input string) error {
	ctx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	// finishing is part of the gate: TurnDone delivery for the previous turn
	// is still fanning out, and starting a synchronous turn inside that
	// window recreates the completion/transport crosstalk the window exists
	// to prevent (Running() already reports true here). closed seals a torn-
	// down controller. Synchronous callers get an error rather than parking:
	// they hold a request/response boundary open and already handle busy.
	if c.running || c.finishing || c.rotating || c.closed {
		c.mu.Unlock()
		cancel()
		return ErrTurnRunning
	}
	c.cancel = cancel
	c.running = true
	c.canceling = false
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.running = false
		c.cancel = nil
		c.canceling = false
		c.mu.Unlock()
		cancel()
	}()
	return c.runTurn(ctx, input)
}

func (c *Controller) runTurnWithRaw(ctx context.Context, input, raw string) error {
	return c.runTurnWithRawDisplay(ctx, input, raw, "")
}

func (c *Controller) runGoalLoopWithRaw(ctx context.Context, input, raw string) error {
	return c.runGoalLoopWithRawDisplay(ctx, input, raw, "")
}

func (c *Controller) runGoalLoopWithRawDisplay(ctx context.Context, input, raw, display string) error {
	return newTurnOrchestrator(c).runGoalLoopWithRawDisplay(ctx, input, raw, display)
}

func (c *Controller) runEditedGoalLoopWithRawDisplay(ctx context.Context, input, raw, display, original string) error {
	return newTurnOrchestrator(c).runEditedGoalLoopWithRawDisplay(ctx, input, raw, display, original)
}

func (c *Controller) runTurnWithRawDisplay(ctx context.Context, input, raw, display string) error {
	return newTurnOrchestrator(c).runTurnWithRawDisplay(ctx, input, raw, display)
}

func (c *Controller) runSubagentSkillSlash(sk skill.Skill, task, raw, display string) {
	c.runGuarded(func(ctx context.Context) error {
		planMode := c.PlanMode()
		runner := c.skillRunner
		if planMode {
			runner = c.readOnlySkillRunner
		}
		if runner == nil {
			return fmt.Errorf("subagent skill runner is unavailable for /%s", sk.Name)
		}
		return newTurnOrchestrator(c).runSubagentSkillGoalLoop(ctx, sk, task, raw, display, runner, planMode)
	})
}

// toolWasCalledLastTurn reports whether the most recent assistant message
// contained any tool calls, indicating the agent made observable progress.
func (c *Controller) toolWasCalledLastTurn() bool {
	msgs := c.History()
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Role == provider.RoleAssistant {
			return len(m.ToolCalls) > 0
		}
		if m.Role == provider.RoleUser {
			return false
		}
	}
	return false
}

func (c *Controller) stopGoal(status string) {
	path, data, ok := c.goals.stop(status, c.goalTodos())
	c.persistGoalState(path, data, ok)
}

// lastAssistantText returns the content of the most recent assistant message with
// non-empty text — the model's final answer for the turn (its plan, in plan mode).
func lastAssistantText(msgs []provider.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleAssistant && strings.TrimSpace(msgs[i].Content) != "" {
			return msgs[i].Content
		}
	}
	return ""
}

// Submit is the one-call entry for a simple frontend: it takes raw user input
// and does everything — slash-command dispatch, @-reference expansion, plan-mode
// composition — emitting all output as events. The HTTP/SSE server uses this so
// a browser client only POSTs the typed line.
//
// Slash commands route to the matching primitive: /compact, /new, and /clear
// run their session op and emit a Notice; /mcp__server__prompt and custom /commands
// resolve to a turn; an unknown slash emits a Notice. Anything else is a normal
// turn with its @-references resolved first.
func (c *Controller) Submit(input string) {
	c.submit(input, "", "")
}

// SubmitHTTP accepts input from the unauthenticated localhost HTTP frontend. It
// deliberately omits the trusted TUI-only "!cmd" shell shortcut and resolves file
// references only through the controller's workspace root.
func (c *Controller) SubmitHTTP(input string) {
	c.submitHTTP(input, "")
}

// SubmitDisplay runs input as a turn while remembering the user-facing display
// text for transcript replay when controller-side composition expands input.
func (c *Controller) SubmitDisplay(display, input string) {
	c.submit(input, display, "")
}

// SubmitInvocationDisplay executes composer-selected invocation entities
// independently of slash-command parsing. Plain string submit entry points keep
// their existing behavior for CLI, HTTP, and backward-compatible clients.
func (c *Controller) SubmitInvocationDisplay(display, input string, invocations []InvocationRequest) {
	c.submitInvocations(input, display, invocations)
}

func (c *Controller) submitInvocations(input, display string, requests []InvocationRequest) {
	if len(requests) == 0 {
		c.SubmitDisplay(display, input)
		return
	}
	ordered := append([]InvocationRequest(nil), requests...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Offset < ordered[j].Offset })
	inline := make([]skill.Skill, 0, len(ordered))
	subagents := make([]skill.Skill, 0, len(ordered))
	for _, request := range ordered {
		sk, _, ok := c.resolveSkillInvocation("/" + strings.TrimSpace(request.Name))
		if !ok {
			c.notice("unknown invocation: /" + strings.TrimSpace(request.Name))
			return
		}
		kind := "skill"
		if sk.RunAs == skill.RunSubagent {
			kind = "subagent"
		}
		if request.Kind != kind {
			c.notice(fmt.Sprintf("invocation /%s is %s, not %s", sk.SlashName(), kind, request.Kind))
			return
		}
		if sk.RunAs == skill.RunSubagent {
			subagents = append(subagents, sk)
		} else {
			inline = append(inline, sk)
		}
	}

	parts := make([]string, 0, len(inline)+1)
	for _, sk := range inline {
		parts = append(parts, skill.Render(sk, ""))
	}
	if strings.TrimSpace(input) != "" {
		parts = append(parts, input)
	}
	composed := strings.Join(parts, "\n\n")
	if len(subagents) == 0 {
		c.runGuarded(func(ctx context.Context) error {
			return c.runGoalLoopWithRawDisplay(ctx, composed, input, display)
		})
		return
	}
	if strings.TrimSpace(input) == "" {
		c.notice("subagent invocation requires a task")
		return
	}
	c.runGuarded(func(ctx context.Context) error {
		planMode := c.PlanMode()
		runner := c.skillRunner
		if planMode {
			runner = c.readOnlySkillRunner
		}
		if runner == nil {
			return fmt.Errorf("subagent skill runner is unavailable")
		}
		return newTurnOrchestrator(c).runSubagentSkillTurnsGoalLoop(ctx, subagents, composed, input, display, runner, planMode)
	})
}

// SubmitEditedDisplay is SubmitDisplay for an inline-edited prompt. The model
// sees input; the saved user message also keeps the pre-edit prompt as local UI
// metadata so the edit survives session rewrites.
func (c *Controller) SubmitEditedDisplay(display, input, original string) {
	c.submit(input, display, original)
}

// SubmitUserTurn starts a normal model turn without interpreting shell or slash
// commands. It still resolves references, so callers can submit trusted
// user-authored prompt text without expanding the command surface.
func (c *Controller) SubmitUserTurn(input, display string) {
	c.runRefTurn(input, display)
}

func (c *Controller) submit(input, display, editedOriginal string) {
	trimmed := strings.TrimSpace(input)
	if note, ok := MemoryQuickAddNote(trimmed); ok {
		c.rememberProjectNote(note)
		return
	}
	if note, ok := RememberCommandNote(trimmed); ok {
		c.rememberProjectNote(note)
		return
	}
	if c.applyGoalCommand(trimmed, display) {
		return
	}
	if strings.HasPrefix(trimmed, "!") {
		c.RunShell(trimmed[1:])
		return
	}
	c.submitCommandOrTurn(trimmed, input, display, false, editedOriginal)
}

func (c *Controller) submitHTTP(input, display string) {
	trimmed := strings.TrimSpace(input)
	if note, ok := MemoryQuickAddNote(trimmed); ok {
		c.rememberProjectNote(note)
		return
	}
	if note, ok := RememberCommandNote(trimmed); ok {
		c.rememberProjectNote(note)
		return
	}
	if c.applyGoalCommand(trimmed, display) {
		return
	}
	if strings.HasPrefix(trimmed, "!") {
		c.notice("shell commands are unavailable from this frontend")
		return
	}
	c.submitCommandOrTurn(trimmed, input, display, true, "")
}

func (c *Controller) submitCommandOrTurn(trimmed, input, display string, scopedRefsOnly bool, editedOriginal string) {
	runRefTurn := c.runRefTurn
	runRefTurnWithRefs := c.runRefTurnWithRefs
	runGoalLoop := c.runGoalLoopWithRawDisplay
	if scopedRefsOnly {
		runRefTurn = c.runScopedRefTurn
		runRefTurnWithRefs = c.runScopedRefTurnWithRefs
	}
	if strings.TrimSpace(editedOriginal) != "" {
		runRefTurn = func(input, display string) {
			c.runEditedRefTurn(input, display, editedOriginal)
		}
		runRefTurnWithRefs = func(input, refLine, display string) {
			c.runEditedRefTurnWithRefs(input, refLine, display, editedOriginal)
		}
		runGoalLoop = func(ctx context.Context, input, raw, display string) error {
			return c.runEditedGoalLoopWithRawDisplay(ctx, input, raw, display, editedOriginal)
		}
	}
	switch {
	case trimmed == "/compact" || strings.HasPrefix(trimmed, "/compact "):
		focus := strings.TrimSpace(strings.TrimPrefix(trimmed, "/compact"))
		go func() {
			if err := c.Compact(context.Background(), focus); err != nil {
				c.notice("compaction failed: " + err.Error())
			} else {
				c.notice("compacted")
				if err := c.SnapshotRewrite(); err != nil {
					slog.Warn("controller: snapshot after compact", "err", err)
				}
			}
		}()
	case trimmed == "/new":
		go func() {
			if err := c.NewSession(); err != nil {
				c.notice("new session failed: " + err.Error())
			} else {
				c.notice("new session")
			}
		}()
	case trimmed == "/clear":
		go func() {
			if err := c.ClearSession(); err != nil {
				c.notice("clear context failed: " + err.Error())
			} else {
				c.notice("context cleared")
			}
		}()
	case strings.HasPrefix(trimmed, "/mcp__"):
		c.runGuarded(func(ctx context.Context) error {
			sent, found, err := c.MCPPrompt(ctx, trimmed)
			if err != nil {
				return err
			}
			if !found {
				c.notice("unknown command: " + trimmed)
				return nil
			}
			return runGoalLoop(ctx, sent, sent, display)
		})
	case SlashCodeCommentLine(trimmed):
		// Slash-prefixed code comments are prompt text, not slash commands.
		runRefTurn(input, display)
	case strings.HasPrefix(trimmed, "/"):
		if ref, ok := FileRefLine(trimmed); ok {
			runRefTurn(ref, display)
			return
		}
		if ref, ok := SlashPathLineRef(trimmed, c.workspaceRoot); ok {
			runRefTurnWithRefs(input, ref, display)
			return
		}
		if SlashPathLikeLine(trimmed) {
			runRefTurn(input, display)
			return
		}
		// Read-only management verbs (/model /memory /skills /hooks /mcp) emit a
		// listing Notice, so Submit-based frontends (desktop, HTTP) get them with
		// no extra wiring. (The chat TUI handles these itself with richer output.)
		fields := strings.Fields(trimmed)
		switch fields[0] {
		case "/tree":
			c.notice(c.BranchTreeText())
			return
		case "/branch":
			args := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
			if turn, name, fromTurn, err := ParseBranchTarget(args); err != nil {
				c.notice(err.Error())
			} else if fromTurn {
				if _, err := c.ForkNamed(turn-1, name); err != nil {
					c.notice(err.Error())
				}
			} else {
				if _, err := c.Branch(name); err != nil {
					c.notice(err.Error())
				}
			}
			return
		case "/switch":
			ref := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
			if _, err := c.SwitchBranch(ref); err != nil {
				c.notice(err.Error())
			}
			return
		case "/rewind":
			args := strings.TrimSpace(strings.TrimPrefix(trimmed, fields[0]))
			turn, scope, err := parseRewind(args, c.Checkpoints())
			if err != nil {
				c.notice("usage: /rewind [turn] [code|conversation|both]")
				return
			}
			if err := c.Rewind(turn, scope); err != nil {
				c.notice(err.Error())
			}
			return
		case "/plan-exec":
			c.applyPlanExec(trimmed, display)
			return
		case "/prometheus":
			c.applyPrometheus(trimmed, display)
			return
		}
		if c.managementNotice(trimmed) {
			return
		}
		// A custom command wins over a skill of the same name; both resolve to a
		// turn. (Built-in slash verbs like /compact are handled above.)
		if sent, ok := c.CustomCommand(trimmed); ok {
			c.runGuarded(func(ctx context.Context) error {
				return runGoalLoop(ctx, sent, sent, display)
			})
			return
		}
		if sk, task, ok := c.resolveSkillInvocation(trimmed); ok {
			if sk.RunAs == skill.RunSubagent {
				if strings.TrimSpace(task) == "" {
					c.notice("usage: /" + sk.Name + " <task>")
					return
				}
				c.runSubagentSkillSlash(sk, task, trimmed, display)
				return
			}
			sent := skill.Render(sk, task)
			c.runGuarded(func(ctx context.Context) error {
				return runGoalLoop(ctx, sent, sent, display)
			})
			return
		}
		c.notice("unknown command: " + trimmed)
	default:
		if c.maybeAutoStartResearchGoal(input, display, editedOriginal) {
			return
		}
		runRefTurn(input, display)
	}
}

func (c *Controller) maybeAutoStartResearchGoal(input, display, editedOriginal string) bool {
	goal, ok := c.autoStartResearchGoalCandidate(input)
	if !ok {
		return false
	}
	if c.runner != nil {
		displayText := display
		if strings.TrimSpace(displayText) == "" {
			displayText = goal
		}
		c.runGuarded(func(ctx context.Context) error {
			c.SetGoalWithResearchMode(goal, GoalResearchOn)
			c.notice(fmt.Sprintf(i18n.M.GoalSetFmt, ShortGoalForNotice(goal)))
			block, errs := c.ResolveRefs(ctx, goal)
			for _, e := range errs {
				c.notice(e)
			}
			sent := "Start pursuing the active goal now."
			if block != "" {
				sent = "Referenced context:\n\n" + block + "\n\n" + sent
			}
			if strings.TrimSpace(editedOriginal) != "" {
				return c.runEditedGoalLoopWithRawDisplay(ctx, sent, goal, displayText, editedOriginal)
			}
			return c.runGoalLoopWithRawDisplay(ctx, sent, goal, displayText)
		})
	}
	return true
}

// AutoStartResearchGoal upgrades a strong long-horizon ordinary prompt into a
// Goal + AutoResearch run for frontends that already accepted an idle turn.
func (c *Controller) AutoStartResearchGoal(input string) (string, bool) {
	goal, ok := c.autoStartResearchGoalCandidate(input)
	if !ok {
		return "", false
	}
	c.SetGoalWithResearchMode(goal, GoalResearchOn)
	c.notice(fmt.Sprintf(i18n.M.GoalSetFmt, ShortGoalForNotice(goal)))
	return goal, true
}

func (c *Controller) autoStartResearchGoalCandidate(input string) (string, bool) {
	goal := strings.TrimSpace(input)
	if !shouldAutoStartResearchGoal(goal) {
		return "", false
	}
	c.mu.Lock()
	plan := c.planMode
	running := c.running
	c.mu.Unlock()
	if plan || running || c.goals.active() {
		return "", false
	}
	return goal, true
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

func (c *Controller) applyGoalCommand(input, display string) bool {
	cmd, ok := ParseGoalCommand(input)
	if !ok {
		return false
	}
	switch cmd.Action {
	case GoalCommandSet:
		c.SetPlanMode(false)
		c.SetGoalWithResearchMode(cmd.Text, cmd.ResearchMode)
		c.GoalStrict(cmd.Strict)
		c.notice(fmt.Sprintf(i18n.M.GoalSetFmt, ShortGoalForNotice(cmd.Text)))
		if c.runner != nil {
			c.runGuarded(func(ctx context.Context) error {
				return c.runGoalLoopWithRawDisplay(ctx, "Start pursuing the active goal now.", cmd.Text, display)
			})
		}
	case GoalCommandClear:
		c.ClearGoal()
		c.notice(i18n.M.GoalCleared)
	default:
		goal := c.Goal()
		if strings.TrimSpace(goal) == "" {
			c.notice(i18n.M.GoalEmpty)
		} else {
			c.notice(fmt.Sprintf(i18n.M.GoalCurrentFmt, goal))
		}
	}
	return true
}

// applyPlanExec reads the current canonical todo list and starts a goal that
// analyzes and dispatches independent steps concurrently via parallel_tasks.
// Supports --strict flag: /plan-exec --strict enables strict goal mode.
func (c *Controller) applyPlanExec(input, display string) {
	todos := c.executor.CanonicalTodoState()
	if len(todos) == 0 {
		c.notice("no active plan with todos to execute")
		return
	}

	// Parse --strict flag.
	strict := false
	fields := strings.Fields(input)
	for _, f := range fields {
		if f == "--strict" {
			strict = true
			break
		}
	}

	// Count completion status.
	total := len(todos)
	done := 0
	for _, t := range todos {
		if t.Status == "completed" {
			done++
		}
	}

	var b strings.Builder
	b.WriteString("You are the execution conductor. Route each step to the right sub-agent by module.\n\n")

	// Detect project structure for module-aware routing.
	modules := c.detectProjectModules()
	if len(modules) > 0 {
		b.WriteString("## Project modules detected\n\n")
		for _, m := range modules {
			fmt.Fprintf(&b, "- %s/", m)
		}
		b.WriteString("\n\nRoute steps to the module they belong to. Steps in different modules can run in parallel.\n\n")
	}

	b.WriteString("## Plan steps\n\n")
	for _, t := range todos {
		status := t.Status
		if status == "" {
			status = "pending"
		}
		mark := " "
		if status == "completed" {
			mark = "x"
		}
		fmt.Fprintf(&b, "- [%s] %s (%s)\n", mark, t.Content, status)
	}
	b.WriteString("\n## Routing rules\n")
	b.WriteString("1. Group steps by MODULE \u2014 same module = serial, different modules = parallel batches\n")
	b.WriteString("2. Research/exploration across modules = use parallel_tasks\n")
	b.WriteString("3. Dispatch each batch via parallel_tasks \u2014 each sub-agent gets one module\u2019s context\n")
	b.WriteString("4. Verify each batch before the next\n")
	b.WriteString("5. Failures: fix before moving on\n")
	b.WriteString("\nGoal: each sub-agent focuses on one module and does not carry irrelevant context.\n")
	if done > 0 {
		fmt.Fprintf(&b, "\nNote: %d/%d steps are already completed. Focus on the remaining %d steps.\n", done, total, total-done)
	}
	prompt := b.String()

	// Show module preview.
	if len(modules) > 0 {
		c.notice(fmt.Sprintf("plan-exec: detected %d modules — %s", len(modules), strings.Join(modules, ", ")))
	}

	c.SetPlanMode(false)
	c.SetGoal("execute plan: " + ShortGoalForNotice(todos[0].Content))
	c.GoalStrict(strict)
	c.notice(fmt.Sprintf("plan-exec: dispatching %d plan steps (strict=%v)", total, strict))
	if c.runner != nil {
		c.runGuarded(func(ctx context.Context) error {
			return c.runGoalLoopWithRawDisplay(ctx, prompt, prompt, display)
		})
	}
}

// prometheusPrompt is the strategic planner system prompt.
const prometheusPrompt = "You are Prometheus, a strategic planner. Interview the user one question at a time. Cover: scope, modules, files, constraints, tests. When ready, output a numbered plan with each step tagged by module. End with [goal:complete]. Do not implement.\n\nFor independent research directions, use parallel_tasks before planning."

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
			return c.runGoalLoopWithRawDisplay(ctx, prompt, prompt, display)
		})
	}
}

// shellTimeout is the maximum time a user-invoked "!command" may run. Matches
// the bash tool's timeout so behaviour is consistent across invocation paths.
const shellTimeout = 120 * time.Second

// shellWaitDelay bounds how long cmd.Run() waits after context cancellation for
// the child's pipes to drain, matching the bash tool's WaitDelay.
const shellWaitDelay = 5 * time.Second

// shellWriter forwards each chunk of shell output to a callback, so RunShell
// can stream live progress to the frontend as the command produces output.
type shellWriter struct{ emit func(string) }

func (w *shellWriter) Write(p []byte) (int, error) {
	w.emit(string(p))
	return len(p), nil
}

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

		c.sink.Emit(event.Event{
			Kind: event.ToolDispatch,
			Tool: event.Tool{
				ID:   id,
				Name: "bash",
				Args: fmt.Sprintf(`{"command":%q}`, command),
			},
		})

		ctx, cancel := context.WithTimeout(ctx, shellTimeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.WaitDelay = shellWaitDelay
		cmd.Dir = c.workspaceRoot
		var buf bytes.Buffer
		w := io.MultiWriter(&buf, &shellWriter{emit: func(chunk string) {
			c.sink.Emit(event.Event{
				Kind: event.ToolProgress,
				Tool: event.Tool{ID: id, Output: chunk},
			})
		}})
		cmd.Stdout = w
		cmd.Stderr = w
		start := time.Now()
		_, err := proc.RunCommand(ctx, cmd, proc.RunOptions{
			Track:           true,
			CancelWaitGrace: shellWaitDelay + time.Second,
			Source:          "user_shell",
			ShellKind:       sh.Kind.String(),
			ShellPath:       sh.Path,
			CommandPreview:  diagnosticPreview,
		})
		durationMs := time.Since(start).Milliseconds()
		out := buf.String()

		if ctx.Err() == context.Canceled {
			c.sink.Emit(event.Event{
				Kind: event.ToolResult,
				Tool: event.Tool{ID: id, Name: "bash", Output: out, Err: i18n.M.TurnCancelled, DurationMs: durationMs},
			})
			return nil
		}
		if ctx.Err() == context.DeadlineExceeded {
			c.sink.Emit(event.Event{
				Kind: event.ToolResult,
				Tool: event.Tool{ID: id, Name: "bash", Output: out, Err: fmt.Sprintf(i18n.M.ShellExecTimeoutFmt, shellTimeout), DurationMs: durationMs},
			})
			return nil
		}
		if err != nil {
			c.sink.Emit(event.Event{
				Kind: event.ToolResult,
				Tool: event.Tool{ID: id, Name: "bash", Output: out, Err: fmt.Sprintf(i18n.M.ShellExecFailedFmt, err), DurationMs: durationMs},
			})
			return nil
		}
		c.sink.Emit(event.Event{
			Kind: event.ToolResult,
			Tool: event.Tool{ID: id, Name: "bash", Output: out, DurationMs: durationMs},
		})
		return nil
	})
}

// runRefTurn resolves a line's @references into a context block and starts a
// turn with it prepended (or the raw line when nothing resolved).
func (c *Controller) runRefTurn(input, display string) {
	c.runRefTurnWithRefs(input, input, display)
}

func (c *Controller) runEditedRefTurn(input, display, original string) {
	c.runEditedRefTurnWithRefs(input, input, display, original)
}

func (c *Controller) runScopedRefTurn(input, display string) {
	c.runScopedRefTurnWithRefs(input, input, display)
}

// runRefTurnWithRefs resolves references from refLine while preserving input as
// the user's actual prompt text. This lets compiler diagnostics such as
// "/path/File.kt:12: error" attach @/path/File.kt without rewriting the error.
func (c *Controller) runRefTurnWithRefs(input, refLine, display string) {
	c.runRefTurnWithResolver(input, refLine, display, c.ResolveRefs)
}

func (c *Controller) runEditedRefTurnWithRefs(input, refLine, display, original string) {
	c.runEditedRefTurnWithResolver(input, refLine, display, original, c.ResolveRefs)
}

func (c *Controller) runScopedRefTurnWithRefs(input, refLine, display string) {
	c.runRefTurnWithResolver(input, refLine, display, c.ResolveScopedRefs)
}

func (c *Controller) runRefTurnWithResolver(input, refLine, display string, resolve func(context.Context, string) (string, []string)) {
	c.runGuarded(func(ctx context.Context) error {
		return c.runRefTurnWithResolverSync(ctx, input, refLine, display, "", resolve)
	})
}

func (c *Controller) runEditedRefTurnWithResolver(input, refLine, display, original string, resolve func(context.Context, string) (string, []string)) {
	c.runGuarded(func(ctx context.Context) error {
		return c.runRefTurnWithResolverSync(ctx, input, refLine, display, original, resolve)
	})
}

func (c *Controller) runRefTurnWithResolverSync(ctx context.Context, input, refLine, display, original string, resolve func(context.Context, string) (string, []string)) error {
	block, errs := resolve(ctx, refLine)
	for _, e := range errs {
		c.notice(e)
	}
	sent := input
	if block != "" {
		sent = "Referenced context:\n\n" + block + "\n\n" + input
	}
	if strings.TrimSpace(original) != "" {
		return c.runEditedGoalLoopWithRawDisplay(ctx, sent, input, display, original)
	}
	return c.runGoalLoopWithRawDisplay(ctx, sent, input, display)
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
func (c *Controller) Run(ctx context.Context, input string) error {
	c.maybeSessionStart(ctx)
	parentSession := c.parentSessionID()
	ctx = agent.WithParentSession(ctx, parentSession)
	ctx = jobs.WithSession(ctx, parentSession)
	ctx = agent.WithUserImages(ctx, c.inputImages(input))
	rawInput := input
	input = c.Compose(input)
	startMessages := c.messageCount()
	defer c.snapshotActivityIfChanged(startMessages)
	if c.guardianSess != nil {
		c.guardianSess.ResetTurn()
	}
	if c.hooks.Enabled() {
		c.mu.Lock()
		c.turn++
		turn := c.turn
		c.mu.Unlock()
		if block, _ := c.hooks.PromptSubmit(ctx, input, turn); block {
			return nil
		}
		defer func() { c.hooks.Stop(context.Background(), lastAssistantText(c.History()), turn) }()
	}
	c.markInFlightTurn(startMessages, true)
	defer c.clearInFlightTurn()
	return c.runner.Run(ctx, c.withCapabilityRoute(input, rawInput))
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
	ctx = agent.WithUserImages(ctx, c.inputImages(task))
	ctx = agent.WithResponseLanguagePreference(ctx, c.responseLanguage)
	ctx = agent.WithReasoningLanguagePreference(ctx, c.reasoningLanguage)
	ctx = agent.WithSubagentDepth(ctx, 0)
	answer, err := runner(ctx, sk, task, skill.SubagentRunOptions{HostInitiated: true})
	if err != nil {
		return "", err
	}
	return tool.GuardSubagentHostDecisionText(answer), nil
}

// Cancel aborts the in-flight turn. A goroutine blocked awaiting approval
// unblocks via the cancelled context.
func (c *Controller) Cancel() {
	c.mu.Lock()
	cancel := c.cancel
	if cancel != nil {
		c.canceling = true
	}
	c.mu.Unlock()
	if cancel != nil {
		c.approval.clearAll()
		cancel()
		return
	}
	if c.goals.active() {
		c.stopGoal(GoalStatusStopped)
	}
}

// Running reports whether a turn is currently in flight.
func (c *Controller) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running || c.finishing
}

// beginRotation claims the session-rotation gate. It fails if a turn is running
// or another rotation is already in progress, so the caller holds exclusive
// rights to swap the executor session from the check here through endRotation.
// This closes the TOCTOU window that a bare `if c.running` check left open:
// between that check and the actual SetSession, a turn could start and then be
// yanked out from under the run loop.
func (c *Controller) beginRotation() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running || c.finishing {
		return errTurnRunningRotation
	}
	if c.rotating {
		return errRotationInProgress
	}
	c.rotating = true
	return nil
}

func (c *Controller) endRotation() {
	c.mu.Lock()
	c.rotating = false
	c.mu.Unlock()
}

// CancelRequested reports whether Cancel has been requested for the active turn.
func (c *Controller) CancelRequested() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.canceling
}

// PendingPrompt reports whether the current turn is blocked waiting for a user
// approval, plan approval, memory approval, or ask-tool answer.
func (c *Controller) PendingPrompt() bool {
	return c.approval.hasPending()
}

// RuntimeStatus reports the active work owned by the foreground controller.
func (c *Controller) RuntimeStatus() RuntimeStatus {
	c.mu.Lock()
	running := c.running
	active := running || c.finishing
	canceling := c.canceling
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

// Approve answers a pending ApprovalRequest by ID: allow runs the call, session
// also remembers a grant for the rest of the session so the same approval scope
// is not re-prompted. Unknown/expired IDs are ignored.
func (c *Controller) Approve(id string, allow, session, persist bool) {
	pending := c.approval.resolve(id)
	if pending.reply != nil {
		pending.reply <- approvalReply{allow: allow, session: session, persist: persist} // buffered, never blocks
	}
}

// EnableInteractiveApproval swaps the executor's gate for one that routes
// approval decisions to the frontend via ApprovalRequest events, and wires the
// controller in as the executor's Asker so the `ask` tool can question the user.
// Interactive frontends (chat, desktop) call this; the headless run keeps the
// silent gate and a nil asker from setup.
func (c *Controller) EnableInteractiveApproval() {
	trustGate := planModeReadOnlyTrustApprover{c}
	escapeApprover := sandboxEscapeApprover{c}
	configApprover := managedConfigWriteApprover{c}
	if c.executor != nil {
		c.executor.SetGate(c.newInteractiveGate())
		c.executor.SetPlanModeReadOnlyTrustGate(trustGate)
		c.executor.SetSandboxEscapeApprover(escapeApprover)
		c.executor.SetConfigWriteApprover(configApprover)
		c.executor.SetAsker(c)
	}
	if setter, ok := c.runner.(interface {
		SetPlanModeReadOnlyTrustGate(agent.PlanModeReadOnlyTrustGate)
	}); ok {
		setter.SetPlanModeReadOnlyTrustGate(trustGate)
	}
	if setter, ok := c.runner.(interface {
		SetSandboxEscapeApprover(sandbox.EscapeApprover)
	}); ok {
		setter.SetSandboxEscapeApprover(escapeApprover)
	}
	if setter, ok := c.runner.(interface {
		SetConfigWriteApprover(tool.ConfigWriteApprover)
	}); ok {
		setter.SetConfigWriteApprover(configApprover)
	}
	if setter, ok := c.runner.(interface {
		SetPlannerPlanApprover(agent.PlannerPlanApprover)
	}); ok {
		setter.SetPlannerPlanApprover(plannerPlanApprover{c: c})
	}
	if setter, ok := c.runner.(interface {
		SetPlannerUserDecisionAsker(agent.PlannerUserDecisionAsker)
	}); ok {
		setter.SetPlannerUserDecisionAsker(plannerUserDecisionAsker{c: c})
	}
}

type plannerPlanApprover struct {
	c *Controller
}

func (p plannerPlanApprover) RunWithPlannerApproval(ctx context.Context, plan string, run func(context.Context) error) error {
	c := p.c
	allow, _, err := c.requestApprovalWithReason(ctx, planApprovalTool, "", nil, "Planner requested host approval before execution.")
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

type plannerUserDecisionAsker struct {
	c *Controller
}

func (p plannerUserDecisionAsker) RunWithPlannerUserDecision(ctx context.Context, _ string, question event.AskQuestion, run func(context.Context, string) error) error {
	answers, err := p.c.Ask(ctx, []event.AskQuestion{question})
	if err != nil {
		return err
	}
	answer := plannerUserDecisionAnswer(question, answers)
	if strings.TrimSpace(answer) == "" {
		return nil
	}
	return run(ctx, answer)
}

func plannerUserDecisionAnswer(question event.AskQuestion, answers []event.AskAnswer) string {
	for _, answer := range answers {
		if answer.QuestionID != question.ID {
			continue
		}
		selected := make([]string, 0, len(answer.Selected))
		for _, item := range answer.Selected {
			if s := strings.TrimSpace(item); s != "" {
				selected = append(selected, s)
			}
		}
		return strings.Join(selected, ", ")
	}
	return ""
}

func (c *Controller) newInteractiveGate() *permission.Gate {
	policy := c.policy
	mode := c.approval.mode()
	switch mode {
	case ToolApprovalAuto, ToolApprovalYolo:
		policy.Mode = permission.Allow
	default:
		policy.Mode = permission.Ask
	}
	policy.Ask = append(policy.Ask,
		permission.Rule{Tool: memoryRememberTool},
		permission.Rule{Tool: memoryForgetTool},
	)
	gate := permission.NewGate(policy, gateApprover{c})
	gate.OnRemember = func(rule string) {
		if c.onRemember != nil {
			_ = c.onRemember(rule)
		}
	}
	return gate
}

func (c *Controller) refreshInteractiveGate() {
	if c.executor != nil {
		c.executor.SetGate(c.newInteractiveGate())
	}
}

// Steer queues mid-turn guidance without interrupting the in-flight request.
func (c *Controller) Steer(text string) {
	c.mu.Lock()
	exec := c.executor
	running := c.running
	c.mu.Unlock()
	if exec == nil {
		return
	}
	if running {
		exec.Steer(text)
		return
	}
	// Agent not running — frontend's runningRef was stale.
	// Convert to a new turn so the user gets a response.
	go func() { c.SubmitDisplay(text, text) }()
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

// Ask implements agent.Asker: it emits an AskRequest and blocks until
// AnswerQuestion(ID, …) answers or ctx is cancelled. promptMu serialises it
// against tool-approval prompts so at most one user prompt is outstanding.
// Unlike tool-approval gates, Ask is NOT bypassed in YOLO mode — the `ask`
// tool exists to get a genuine user decision, and YOLO only auto-approves
// tool calls; it must not answer the user's questions for them.
func (c *Controller) Ask(ctx context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error) {
	c.approval.promptMu.Lock()
	defer c.approval.promptMu.Unlock()

	id, reply := c.approval.registerAsk(questions)
	c.sink.Emit(event.Event{Kind: event.AskRequest, Ask: event.Ask{ID: id, Questions: questions}})

	waitCtx, cancelWait := c.approval.waitContext(ctx)
	defer cancelWait()

	select {
	case ans := <-reply:
		return ans, nil
	case <-waitCtx.Done():
		c.approval.cancelAsk(id)
		return nil, waitCtx.Err()
	}
}

// AnswerQuestion resolves a pending AskRequest by ID with the user's selections.
// Unknown/expired IDs are ignored.
func (c *Controller) AnswerQuestion(id string, answers []event.AskAnswer) {
	if pending, ok := c.approval.resolveAsk(id); ok {
		pending.reply <- answers // buffered, never blocks
	}
}

// ReplayPendingPrompts re-emits the ApprovalRequest / AskRequest event for every
// prompt currently blocking the run loop. A frontend that reconnected or reloaded
// after the original event has no way to rebuild its approval/ask modal otherwise,
// so the blocked gate goroutine stays stuck forever while the session shows a
// "waiting" status with no actionable prompt. promptMu serialises Ask and
// requestApproval, so in practice at most one prompt is outstanding; the loops
// stay general so a future concurrent prompt would still replay correctly.
func (c *Controller) ReplayPendingPrompts() {
	approvals, asks := c.approval.snapshotPrompts()
	for _, a := range approvals {
		c.sink.Emit(event.Event{Kind: event.ApprovalRequest, Approval: a})
	}
	for _, a := range asks {
		c.sink.Emit(event.Event{Kind: event.AskRequest, Ask: a})
	}
}

// SetPlanMode flips the executor's read-only gate without touching the
// cache-stable prompt prefix, and remembers the state so Compose can prepend the
// plan-mode marker to outgoing turns.
func (c *Controller) SetPlanMode(v bool) {
	c.mu.Lock()
	c.planMode = v
	c.mu.Unlock()
	if c.executor != nil {
		c.executor.SetPlanMode(v)
	}
	if setter, ok := c.runner.(interface{ SetPlanMode(bool) }); ok {
		setter.SetPlanMode(v)
	}
}

// SetAutoPlan updates the interactive auto-plan gate for subsequent turns.
func (c *Controller) SetAutoPlan(mode string) {
	c.mu.Lock()
	c.autoPlan = normalizeAutoPlan(mode)
	c.mu.Unlock()
}

// SetResponseLanguage updates the final-answer language preference for
// subsequent turns.
func (c *Controller) SetResponseLanguage(lang string) {
	mode := config.NormalizeLanguage(lang)
	c.mu.Lock()
	c.responseLanguage = mode
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
	c.reasoningLanguage = mode
	c.mu.Unlock()
	if setter, ok := c.runner.(interface{ SetReasoningLanguage(string) }); ok {
		setter.SetReasoningLanguage(mode)
	} else if c.executor != nil {
		c.executor.SetReasoningLanguage(mode)
	}
}

// SetMemoryCompilerEnabled updates the Memory v5 runtime for subsequent turns
// without rebuilding the controller or changing the stable provider prefix.
func (c *Controller) SetMemoryCompilerEnabled(enabled bool) {
	if c == nil || c.executor == nil {
		return
	}
	var rt *memorycompiler.Runtime
	if enabled {
		rt = memorycompiler.New(config.MemoryCompilerDir(c.workspaceRoot))
	}
	c.executor.SetMemoryCompiler(rt)
}

func (c *Controller) SetMemoryCompilerVerbosity(verbosity string) {
	if c == nil || c.executor == nil {
		return
	}
	c.executor.SetMemoryCompilerVerbosity(verbosity)
}

// PlanMode reports whether outgoing turns currently receive the plan-mode
// marker. Frontends use it after Compose because auto-plan may flip the mode.
func (c *Controller) PlanMode() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.planMode
}

// GoalStrict enables or disables strict goal mode. In strict mode the agent
// cannot override an incomplete-todo intercept — it must actually finish or
// update all items before [goal:complete] is accepted.
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

func (c *Controller) SetGoalWithResearchMode(goal string, researchMode GoalResearchMode) {
	taskID, blockReason := c.ensureAutoResearchTask(goal, researchMode)
	path, data, ok := c.goals.set(goal, researchMode, taskID, c.goalTodos())
	c.persistGoalState(path, data, ok)
	if blockReason != "" {
		path, data, ok := c.goals.stop(GoalStatusBlocked, c.goalTodos())
		c.persistGoalState(path, data, ok)
		c.notice("autoresearch resume failed: " + blockReason)
	}
}

func (c *Controller) ensureAutoResearchTask(goal string, researchMode GoalResearchMode) (string, string) {
	goal = strings.TrimSpace(goal)
	if goal == "" || c.autoResearch == nil || !shouldUseAutoResearch(goal, researchMode) {
		return "", ""
	}
	currentGoal, currentStatus, _, currentTaskID := c.goals.snapshot()
	if strings.TrimSpace(currentGoal) == goal && currentStatus == GoalStatusRunning && strings.TrimSpace(currentTaskID) != "" {
		return currentTaskID, ""
	}
	if task, ok, err := c.autoResearch.ResumeFromGoalText(goal); err != nil {
		slog.Warn("controller: resume autoresearch task", "err", err)
		if ok {
			return "", err.Error()
		}
	} else if ok {
		c.notice("autoresearch task resumed: " + task.ID)
		return task.ID, ""
	}
	task, err := c.autoResearch.CreateTask(goal, autoresearch.CreateOptions{
		AllowedOperations: autoresearch.AllowedOperations{
			Write:   true,
			Network: false,
			Publish: false,
		},
		SuccessCriteria: defaultAutoResearchSuccessCriteria(),
	})
	if err != nil {
		slog.Warn("controller: create autoresearch task", "err", err)
		return "", ""
	}
	c.notice("autoresearch task created: " + task.ID)
	return task.ID, ""
}

func defaultAutoResearchSuccessCriteria() []autoresearch.SuccessCriterion {
	return []autoresearch.SuccessCriterion{
		{
			ID:          "objective_evidence",
			Description: "The goal outcome is supported by direct evidence, such as inspected code, reproduced behavior, source material, or concrete findings.",
			Required:    true,
		},
		{
			ID:          "verification",
			Description: "The result has relevant verification evidence, such as tests, commands, benchmarks, manual checks, or a documented reason why verification is not applicable.",
			Required:    true,
		},
	}
}

func (c *Controller) appendAutoResearchHeartbeat(taskID, status, message string) {
	if c.autoResearch == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	iteration := 0
	if summary, err := c.autoResearch.Summary(taskID); err == nil {
		iteration = summary.Iteration
	}
	if err := c.autoResearch.AppendHeartbeat(taskID, autoresearch.Heartbeat{
		Status:    status,
		Iteration: iteration,
		Message:   message,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		slog.Warn("controller: append autoresearch heartbeat", "task_id", taskID, "status", status, "err", err)
	}
}

func (c *Controller) autoResearchAcceptedEvidenceIDs(taskID string) map[string]bool {
	if c.autoResearch == nil || strings.TrimSpace(taskID) == "" {
		return nil
	}
	findings, err := c.autoResearch.Findings(taskID, 0)
	if err != nil {
		slog.Warn("controller: read autoresearch findings", "task_id", taskID, "err", err)
		return nil
	}
	accepted := make(map[string]bool, len(findings))
	for _, finding := range findings {
		if finding.Accepted {
			accepted[finding.ID] = true
		}
	}
	return accepted
}

func (c *Controller) recordAutoResearchTurnProgress(taskID string, acceptedBefore map[string]bool) {
	if c.autoResearch == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	acceptedAfter := c.autoResearchAcceptedEvidenceIDs(taskID)
	newAccepted := make([]string, 0)
	for id := range acceptedAfter {
		if acceptedBefore == nil || !acceptedBefore[id] {
			newAccepted = append(newAccepted, id)
		}
	}
	sort.Strings(newAccepted)
	summary := autoResearchDirectionSummary(lastAssistantText(c.History()))
	if _, err := c.autoResearch.RecordDirection(taskID, autoresearch.Direction{
		Summary:             summary,
		AcceptedEvidenceIDs: newAccepted,
		Now:                 time.Now().UTC(),
	}); err != nil {
		slog.Warn("controller: record autoresearch direction", "task_id", taskID, "err", err)
	}
}

func (c *Controller) recordAutoResearchEvidenceFromAssistant(taskID, text string) {
	if c.autoResearch == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	for _, item := range parseAutoResearchEvidenceBlocks(text) {
		if err := c.recordAutoResearchEvidenceForTask(taskID, item.CriterionID, AutoResearchEvidenceInput{
			ID:       item.ID,
			Kind:     item.Kind,
			Summary:  item.Summary,
			Source:   item.Source,
			Command:  item.Command,
			Paths:    append([]string(nil), item.Paths...),
			Accepted: item.Accepted,
		}); err != nil {
			slog.Warn("controller: record autoresearch evidence block", "task_id", taskID, "criterion_id", item.CriterionID, "err", err)
		}
	}
}

type autoResearchEvidenceBlock struct {
	CriterionID string   `json:"criterion_id"`
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Summary     string   `json:"summary"`
	Source      string   `json:"source"`
	Command     string   `json:"command"`
	Paths       []string `json:"paths"`
	Accepted    bool     `json:"accepted"`
}

const (
	autoResearchEvidenceOpen  = "<autoresearch-evidence>"
	autoResearchEvidenceClose = "</autoresearch-evidence>"
)

func parseAutoResearchEvidenceBlocks(text string) []autoResearchEvidenceBlock {
	var out []autoResearchEvidenceBlock
	rest := text
	for {
		start := strings.Index(rest, autoResearchEvidenceOpen)
		if start < 0 {
			return out
		}
		rest = rest[start+len(autoResearchEvidenceOpen):]
		end := strings.Index(rest, autoResearchEvidenceClose)
		if end < 0 {
			return out
		}
		raw := strings.TrimSpace(rest[:end])
		rest = rest[end+len(autoResearchEvidenceClose):]
		if raw == "" {
			continue
		}
		var many []autoResearchEvidenceBlock
		if err := json.Unmarshal([]byte(raw), &many); err == nil {
			out = append(out, many...)
			continue
		}
		var one autoResearchEvidenceBlock
		if err := json.Unmarshal([]byte(raw), &one); err == nil {
			out = append(out, one)
		}
	}
}

func autoResearchDirectionSummary(text string) string {
	text = stripAutoResearchEvidenceBlocks(text)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if line == "" || strings.HasPrefix(lower, "[goal:") {
			continue
		}
		if len(line) > 160 {
			line = line[:160]
		}
		return line
	}
	return "turn completed"
}

func stripAutoResearchEvidenceBlocks(text string) string {
	var b strings.Builder
	rest := text
	for {
		start := strings.Index(rest, autoResearchEvidenceOpen)
		if start < 0 {
			b.WriteString(rest)
			return b.String()
		}
		b.WriteString(rest[:start])
		afterOpen := rest[start+len(autoResearchEvidenceOpen):]
		end := strings.Index(afterOpen, autoResearchEvidenceClose)
		if end < 0 {
			return b.String()
		}
		rest = afterOpen[end+len(autoResearchEvidenceClose):]
	}
}

func (c *Controller) autoResearchReadinessFailure() string {
	taskID := c.goals.currentAutoResearchTaskID()
	if c.autoResearch == nil || strings.TrimSpace(taskID) == "" {
		return ""
	}
	report, err := c.autoResearch.Readiness(taskID)
	if err != nil {
		return "AutoResearch readiness check failed: " + err.Error()
	}
	if report.Ready {
		return ""
	}
	var parts []string
	if len(report.MissingCriteria) > 0 {
		parts = append(parts, "missing criteria: "+strings.Join(report.MissingCriteria, ", "))
	}
	if report.BlockedReason != "" {
		parts = append(parts, "blocked: "+report.BlockedReason)
	}
	if len(report.Errors) > 0 {
		parts = append(parts, "state errors: "+strings.Join(report.Errors, "; "))
	}
	if len(parts) == 0 {
		parts = append(parts, "task is not ready")
	}
	return "AutoResearch readiness check failed: " + strings.Join(parts, "; ")
}

func (c *Controller) AutoResearchSummary() (*autoresearch.Summary, bool) {
	taskID := c.goals.currentAutoResearchTaskID()
	if c.autoResearch == nil || strings.TrimSpace(taskID) == "" {
		return nil, false
	}
	summary, err := c.autoResearch.Summary(taskID)
	if err != nil {
		return &autoresearch.Summary{
			TaskID:  taskID,
			Status:  autoresearch.StatusInvalid,
			Blocker: err.Error(),
		}, true
	}
	return summary, true
}

func (c *Controller) AutoResearchList() ([]autoresearch.Summary, bool) {
	if c.autoResearch == nil {
		return nil, false
	}
	summaries, err := c.autoResearch.ListSummaries()
	if err != nil {
		slog.Warn("controller: list autoresearch tasks", "err", err)
		return nil, true
	}
	return summaries, true
}

func (c *Controller) AutoResearchFindings(limit int) ([]autoresearch.Finding, bool) {
	taskID := c.goals.currentAutoResearchTaskID()
	if c.autoResearch == nil || strings.TrimSpace(taskID) == "" {
		return nil, false
	}
	findings, err := c.autoResearch.Findings(taskID, limit)
	if err != nil {
		return nil, true
	}
	return findings, true
}

func (c *Controller) RecordAutoResearchEvidence(criterionID string, input AutoResearchEvidenceInput) error {
	taskID := c.goals.currentAutoResearchTaskID()
	if c.autoResearch == nil || strings.TrimSpace(taskID) == "" {
		return errors.New("autoresearch: no active task")
	}
	return c.recordAutoResearchEvidenceForTask(taskID, criterionID, input)
}

func (c *Controller) recordAutoResearchEvidenceForTask(taskID, criterionID string, input AutoResearchEvidenceInput) error {
	if c.autoResearch == nil || strings.TrimSpace(taskID) == "" {
		return errors.New("autoresearch: no active task")
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = c.nextAutoResearchFindingID(taskID)
	}
	kind := strings.TrimSpace(input.Kind)
	if kind == "" {
		kind = autoresearch.FindingKindManual
	}
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = autoresearch.FindingSourceManual
	}
	finding := autoresearch.Finding{
		ID:        id,
		Kind:      kind,
		Summary:   strings.TrimSpace(input.Summary),
		Source:    source,
		Command:   strings.TrimSpace(input.Command),
		Paths:     append([]string(nil), input.Paths...),
		Accepted:  input.Accepted,
		CreatedAt: time.Now().UTC(),
	}
	return c.autoResearch.RecordEvidence(taskID, criterionID, finding)
}

func (c *Controller) nextAutoResearchFindingID(taskID string) string {
	findings, err := c.autoResearch.Findings(taskID, 0)
	if err != nil {
		return fmt.Sprintf("f%d", time.Now().UTC().UnixNano())
	}
	used := make(map[string]bool, len(findings))
	for _, finding := range findings {
		used[finding.ID] = true
	}
	for i := 1; ; i++ {
		id := fmt.Sprintf("f%d", len(findings)+i)
		if !used[id] {
			return id
		}
	}
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
	// The run loop is the only sanctioned writer of the live session during a
	// turn; a manual compact would rewrite the log underneath it. The rotation
	// gate (not a bare Running() check) also blocks a turn from starting while
	// the compaction rewrites the session — see beginRotation.
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return fmt.Errorf("cannot compact while a turn is running")
		}
		return err
	}
	defer c.endRotation()
	return c.executor.CompactNow(ctx, instructions)
}

// maybeSessionStart fires the SessionStart hook exactly once per session, lazily
// on the first turn — by then the sink/notify is wired, and a resumed session
// fires it too (its first post-resume turn).
func (c *Controller) maybeSessionStart(ctx context.Context) {
	c.mu.Lock()
	if c.startedOnce {
		c.mu.Unlock()
		return
	}
	c.startedOnce = true
	c.mu.Unlock()
	c.enqueueHookContexts(c.hooks.SessionStart(ctx))
}

// NewSession snapshots the current conversation, rotates to a fresh file, and
// resets the executor to a clean session carrying the same system prompt. It
// ends the old session and starts the new one for lifecycle hooks.
func (c *Controller) NewSession() error {
	if c.executor == nil {
		return nil
	}
	// Claim the rotation gate for the whole snapshot-then-swap sequence. A bare
	// `if c.running` check released before Snapshot() left a window where a turn
	// could start during the snapshot and then have its live session replaced by
	// the SetSession below. Submit ("/new") and the bot gateway call this
	// asynchronously, so the gate is load-bearing, not defensive.
	if err := c.beginRotation(); err != nil {
		return err
	}
	defer c.endRotation()
	if err := c.Snapshot(); err != nil {
		return err
	}
	c.hooks.SessionEnd(context.Background())
	// Hold snapshotMu across the swap so an in-flight save cannot pair the old
	// path with the fresh session (or the fresh path with the old session).
	c.snapshotMu.Lock()
	if c.sessionDir != "" {
		c.mu.Lock()
		c.sessionPath = agent.NewSessionPath(c.sessionDir, c.label)
		c.guardianPath = guardian.PathFor(c.sessionPath)
		c.mu.Unlock()
	}
	c.setActiveJobSession(c.SessionPath())
	c.executor.SetSession(agent.NewSession(c.systemPrompt))
	if c.guardianSess != nil {
		c.guardianSess.Reset()
	}
	c.ResetPlannerSession()
	c.rebindCheckpoints(c.SessionPath())
	c.snapshotMu.Unlock()
	// A new session starts with no active goal: without this, a running goal's
	// text kept injecting into the fresh session's first turns. The old
	// session's goal-state sidecar was persisted before the rotation and stays
	// intact, so resuming it restores its goal; the cleared state below lands
	// on the NEW path (rebindCheckpoints just moved it).
	c.ClearGoal()
	c.mu.Lock()
	c.startedOnce = true // NewSession fires SessionStart itself; don't re-fire on the next turn
	c.mu.Unlock()
	c.enqueueHookContexts(c.hooks.SessionStart(context.Background()))
	return nil
}

// ClearSession discards the current conversation without preserving it in
// resume/history, then rotates to a clean session carrying the same system prompt.
func (c *Controller) ClearSession() error {
	if c.executor == nil {
		return nil
	}
	// Same rotation gate as NewSession: hold it across the whole
	// destroy-then-swap so a turn cannot start during the sequence and have its
	// live session replaced.
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return fmt.Errorf("cannot clear while a turn is running")
		}
		return err
	}
	defer c.endRotation()
	c.mu.Lock()
	oldPath := c.sessionPath
	c.mu.Unlock()
	preMarkedCleanup := c.hasUnfinishedSessionJobs(oldPath)
	if preMarkedCleanup {
		if err := agent.MarkCleanupPending(oldPath, "clear"); err != nil {
			return err
		}
	}
	// Hold snapshotMu from artifact removal through the swap: a save slipping
	// in between would resurrect the just-removed transcript, and one that
	// overlapped the swap could pair the old path with the fresh session.
	c.snapshotMu.Lock()
	destroy := c.BeginDestroySession(oldPath)
	if !destroy.Async {
		if err := removeSessionArtifacts(oldPath); err != nil {
			destroy.Finish()
			c.snapshotMu.Unlock()
			return err
		}
		destroy.Finish()
	}
	c.hooks.SessionEnd(context.Background())
	if c.sessionDir != "" {
		c.mu.Lock()
		c.sessionPath = agent.NewSessionPath(c.sessionDir, c.label)
		c.guardianPath = guardian.PathFor(c.sessionPath)
		c.mu.Unlock()
	}
	c.setActiveJobSession(c.SessionPath())
	c.executor.SetSession(agent.NewSession(c.systemPrompt))
	if c.guardianSess != nil {
		c.guardianSess.Reset()
	}
	c.ResetPlannerSession()
	c.rebindCheckpoints(c.SessionPath())
	c.snapshotMu.Unlock()
	// Same contract as NewSession: the fresh session starts with no active goal.
	c.ClearGoal()
	c.mu.Lock()
	c.startedOnce = true
	c.mu.Unlock()
	c.enqueueHookContexts(c.hooks.SessionStart(context.Background()))
	if destroy.Async {
		go func() {
			result := destroy.Wait()
			if result.HasTimedOut() && destroy.WaitAll != nil {
				if err := agent.MarkCleanupPending(oldPath, "clear"); err != nil {
					c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "mark cleanup pending failed: " + err.Error()})
				}
				destroy.WaitAll()
			}
			if err := removeSessionArtifacts(oldPath); err != nil {
				c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "clear session cleanup failed: " + err.Error()})
			}
			destroy.Finish()
		}()
	}
	return nil
}

func (c *Controller) hasUnfinishedSessionJobs(sessionPath string) bool {
	if c.jobs == nil {
		return false
	}
	return c.jobs.HasUnfinishedForSession(agent.BranchID(sessionPath))
}

func removeSessionArtifacts(path string) error {
	if path == "" {
		return nil
	}
	if err := jobs.RemoveArtifacts(path); err != nil {
		return err
	}
	remove := []string{path}
	// Sidecars include the event log — the authoritative transcript. Leaving
	// it behind would both leak the cleared conversation and let LoadSession
	// resurrect it on the recycled path. The guardian transcript saves through
	// the same session layer, so its sidecars are swept too.
	remove = append(remove, store.SessionSidecarFiles(path)...)
	remove = append(remove, guardian.PathFor(path), guardian.CursorPathFor(path))
	remove = append(remove, store.SessionSidecarFiles(guardian.PathFor(path))...)
	for _, p := range remove {
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
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
func (c *Controller) Checkpoints() []checkpoint.Meta {
	return c.checkpoints.list()
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

// Rewind restores the session to the start of `turn`: Code reverts every file that
// turn (or a later one) changed to its pre-turn content; Conversation truncates the
// message log back to that turn; Both does both. Refused while a turn is running.
// Conversation rewind relies on the live boundary recorded at turn start, so it is
// unavailable for turns inherited from a resumed session (code rewind still works).
// Frontends re-render their transcript from History after the call.
func (c *Controller) Rewind(turn int, scope RewindScope) error {
	if !c.checkpoints.enabled() || c.executor == nil {
		return c.rewindFail(fmt.Errorf("checkpoints unavailable"))
	}
	// Rewind rewrites the live session (conversation scope) and restores files;
	// hold the rotation gate across the whole operation so a turn cannot start
	// between the check and the Replace/SnapshotRewrite below.
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return c.rewindFail(fmt.Errorf("cannot rewind while a turn is running"))
		}
		return c.rewindFail(err)
	}
	defer c.endRotation()
	boundary, hasBound := c.checkpoints.boundary(turn)

	if scope == RewindCode || scope == RewindBoth {
		written, deleted, err := c.checkpoints.restoreCode(turn)
		if err != nil {
			return c.rewindFail(fmt.Errorf("rewind code: %w", err))
		}
		if len(written) > 0 || len(deleted) > 0 {
			c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
				Text: fmt.Sprintf("rewound code to turn %d — %d file(s) restored, %d removed", turn, len(written), len(deleted))})
		}
	}
	if scope == RewindConversation || scope == RewindBoth {
		if !hasBound {
			return c.rewindFail(fmt.Errorf("conversation rewind unavailable for turn %d (resumed session)", turn))
		}
		s := c.executor.Session()
		// boundary is the message-log index at turn start; compaction shrinks the
		// log without rewriting boundaries, so a stale boundary past the end means
		// the turn was compacted away — fail loudly instead of skipping silently.
		// Snapshot/Replace keep the truncation safe against concurrent History/Save
		// readers on other goroutines.
		msgs := s.Snapshot()
		if boundary > len(msgs) {
			return c.rewindFail(fmt.Errorf("conversation rewind unavailable for turn %d: the conversation was compacted past this point", turn))
		}
		s.Replace(msgs[:boundary])
		c.checkpoints.truncateFrom(turn) // renumber future turns from here; later turns are gone
		if err := c.SnapshotRewrite(); err != nil {
			slog.Warn("controller: snapshot after rewind", "err", err)
		}
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
			Text: fmt.Sprintf("rewound conversation to turn %d", turn)})
	}
	return nil
}

// Fork branches the conversation at the start of turn into a NEW session file,
// preserving the current one as the branch point, and switches to the branch. Code
// is untouched (it's a conversation operation). Like a conversation rewind it needs
// the live boundary, so it is unavailable for resumed-session turns and refused
// while a turn runs. Returns the new session path.
func (c *Controller) Fork(turn int) (string, error) {
	return c.ForkNamed(turn, "")
}

func (c *Controller) ForkNamed(turn int, name string) (string, error) {
	return c.forkNamed(turn, name, true)
}

// ForkSession copies the conversation at the start of turn into a new session
// file without switching this controller to it. Desktop uses this to open the
// branch in a new tab while the source tab keeps its current transcript.
func (c *Controller) ForkSession(turn int, name string) (string, error) {
	return c.forkNamed(turn, name, false)
}

func (c *Controller) forkNamed(turn int, name string, switchToFork bool) (string, error) {
	if c.executor == nil {
		return "", c.rewindFail(fmt.Errorf("checkpoints unavailable"))
	}
	if c.sessionDir == "" {
		return "", c.rewindFail(fmt.Errorf("fork needs session persistence, which is disabled"))
	}
	// Hold the rotation gate from before the pre-fork Snapshot through the
	// switch below: a bare Running() check released here would let a turn start
	// during the snapshot and then be switched onto the fork.
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return "", c.rewindFail(fmt.Errorf("cannot fork while a turn is running"))
		}
		return "", c.rewindFail(err)
	}
	defer c.endRotation()
	boundary, hasBound := c.checkpoints.boundary(turn)
	if !hasBound {
		return "", c.rewindFail(fmt.Errorf("fork unavailable for turn %d (resumed session)", turn))
	}

	// Persist the current conversation first so the branch point survives, then
	// seed a fresh session with the messages up to the fork and switch to it.
	if err := c.Snapshot(); err != nil {
		slog.Warn("controller: pre-fork snapshot", "err", err)
	}
	parentPath := c.SessionPath()
	parentID := agent.BranchID(parentPath)
	src := c.executor.Session().Snapshot()
	if boundary > len(src) {
		boundary = len(src)
	}
	forked := append([]provider.Message(nil), src[:boundary]...)
	sess := agent.NewSession("")
	sess.Messages = forked

	newPath := agent.NewSessionPath(c.sessionDir, c.label)
	if err := sess.Save(newPath); err != nil {
		return "", c.rewindFail(err)
	}
	forkPreview, forkTurns := agent.SessionPreviewFromMessages(forked)
	if err := agent.SaveBranchMeta(newPath, agent.BranchMeta{
		Name:             strings.TrimSpace(name),
		ParentID:         parentID,
		ForkTurn:         turn,
		ForkMessageIndex: boundary,
		Preview:          forkPreview,
		Turns:            forkTurns,
		SchemaVersion:    agent.BranchMetaCountsVersion,
	}); err != nil {
		return "", c.rewindFail(err)
	}
	if switchToFork {
		// See snapshotMu: the swap must not interleave with an in-flight save.
		c.snapshotMu.Lock()
		c.executor.SetSession(sess)
		c.ResetPlannerSession()
		c.mu.Lock()
		c.sessionPath = newPath
		c.guardianPath = guardian.PathFor(newPath)
		c.mu.Unlock()
		c.setActiveJobSession(newPath)
		c.rebindCheckpoints(newPath)
		if c.guardianSess != nil {
			c.guardianSess.Reset()
		}
		c.snapshotMu.Unlock()
	}
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("forked conversation at turn %d into a new session", turn)})
	return newPath, nil
}

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

// Branch copies the current conversation into a child branch and switches to it.
// Unlike Fork, it branches at the current tip and does not require a checkpoint.
func (c *Controller) Branch(name string) (string, error) {
	if c.executor == nil {
		return "", c.rewindFail(fmt.Errorf("branch unavailable"))
	}
	if c.sessionDir == "" {
		return "", c.rewindFail(fmt.Errorf("branch needs session persistence, which is disabled"))
	}
	// Hold the rotation gate across the Snapshot and the switch below so a turn
	// cannot start mid-branch and then have its session replaced.
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return "", c.rewindFail(fmt.Errorf("cannot branch while a turn is running"))
		}
		return "", c.rewindFail(err)
	}
	defer c.endRotation()
	if !c.executor.Session().HasContent() {
		return "", c.rewindFail(fmt.Errorf("nothing to branch yet"))
	}
	if err := c.Snapshot(); err != nil {
		return "", c.rewindFail(err)
	}
	parentPath := c.SessionPath()
	parentID := agent.BranchID(parentPath)
	src := c.executor.Session().Snapshot()
	branched := append([]provider.Message(nil), src...)
	sess := agent.NewSession("")
	sess.Messages = branched

	newPath := agent.NewSessionPath(c.sessionDir, c.label)
	if err := sess.Save(newPath); err != nil {
		return "", c.rewindFail(err)
	}
	branchPreview, branchTurns := agent.SessionPreviewFromMessages(branched)
	if err := agent.SaveBranchMeta(newPath, agent.BranchMeta{
		Name:             strings.TrimSpace(name),
		ParentID:         parentID,
		ForkTurn:         -1,
		ForkMessageIndex: len(branched),
		Preview:          branchPreview,
		Turns:            branchTurns,
		SchemaVersion:    agent.BranchMetaCountsVersion,
	}); err != nil {
		return "", c.rewindFail(err)
	}
	// See snapshotMu: the swap must not interleave with an in-flight save.
	c.snapshotMu.Lock()
	c.executor.SetSession(sess)
	c.ResetPlannerSession()
	c.mu.Lock()
	c.sessionPath = newPath
	c.guardianPath = guardian.PathFor(newPath)
	c.mu.Unlock()
	c.setActiveJobSession(newPath)
	c.rebindCheckpoints(newPath)
	if c.guardianSess != nil {
		c.guardianSess.Reset()
	}
	c.snapshotMu.Unlock()
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("created branch %s", agent.BranchID(newPath))})
	return newPath, nil
}

// Branches lists saved conversation branches in this controller's session dir.
func (c *Controller) Branches() ([]agent.BranchInfo, error) {
	if c.sessionDir == "" {
		return nil, fmt.Errorf("session persistence is disabled")
	}
	if err := c.Snapshot(); err != nil {
		return nil, err
	}
	return agent.ListBranches(c.sessionDir)
}

func (c *Controller) SwitchBranch(ref string) (agent.BranchInfo, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return agent.BranchInfo{}, c.rewindFail(fmt.Errorf("usage: /switch <branch id|name>"))
	}
	// Hold the rotation gate across the branch listing/load and the switch so a
	// turn cannot start between the check and the SetSession below.
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return agent.BranchInfo{}, c.rewindFail(fmt.Errorf("cannot switch branches while a turn is running"))
		}
		return agent.BranchInfo{}, c.rewindFail(err)
	}
	defer c.endRotation()
	branches, err := c.Branches()
	if err != nil {
		return agent.BranchInfo{}, c.rewindFail(err)
	}
	match, err := resolveBranch(branches, ref)
	if err != nil {
		return agent.BranchInfo{}, c.rewindFail(err)
	}
	if !agent.IsVisibleSession(match.Path) {
		return agent.BranchInfo{}, c.rewindFail(fmt.Errorf("branch %q not found", ref))
	}
	loaded, err := agent.LoadSession(match.Path)
	if err != nil {
		return agent.BranchInfo{}, c.rewindFail(err)
	}
	// See snapshotMu: the swap must not interleave with an in-flight save.
	c.snapshotMu.Lock()
	if c.executor != nil {
		c.executor.SetSession(loaded)
	}
	c.ResetPlannerSession()
	c.mu.Lock()
	c.sessionPath = match.Path
	c.guardianPath = guardian.PathFor(match.Path)
	c.mu.Unlock()
	c.setActiveJobSession(match.Path)
	c.rebindCheckpoints(match.Path)
	c.restoreTerminalGoalTodos(match.Path)
	c.loadGuardianSession()
	c.snapshotMu.Unlock()
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("switched to branch %s", branchDisplayName(match))})
	return match, nil
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

// SummarizeFrom compresses the conversation from turn onward into one summary;
// SummarizeUpTo compresses everything before it. Both are Claude Code's "summarize
// from/up to here" — they restructure the message log (keeping code untouched), so
// afterwards the per-turn boundaries no longer map and conversation rewind/fork
// report "unavailable" until new turns rebuild them (code rewind, file-based, is
// unaffected). Refused while a turn runs; need the live boundary.
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
	// Summarize rewrites the live session AFTER a provider round-trip, so the
	// bare Running() check left a seconds-wide window for a turn to start and
	// then have the log replaced under it. Hold the rotation gate from the
	// boundary read through the post-rewrite snapshot.
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
	// The log was restructured; existing boundaries no longer map. Drop them (keep
	// the turn counter monotonic so new turns don't collide with the store) —
	// conversation rewind degrades to "unavailable" until fresh turns rebuild them.
	c.checkpoints.clearBounds()
	if err := c.SnapshotRewrite(); err != nil {
		slog.Warn("controller: post-summarize snapshot", "err", err)
	}
	return nil
}

// Resume seeds the session from a loaded transcript and pins the active file to
// its path so auto-save keeps appending there.
func (c *Controller) Resume(s *agent.Session, path string) {
	// See snapshotMu: the swap must not interleave with an in-flight save.
	// recoverInterruptedTurn and maybeColdResumePrune snapshot on their own,
	// so they stay outside the locked section (snapshotMu is not reentrant).
	c.snapshotMu.Lock()
	if c.executor != nil {
		c.executor.SetSession(s)
	}
	c.ResetPlannerSession()
	c.mu.Lock()
	c.sessionPath = path
	c.guardianPath = guardian.PathFor(path)
	c.mu.Unlock()
	c.setActiveJobSession(path)
	c.rebindCheckpoints(path)
	c.goals.restoreRunningFromState(path)
	c.restoreTerminalGoalTodos(path)
	c.loadGuardianSession()
	c.snapshotMu.Unlock()
	c.recoverInterruptedTurn(path)
	c.maybeColdResumePrune(path)
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

// cacheColdAfter approximates how long the provider keeps a prompt prefix
// cached. A session idle longer than this resumes against a cold cache, so a
// history rewrite at that moment costs no extra cache misses — it only shrinks
// the full-price first request. Deliberately conservative: too small burns a
// live cache (~4× the miss tokens, measured), too large only forgoes a prune.
// Tighten from benchmarks/cache-ttl-probe data, never below measured retention.
var cacheColdAfter = 24 * time.Hour

// maybeColdResumePrune elides stale tool results when a resumed session has
// been idle past the provider's cache retention, then persists the pruned
// transcript so the saved file and the prompt stay in sync.
func (c *Controller) maybeColdResumePrune(path string) {
	if c.disableColdResumePrune || c.executor == nil || path == "" {
		return
	}
	// Idle time comes from branch meta only — every session the controller has
	// ever snapshotted carries one. A meta-less transcript (e.g. a legacy import
	// not yet saved) skips the prune until its first snapshot creates the meta.
	m, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok || m.UpdatedAt.IsZero() {
		return
	}
	last := m.UpdatedAt
	if time.Since(last) < cacheColdAfter {
		return
	}
	st, err := c.executor.PruneStaleToolResults()
	if err != nil || st.Results == 0 {
		return
	}
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf(
		"resumed after %s idle (provider cache expired) — elided %d stale tool results to cheapen the cold restart",
		time.Since(last).Round(time.Minute), st.Results)})
	if err := c.SnapshotRewrite(); err != nil {
		slog.Warn("controller: post-prune snapshot", "err", err)
	}
}

// Snapshot writes the executor's conversation to the active session file. No-op
// when the executor is absent or the session has never been used (no user
// interaction). Returns errNoSessionPath when there IS content but no resolved
// path, so a misconfigured deployment surfaces instead of dropping data.
// Called after every turn so a crash loses at most one in-flight prompt.
func (c *Controller) Snapshot() error {
	return c.snapshot(false, false)
}

// SnapshotActivity writes the active conversation and marks the session as
// recently active. Use it only after a real user/model turn changes the
// transcript; switch/close snapshots should call Snapshot so they do not reorder
// recent-session pickers.
func (c *Controller) SnapshotActivity() error {
	return c.snapshot(true, false)
}

// SnapshotRewrite persists an intentional history rewrite, such as rewind or
// manual compaction. Ordinary autosave paths should use Snapshot so stale
// controllers cannot overwrite a newer transcript.
func (c *Controller) SnapshotRewrite() error {
	return c.snapshot(false, true)
}

// midTurnSnapshotInterval is atomic (nanoseconds) so a test shrinking it
// cannot race a previous test's still-parking autosave goroutine.
var midTurnSnapshotInterval atomic.Int64

func init() { midTurnSnapshotInterval.Store(int64(30 * time.Second)) }

// autosaveWhileRunning snapshots the session periodically while a turn runs,
// so an abrupt kill (SSH drop, force-quit) loses at most one interval of a
// long turn instead of all of it (#3772). Session.Save copies under the lock
// and replaces the file atomically, so racing the turn's appends is safe.
func (c *Controller) autosaveWhileRunning(ctx context.Context) {
	t := time.NewTicker(time.Duration(midTurnSnapshotInterval.Load()))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.snapshot(false, false); err != nil {
				slog.Warn("controller: mid-turn snapshot", "err", err)
			}
		}
	}
}

func (c *Controller) snapshot(markActivity, forceRewrite bool) error {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()

	c.mu.Lock()
	path := c.sessionPath
	modelRef := c.modelRef
	c.mu.Unlock()
	if c.executor == nil {
		return nil
	}
	s := c.executor.Session()
	if !s.HasContent() {
		// Nothing to persist yet (e.g. a fresh session with only a system
		// prompt) — staying quiet here is correct, not a data-loss path.
		return nil
	}
	if !s.HasSystemMessage() {
		// The session has user/assistant/tool messages but no leading system
		// prompt.  Persisting it would create a session file that, when
		// reloaded, has no agent-identity contract — the model falls back to
		// its training-data defaults, giving wrong answers to identity
		// queries ("who are you?").  Log the anomaly so the root cause
		// (typically an empty sysPrompt reaching NewSession) can be
		// diagnosed, then refuse to write a corrupted transcript.
		slog.Warn("controller: refusing to snapshot session with content but no system message",
			"label", c.Label(), "session_dir", c.SessionDir(), "message_count", len(s.Snapshot()))
		return nil
	}
	if path == "" {
		// There IS content but nowhere to write it: this silently dropped whole
		// bot conversations (#4414). Surface it loudly instead of returning nil
		// so the missing session path can be diagnosed and fixed at the source.
		slog.Warn("controller: session has content but no session path; conversation will not be persisted",
			"label", c.Label(), "session_dir", c.SessionDir())
		return errNoSessionPath
	}
	forceRewrite = forceRewrite || s.NeedsRewriteSave()
	var err error
	if forceRewrite {
		err = s.SaveRewrite(path)
	} else {
		err = s.SaveSnapshot(path)
		if errors.Is(err, agent.ErrSessionSnapshotConflict) {
			// The no-rewrite decision may already be stale: auto-compaction
			// can rewrite history between the decision and the write. Re-check
			// and retry once as an owned rewrite before treating the failure as
			// a real cross-runtime conflict.
			if s.NeedsRewriteSave() {
				forceRewrite = true
				err = s.SaveRewrite(path)
			}
		}
	}
	if err != nil {
		if !errors.Is(err, agent.ErrSessionSnapshotConflict) {
			return err
		}
		recoveredPath, outcome, recoverErr := c.recoverSnapshotConflict(path, err, forceRewrite)
		if recoverErr != nil {
			return recoverErr
		}
		if outcome == conflictDropped {
			return nil
		}
		// Whatever recovery did — adopted the disk transcript, force-saved
		// the depth-capped branch, or forked — the rewrite baseline lives on
		// the session object and was advanced by the save that succeeded, so
		// there is nothing to re-anchor here.
		path = recoveredPath
		s = c.executor.Session()
	}
	// Persist guardian session so the prefix cache stays warm after restart.
	if c.guardianSess != nil {
		gp := c.guardianPath
		if gp != "" {
			if gerr := c.guardianSess.Save(gp); gerr != nil {
				slog.Warn("controller: guardian snapshot", "err", gerr)
			}
		}
	}
	// Record the listing-only sidecar fields (model, preview, user-turn count)
	// straight from the in-memory conversation, so the sidebar and resume picker
	// never have to decode the whole .jsonl just to show them. markActivity bumps
	// UpdatedAt exactly like the previous TouchBranchMeta did; false preserves it
	// like SetBranchModelPreserveUpdated. The single write subsumes the old
	// EnsureBranchMeta / SetBranchModel / TouchBranchMeta sequence.
	preview, turns := agent.SessionPreviewFromMessages(s.Snapshot())
	if err := agent.UpdateSessionMeta(path, modelRef, preview, turns, markActivity); err != nil {
		return err
	}
	return nil
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

type snapshotConflictDiagnostic struct {
	At               time.Time `json:"at"`
	BranchID         string    `json:"branch_id"`
	Mode             string    `json:"mode"`
	Outcome          string    `json:"outcome"`
	Kind             string    `json:"kind,omitempty"`
	DiskMessages     int       `json:"disk_messages,omitempty"`
	SnapshotMessages int       `json:"snapshot_messages,omitempty"`
	BaseRevision     int64     `json:"base_revision,omitempty"`
	DiskRevision     int64     `json:"disk_revision,omitempty"`
	RecoveryBranchID string    `json:"recovery_branch_id,omitempty"`
	ExistingRecovery bool      `json:"existing_recovery,omitempty"`
}

func appendSnapshotConflictDiagnostic(path, mode, outcome string, saveErr error, recoveryPath string, existing bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	rec := snapshotConflictDiagnostic{
		At:       time.Now(),
		BranchID: agent.BranchID(path),
		Mode:     mode,
		Outcome:  outcome,
	}
	var conflict *agent.SessionSnapshotConflictError
	if errors.As(saveErr, &conflict) && conflict != nil {
		rec.Kind = string(conflict.Kind)
		rec.DiskMessages = conflict.ExistingMessages
		rec.SnapshotMessages = conflict.SnapshotMessages
		rec.BaseRevision = conflict.BaseRevision
		rec.DiskRevision = conflict.DiskRevision
	}
	if recoveryPath != "" {
		rec.RecoveryBranchID = agent.BranchID(recoveryPath)
		rec.ExistingRecovery = existing
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	logPath := store.SessionConflictLog(path)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
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
	// conflictForceSavedBranch: recovery depth was exhausted and the same
	// in-memory session was force-saved onto the same branch; that save
	// advanced the session-owned rewrite baseline like any other full save.
	conflictForceSavedBranch
	// conflictForkedBranch: the same in-memory session moved to a freshly
	// forked recovery branch path.
	conflictForkedBranch
)

const recoveryDepthCapNoticeText = "repeated save conflicts were detected; saved the current conflict copy in place"

func (c *Controller) emitRecoveryDepthCapNotice(path string) {
	key := filepath.Clean(strings.TrimSpace(path))
	c.mu.Lock()
	if c.recoveryDepthCapNotices == nil {
		c.recoveryDepthCapNotices = make(map[string]bool)
	}
	if c.recoveryDepthCapNotices[key] {
		c.mu.Unlock()
		return
	}
	c.recoveryDepthCapNotices[key] = true
	c.mu.Unlock()
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: recoveryDepthCapNoticeText})
}

func (c *Controller) recoverSnapshotConflict(path string, saveErr error, forceRewrite bool) (string, conflictOutcome, error) {
	if c.executor == nil || strings.TrimSpace(path) == "" {
		return "", conflictDropped, saveErr
	}
	mode := "snapshot"
	if forceRewrite {
		mode = "rewrite"
	}
	logAttrs := snapshotConflictLogAttrs(saveErr, path, mode)
	if kind, ok := agent.SnapshotConflictKind(saveErr); ok && kind == agent.SessionSnapshotConflictStalePrefix {
		if c.adoptDiskSession(path) {
			appendSnapshotConflictDiagnostic(path, mode, "adopted_newer_disk_transcript", saveErr, "", false)
			slog.Warn("controller: snapshot conflict; adopted newer disk transcript", logAttrs...)
			c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
				Text: "session changed on disk; adopted the newer transcript"})
			return path, conflictAdoptedDisk, nil
		}
	}
	reason := "snapshot conflict"
	if forceRewrite {
		reason = "rewrite conflict"
	}
	req := SessionRecoveryRequest{OriginalPath: path, Reason: reason, Mode: mode}
	meta := agent.BranchMeta{}
	if c.sessionRecoveryMeta != nil {
		meta = c.sessionRecoveryMeta(req)
	}
	info, err := c.executor.Session().SaveRecoveryBranch(agent.RecoveryBranchOptions{
		OriginalPath: path,
		Reason:       reason,
		BranchMeta:   meta,
	})
	if err != nil {
		if errors.Is(err, agent.ErrSessionRecoveryDepthExceeded) {
			// Saves keep conflicting on recovery branches this runtime itself
			// created; forking again multiplies session files without
			// converging (#5993 reached 8 nested levels). This runtime is the
			// only writer of its own recovery branches, so force-writing the
			// transcript back onto the current branch keeps the data and
			// stops the chain.
			if forceErr := c.executor.Session().Save(path); forceErr != nil {
				return "", conflictDropped, fmt.Errorf("recovery chain depth exceeded; force save failed: %w", forceErr)
			}
			appendSnapshotConflictDiagnostic(path, mode, "recovery_depth_cap_force_saved", saveErr, path, false)
			slog.Warn("controller: snapshot conflict; recovery depth cap reached, force-saved onto current branch", logAttrs...)
			c.emitRecoveryDepthCapNotice(path)
			return path, conflictForceSavedBranch, nil
		}
		if errors.Is(err, agent.ErrSessionRecoveryNotNeeded) {
			if c.adoptDiskSession(path) {
				appendSnapshotConflictDiagnostic(path, mode, "recovery_not_needed_adopted_disk_transcript", saveErr, "", false)
				slog.Warn("controller: snapshot conflict; recovery not needed, adopted disk transcript", logAttrs...)
				c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
					Text: "session changed on disk; adopted the newer transcript (local changes already covered)"})
				return path, conflictAdoptedDisk, nil
			}
			// Nothing was recovered AND the disk transcript could not be
			// adopted: the snapshot is silently dropped. Leave a trace so
			// "my last turns vanished" reports can be tied to this path.
			appendSnapshotConflictDiagnostic(path, mode, "recovery_not_needed_adopt_failed", saveErr, "", false)
			slog.Warn("controller: snapshot conflict; recovery not needed but disk transcript could not be adopted", logAttrs...)
			return "", conflictDropped, nil
		}
		return "", conflictDropped, fmt.Errorf("recover stale session snapshot: %w", err)
	}
	recoveryInfo := SessionRecoveryInfo{
		OriginalPath: path,
		RecoveryPath: info.Path,
		Existing:     info.Existing,
		Reason:       reason,
		Meta:         info.Meta,
	}
	if c.onSessionRecovered != nil {
		if err := c.onSessionRecovered(recoveryInfo); err != nil {
			return "", conflictDropped, fmt.Errorf("commit recovered session: %w", err)
		}
	}
	c.mu.Lock()
	c.sessionPath = info.Path
	c.guardianPath = guardian.PathFor(info.Path)
	c.mu.Unlock()
	c.setActiveJobSession(info.Path)
	c.rebindCheckpoints(info.Path)
	c.transplantInFlightTurnMarker(path, info.Path)
	appendSnapshotConflictDiagnostic(path, mode, "forked_recovery_branch", saveErr, info.Path, info.Existing)
	slog.Warn("controller: snapshot conflict; forked recovery branch",
		append(logAttrs, "recovery", info.Path, "existing", info.Existing)...)
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
		Text: "session changed on disk; unsaved local transcript was saved as a conflict copy"})
	return info.Path, conflictForkedBranch, nil
}

func (c *Controller) adoptDiskSession(path string) bool {
	loaded, err := agent.LoadSession(path)
	if err != nil || loaded == nil {
		return false
	}
	c.executor.SetSession(loaded)
	c.ResetPlannerSession()
	c.rebindCheckpoints(path)
	c.setActiveJobSession(path)
	return true
}

func (c *Controller) messageCount() int {
	if c.executor == nil {
		return 0
	}
	return c.executor.Session().Len()
}

func (c *Controller) markInFlightTurn(startMessageIndex int, preserveUser bool) {
	path := c.SessionPath()
	if path == "" {
		return
	}
	if err := agent.MarkSessionInFlightTurn(path, startMessageIndex, preserveUser); err != nil {
		slog.Warn("controller: mark in-flight turn", "err", err)
	}
}

func (c *Controller) clearInFlightTurn() {
	path := c.SessionPath()
	if path == "" {
		return
	}
	if err := agent.ClearSessionInFlightTurn(path); err != nil {
		slog.Warn("controller: clear in-flight turn", "err", err)
	}
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
	if err := agent.MarkSessionInFlightTurn(toPath, marker.StartMessageIndex, marker.PreserveUser); err != nil {
		// Keep the original marker: a turn boundary on the wrong branch beats
		// no boundary anywhere if the runtime dies before the turn completes.
		slog.Warn("controller: transplant in-flight turn marker", "path", toPath, "err", err)
		return
	}
	if err := agent.ClearSessionInFlightTurn(fromPath); err != nil {
		slog.Warn("controller: clear in-flight turn marker on forked-from branch", "path", fromPath, "err", err)
	}
}

func (c *Controller) recoverInterruptedTurn(path string) {
	if c.executor == nil || path == "" {
		return
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok || meta.InFlightTurn == nil {
		if err != nil {
			slog.Warn("controller: load in-flight turn marker", "err", err)
		}
		return
	}
	marker := meta.InFlightTurn
	if interruptedTurnContinuedOnRecoveryBranch(path, marker) {
		// The "interrupted" turn did not die with a runtime: a recovery branch
		// forked off this session after the marker was set, so the turn kept
		// running (and completing) there. Runtimes predating the marker
		// transplant in recoverSnapshotConflict left the marker behind on the
		// forked-from branch; stripping now would truncate a transcript the
		// completed turn already superseded. Clear the stale marker instead.
		if err := agent.ClearSessionInFlightTurn(path); err != nil {
			slog.Warn("controller: clear fork-orphaned in-flight turn", "err", err)
		}
		return
	}
	msgs := c.executor.Session().Snapshot()
	changed := marker.StartMessageIndex >= 0 && len(msgs) > marker.StartMessageIndex
	if changed {
		if marker.PreserveUser {
			c.stripCancelledVisibleTurnMessagesAfter(marker.StartMessageIndex)
		} else {
			c.stripTurnMessagesAfter(marker.StartMessageIndex)
		}
		if err := c.snapshot(false, true); err != nil {
			slog.Warn("controller: post-interrupted-turn snapshot", "err", err)
		}
	}
	if err := agent.ClearSessionInFlightTurn(path); err != nil {
		slog.Warn("controller: clear stale in-flight turn", "err", err)
	}
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

// stripCancelledVisibleTurnMessagesAfter removes assistant/tool remnants from a
// cancelled visible turn while preserving the real user prompt that started it.
func (c *Controller) stripCancelledVisibleTurnMessagesAfter(idx int) {
	if c.executor == nil {
		return
	}
	msgs := c.executor.Session().Snapshot()
	if len(msgs) <= idx {
		return
	}
	next := append([]provider.Message{}, msgs[:idx]...)
	for _, m := range msgs[idx:] {
		if m.Role != provider.RoleUser {
			continue
		}
		if IsSyntheticUserMessage(m.Content) {
			continue
		}
		if _, ok := agent.SteerText(m.Content); ok {
			continue
		}
		next = append(next, m)
		break
	}
	c.replaceSessionAfterCancel(next)
}

func (c *Controller) replaceSessionAfterCancel(msgs []provider.Message) {
	// The whole cleanup is a save/recovery handoff like snapshot's: hold
	// snapshotMu from the in-memory truncation onward. Truncating outside the
	// lock would let an in-flight save capture the shortened transcript, read
	// the longer partial autosave on disk as a stale-prefix conflict, and
	// adopt it back into the executor — silently undoing the cancel cleanup
	// before the flush below could persist it.
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()
	c.executor.Session().Replace(append([]provider.Message(nil), msgs...))
	// Rebuild canonical todo state from the truncated transcript so
	// Controller.Todos(), goal readiness, and the task panel no longer see
	// the in_progress items written by the cancelled turn.
	c.executor.RebuildTodoState()
	// The mid-turn autosave may have already written a partial transcript to
	// disk. snapshotActivityIfChanged skips the write when messageCount()
	// returns to startMessages, so flush the cleaned transcript here. SaveRewrite
	// still checks that this controller owns the current on-disk baseline before
	// overwriting it, and also covers the edge case where the strip leaves only a
	// system message (HasContent() == false). The path is read under the lock so
	// an in-flight recovery retarget cannot leave it stale.
	c.mu.Lock()
	path := c.sessionPath
	c.mu.Unlock()
	if path != "" {
		if err := c.executor.Session().SaveRewrite(path); err != nil {
			if errors.Is(err, agent.ErrSessionSnapshotConflict) {
				if _, outcome, recoverErr := c.recoverSnapshotConflict(path, err, true); recoverErr != nil {
					slog.Warn("controller: post-cancel transcript recovery", "err", recoverErr)
				} else if outcome == conflictDropped {
					slog.Warn("controller: post-cancel transcript dropped after conflict", "path", path)
				}
			} else {
				slog.Warn("controller: post-cancel transcript flush", "err", err)
			}
		}
	}
}

func (c *Controller) snapshotActivityIfChanged(startMessages int) {
	if c.messageCount() <= startMessages {
		return
	}
	if err := c.SnapshotActivity(); err != nil {
		slog.Warn("controller: activity snapshot", "err", err)
	}
}

// SetSessionPath pins where auto-save lands (a fresh session file minted by the
// caller when no resume path applies).
func (c *Controller) SetSessionPath(p string) {
	// See snapshotMu: the swap must not interleave with an in-flight save.
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()
	c.mu.Lock()
	c.sessionPath = p
	c.guardianPath = guardian.PathFor(p)
	c.mu.Unlock()
	c.setActiveJobSession(p)
	c.rebindCheckpoints(p)
}

// SessionDestroyHandle separates waiting for cancelled jobs from ending the
// destroy window, so callers can move/delete persistent artifacts in between.
type SessionDestroyHandle struct {
	Wait    func() jobs.TeardownResult
	WaitAll func()
	Finish  func()
	Async   bool
}

// BeginDestroySession marks a session as leaving active use and cancels its
// background jobs. Call Wait before moving/deleting artifacts, then Finish after
// persistent cleanup/move work is complete.
func (c *Controller) BeginDestroySession(sessionPath string) SessionDestroyHandle {
	parentSession := agent.BranchID(sessionPath)
	if c.jobs == nil || parentSession == "" {
		wait := func() jobs.TeardownResult { return jobs.TeardownResult{} }
		noop := func() {}
		return SessionDestroyHandle{Wait: wait, WaitAll: noop, Finish: noop}
	}
	teardown := c.jobs.BeginDestroySession(parentSession)
	return SessionDestroyHandle{
		Wait: func() jobs.TeardownResult {
			return c.jobs.WaitTeardown(context.Background(), teardown, c.jobs.TeardownGrace())
		},
		WaitAll: func() {
			for _, ch := range teardown.DoneChannels() {
				<-ch
			}
		},
		Finish: func() {
			c.jobs.FinishDestroySession(parentSession)
		},
		Async: teardown.Async(),
	}
}

// IsDestroyingSession reports whether sessionPath is currently in the destroy
// window for this controller's job manager.
func (c *Controller) IsDestroyingSession(sessionPath string) bool {
	if c.jobs == nil {
		return false
	}
	return c.jobs.IsDestroying(agent.BranchID(sessionPath))
}

func (c *Controller) setActiveJobSession(sessionPath string) {
	if c.jobs != nil {
		c.jobs.SetActiveSessionPath(agent.BranchID(sessionPath), sessionPath)
	}
}

// SessionDir reports the directory new session files land in ("" disables
// persistence), so the caller can decide whether to mint a path.
func (c *Controller) SessionDir() string { return c.sessionDir }

// SessionPath reports the file the current conversation auto-saves to ("" when
// persistence is disabled), so a history view can mark the active session.
func (c *Controller) SessionPath() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionPath
}

func (c *Controller) parentSessionID() string {
	return agent.BranchID(c.SessionPath())
}

// History returns the executor's current message log (for repopulating a
// resumed frontend's view).
func (c *Controller) History() []provider.Message {
	if c.executor == nil {
		return nil
	}
	return c.executor.Session().Snapshot() // copy — a turn may be appending concurrently
}

// ContextSnapshot returns (usedTokens, contextWindow) from the most recent
// turn. Both zero means no data yet — a gauge hides itself.
// usedTokens is promptTokens + completionTokens so the GUI breakdown and
// gauge reflect the full token usage, not just the prompt fill.
func (c *Controller) ContextSnapshot() (int, int) {
	if c.executor == nil {
		return 0, 0
	}
	u := c.executor.LastUsage()
	if u == nil {
		return 0, c.executor.ContextWindow()
	}
	return u.PromptTokens + u.CompletionTokens, c.executor.ContextWindow()
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
	Args   string `json:"args"`
	Output string `json:"output"`
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
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != provider.RoleTool || msgs[i].ToolCallID != toolID {
			continue
		}
		out := &ToolResultData{
			Args:   "",
			Output: msgs[i].Content,
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

// Balance queries the active provider's wallet balance, or (nil, nil) when the
// provider declares no balance_url — so a caller treats "not configured" and
// "fetched" the same and just omits the readout when nil.
func (c *Controller) Balance(ctx context.Context) (*billing.Balance, error) {
	if strings.TrimSpace(c.balanceURL) == "" {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return billing.FetchWithClient(ctx, c.balanceClient, c.balanceURL, c.balanceKey)
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
	cmdSkills := c.SlashSkills()

	entries := make([]command.SlashEntry, 0, len(cmdSkills)+len(cmds))
	for _, sk := range cmdSkills {
		sk := sk
		entries = append(entries, command.SlashEntry{
			Name:        sk.SlashName(),
			Description: sk.Description,
			Render:      func(args []string) string { return skill.Render(sk, strings.Join(args, " ")) },
		})
	}
	for _, cmd := range cmds {
		if cmd.Hidden {
			continue
		}
		cmd := cmd
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

// Skills returns the discoverable skills (for the slash menu and `/skills`).
// When a live Store is available, scan it on demand so skills installed during
// this session appear without rewriting the cache-stable system prompt.
// Executor returns the underlying agent when present (nil for pure runners).
func (c *Controller) Executor() *agent.Agent {
	if c == nil {
		return nil
	}
	return c.executor
}

func (c *Controller) Skills() []skill.Skill {
	return c.skills.list()
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

// DisabledSkills returns all discoverable skills that are disabled in config.
func (c *Controller) DisabledSkills() []skill.Skill {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	var out []skill.Skill
	for _, sk := range c.AllSkills() {
		if cfg.IsSkillDisabled(sk.Name) {
			out = append(out, sk)
		}
	}
	return out
}

// SkillEnabled reports whether a discoverable skill is enabled.
func (c *Controller) SkillEnabled(name string) bool {
	cfg, err := config.Load()
	if err != nil {
		return true
	}
	return !cfg.IsSkillDisabled(name)
}

// SetSkillEnabled persists a skill enable/disable preference. The caller should
// rebuild the controller for the prompt/tool registry to reflect it immediately.
func (c *Controller) SetSkillEnabled(name string, enabled bool) error {
	found := false
	for _, sk := range c.AllSkills() {
		if config.SkillNameKey(sk.Name) == config.SkillNameKey(name) {
			name = sk.Name
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("unknown skill: %s", name)
	}
	// Serialize the load-modify-save against other in-process user-config
	// editors so concurrent writers (bot mapping persistence, desktop
	// settings) don't drop this toggle or lose their own fields.
	unlock := config.LockUserConfigEdits()
	defer unlock()
	cfg := config.LoadForEdit(config.UserConfigPath())
	if err := cfg.SetSkillEnabled(name, enabled); err != nil {
		return err
	}
	return cfg.SaveTo(config.UserConfigPath())
}

// CreateSkill writes a new skill file at the given scope and returns its
// path. Skills()/AllSkills()/RunSkill() read the live store on demand, so the
// new skill is usable (by name) immediately with no rebuild; the caller
// should still rebuild the controller for the pinned Skills index and tool
// registry to reflect it on the model's next turn, mirroring how
// SetSkillEnabled's callers already rebuild after a config change.
func (c *Controller) CreateSkill(name string, scope skill.Scope, content string) (string, error) {
	w := c.skills.writer()
	if w == nil {
		return "", fmt.Errorf("no writable skill store in this session")
	}
	return w.CreateWithContent(name, scope, content)
}

// UpdateSkill overwrites an existing user-authored skill file in place. See
// skill.Store.UpdateContent for the builtin-refusal and scope-match rules.
func (c *Controller) UpdateSkill(name string, scope skill.Scope, content string) error {
	w := c.skills.writer()
	if w == nil {
		return fmt.Errorf("no writable skill store in this session")
	}
	return w.UpdateContent(name, scope, content)
}

// DeleteSkill removes a user-authored skill file at the given scope. See
// skill.Store.Delete for the builtin-refusal and scope-match rules.
func (c *Controller) DeleteSkill(name string, scope skill.Scope) error {
	w := c.skills.writer()
	if w == nil {
		return fmt.Errorf("no writable skill store in this session")
	}
	return w.Delete(name, scope)
}

// HookRunner returns the session's hook runner (nil-safe; may hold zero hooks),
// so a frontend can list the active hooks via `/hooks`.
func (c *Controller) HookRunner() *hook.Runner { return c.hooks }

// AddMCPServer connects an MCP server live and persists it to the config file. Its
// tools are registered immediately and become available on the next turn (the
// agent reads the registry per turn). The raw entry — ${VARS} intact — is what's
// written to disk; the live connection uses the expanded form. Returns the number
// of tools the server exposed. A save failure after a successful connect is
// reported but non-fatal: the server still works this session.
func (c *Controller) AddMCPServer(e config.PluginEntry) (int, error) {
	n, err := c.connectMCPServer(e)
	if err != nil {
		return 0, err
	}
	cfg, lerr := config.Load()
	if lerr != nil {
		return n, fmt.Errorf("connected, but reloading config to save failed: %w", lerr)
	}
	if err := cfg.UpsertPlugin(e); err != nil {
		return n, fmt.Errorf("connected, but config rejected the entry: %w", err)
	}
	if err := cfg.Save(); err != nil {
		return n, fmt.Errorf("connected, but saving config failed: %w", err)
	}
	return n, nil
}

// ConnectMCPServer connects an MCP server entry for this session without writing
// it to config. Desktop owns config placement so it can keep user-level settings
// out of project reasonix.toml while preserving the CLI AddMCPServer semantics.
func (c *Controller) ConnectMCPServer(e config.PluginEntry) (int, error) {
	return c.connectMCPServer(e)
}

// connectMCPServer expands an entry's ${VARS}, applies the known-server
// overrides scoped to the workspace, and connects it live via the mcp manager.
func (c *Controller) connectMCPServer(e config.PluginEntry) (int, error) {
	exp := e.ExpandedPlugin()
	return c.mcp.connectSpec(plugin.ApplyKnownOverrides(plugin.Spec{
		Name:              exp.Name,
		Type:              exp.Type,
		Command:           exp.Command,
		Args:              exp.Args,
		Env:               exp.Env,
		URL:               exp.URL,
		Headers:           exp.Headers,
		ReadOnlyToolNames: trustedReadOnlyToolNames(exp.TrustedReadOnlyTools),
	}, c.WorkspaceRoot()))
}

func trustedReadOnlyToolNames(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}
	out := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ImportMCPEntries persists selected MCP entries and attempts to connect them
// live. A connection failure does not roll back the config import: the user can
// fix local dependencies and reconnect in a later session.
func (c *Controller) ImportMCPEntries(entries []config.PluginEntry) (total, added, updated, connected, failed, skipped int, err error) {
	cfg, lerr := config.Load()
	if lerr != nil {
		return 0, 0, 0, 0, 0, 0, lerr
	}
	existing := make(map[string]bool, len(cfg.Plugins))
	for _, p := range cfg.Plugins {
		existing[p.Name] = true
	}
	for _, e := range entries {
		if existing[e.Name] {
			updated++
		} else {
			added++
		}
		if err := cfg.UpsertPlugin(e); err != nil {
			return 0, 0, 0, 0, 0, 0, err
		}
		existing[e.Name] = true
	}
	if err := cfg.Save(); err != nil {
		return 0, 0, 0, 0, 0, 0, err
	}
	for _, e := range entries {
		if c.mcp.hasServer(e.Name) {
			skipped++
			continue
		}
		if _, err := c.AddMCPServer(e); err != nil {
			failed++
			continue
		}
		connected++
	}
	return len(entries), added, updated, connected, failed, skipped, nil
}

func (c *Controller) ConfiguredMCPNames() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Plugins))
	for _, p := range cfg.Plugins {
		names = append(names, p.Name)
	}
	return names
}

func (c *Controller) DisconnectedMCPNames() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	connected := map[string]bool{}
	for _, name := range c.mcp.serverNames() {
		connected[name] = true
	}
	var names []string
	for _, p := range cfg.Plugins {
		if !connected[p.Name] {
			names = append(names, p.Name)
		}
	}
	return names
}

func (c *Controller) ConnectConfiguredMCPServer(name string) (int, error) {
	cfg, err := config.Load()
	if err != nil {
		return 0, err
	}
	for _, p := range cfg.Plugins {
		if p.Name == name {
			return c.connectMCPServer(p)
		}
	}
	return 0, fmt.Errorf("no configured MCP server named %q", name)
}

// RemoveMCPServer removes a writable MCP configuration before disconnecting the
// live server, so a persistence failure never produces a false-successful
// session-only removal. MCPs contributed by an installed plugin package are
// managed with that package and cannot be removed independently.
func (c *Controller) RemoveMCPServer(name string) (disconnected bool, err error) {
	cfg, lerr := config.LoadForRoot(c.workspaceRoot)
	if lerr != nil {
		return false, lerr
	}
	if owner, ok := cfg.PluginPackageOwner(name); ok {
		return false, fmt.Errorf("MCP server %q is managed by plugin %q; disable or remove the plugin instead", name, owner)
	}
	removed, rerr := config.RemovePluginFromSourcesForRoot(c.workspaceRoot, name)
	if rerr != nil {
		return false, rerr
	}
	if !removed {
		return false, fmt.Errorf("no removable MCP server named %q", name)
	}
	disconnected = c.mcp.disconnect(name)
	if !disconnected {
		c.mcp.removeToolPrefix(name)
	}
	return disconnected, nil
}

// DisconnectMCPServer disconnects a live server for this session without touching
// config — the connector toggle's "off". Its tools vanish next turn; it reconnects
// on the next session start, or now via ConnectConfiguredMCPServer (the "on").
// Reports whether a live server was actually disconnected.
func (c *Controller) DisconnectMCPServer(name string) bool {
	disconnected := c.mcp.disconnect(name)
	removedPlaceholder := 0
	if !disconnected {
		removedPlaceholder = c.mcp.removeToolPrefix(name)
	}
	return disconnected || removedPlaceholder > 0
}

// UnregisterMCPServerTools hides a shared MCP server from this controller only.
// The desktop shared-host path uses this for per-tab connector toggles: the
// shared client stays alive for sibling tabs, while this session's registry drops
// the server's provider-visible tools before the next turn.
func (c *Controller) UnregisterMCPServerTools(name string) bool {
	return c.mcp.suspendToolPrefix(name)
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
		// Seal turn admission and drop anything already parked: a parked turn
		// must not start against a controller that is being torn down, and
		// without the closed flag a submit landing after this critical
		// section (while a running turn's TurnDone delivery is still in
		// flight) would park again and start after teardown.
		c.closed = true
		c.parkedTurns = nil
		c.mu.Unlock()
		if fireSessionEnd && started {
			c.hooks.SessionEnd(context.Background())
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
	})
}

// Jobs returns the still-running background jobs for the status bar (nil when
// background jobs are disabled).
func (c *Controller) Jobs() []jobs.View {
	if c.jobs == nil {
		return nil
	}
	return c.jobs.RunningForSession(c.parentSessionID())
}

// SetToolApprovalMode changes the runtime approval posture for permission-gated
// tools. It does not answer business asks or plan approval.
func (c *Controller) SetToolApprovalMode(mode string) {
	pending := c.approval.setMode(normalizeToolApprovalMode(mode))
	c.refreshInteractiveGate()
	for _, reply := range pending {
		reply <- approvalReply{allow: true}
	}
}

func (c *Controller) ToolApprovalMode() string {
	return c.approval.mode()
}

// SetAutoApproveTools turns YOLO/full-access mode on or off for the session:
// while on, every tool approval request is auto-allowed (writers and bash run
// without asking). Ask requests and plan approval still reach the user. Deny
// rules still block. Runtime-only — never written to config.
func (c *Controller) SetAutoApproveTools(on bool) {
	if on {
		c.SetToolApprovalMode(ToolApprovalYolo)
		return
	}
	c.SetToolApprovalMode(ToolApprovalAsk)
}

// SetBypass is the legacy name for SetAutoApproveTools. Keep it for existing
// desktop/serve bindings and CLI code that still uses the bypass wording.
func (c *Controller) SetBypass(on bool) {
	c.SetAutoApproveTools(on)
}

// SetMode applies plan (read-only) and tool auto-approval together so a turn
// submitted right after a composer mode switch can't observe a half-applied
// gate. Turning tool auto-approval on drains any pending tool approval.
func (c *Controller) SetMode(plan, autoApproveTools bool) {
	c.mu.Lock()
	c.planMode = plan
	c.mu.Unlock()

	if c.executor != nil {
		c.executor.SetPlanMode(plan)
	}
	if autoApproveTools {
		c.SetToolApprovalMode(ToolApprovalYolo)
	} else {
		c.SetToolApprovalMode(ToolApprovalAsk)
	}
}

// AutoApproveTools reports whether YOLO/full-access tool auto-approval is on,
// for status indicators and mode persistence.
func (c *Controller) AutoApproveTools() bool {
	return c.ToolApprovalMode() == ToolApprovalYolo
}

// Bypass is the legacy name for AutoApproveTools.
func (c *Controller) Bypass() bool {
	return c.AutoApproveTools()
}

// --- memory ---
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

// Memory returns the loaded memory snapshot (nil when memory is disabled), for
// frontends that surface a memory panel or the /memory command. The returned
// *Set is immutable — mutations go through QuickAdd / SaveDoc.
func (c *Controller) Memory() *memory.Set {
	return c.memory.current()
}

// --- approval bridge (agent gate → events) ---

// gateApprover adapts the Controller to permission.Approver. It is distinct
// from the public Approve command (different signature, different direction).
type gateApprover struct{ c *Controller }

func (g gateApprover) Approve(ctx context.Context, tool, subject string, args json.RawMessage) (bool, bool, error) {
	allow, remember, _, err := g.ApproveWithReason(ctx, tool, subject, args)
	return allow, remember, err
}

func (g gateApprover) ApproveWithReason(ctx context.Context, tool, subject string, args json.RawMessage) (bool, bool, string, error) {
	subject = approvalDisplaySubject(tool, subject, args)
	// requestApproval short-circuits the YOLO / just-approved-plan window and any
	// session grant before it emits a prompt, so the auto-allow paths need no
	// special-casing here. Deny rules already bit before this point.
	if g.c.guardianSess != nil && !g.c.approval.preApproved(tool, subject) {
		allow, reason, reviewErr := g.c.guardianSess.Review(ctx, tool, args, g.c.executor.Session())
		if reviewErr != nil {
			return false, false, "", reviewErr
		}
		if allow && !requiresFreshApprovalTool(tool) {
			return true, false, "", nil
		}
		humanAllow, remember, err := g.c.requestApprovalWithReason(ctx, tool, subject, args, reason)
		if err != nil {
			return false, false, reason, err
		}
		if !humanAllow {
			return false, false, reason, nil
		}
		return true, remember, "", nil
	}
	allow, remember, err := g.c.requestApproval(ctx, tool, subject, args)
	return allow, remember, "", err
}

type planModeReadOnlyTrustApprover struct{ c *Controller }

type sandboxEscapeApprover struct{ c *Controller }

func (s sandboxEscapeApprover) ApproveSandboxEscape(ctx context.Context, req sandbox.EscapeRequest) (bool, string, error) {
	subject := sandboxEscapeApprovalSubject(req.Command)
	reason := sandboxEscapeApprovalReason(req.Reason)
	reply, err := s.c.requestFreshApprovalDecision(ctx, SandboxEscapeApprovalTool, subject, req.Args, reason)
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
	return s.c.approval.preApprovedForDecision(SandboxEscapeApprovalTool, sandboxEscapeApprovalSubject(req.Command), true)
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
	reply, err := m.c.requestFreshApprovalDecision(ctx, ManagedConfigWriteApprovalTool, subject, args, i18n.M.ConfigWriteReason)
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
	return m.c.approval.preApprovedForDecision(ManagedConfigWriteApprovalTool, managedConfigWriteApprovalSubject(req.Path), true)
}

func managedConfigWriteApprovalSubject(path string) string {
	return i18n.M.ConfigWriteSubjectPrefix + strings.TrimSpace(path)
}

func (p planModeReadOnlyTrustApprover) CheckPlanModeReadOnlyTrust(ctx context.Context, req agent.PlanModeReadOnlyTrustRequest) (bool, string, error) {
	if prefix := normalizePlanModeReadOnlyCommandPrefix(req.Prefix); prefix != "" {
		return p.checkBashReadOnlyCommandTrust(ctx, req, prefix)
	}
	server := strings.TrimSpace(req.ServerName)
	rawTool := strings.TrimSpace(req.RawToolName)
	if server == "" || rawTool == "" {
		return false, i18n.M.PlanModeMCPTrustMetadataMissing, nil
	}
	subject := fmt.Sprintf(i18n.M.PlanModeMCPTrustSubjectFmt, server, rawTool)
	reason := i18n.M.PlanModeMCPTrustReason
	reply, err := p.c.requestFreshApprovalDecision(ctx, req.ToolName, subject, req.Args, reason)
	if err != nil {
		return false, "approval aborted", err
	}
	if !reply.allow {
		return false, i18n.M.PlanModeMCPTrustDeclined, nil
	}
	if reply.session {
		p.c.approval.grantSession(req.ToolName, subject)
	}
	if reply.persist && p.c.onRememberMCPReadOnlyTrust != nil {
		p.c.emitMCPReadOnlyTrustResult(p.c.onRememberMCPReadOnlyTrust(server, rawTool))
	}
	return true, "", nil
}

func (p planModeReadOnlyTrustApprover) checkBashReadOnlyCommandTrust(ctx context.Context, req agent.PlanModeReadOnlyTrustRequest, prefix string) (bool, string, error) {
	if p.c.approval.planModeReadOnlyCommandTrusted(prefix) {
		return true, "", nil
	}
	command := strings.TrimSpace(req.Command)
	if command == "" {
		command = strings.TrimSpace(string(req.Args))
	}
	subject := fmt.Sprintf(i18n.M.PlanModeBashTrustSubjectFmt, prefix, command)
	reason := i18n.M.PlanModeBashTrustReason
	reply, err := p.c.requestFreshApprovalDecision(ctx, agent.PlanModeReadOnlyCommandApprovalTool, subject, req.Args, reason)
	if err != nil {
		return false, "approval aborted", err
	}
	if !reply.allow {
		return false, i18n.M.PlanModeBashTrustDeclined, nil
	}
	if reply.session {
		p.c.approval.grantPlanModeReadOnlyCommand(prefix)
	}
	if reply.persist && p.c.onRememberPlanModeReadOnlyCommand != nil {
		p.c.emitPlanModeReadOnlyCommandTrustResult(p.c.onRememberPlanModeReadOnlyCommand(prefix))
		p.c.approval.grantPlanModeReadOnlyCommand(prefix)
	}
	return true, "", nil
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

type seedTodo struct {
	Content string `json:"content"`
	Status  string `json:"status"`
	Level   int    `json:"level,omitempty"`
}

// seedPlanTodos turns an approved plan into a starter task list and emits it as a
// synthetic todo_write event, so the live task panel populates the instant the
// user approves — a structural guarantee, not a prompt the model might ignore.
// The model still flips item status as it works (only it knows its own
// progress); this just makes the list exist. No-op when the plan has no list.
func (c *Controller) seedPlanTodos(plan string) string {
	args := PlanTodosJSON(plan)
	if args == "" {
		return ""
	}
	t := event.Tool{ID: "plan-seed", Name: "todo_write", Args: args, ReadOnly: true}
	c.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: t})
	t.Output = "task list seeded from the approved plan"
	c.sink.Emit(event.Event{Kind: event.ToolResult, Tool: t})
	c.seedAgentTodoState(args)
	return args
}

func (c *Controller) seedAgentTodoState(args string) {
	if c.executor == nil {
		return
	}
	todos := agentTodoStateFromArgs(args)
	if len(todos) == 0 {
		return
	}
	c.executor.SeedTodoState(todos)
}

func (c *Controller) completePlanTodos(args string) {
	if args == "" {
		return
	}
	done := completedPlanTodosJSON(args)
	if done == "" {
		return
	}
	t := event.Tool{ID: "plan-seed", Name: "todo_write", Args: done, ReadOnly: true}
	c.sink.Emit(event.Event{Kind: event.ToolDispatch, Tool: t})
	t.Output = "approved plan finished"
	c.sink.Emit(event.Event{Kind: event.ToolResult, Tool: t})
	c.replaceAgentTodoState(done)
}

func (c *Controller) replaceAgentTodoState(args string) {
	if c.executor == nil {
		return
	}
	todos := agentTodoStateFromArgs(args)
	if len(todos) == 0 {
		return
	}
	c.executor.ReplaceTodoState(todos)
}

func agentTodoStateFromArgs(args string) []evidence.TodoItem {
	var payload struct {
		Todos []evidence.TodoItem `json:"todos"`
	}
	if err := json.Unmarshal([]byte(args), &payload); err != nil {
		return nil
	}
	return payload.Todos
}

// PlanTodosJSON parses an approved plan's markdown into todo_write-shaped args
// JSON ({"todos":[...]}), or "" when the plan has no list items. The exit_plan_mode
// path seeds via seedPlanTodos (an event); a frontend whose own approval flow
// bypasses exit_plan_mode (the chat TUI's text-plan approval) calls this directly
// to render the same starter checklist. Shared parsing keeps the two consistent.
func PlanTodosJSON(plan string) string {
	items := parsePlanTodos(plan)
	if len(items) == 0 {
		return ""
	}
	b, err := json.Marshal(map[string]any{"todos": items})
	if err != nil {
		return ""
	}
	return string(b)
}

func completedPlanTodosJSON(args string) string {
	var p struct {
		Todos []seedTodo `json:"todos"`
	}
	if err := json.Unmarshal([]byte(args), &p); err != nil || len(p.Todos) == 0 {
		return ""
	}
	for i := range p.Todos {
		p.Todos[i].Status = "completed"
	}
	b, err := json.Marshal(map[string]any{"todos": p.Todos})
	if err != nil {
		return ""
	}
	return string(b)
}

// parsePlanTodos extracts a starter task list from an approved plan's markdown
// list items (bulleted or numbered): the first is in_progress, the rest pending,
// capped so a long plan can't flood the panel. It understands ONLY markdown lists
// — an unambiguous, standard structure — and deliberately does not guess at prose,
// tables, or arrow sequences (those need brittle, language-specific heuristics).
// The plan-mode marker steers the model to present its plan as a list, so this
// catches the normal case; anything it misses is covered by the model's own
// todo_write calls as it executes.
func parsePlanTodos(plan string) []seedTodo {
	var todos []seedTodo
	for _, raw := range strings.Split(plan, "\n") {
		item, level, ok := listItem(raw)
		if !ok {
			continue
		}
		status := "pending"
		if len(todos) == 0 {
			status = "in_progress"
		}
		todos = append(todos, seedTodo{Content: item, Status: status, Level: level})
		if len(todos) >= 20 {
			break
		}
	}
	return todos
}

func (c *Controller) sessionMessageCount() int {
	if c.executor == nil {
		return 0
	}
	return c.executor.Session().Len()
}

// hasTodoUpdateSince reports whether the model emitted its own todo_write after
// index start, so the seeded plan todos aren't auto-completed over the model's
// own bookkeeping.
func (c *Controller) hasTodoUpdateSince(start int) bool {
	if c.executor == nil {
		return false
	}
	msgs := c.executor.Session().Messages
	if start < 0 || start > len(msgs) {
		start = len(msgs)
	}
	_, ok := latestTodoArgsSince(msgs, start)
	return ok
}

func latestTodoArgsSince(msgs []provider.Message, start int) (string, bool) {
	for i := len(msgs) - 1; i >= start; i-- {
		for j := len(msgs[i].ToolCalls) - 1; j >= 0; j-- {
			tc := msgs[i].ToolCalls[j]
			if tc.Name == "todo_write" {
				return tc.Arguments, true
			}
		}
	}
	return "", false
}

// listItem parses a markdown list line ("- x", "* x", "1. x", "2) x") into its
// task text and a nesting level derived from leading indentation (0 for a
// top-level item, 1 for an indented sub-step — capped at 1 since the plan is
// two-level). ok is false when the line isn't a list item. Light inline-markdown
// stripping keeps the checklist readable.
func listItem(line string) (content string, level int, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return "", 0, false
	}
	indent := 0
	for _, c := range line[:len(line)-len(trimmed)] {
		if c == '\t' {
			indent += 4
		} else {
			indent++
		}
	}
	s := trimmed
	// A numbered markdown heading ("### 1. Add the loader") is how models often
	// write a phase even when asked for a list; strip the heading marker and
	// treat it as a top-level phase. A heading without a number (a section
	// title like "## Plan") falls through and is ignored.
	heading := false
	if h := strings.TrimLeft(s, "#"); h != s && strings.HasPrefix(h, " ") {
		heading = true
		s = strings.TrimSpace(h)
	}
	switch {
	case strings.HasPrefix(s, "- "), strings.HasPrefix(s, "* "), strings.HasPrefix(s, "+ "):
		s = s[2:]
	default:
		// numbered: leading digits, then "." or ")", then a space
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == 0 || i+1 >= len(s) || (s[i] != '.' && s[i] != ')') || s[i+1] != ' ' {
			return "", 0, false
		}
		s = s[i+2:]
	}
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[ ] ")
	s = strings.TrimPrefix(s, "[x] ")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, false
	}
	if heading {
		return s, 0, true // a heading is always a top-level phase
	}
	if indent >= 2 {
		return s, 1, true
	}
	return s, 0, true
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

// requestApproval emits an ApprovalRequest and blocks until Approve(ID, …)
// answers or ctx is cancelled. A prior session grant (or a bypass posture) for
// the same approval scope short-circuits. The approvalManager's promptMu
// serialises outstanding prompts; this method keeps the I/O (events, hooks,
// remember) that the manager deliberately stays out of.
func (c *Controller) requestApproval(ctx context.Context, tool, subject string, args json.RawMessage) (bool, bool, error) {
	return c.requestApprovalWithReason(ctx, tool, subject, args, "")
}

func (c *Controller) requestApprovalWithReason(ctx context.Context, tool, subject string, args json.RawMessage, reason string) (bool, bool, error) {
	r, err := c.requestApprovalDecision(ctx, tool, subject, args, reason)
	if err != nil {
		return false, false, err
	}
	// Plan approvals are one-shot — never persist a session grant for them, or
	// every future plan would auto-approve.
	if r.allow && r.session && !requiresFreshApprovalTool(tool) {
		c.approval.grantSession(tool, subject)
	}
	if r.allow && r.persist && !requiresFreshApprovalTool(tool) && c.onRemember != nil {
		c.emitRememberResult(c.onRemember(permission.RememberRuleForScope(tool, subject)))
	}
	return r.allow, false, nil
}

func (c *Controller) requestApprovalDecision(ctx context.Context, tool, subject string, args json.RawMessage, reason string) (approvalReply, error) {
	return c.requestApprovalDecisionWithOptions(ctx, tool, subject, args, reason, approvalDecisionOptions{})
}

func (c *Controller) requestFreshApprovalDecision(ctx context.Context, tool, subject string, args json.RawMessage, reason string) (approvalReply, error) {
	return c.requestApprovalDecisionWithOptions(ctx, tool, subject, args, reason, approvalDecisionOptions{fresh: true})
}

type approvalDecisionOptions struct {
	// fresh marks a user trust/business decision rather than an ordinary tool
	// permission. It may reuse an explicit session grant, but YOLO/auto approval
	// must not answer or drain the prompt.
	fresh bool
}

func (c *Controller) requestApprovalDecisionWithOptions(ctx context.Context, tool, subject string, args json.RawMessage, reason string, opts approvalDecisionOptions) (approvalReply, error) {
	// YOLO/full access and the just-approved-plan execution window auto-allow
	// approval-gated tools without prompting. Plan approval is a user decision,
	// not a tool permission, so it deliberately stays interactive.
	if c.approval.preApprovedForDecision(tool, subject, opts.fresh) {
		return approvalReply{allow: true}, nil
	}

	c.approval.promptMu.Lock()
	defer c.approval.promptMu.Unlock()

	// Re-check: a session grant may have landed while we queued behind another
	// prompt for the same subject.
	if c.approval.preApprovedForDecision(tool, subject, opts.fresh) {
		return approvalReply{allow: true}, nil
	}
	var id string
	var reply chan approvalReply
	if opts.fresh {
		id, reply = c.approval.registerDecision(tool, subject, reason, true)
	} else {
		id, reply = c.approval.register(tool, subject, reason)
	}

	c.sink.Emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: id, Tool: tool, Subject: subject, Reason: reason}})
	if hookSubject, hookArgs, ok := permissionRequestHookPayload(tool, subject, args); ok {
		go c.hooks.PermissionRequest(ctx, tool, hookSubject, hookArgs)
	}
	// The agent now needs the user's attention; a Notification hook can ping an
	// external channel (desktop notice, phone) while the run blocks on the reply.
	go c.hooks.Notification(ctx, approvalNotificationText(tool, subject))

	waitCtx, cancelWait := c.approval.waitContext(ctx)
	defer cancelWait()

	select {
	case r := <-reply:
		return r, nil
	case <-waitCtx.Done():
		c.approval.cancel(id)
		return approvalReply{}, waitCtx.Err()
	}
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

func (c *Controller) emitMCPReadOnlyTrustResult(r MCPReadOnlyTrustResult) {
	server := strings.TrimSpace(r.Server)
	toolName := strings.TrimSpace(r.Tool)
	if r.Err != nil {
		c.sink.Emit(event.Event{
			Kind:  event.Notice,
			Level: event.LevelWarn,
			Text:  fmt.Sprintf(i18n.M.MCPReadOnlyTrustFailedFmt, server, toolName, r.Err),
		})
		return
	}
	switch {
	case r.Saved:
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf(i18n.M.MCPReadOnlyTrustSavedFmt, r.Path, server, toolName)})
	case strings.TrimSpace(r.CoveredBy) != "":
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf(i18n.M.MCPReadOnlyTrustAlreadyFmt, r.Path, server, r.CoveredBy)})
	}
}

func (c *Controller) emitPlanModeReadOnlyCommandTrustResult(r PlanModeReadOnlyCommandTrustResult) {
	prefix := strings.TrimSpace(r.Prefix)
	if r.Err != nil {
		c.sink.Emit(event.Event{
			Kind:  event.Notice,
			Level: event.LevelWarn,
			Text:  fmt.Sprintf(i18n.M.PlanModeReadOnlyCommandTrustFailedFmt, prefix, r.Err),
		})
		return
	}
	switch {
	case r.Saved:
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf(i18n.M.PlanModeReadOnlyCommandTrustSavedFmt, r.Path, prefix)})
	case strings.TrimSpace(r.CoveredBy) != "":
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: fmt.Sprintf(i18n.M.PlanModeReadOnlyCommandTrustAlreadyFmt, r.Path, r.CoveredBy)})
	}
}

// detectProjectModules scans the workspace root for top-level source directories
// to enable module-aware task routing in /plan-exec.
func (c *Controller) detectProjectModules() []string {
	root := c.sessionDir
	for i := 0; i < 3 && root != ""; i++ {
		if hasFile(root, "go.mod") || hasFile(root, "package.json") || hasFile(root, ".git") {
			return listSourceDirs(root, 2)
		}
		root = filepath.Dir(root)
		if root == filepath.Dir(root) {
			break
		}
	}
	return nil
}

func hasFile(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

func listSourceDirs(root string, maxDepth int) []string {
	skip := map[string]bool{
		".git": true, ".github": true, "node_modules": true,
		"vendor": true, ".reasonix": true, "desktop": true,
		"dist": true, "build": true, ".cache": true, "bin": true,
	}
	var dirs []string
	walkDir(root, "", skip, maxDepth, &dirs)
	return dirs
}

func walkDir(root, rel string, skip map[string]bool, depth int, out *[]string) {
	if depth <= 0 {
		return
	}
	dir := root
	if rel != "" {
		dir = filepath.Join(root, rel)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || skip[name] || strings.HasPrefix(name, ".") {
			continue
		}
		childRel := name
		if rel != "" {
			childRel = rel + "/" + name
		}
		if hasSourceFiles(filepath.Join(root, childRel)) {
			*out = append(*out, childRel)
		}
		walkDir(root, childRel, skip, depth-1, out)
	}
}

func hasSourceFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			return true
		}
	}
	return false
}
