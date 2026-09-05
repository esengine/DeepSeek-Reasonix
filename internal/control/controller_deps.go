package control

import (
	"context"

	"reasonix/internal/ablation"
	"reasonix/internal/agent"
	"reasonix/internal/billing"
	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/goaleval"
	"reasonix/internal/guardian"
	"reasonix/internal/hook"
	"reasonix/internal/jobs"
	"reasonix/internal/permission"
	"reasonix/internal/plugin"
	"reasonix/internal/recovery"
	"reasonix/internal/sandbox"
	"reasonix/internal/skill"
	"reasonix/internal/workspacelease"
)

// controllerDeps is what assembly hands the controller and never rebinds. The
// split is for the reader and the lock discipline: c.turn and c.policy read
// alike, and only one can change under you. Embedded, so existing reads resolve.
type controllerDeps struct {
	runner       agent.Runner
	executor     *agent.Agent
	guardianSess *guardian.Session // nil when guardian is disabled
	// recoveryGate is the shared Auto Guard state for this controller.
	// nil when the feature is not wired for this controller.
	recoveryGate *recovery.Gate

	// taskBudget is the configured spend gate, as passed at construction.
	taskBudget agent.TaskBudget
	// goalTokenBudget bounds an unattended Goal loop; 0 leaves it unbounded.
	goalTokenBudget int
	// evaluator is the bounded Goal completion evaluator consulted when the
	// working model submits no update_goal report. nil fails closed: the goal
	// pauses instead of defaulting to continue.
	evaluator goaleval.Evaluator
	// goalUsageTee accounts billable usage events into the active goal turn's
	// observational token total. It wraps the public sink when the caller didn't provide one.
	goalUsageTee *goalUsageTee
	sink         event.Sink
	policy       permission.Policy
	// subagentGate is the shared gate every headless-only sub-agent surface
	// reads from (see Options.SubagentGate). Nil when the caller didn't build
	// one — sub-agents then keep whatever gate they were constructed with.
	subagentGate *SharedHeadlessGate

	label      string
	modelRef   string
	sessionDir string
	// skills owns the session's discovered skills (enabled subset, full set, and
	// the reloadable stores) — the skills slice of the Capabilities concern. See
	// skill.go.
	skills              skillSet
	skillRunner         skill.SubagentRunner
	readOnlySkillRunner skill.SubagentRunner
	skillProfile        skill.ProfileResolver
	hooks               *hook.Runner // session hook runner; nil-safe (no hooks configured)
	// memory owns the loaded memory snapshot, the pending turn-tail notes queue,
	// and write serialization behind its own locks, off c.mu — so a memory-panel
	// save never stalls an approval or status poll. See memory.go.
	memory                 memoryManager
	cleanup                func()
	display                displayPrefs // what this session shows; see display_prefs.go
	disableColdResumePrune bool         // legacy; rewrite elision removed, still gates cold notice

	shell               sandbox.Shell                    // interpreter for user-invoked "!" commands; zero = auto
	onRemember          func(rule string) RememberResult // set via Options; invoked when user picks "always allow"
	sessionRecoveryMeta func(SessionRecoveryRequest) agent.BranchMeta

	// balance is the active provider's optional wallet endpoint (nil-answering
	// when the provider declares none). Captured at build so a model/key switch —
	// which rebuilds the controller — refreshes it.
	balance *billing.Cache

	// jobs is the session-scoped background-job manager. The agent's background
	// tools spawn into it; Compose drains its completion notes into the next turn;
	// Close cancels its still-running jobs.
	jobs *jobs.Manager
	// workspaceLease is the Delivery writer owner shared with the executor.
	// It is exposed only through a sanitized state snapshot for Desktop recovery.
	workspaceLease *workspacelease.Owner

	// mcp owns the live tool/plugin surface — Host, registry, and the session
	// context a hot-added stdio server binds to — behind its own lock, off c.mu.
	// The Controller keeps the config-facing orchestration. See mcp.go.
	mcp               mcpManager
	mcpConfigureSpec  func(*plugin.Spec)
	capabilityRuntime *agent.MCPCapabilityRuntime
	runtimeOwner      *extension.RuntimeOwner
	ablation          ablation.Set

	// goals owns the active goal's FSM (status, intercepts, idle/turn counters)
	// and its persistence, behind its own mutex so a per-turn goal save never
	// stalls an approval or status poll on c.mu. See goal.go.
	goals goalMachine

	// Base for @-refs and slash path refs, cwd for user "!" commands and command
	// discovery, and the guard root for checkpoint restore writes. Frontends read
	// it through WorkspaceRoot().
	workspaceRoot string

	// approval owns prompt bookkeeping and the runtime posture (ask/auto/yolo,
	// session grants, the just-approved-plan window) behind its own locks, off
	// c.mu. The Controller keeps the I/O orchestration. See approval.go.
	approval approvalManager
}

// newControllerDeps binds everything assembly settled on, once.
func newControllerDeps(opts Options, sink event.Sink, usageTee *goalUsageTee, runtimeOwner *extension.RuntimeOwner, pluginCtx context.Context) controllerDeps {
	return controllerDeps{
		taskBudget:             opts.TaskBudget,
		goalTokenBudget:        opts.GoalTokenBudget,
		goals:                  goalMachine{tokenBudget: opts.GoalTokenBudget},
		runner:                 opts.Runner,
		executor:               opts.Executor,
		guardianSess:           opts.Guardian,
		evaluator:              opts.GoalEvaluator,
		goalUsageTee:           usageTee,
		sink:                   sink,
		policy:                 opts.Policy,
		subagentGate:           opts.SubagentGate,
		label:                  opts.Label,
		modelRef:               opts.ModelRef,
		sessionDir:             opts.SessionDir,
		skills:                 newSkillSet(opts.Skills, opts.AllSkills, opts.SkillStore, opts.AllSkillStore, opts.DisableImplicitSkillInvocation),
		skillRunner:            opts.SkillRunner,
		readOnlySkillRunner:    opts.ReadOnlySkillRunner,
		skillProfile:           opts.SkillProfile,
		hooks:                  opts.Hooks,
		memory:                 newMemoryManager(opts.Memory),
		cleanup:                opts.Cleanup,
		display:                displayPrefsFrom(opts),
		disableColdResumePrune: opts.DisableColdResumePrune,
		shell:                  opts.Shell,
		onRemember:             opts.OnRemember,
		sessionRecoveryMeta:    opts.SessionRecoveryMeta,
		balance:                opts.Balance,
		jobs:                   opts.Jobs,
		workspaceLease:         opts.WorkspaceLease,
		mcp:                    newMcpManager(opts.Host, opts.Registry, pluginCtx, opts.MCPDefaultCallTimeout),
		mcpConfigureSpec:       opts.MCPConfigureSpec,
		capabilityRuntime:      opts.CapabilityRuntime,
		ablation:               opts.Ablation,
		workspaceRoot:          opts.WorkspaceRoot,
		runtimeOwner:           runtimeOwner,
		approval:               newApprovalManager(opts.Policy, ToolApprovalAsk, opts.ApprovalTimeout),
	}
}

// publishPerProjectContext owes this session's per-project text to its first
// real turn: the skills catalog and the standing instructions. Neither is in
// the cached prefix — that is what makes the prefix identical across projects.
func (c *Controller) publishPerProjectContext(opts Options) {
	c.memory.publishInstructions(opts.Memory)
}
