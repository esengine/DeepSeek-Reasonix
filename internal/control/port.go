package control

import (
	"context"

	"reasonix/internal/agent"
	"reasonix/internal/billing"
	"reasonix/internal/checkpoint"
	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/hook"
	"reasonix/internal/jobs"
	"reasonix/internal/memory"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
)

// This file defines the driving port: the typed, segregated interface surface
// that frontends (cli, bot, acp, serve) consume instead of coupling to the
// concrete *Controller and its whole method surface. Each depends on the
// sub-ports it actually drives, so the bot never sees checkpoint or memory
// methods; port_segregation_test keeps that true.
//
// The sub-ports are also the intended decomposition boundary for Controller
// itself: the port comes first and gives the later collaborator splits a spec to
// follow. *Controller implements every sub-port (asserted below). The full
// SessionAPI composition will accrete here as the remaining frontends migrate.

// Lifecycle covers a session's identity and lifecycle: minting, resuming,
// clearing, and locating the active session.
type Lifecycle interface {
	NewSession() error
	ClearSession() error
	Resume(s *agent.Session, path string) error
	SetSessionPath(p string)
	SessionPath() string
	SessionDir() string
	Label() string
	ModelRef() string
	WorkspaceRoot() string
	// CapabilityScope describes that workspace the way a capability listing
	// names it: a shell holds several projects at once, so every listing says
	// which folder it is answering for.
	CapabilityScope() CapabilityScope
	Close()
}

// TurnControl covers driving a model turn and observing its run state: the
// various submit/run entry points, cancellation, steering, and status reads.
type TurnControl interface {
	// One way in per shape of turn: the dropped-argument variants stay on
	// *Controller for tests. All of them now take display before input — they
	// are pairs of strings, and the odd one out was a swap no compiler catches.
	SubmitDisplay(display, input string)
	SubmitDeliveryRecovery(display, input string)
	SubmitInvocationDisplay(display, input string, invocations []InvocationRequest)
	SubmitHTTPFormat(input, format string)
	Send(input string)
	SendWithRaw(input, raw string)
	Run(ctx context.Context, input string) error
	RunTurn(ctx context.Context, input string) error
	RunShell(command string)
	Cancel()
	Steer(text string)
	SteerConsumed() bool
	Running() bool
	CancelRequested() bool
	RuntimeStatus() RuntimeStatus
	Turn() int
	History() []provider.Message
	ToolResult(toolID string) *ToolResultData
}

// Approvals covers tool-approval and ask prompts plus the runtime approval
// posture (ask/auto/yolo). It mirrors the approvalManager surface.
type Approvals interface {
	Approve(id string, allow, session, persist bool)
	// ResolveRecovery answers an Auto Guard card: continue|continue_task|revise. Revise
	// refuses the mutation and steers feedback.
	ResolveRecovery(id string, action agent.RecoveryAction, feedback string) error
	AnswerQuestion(id string, answers []event.AskAnswer)
	Ask(ctx context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error)
	ReplayPendingPrompts()
	ReplayPendingPromptsWith(sinkFactory func() event.Sink)
	PendingPrompt() bool
	EnableInteractiveApproval()
	ToolApprovalMode() string
	SetToolApprovalMode(mode string)
	AutoApproveTools() bool
	SetAutoApproveTools(on bool)
	Bypass() bool
	SetBypass(on bool)
	SetMode(plan, autoApproveTools bool)
}

// PlanDecisions answers the plan card. It is apart from Approvals because a
// plan decision is a workflow transition rather than a permission answer, and
// only a frontend that draws the three outcomes drives it — the chat gateway
// and the editor bridge have no screen for revising a plan.
type PlanDecisions interface {
	ResolvePlanDecision(id string, action PlanDecisionAction) error
}

// Goals covers the active-goal FSM and plan mode.
type Goals interface {
	Goal() string
	GoalStatus() string
	SetGoal(goal string)
	// SetGoalWithResearchMode is retained for deprecated CLI budget flags. The
	// mode is translated at the boundary and is not stored in the Goal runtime.
	SetGoalWithResearchMode(goal string, researchMode GoalResearchMode)
	ResumeGoal() bool
	PauseGoal() bool
	GoalRuntime() GoalRuntimeView
	GoalStrict(strict bool)
	ClearGoal()
	ResetPlannerSession()
	PlanMode() bool
	SetPlanMode(v bool)
	// AgentPreset is the session role setting (balanced|delivery).
	AgentPreset() string
	// SetAgentPreset updates the role setting for subsequent turns without
	// rebuilding the controller.
	SetAgentPreset(preset string)
}

// SessionHistory covers checkpoint/rewind, branch/fork, and the log-restructuring
// operations (compact, summarize).
type SessionHistory interface {
	// Adjudications is what the host asked a person and how it ended: an
	// interruption still owed, and the resolved, cancelled or superseded
	// record behind it. History, not a prompt — nothing here is answerable.
	Adjudications() (active, history []AdjudicationEntry)
	// ExecutionGraph is the run graph recomputed from what survived. It is the
	// authority a reader starts from; the delta stream carries the same facts
	// live, and is never a history to replay into a state.
	ExecutionGraph() ExecutionGraphSnapshot
	Checkpoints() []checkpoint.Meta
	CheckpointFileState(path string) (checkpoint.FileState, bool)
	CheckpointTurnsByMessageIndex() map[int]int
	CheckpointHasBoundary(turn int) bool
	Rewind(turn int, scope RewindScope) error
	PrepareRewind(turn int, scope RewindScope) (checkpoint.RewindPlan, error)
	CommitRewind(planID string) (checkpoint.RewindResult, error)
	UndoRewind(transactionID string) (checkpoint.RewindResult, error)
	PrepareFileRevert(path string) (checkpoint.RewindPlan, error)
	CommitFileRevert(planID string, resolution checkpoint.ConflictResolution) (checkpoint.RewindResult, error)
	Branch(name string) (string, error)
	Branches() ([]agent.BranchInfo, error)
	BranchTreeText() string
	SwitchBranch(ref string) (agent.BranchInfo, error)
	Compact(ctx context.Context, instructions string) error
	CompactRatio() float64
	ContextReport() (summary, detail string)
	SummarizeFrom(ctx context.Context, turn int) error
	SummarizeUpTo(ctx context.Context, turn int) error
}

// MemoryControl covers session/project memory reads and mutations.
type MemoryControl interface {
	Memory() *memory.Set
	QuickAdd(scope memory.Scope, note string) (string, error)
	SaveDoc(path, body string) (string, error)
	SaveMemory(m memory.Memory) (string, error)
	ForgetMemory(name string) error
	QueueMemory(note string)
	MemoryRevisions(ref string) []memory.Memory
	RestoreMemory(ref string, revision int) (memory.Memory, error)
	RestoreArchivedMemory(archivePath string) (memory.Memory, error)
	LastMemoryRecall() memory.RecallResult
}

// The capability surface is one dispatcher and five listings, kept apart
// because frontends drive them apart: an editor resolves slash input and never
// edits a permission rule; a skills pane never reconnects an MCP server.

// SlashDispatch resolves a typed line into whatever runs it — a custom command,
// an MCP prompt, or a skill — and enumerates what is on offer.
type SlashDispatch interface {
	Commands() []command.Command
	ReloadCommands(ctx context.Context) error
	SlashSkills() []skill.Skill
	CustomCommand(input string) (sent string, found bool)
	MCPPrompt(ctx context.Context, input string) (sent string, found bool, err error)
	RunSkill(input string) (sent string, found bool)
}

// Skills covers the session's skill catalog and what a pane may change about
// it: activation per scope, and authoring.
type Skills interface {
	Skills() []skill.Skill
	AllSkills() []skill.Skill
	DisabledSkills() []skill.Skill
	SkillEnabled(name string) bool
	SkillOverrideScope(name string) (config.ActivationScope, bool)
	SetSkillEnabled(name string, scope config.ActivationScope, enabled bool) error
	ClearSkillOverride(name string, scope config.ActivationScope) error
	ImplicitSkillInvocationEnabled() bool
	CreateSkill(name string, scope skill.Scope, content string) (string, error)
	UpdateSkill(name string, scope skill.Scope, content string) error
	DeleteSkill(name string, scope skill.Scope) error
}

// Hooks covers the session's lifecycle hooks: the live runner, what is
// configured, and editing or dry-running a config.
type Hooks interface {
	HookRunner() *hook.Runner
	InspectHooks() hook.Inspection
	SaveHooks(scope hook.Scope, settings hook.Settings) error
	DryRunHook(ctx context.Context, cfg hook.HookConfig, event hook.Event) (hook.DryRunResult, error)
	HookSettingsPath(scope hook.Scope) string
}

// MCPControl covers the live MCP surface: the running host, and adding,
// connecting, enabling, and removing servers.
type MCPControl interface {
	Host() *plugin.Host
	AddMCPServer(e config.PluginEntry) (int, error)
	ConnectMCPServer(e config.PluginEntry) (int, error)
	RegisterMCPServerOnDemand(e config.PluginEntry) (int, error)
	ConnectConfiguredMCPServer(name string) (int, error)
	InstallMCPServer(e config.PluginEntry, scope MCPScope) (plugin.MCPInstallResult, error)
	ReconnectMCPServer(name string) (int, error)
	MCPServerEnabled(name string) (bool, error)
	SetMCPServerEnabled(name string, scope config.ActivationScope, enabled bool) error
	ClearMCPServerOverride(name string, scope config.ActivationScope) error
	DisconnectMCPServer(name string) bool
	RemoveMCPServer(name string) (disconnected bool, err error)
	ConfiguredMCPNames() []string
	ConfiguredMCPServers() []MCPServerState
	MCPCatalogTools() map[string]int
	DisconnectedMCPNames() []string
	UnregisterMCPServerTools(name string) bool
	ImportMCPEntries(entries []config.PluginEntry) (total, added, updated, connected, failed, skipped int, err error)
}

// RuntimeSettings covers the machine-facing settings a session runs under —
// network, shell, permissions, sandbox — plus repairing a config file that
// failed to parse. None of it is a capability listing, so a pane that lists
// skills or MCP servers cannot reach a permission rule from the same port.
type RuntimeSettings interface {
	NetworkSettings() NetworkSettings
	SaveNetworkSettings(in NetworkSettings, password string, clearPassword bool) error
	DiagnoseNetwork(ctx context.Context) []NetworkProbe
	ShellSettings() ShellSettings
	SaveShellSettings(prefer, path string) error
	PermissionRules() PermissionRules
	SavePermissionRules(in PermissionLists) error
	SandboxSettings() SandboxSettings
	SaveSandboxSettings(in SandboxSettings) error
	CompactionSettings() CompactionSettings
	SaveCompactionSettings(softLimitTokens int) error
	ConfigProblem() *ConfigProblem
	RepairConfigFile() (string, error)
}

// Extensions covers what handshake-declared sidecars contribute to a frontend:
// their actions, the form values coming back, and the providers they add.
type Extensions interface {
	// Extension UI: enumerate handshake-declared extension actions, invoke one
	// by its public /<plugin>:<action> name, and deliver a published form's
	// values back to the owning sidecar. Nil hub → empty / error.
	ExtensionActions() []ExtensionActionView
	InvokeExtensionAction(ctx context.Context, name string, args map[string]string) (string, error)
	SubmitExtensionForm(ctx context.Context, pluginID, surfaceID string, values map[string]any) error
	// ProviderCatalog is the session's merged provider catalog — config/broker
	// base plus sidecar-declared extension providers (plugin/... refs). Nil
	// when no extension declared providers; frontends merge it into their
	// model pickers and skip nil.
	ProviderCatalog() []provider.Descriptor
}

// Capabilities is the whole pluggable surface, for a frontend that drives all
// of it. One that drives part of it names that part instead.
type Capabilities interface {
	SlashDispatch
	Skills
	Hooks
	MCPControl
	RuntimeSettings
	Extensions
}

// Status covers read-only run/usage/billing telemetry and task list state.
type Status interface {
	ContextSnapshot() (int, int)
	ContextBreakdown() agent.ContextBreakdown
	ContextMaintenanceSnapshot() agent.ContextMaintenanceSnapshot
	LastUsage() *provider.Usage
	Balance(ctx context.Context) billing.Reading
	Jobs() []jobs.View
	Todos() []evidence.TodoItem
}

// SessionPersistence covers snapshotting a session and tearing down its on-disk
// state.
type SessionPersistence interface {
	Snapshot() error
	SnapshotForShutdown() error
	SnapshotActivity() error
	// SessionHasUnsavedChanges reports whether the in-memory transcript is
	// newer than the durable session file. Frontends use this to avoid
	// replacing a failed/contended save with stale disk history.
	SessionHasUnsavedChanges() bool
	SessionCache() (hit, miss int)
	BeginDestroySession(sessionPath string) SessionDestroyHandle
	CloseAfterDestroy()
	ReleaseResources()
}

// Input covers composing a turn's text (plan/goal/memory injection) and
// resolving @-references before submission — including what the composer can
// still offer to complete while the line is being typed.
type Input interface {
	Compose(text string) string
	ComposeSynthetic(text string) string
	ResolveRefs(ctx context.Context, line string) (block string, errs []string)
	HasRefs(line string) bool
	CompletionData() CompletionData
	ImageInputEnabled() bool
	DroppedRef(path string) (token, displayPath string, err error)
}

// Settings covers runtime session settings that don't fit a richer domain.
type Settings interface {
	SetResponseLanguage(lang string)
	SetReasoningLanguage(lang string)
	SetDisplayRecorder(fn func(content, display string))
}

// SessionAPI is the full driving port — the composition of every sub-port, for
// a frontend that drives all of it: the TUI does, and the HTTP server all but
// one. Leaner frontends name GatewayAPI or EditorAPI instead.
type SessionAPI interface {
	Lifecycle
	TurnControl
	Approvals
	PlanDecisions
	Goals
	SessionHistory
	MemoryControl
	Capabilities
	Status
	SessionPersistence
	Input
	Settings
	Inbox
}

// GatewayAPI is what a chat gateway drives: messages in, queued, run as turns,
// approvals answered. What it does not name is the point — no transcript, no
// memory pane, no checkpoint UI, so it cannot reach for them by accident.
type GatewayAPI interface {
	Lifecycle
	TurnControl
	Approvals
	Inbox
}

// EditorAPI is what an editor integration drives over ACP: a gateway's four,
// plus the slash dispatch, MCP surface and sidecar extensions it puts on
// screen, and the goals and persistence it shows. What it does not name is the
// point — an editor authors no skill, edits no hook, and saves no sandbox rule.
type EditorAPI interface {
	Lifecycle
	TurnControl
	Approvals
	Inbox
	SlashDispatch
	MCPControl
	Extensions
	Goals
	SessionPersistence
}

// Compile-time proof that the concrete controller satisfies each sub-port and
// the full port, so frontend migrations to the interfaces are mechanical and can
// never silently drift from the implementation.
var (
	_ GatewayAPI         = (*Controller)(nil)
	_ EditorAPI          = (*Controller)(nil)
	_ Lifecycle          = (*Controller)(nil)
	_ TurnControl        = (*Controller)(nil)
	_ Approvals          = (*Controller)(nil)
	_ Goals              = (*Controller)(nil)
	_ SessionHistory     = (*Controller)(nil)
	_ MemoryControl      = (*Controller)(nil)
	_ SlashDispatch      = (*Controller)(nil)
	_ Skills             = (*Controller)(nil)
	_ Hooks              = (*Controller)(nil)
	_ MCPControl         = (*Controller)(nil)
	_ RuntimeSettings    = (*Controller)(nil)
	_ Extensions         = (*Controller)(nil)
	_ Capabilities       = (*Controller)(nil)
	_ Status             = (*Controller)(nil)
	_ SessionPersistence = (*Controller)(nil)
	_ Input              = (*Controller)(nil)
	_ Settings           = (*Controller)(nil)
	_ Inbox              = (*Controller)(nil)
	_ SessionAPI         = (*Controller)(nil)
)
