package boot

import (
	"io"
	"reasonix/internal/state/sessionstore"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/platform/browser"
	"reasonix/internal/platform/feedback"
	"reasonix/internal/platform/gitcmd"

	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/surface"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/model/billing"
	"reasonix/internal/runtime/taskmonitor"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessiontemp"
	"reasonix/internal/tools/builtin"
)

// Options carries the per-run knobs a frontend chooses; everything else is read
// from configuration. Model "" takes the configured default_model, MaxSteps 0
// automatic execution. RequireKey forces the executor's key to be present:
// run/serve pass true so a missing key fails fast, chat/desktop false so the UI
// is reachable before one is set.
type Options struct {
	// Version is the host declaring a real installation, which is what lets an
	// assembly record the last-known-good config snapshot recovery reads back.
	Version     string
	Model       string
	MaxSteps    int
	MaxStepsKey string
	RequireKey  bool
	Sink        event.Sink
	// OpenOnFallbackModel lets an unnamed build pass over a default_model nothing
	// configured serves. Only a window sets it: it must open to be fixed, while a
	// headless run must not move to another provider and its billing unasked.
	OpenOnFallbackModel bool
	// WorkspaceScanLimit is a test seam: it can only lower the walk bound.
	// Zero is the production limit.
	WorkspaceScanLimit int
	// EffortOverride is a session-local reasoning effort override. Nil means use
	// the resolved provider config; a non-nil empty string means provider default.
	EffortOverride *string
	// PermissionAllow adds process-local allow rules (for example CLI
	// --allowed-tools). They override configured ask rules but never deny rules
	// and are not persisted.
	PermissionAllow []string
	// AdditionalDirs grants this session's file writers and sandboxed shell
	// access to extra directories without changing persisted sandbox config.
	AdditionalDirs []string
	// Diagnostic warnings and plugin subprocess stderr; nil means os.Stderr.
	// Interactive terminals must pass a private writer (or io.Discard) so
	// background output cannot corrupt the TUI's raw mode.
	Stderr io.Writer
	// OnPhase receives completed assembly timings synchronously, including on a later failure.
	OnPhase func(Phase)
	// Project root for config, skills, memory, commands, hooks, and tool
	// confinement; empty means the process cwd. Per-tab roots are what let
	// concurrent sessions load different projects without a chdir.
	WorkspaceRoot string
	// Home states where this build's config, credentials, state and hooks live;
	// empty follows the environment. It binds this assembly only — a
	// Controller's later re-reads and the history index still read the process.
	Home string
	// Tests that compare prompt bytes must not depend on host discovery probes.
	resolvedShell *sandbox.Shell
	// StatsSource labels this frontend's usage records. Unset — or a value this
	// build does not know — disables usage recording rather than filing turns
	// under a label nothing can read back.
	StatsSource surface.Surface
	// FeedbackSurface is the frontend a report from this session names. Empty
	// leaves /feedback unavailable: a headless run or an editor has no form.
	FeedbackSurface feedback.Surface
	// BalanceStore lets a host that builds several runtimes — a window with more
	// than one pane — read one wallet through one cache. Nil gives each runtime
	// a private one.
	BalanceStore *billing.Store
	TaskStore    taskmonitor.WriteStore // Authoritative store, never a SQLite catalog.
	// OnConfigLoadWarnings accepts resilient-loader warnings. Returning true
	// lets boot suppress the duplicate migration diagnostic.
	OnConfigLoadWarnings func([]string) bool
	// ExtraPlugins are session-scoped MCP servers supplied by a host transport
	// (for example ACP session/new). They are connected eagerly for this
	// controller but are not persisted to reasonix.toml.
	ExtraPlugins []plugin.Spec
	// AgentPreset selects the session role setting (balanced|delivery). Empty
	// falls back to balanced. It controls verification breadth without changing
	// the provider-visible tool schema or base system prompt.
	AgentPreset string
	// TokenMode is the deprecated one-version fallback for AgentPreset.
	// When AgentPreset is empty, TokenMode is normalized through the legacy
	// economy/full/delivery mapping. Prefer AgentPreset.
	TokenMode string
	// SessionDir overrides where persisted chat transcripts are written. When
	// empty, the shared CLI/global session directory is used.
	SessionDir string
	// A plugin.Host shared across controllers of one workspace root: set, the
	// build reuses its running clients instead of spawning duplicates and the
	// caller owns its lifecycle; nil, the build creates and owns one.
	SharedHost *plugin.Host
	// CleanupPendingReconciler retries delayed physical cleanup for session
	// artifacts left by a previous process. Nil uses the core physical-delete
	// reconciler; frontends with different deletion semantics can override it.
	CleanupPendingReconciler func(sessionDir string) error
	// Bounds how long an approval or ask prompt blocks. Zero waits forever,
	// correct for a terminal; headless frontends pass a positive value so
	// an unanswered prompt cannot wedge the session (#4626, #4402).
	ApprovalTimeout time.Duration
	// The non-interactive approval contract for every headless gate this build
	// constructs; empty keeps the fail-closed default. A later
	// ApplyHeadlessApprovalMode must match, or sub-agent gates diverge.
	HeadlessApprovalMode string

	// Observe, when set, builds the read-only unattended posture; see ObserveOptions.
	Observe *ObserveOptions

	GoalTurnsUnreachable bool // this assembly never arms a Goal turn; see GoalOnlyToolNames
	UnattendedChild      bool // an unattended child run: it offers neither best_of_n nor isolation
	// SessionRecoveryMeta and OnSessionRecovered let richer frontends attach
	// local UI metadata to automatic transcript recovery branches.
	SessionRecoveryMeta func(control.SessionRecoveryRequest) sessionstore.BranchMeta
	OnSessionRecovered  func(control.SessionRecoveryInfo) error
	// SubagentParentLive reports whether this process currently owns or is
	// building the parent session. Desktop uses it to avoid probing a live tab's
	// lease during stale-subagent cleanup. Nil preserves lease-only cleanup.
	SubagentParentLive func(sessionPath string) bool
	// Let a host transport (ACP) serve files from editor buffers and run bash
	// in its own terminal. Both move where tool I/O happens; names, docs, and
	// schemas stay byte-identical, so the provider-visible surface is unchanged.
	FileOverlay    builtin.FileOverlay
	TerminalRunner builtin.TerminalRunner
	// ProviderResolver routes every model role through a caller-owned provider
	// catalog. Nil preserves local behavior.
	ProviderResolver provider.Resolver
	// Switches subsystems off for a benchmark arm, and is the process-local
	// override supervised ACP workers force the planner off with. It beats
	// config without mutating it or changing the prompt/tool surface.
	Ablation ablation.Set
	// SandboxNetworkOverride and WorkspaceOnly are process-local hard bounds for
	// supervised ACP workers. Nil/false preserve normal Reasonix config.
	SandboxNetworkOverride *bool
	SandboxBashOverride    string
	WorkspaceOnly          bool
	// WorkspaceRepo is the git identity the session opened with; a rebuild of
	// the same workspace passes it on rather than resolving it again.
	WorkspaceRepo gitcmd.Repo
	// SessionTemp is the session-private temp manager; Rebuild reuses old's.
	SessionTemp *sessiontemp.Manager
	// BrowserSession is the agent's browser and the tabs it has open; Rebuild
	// reuses old's so a model switch does not close them.
	BrowserSession *browser.Session
	RuntimeReload
	// deferPublish keeps a replacement generation private until migration and
	// commit succeed. Cold BuildRuntime leaves this false and publishes at boot.
	deferPublish bool
}

// roots is the storage binding this build resolves every user-global path
// against.
func (o Options) roots() config.Roots { return config.RootsForHome(o.Home) }
