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
// that frontends (cli, desktop, bot, acp, serve) consume instead of coupling to
// the concrete *Controller and its ~99 methods. Each frontend depends only on
// the sub-ports it actually uses (interface segregation), so e.g. the bot never
// sees checkpoint or memory methods.
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
	Resume(s *agent.Session, path string)
	SetSessionPath(p string)
	SessionPath() string
	SessionDir() string
	Label() string
	WorkspaceRoot() string
	Close()
}

// TurnControl covers driving a model turn and observing its run state: the
// various submit/run entry points, cancellation, steering, and status reads.
type TurnControl interface {
	Submit(input string)
	SubmitDisplay(display, input string)
	SubmitHTTP(input string)
	SubmitUserTurn(input, display string)
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
	AnswerQuestion(id string, answers []event.AskAnswer)
	Ask(ctx context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error)
	ReplayPendingPrompts()
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

// Goals covers the active-goal FSM and plan mode.
type Goals interface {
	Goal() string
	GoalStatus() string
	SetGoal(goal string)
	SetGoalWithResearchMode(goal string, researchMode GoalResearchMode)
	GoalStrict(strict bool)
	ClearGoal()
	AutoStartResearchGoal(input string) (string, bool)
	ResetPlannerSession()
	PlanMode() bool
	SetPlanMode(v bool)
	SetAutoPlan(mode string)
}

// SessionHistory covers checkpoint/rewind, branch/fork, and the log-restructuring
// operations (compact, summarize).
type SessionHistory interface {
	Checkpoints() []checkpoint.Meta
	CheckpointTurnsByMessageIndex() map[int]int
	CheckpointHasBoundary(turn int) bool
	Rewind(turn int, scope RewindScope) error
	Fork(turn int) (string, error)
	ForkNamed(turn int, name string) (string, error)
	ForkSession(turn int, name string) (string, error)
	Branch(name string) (string, error)
	Branches() ([]agent.BranchInfo, error)
	BranchTreeText() string
	SwitchBranch(ref string) (agent.BranchInfo, error)
	Compact(ctx context.Context, instructions string) error
	CompactRatio() float64
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
}

// Capabilities covers the session's pluggable surface — MCP servers, skills,
// slash commands, hooks — and resolving prompt/command/skill inputs.
type Capabilities interface {
	Host() *plugin.Host
	Commands() []command.Command
	ReloadCommands(ctx context.Context) error
	Skills() []skill.Skill
	AllSkills() []skill.Skill
	DisabledSkills() []skill.Skill
	SkillEnabled(name string) bool
	SetSkillEnabled(name string, enabled bool) error
	HookRunner() *hook.Runner
	CustomCommand(input string) (sent string, found bool)
	MCPPrompt(ctx context.Context, input string) (sent string, found bool, err error)
	RunSkill(input string) (sent string, found bool)
	AddMCPServer(e config.PluginEntry) (int, error)
	ConnectMCPServer(e config.PluginEntry) (int, error)
	ConnectConfiguredMCPServer(name string) (int, error)
	DisconnectMCPServer(name string) bool
	RemoveMCPServer(name string) (disconnected bool, err error)
	ConfiguredMCPNames() []string
	DisconnectedMCPNames() []string
	UnregisterMCPServerTools(name string) bool
	ImportMCPEntries(entries []config.PluginEntry) (total, added, updated, connected, failed, skipped int, err error)
}

// Status covers read-only run/usage/billing telemetry and task list state.
type Status interface {
	ContextSnapshot() (int, int)
	LastUsage() *provider.Usage
	Balance(ctx context.Context) (*billing.Balance, error)
	Jobs() []jobs.View
	Todos() []evidence.TodoItem
}

// SessionPersistence covers snapshotting a session and tearing down its on-disk
// state.
type SessionPersistence interface {
	Snapshot() error
	SnapshotActivity() error
	SessionCache() (hit, miss int)
	BeginDestroySession(sessionPath string) SessionDestroyHandle
	CloseAfterDestroy()
	IsDestroyingSession(sessionPath string) bool
	ReleaseResources()
}

// Input covers composing a turn's text (plan/goal/memory injection) and
// resolving @-references before submission.
type Input interface {
	Compose(text string) string
	ComposeSynthetic(text string) string
	ResolveRefs(ctx context.Context, line string) (block string, errs []string)
	HasRefs(line string) bool
	ImageInputEnabled() bool
	RegisterExternalFolderRef(path string) (token, displayPath string, err error)
}

// Settings covers runtime session settings that don't fit a richer domain.
type Settings interface {
	SetResponseLanguage(lang string)
	SetReasoningLanguage(lang string)
	SetMemoryCompilerEnabled(enabled bool)
	SetDisplayRecorder(fn func(content, display string))
}

// SessionAPI is the full driving port — the composition of every sub-port. A
// rich frontend (the HTTP server, the desktop app, the TUI) depends on this;
// leaner frontends (bot, acp) depend on just the sub-ports they use.
type SessionAPI interface {
	Lifecycle
	TurnControl
	Approvals
	Goals
	SessionHistory
	MemoryControl
	Capabilities
	Status
	SessionPersistence
	Input
	Settings
}

// Compile-time proof that the concrete controller satisfies each sub-port and
// the full port, so frontend migrations to the interfaces are mechanical and can
// never silently drift from the implementation.
var (
	_ Lifecycle          = (*Controller)(nil)
	_ TurnControl        = (*Controller)(nil)
	_ Approvals          = (*Controller)(nil)
	_ Goals              = (*Controller)(nil)
	_ SessionHistory     = (*Controller)(nil)
	_ MemoryControl      = (*Controller)(nil)
	_ Capabilities       = (*Controller)(nil)
	_ Status             = (*Controller)(nil)
	_ SessionPersistence = (*Controller)(nil)
	_ Input              = (*Controller)(nil)
	_ Settings           = (*Controller)(nil)
	_ SessionAPI         = (*Controller)(nil)
)
