package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/checkenv"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/sessionstore"
	"strconv"
	"strings"

	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/memory"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/workspacelease"
	"reasonix/internal/tools/jobs"
)

// DefaultTaskSystemPrompt steers a sub-agent toward focused, terse delivery —
// it doesn't see the parent's conversation so it must self-contain.
const DefaultTaskSystemPrompt = `You are a sub-agent invoked by a parent coding agent to carry out one focused task.
Use the provided tools to investigate or act. For MCP, use the stable use_capability
proxy (list → inspect → call); do not expect direct mcp__* tool schemas. Return a
single final answer that is concise and self-contained — the parent will see only
that answer, not your tool calls or reasoning. If you need to ask for clarification,
fail with a precise question instead of guessing.`

// DefaultReadOnlyTaskSystemPrompt steers read-only sub-agents toward isolated
// research. They never receive writer tools, persisted transcript controls, or
// background process controls, so their final answer is the only handoff.
const DefaultReadOnlyTaskSystemPrompt = `You are a read-only research sub-agent invoked by a parent coding agent.
Use only the provided read-only tools to inspect code, docs, history, and safe shell output.
For MCP, use use_capability only for authorized tools that declare readOnly and are
not destructive; never treat missing readOnlyHint as permission to call. Do not
attempt to write files, install capabilities, mutate memory, control long-lived
processes, or delegate to writer-capable agents. If a read-only delegation tool is
available and genuinely useful, you may use it within the configured depth limit.
Return a concise, self-contained final answer with the evidence the parent needs.`

const subagentToolBoundarySummary = "Recursive agent/skill tools are exposed only while max_subagent_depth leaves another delegation layer; parallel_tasks is excluded. Isolated writer runs own their background jobs and may expose wait, bash_output, kill_shell and background bash when allowed by the tool whitelist. Other subagents expose foreground-only bash and exclude job tools."

// maxConcurrentBackgroundTasks is the legacy writer-background fallback used
// only when a TaskTool has no session scheduler (tests). Production boots
// inject MaxParallelWriters via SubagentScheduler.
const maxConcurrentBackgroundTasks = 3

// TaskTool spawns a sub-agent in its own session for a focused sub-task. The
// sub-agent runs with a filtered tool whitelist and the same step budget shape
// as the parent (see Execute); its tool calls are forwarded to the parent's
// event stream nested under this call, while only its final assistant message is
// returned to the parent model. Use cases: keep noisy tool sequences (multi-file
// exploration, repeated grep / read_file) out of the parent's context budget, or
// parallel research across independent areas (the parallel-dispatch path picks
// these up only when readOnly, which task is not).
type TaskTool struct {
	prov                          provider.Provider
	pricing                       *provider.Pricing
	parentReg                     *tool.Registry
	maxSteps                      int
	providerRetries               int
	contextWindow                 int
	compactRatio                  float64
	recentKeep                    int
	budgets                       agent.CompactionBudgets
	temperature                   float64
	archiveDir                    string
	keepPolicy                    agent.KeepPolicy
	sysPrompt                     string
	gate                          agent.Gate
	subagentModel, subagentEffort string
	inheritedEffort               func(modelRef string) string
	resolveProvider               func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error)
	transcripts                   *SubagentStore
	workspaceRoot                 string
	optimisticClaims              bool
	extraRoots                    []string
	baseModel, baseEffort         string
	identityProfile               func(modelRef, effort string) (string, string)
	maxSubagentDepth              int
	deliveryProfile               bool
	ablation                      ablation.Set
	workspaceLease                *workspacelease.Owner
	mutations                     *mutation.Coordinator
	executionPresence             func(context.Context, string, bool) (func(), error)
	checkEnvironment              *checkenv.Spec
	processSandbox                sandbox.Spec
	// scheduler is the session-scoped concurrency + write-claim controller.
	// nil falls back to the legacy jobs.ReserveStart cap for background tasks.
	scheduler *writeclaim.SubagentScheduler
	// profileLookup resolves profile= names from the live Skill store without
	// embedding the name list in the tool schema (cache stability).
	profileLookup ProfileLookup
	// profileConfigModel/Effort look up persistent per-profile overrides
	// (agent.subagent_models / subagent_efforts).
	profileConfigModel  func(profile string) string
	profileConfigEffort func(profile string) string
	// bashSandboxEnforced reports whether OS sandbox can honour write roots
	// for bash inside path-bound writer sub-agents.
	bashSandboxEnforced func() bool
	// childTools names the tools a child gains from the model it runs on, beyond
	// what the parent holds: a tool the parent's model has no use for is not in
	// the parent's registry, so a child on another model would never see it.
	childTools func(modelRef string) []tool.Tool
	// mutationObserver is shared with spawned sub-agents for checkpoint capture.
	mutationObserver *checkpoint.MutationObserver
	// recoveryGate is the shared Auto Guard boundary for
	// this session (root + sub-agents). nil disables recovery in children.
	recoveryGate agent.RecoveryGate
	// capabilityRuntime is the session-shared MCP Host/specs substrate. Each
	// sub-agent gets its own use_capability frontend so ledger state stays
	// isolated while connections reuse the parent Host.
	capabilityRuntime     *usecap.MCPCapabilityRuntime
	isolated              *taskIsolation // nil unless the session offers worktree isolation
	environment           *environmentPolicy
	environmentID         string
	environmentGeneration string
	environmentSource     string
	checkTargetAccess     tool.TargetAccessCheck
	hooksForRole          func(string) agent.ToolHooks // nil: children fire no hooks
}

// TaskToolOptions holds the construction parameters for a TaskTool.
// Prefer NewTaskToolWithOptions for new call sites; the positional NewTaskTool
// remains as a compatibility wrapper for one full iteration cycle.
type TaskToolOptions struct {
	Provider                              provider.Provider
	Pricing                               *provider.Pricing
	ParentRegistry                        *tool.Registry
	MaxSteps                              int
	ProviderRetries                       int
	ContextWindow                         int
	RecentKeep                            int
	CompactionBudgets                     agent.CompactionBudgets
	CompactRatio                          float64
	Temperature                           float64
	ContextEditing, ArchiveDir, SysPrompt string
	Gate                                  agent.Gate
	CheckTargetAccess                     tool.TargetAccessCheck
	KeepPolicy                            agent.KeepPolicy
	SubagentModel                         string
	SubagentEffort                        string
	InheritedEffort                       func(modelRef string) string
	ResolveProvider                       func(string, string) (provider.Provider, *provider.Pricing, int, error)
	// HooksForRole gives each child the session's hooks under a role of its own.
	HooksForRole func(role string) agent.ToolHooks
}

// NewTaskToolWithOptions is the internal standard constructor for TaskTool.
// An empty SysPrompt still resolves to DefaultTaskSystemPrompt. No extra
// validation or default overrides are applied beyond the historical NewTaskTool
// behavior.
func NewTaskToolWithOptions(opts TaskToolOptions) *TaskTool {
	sysPrompt := opts.SysPrompt
	if sysPrompt == "" {
		sysPrompt = DefaultTaskSystemPrompt
	}
	return &TaskTool{
		prov:              opts.Provider,
		pricing:           opts.Pricing,
		parentReg:         opts.ParentRegistry,
		maxSteps:          opts.MaxSteps,
		providerRetries:   opts.ProviderRetries,
		contextWindow:     opts.ContextWindow,
		recentKeep:        opts.RecentKeep,
		budgets:           opts.CompactionBudgets,
		compactRatio:      opts.CompactRatio,
		temperature:       opts.Temperature,
		archiveDir:        opts.ArchiveDir,
		keepPolicy:        opts.KeepPolicy,
		sysPrompt:         sysPrompt,
		gate:              opts.Gate,
		checkTargetAccess: opts.CheckTargetAccess,
		hooksForRole:      opts.HooksForRole,
		subagentModel:     opts.SubagentModel,
		subagentEffort:    opts.SubagentEffort,
		inheritedEffort:   opts.InheritedEffort,
		resolveProvider:   opts.ResolveProvider,
		maxSubagentDepth:  agent.DefaultMaxSubagentDepth,
	}
}

// NewTaskTool wires a task tool to the parent agent's environment so its
// sub-agents can use the same provider and tools. sysPrompt is the system
// prompt every sub-agent starts with; pass "" for DefaultTaskSystemPrompt. gate
// is the permission gate sub-agents inherit — pass the headless variant so
// deny rules still bite while autonomous sub-agents are never blocked on an
// interactive prompt (there is no UI to answer one).
//
// Compatibility wrapper: new call sites should prefer NewTaskToolWithOptions.
// An omitted or nil access check refuses file-target calls.
func NewTaskTool(prov provider.Provider, pricing *provider.Pricing, parentReg *tool.Registry,
	maxSteps, contextWindow, recentKeep int, compactRatio, temperature float64, archiveDir, sysPrompt string, gate agent.Gate,
	keepPolicy agent.KeepPolicy, subagentModel, subagentEffort string, resolveProvider func(string, string) (provider.Provider, *provider.Pricing, int, error), targetAccess ...tool.TargetAccessCheck) *TaskTool {
	return NewTaskToolWithOptions(TaskToolOptions{
		Provider:          prov,
		Pricing:           pricing,
		ParentRegistry:    parentReg,
		MaxSteps:          maxSteps,
		ContextWindow:     contextWindow,
		RecentKeep:        recentKeep,
		CompactRatio:      compactRatio,
		Temperature:       temperature,
		ArchiveDir:        archiveDir,
		SysPrompt:         sysPrompt,
		Gate:              gate,
		CheckTargetAccess: legacyTaskTargetAccess(targetAccess),
		KeepPolicy:        keepPolicy,
		SubagentModel:     subagentModel,
		SubagentEffort:    subagentEffort,
		ResolveProvider:   resolveProvider,
	})
}

// WithOptimisticClaims makes a writer that declared no write_paths take an
// optimistic claim: it reserves nothing up front and holds only the path of the
// write in progress. The default is the strict whole-workspace claim.
func (t *TaskTool) WithOptimisticClaims(on bool) *TaskTool {
	t.optimisticClaims = on
	return t
}

// UndeclaredWriteClaim is the claim a writer takes when it names no paths.
func (t *TaskTool) UndeclaredWriteClaim() (writeclaim.WritePathSet, error) {
	if t.optimisticClaims {
		return writeclaim.OptimisticWorkspaceClaim(t.workspaceRoot)
	}
	return writeclaim.WholeWorkspaceWriteClaim(t.workspaceRoot)
}

// WithTranscripts enables persisted sub-agent transcript continuation for this
// task tool. The base model/effort are the parent provider identity used when no
// subagent override is configured.
func (t *TaskTool) WithTranscripts(store *SubagentStore, workspaceRoot, baseModel, baseEffort string) *TaskTool {
	t.transcripts = store
	t.workspaceRoot = strings.TrimSpace(workspaceRoot)
	t.baseModel = strings.TrimSpace(baseModel)
	t.baseEffort = strings.TrimSpace(baseEffort)
	return t
}

func (t *TaskTool) WithTranscriptIdentityResolver(resolve func(modelRef, effort string) (string, string)) *TaskTool {
	t.identityProfile = resolve
	return t
}

func (t *TaskTool) WithMaxSubagentDepth(depth int) *TaskTool {
	t.maxSubagentDepth = agent.NormalizeMaxSubagentDepth(depth)
	return t
}

// WithDeliveryProfile propagates the parent's runtime delivery contract into
// writer-capable sub-agents. Read-only sub-agents may receive the flag too, but
// the mutation gate remains dormant for them.
func (t *TaskTool) WithDeliveryProfile(enabled bool) *TaskTool {
	t.deliveryProfile = enabled
	return t
}

// WithAblation propagates the parent's benchmark arm so a sub-agent runs with
// the same subsystems switched off.
func (t *TaskTool) WithAblation(set ablation.Set) *TaskTool {
	t.ablation = set
	return t
}

// WithWorkspaceLease shares the parent's workspace-wide delivery write lease
// with every spawned sub-agent. A shared owner is required: independent owners
// in one session would deadlock when a child tries to write while its parent
// already retains the lease.
func (t *TaskTool) WithWorkspaceLease(owner *workspacelease.Owner) *TaskTool {
	t.workspaceLease = owner
	return t
}

// WithScheduler attaches the session-scoped concurrency and write-claim
// controller used by task, fleet, parallel_tasks, and profile skill runners.
func (t *TaskTool) WithScheduler(s *writeclaim.SubagentScheduler) *TaskTool {
	t.scheduler = s
	return t
}

// Scheduler returns the attached session scheduler (may be nil in unit tests).
func (t *TaskTool) Scheduler() *writeclaim.SubagentScheduler {
	if t == nil {
		return nil
	}
	return t.scheduler
}

// WithProfileLookup enables task/fleet profile= resolution from the Skill store.
func (t *TaskTool) WithProfileLookup(lookup ProfileLookup) *TaskTool {
	t.profileLookup = lookup
	return t
}

// WithProfileConfigResolvers supplies persistent per-profile model/effort
// overrides (agent.subagent_models / subagent_efforts).
func (t *TaskTool) WithProfileConfigResolvers(model, effort func(profile string) string) *TaskTool {
	t.profileConfigModel = model
	t.profileConfigEffort = effort
	return t
}

// WithBashSandboxEnforced tells path-bound writer runs whether bash can keep
// the same write roots under the OS sandbox.
func (t *TaskTool) WithBashSandboxEnforced(fn func() bool) *TaskTool {
	t.bashSandboxEnforced = fn
	return t
}

func (t *TaskTool) WithProcessSandbox(spec sandbox.Spec) *TaskTool {
	t.processSandbox = spec
	return t
}

// WithChildTools supplies the tools a child gets because of its own model (an
// image reader for a model that reads images). The parent's tool block is not
// touched; only the child's registry is.
func (t *TaskTool) WithChildTools(fn func(modelRef string) []tool.Tool) *TaskTool {
	t.childTools = fn
	return t
}

// WithCapabilityRuntime attaches the session-shared MCP runtime so ordinary and
// read-only sub-agents receive a stable use_capability frontend without
// inheriting dynamic mcp__* schemas.
func (t *TaskTool) WithCapabilityRuntime(rt *usecap.MCPCapabilityRuntime) *TaskTool {
	if t != nil {
		t.capabilityRuntime = rt
	}
	return t
}

// ReadOnly is false: a sub-agent can invoke any whitelisted tool, including
// writers. Conservative classification keeps the parallel-dispatch path from
// running two sub-agents at once and letting their writes race.
func (t *TaskTool) ReadOnly() bool { return false }

// ReadOnlyTaskTool runs an isolated sub-agent with a strictly read-only tool
// registry. It intentionally omits background execution and transcript
// continuation/fork controls so the call has no durable host side effects.
type ReadOnlyTaskTool struct {
	task *TaskTool
}

func NewReadOnlyTaskTool(task *TaskTool) *ReadOnlyTaskTool {
	return &ReadOnlyTaskTool{task: task}
}

func (*ReadOnlyTaskTool) Name() string { return "read_only_task" }

func (*ReadOnlyTaskTool) Description() string {
	return "Spawn a read-only research sub-agent for a focused investigation. The sub-agent runs in an isolated, ephemeral session with read-only tools only; bash is wrapped to allow only permission-classified foreground read-only commands. It cannot write files, install capabilities, mutate memory, run background jobs, continue/fork transcripts, or delegate to writer-capable agents. Read-only nested delegation may be available until max_subagent_depth is reached. Only its final answer is returned."
}

func (*ReadOnlyTaskTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "prompt":{"type":"string","description":"What the read-only sub-agent should investigate. Be specific about the evidence or summary to return — the sub-agent does not see this conversation."},
  "description":{"type":"string","description":"Short label for the read-only sub-task (3-7 words). Surfaced in the dispatch line so the user sees what's running."},
  "tools":{"type":"array","items":{"type":"string"},"description":"Optional read-only tool whitelist. Writer, installer, memory mutation, background job, and delegation tools are never exposed."},
  "max_steps":{"type":"integer","description":"Optional cap on tool-call rounds. Defaults to half the parent's cap (min 5).","minimum":1},
  "model":{"type":"string","description":"Optional model override for the sub-agent (a configured provider/model name)."},
  "effort":{"type":"string","description":"Optional reasoning effort for the sub-agent (e.g. high, max)."}
},
"required":["prompt"]
}`)
}

func (*ReadOnlyTaskTool) ReadOnly() bool { return true }

// PlanModeSafe reports true: read_only_task spawns a strictly read-only research
// sub-agent (no writers, installers, memory mutation, background jobs, or
// delegation), so it is safe to run while planning.
func (*ReadOnlyTaskTool) PlanModeSafe() bool { return true }

func (r *ReadOnlyTaskTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if r == nil || r.task == nil {
		return "", fmt.Errorf("read_only_task is not configured")
	}
	var p struct {
		Prompt      string   `json:"prompt"`
		Description string   `json:"description"`
		Tools       []string `json:"tools"`
		MaxSteps    int      `json:"max_steps"`
		Model       string   `json:"model"`
		Effort      string   `json:"effort"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	// Every entry point compiles to a spec and runs through RunProfileSpec, so a
	// boundary added there cannot be missed by one caller. read_only_task keeps
	// its own promise of no durable side effects through Ephemeral.
	spec, err := r.task.buildTaskSpec(ctx, p.Prompt, p.Description, "", nil, p.Tools, p.MaxSteps, p.Model, p.Effort, "", "", false, true)
	if err != nil {
		return "", err
	}
	spec.Worker.SystemPrompt = DefaultReadOnlyTaskSystemPrompt
	spec.Context.Ephemeral = true
	return r.task.RunProfileSpec(ctx, spec)
}

func (t *TaskTool) executeTask(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Prompt          string   `json:"prompt"`
		Description     string   `json:"description"`
		Profile         string   `json:"profile"`
		WritePaths      []string `json:"write_paths"`
		Tools           []string `json:"tools"`
		MaxSteps        int      `json:"max_steps"`
		RunInBackground bool     `json:"run_in_background"`
		Model           string   `json:"model"`
		Effort          string   `json:"effort"`
		ContinueFrom    string   `json:"continue_from"`
		ForkFrom        string   `json:"fork_from"`
		Isolation       string   `json:"isolation"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if p.Isolation != "" && t.environment == nil {
		return t.executeIsolated(ctx, isolatedCall{mode: p.Isolation, prompt: p.Prompt, model: p.Model,
			unsupported: isolatedUnsupported(p.Profile, p.WritePaths, p.Tools, p.ContinueFrom, p.ForkFrom, p.RunInBackground)})
	}

	spec, err := t.buildTaskSpec(ctx, p.Prompt, p.Description, p.Profile, p.WritePaths, p.Tools, p.MaxSteps, p.Model, p.Effort, p.ContinueFrom, p.ForkFrom, p.RunInBackground, false)
	if err != nil {
		return "", err
	}
	spec.Sched.Environment = p.Isolation
	return t.RunProfileSpec(ctx, spec)
}

// buildTaskSpec resolves profile, tools, model/effort, and write claims for a
// single task/fleet item. forceReadOnly forces the read-only registry.
func (t *TaskTool) buildTaskSpec(ctx context.Context, prompt, description, profile string, writePaths, tools []string, maxSteps int, model, effort, continueFrom, forkFrom string, background, forceReadOnly bool) (ProfileExecSpec, error) {
	spec := ProfileExecSpec{
		Task:    TaskSpec{Objective: prompt, Description: description},
		Worker:  WorkerSpec{Kind: "task", Name: "task", SystemPrompt: t.sysPrompt},
		Grant:   CapabilityGrant{CallTools: tools},
		Context: ContextRequest{ContinueFrom: strings.TrimSpace(continueFrom), ForkFrom: strings.TrimSpace(forkFrom)},
		Sched:   SchedulerPolicy{MaxSteps: maxSteps, RunInBackground: background, Nested: agent.SubagentDepth(ctx) > 0},
	}
	profile = strings.TrimSpace(profile)
	readOnly := forceReadOnly
	var profileTools []string
	var profileModel, profileEffort string
	if profile != "" {
		def, err := ResolveProfileDefinition(t.profileLookup, profile)
		if err != nil {
			return ProfileExecSpec{}, err
		}
		spec.Worker.Profile = def.Name
		spec.Worker.Name = def.Name
		spec.Worker.Kind = "skill"
		spec.Worker.SystemPrompt = def.Body
		spec.Worker.UseProfilePrompt = true
		// What the worker owes comes with the worker, and so does what it may
		// prove. Deriving either from the profile's name here would put an
		// undeclared rule in a second place.
		spec.Worker.ReviewReport = def.Delivery.ReviewReport
		spec.Worker.ReviewAuthority = def.Authority.Review
		profileTools = def.AllowedTools
		profileModel, profileEffort = def.Model, def.Effort
		if def.ReadOnly {
			readOnly = true
		}
	}
	spec.Grant.ReadOnly = readOnly
	spec.Grant.ProfileTools = profileTools

	configModel, configEffort := "", ""
	if profile != "" {
		if t.profileConfigModel != nil {
			configModel = t.profileConfigModel(profile)
		}
		if t.profileConfigEffort != nil {
			configEffort = t.profileConfigEffort(profile)
		}
	}
	spec.Worker.Model, spec.Worker.Effort = t.resolveModelEffort(
		configModel, configEffort,
		model, effort,
		profileModel, profileEffort,
	)

	if !readOnly {
		// Every writer carries a claim. Omitting write_paths conservatively claims
		// the whole workspace, including foreground task calls, so they cannot
		// bypass an already-running background/fleet writer claim. Direct legacy
		// TaskTool constructions without a workspace/scheduler keep their old
		// no-claim behavior; production boot always configures both.
		requireClaim := t.scheduler != nil || strings.TrimSpace(t.workspaceRoot) != "" || background || len(writePaths) > 0
		claims, err := t.resolveWriterClaims(writePaths, requireClaim)
		if err != nil {
			return ProfileExecSpec{}, err
		}
		spec.Grant.WritePaths = claims
		if requireClaim && claims.Empty() {
			return ProfileExecSpec{}, fmt.Errorf("writer claim resolved empty")
		}
	} else if len(writePaths) > 0 {
		return ProfileExecSpec{}, fmt.Errorf("write_paths is not valid for read-only tasks")
	}
	return spec, nil
}

func (t *TaskTool) resolveWriterClaims(writePaths []string, requireClaim bool) (writeclaim.WritePathSet, error) {
	if len(writePaths) > 0 {
		return writeclaim.NormalizeWritePaths(t.workspaceRoot, writePaths)
	}
	if !requireClaim {
		return writeclaim.WritePathSet{}, nil
	}
	return t.UndeclaredWriteClaim()
}

// settleSpecPrompts rejects a run with nothing to do and fills in the system
// prompt a plain task inherits. A profile that declared one and lost it is an
// error rather than a fallback: it would run under the wrong instructions.
func (t *TaskTool) settleSpecPrompts(spec *ProfileExecSpec) error {
	if strings.TrimSpace(spec.Task.Objective) == "" {
		return fmt.Errorf("prompt is required")
	}
	if strings.TrimSpace(spec.Worker.SystemPrompt) == "" {
		if spec.Worker.UseProfilePrompt {
			return fmt.Errorf("profile system prompt is empty")
		}
		spec.Worker.SystemPrompt = t.sysPrompt
	}
	return nil
}

// RunProfileSpec executes a unified profile/task specification. Shared by task,
// fleet items, and boot-wired skill runners so prompt, tools, claims, and
// scheduling cannot drift across entry points.
func (t *TaskTool) runProfileSpec(ctx context.Context, spec ProfileExecSpec, environment *environmentRequest) (result string, err error) {
	trk := newSubagentProgressTracker(ctx, subSink(ctx))
	handoff := false
	defer func() {
		if !handoff {
			environment.release()
		}
	}()
	defer trk.finishReturning(ctx, &handoff, &err)
	if err := t.settleSpecPrompts(&spec); err != nil {
		return "", err
	}
	releaseResident, err := t.reserveResident()
	if err != nil {
		return "", err
	}
	defer func() {
		if !handoff {
			releaseResident()
		}
	}()
	ctx = agent.BindTaskImages(ctx, spec.Task.Objective)
	plan, err := t.prepareAdmissionProfile(ctx, spec, trk, environment)
	if err != nil {
		return "", err
	}
	plan.environment = environment
	req, err := t.acquireRequestFor(&spec)
	if err != nil {
		return "", err
	}
	life, err := t.openDelegation(ctx, &spec, &req, trk)
	if err != nil {
		return "", err
	}
	plan.spec = spec
	defer (*environmentTerminal)(nil).settleLifecycle(ctx, life, &handoff, &result, &err)
	parentID, _, _, _ := agent.CallContext(ctx)
	execution := life.executionID(parentID)
	if spec.Sched.RunInBackground {
		result, err = t.startProfileBackground(ctx, plan, environment, req, execution, life, trk, releaseResident)
		handoff = err == nil
		return result, err
	}
	return t.runScheduledProfile(ctx, plan, environment, req, execution, life, trk)
}

// endedRunError records how a run that executed ended and returns its error. A
// cancellation carries the transcript's reference only once its terminal state
// is saved: an unsaved run cannot be read back, so naming it would mislead.
func (t *TaskTool) endedRunError(run *SubagentRun, err error) error {
	if saveErr := t.saveRunTerminal(run, err); saveErr != nil {
		return errors.Join(err, saveErr)
	}
	return withStoppedRunRef(err, run)
}

// saveRunTerminal records what happened to a child that actually ran. It is the
// only place a cancellation may be read as a terminal outcome: everywhere else
// a context error means the host failed to do something, and classifying those
// would file an infrastructure failure as work the caller stopped.
func (t *TaskTool) saveRunTerminal(run *SubagentRun, runErr error) error {
	switch {
	case errors.Is(runErr, context.DeadlineExceeded):
		return t.transcripts.SaveCancelled(run, sessionstore.TerminalDeadline)
	case errors.Is(runErr, context.Canceled):
		return t.transcripts.SaveCancelled(run, sessionstore.TerminalCancelled)
	default:
		return t.transcripts.SaveFailed(run)
	}
}

func (t *TaskTool) bashCanEnforceWriteRoots() bool {
	if t != nil && t.bashSandboxEnforced != nil {
		return t.bashSandboxEnforced()
	}
	return false
}

func childToolIdentityContext(ctx context.Context) context.Context {
	ctx = mutation.WithoutLease(ctx)
	ctx = tool.WithoutGoalTurnRecorder(ctx)
	ctx = memory.WithoutQueue(ctx)
	ctx = jobs.WithoutManager(ctx)
	return planmode.WithActive(ctx, agent.PlanModeFromContext(ctx))
}

func (t *TaskTool) effectiveIdentity(modelRef, effort string) (string, string) {
	if t.identityProfile != nil {
		model, eff := t.identityProfile(modelRef, effort)
		return strings.TrimSpace(model), strings.TrimSpace(eff)
	}
	return t.effectiveModelIdentity(modelRef), t.effectiveEffortIdentity(effort)
}

// usageModelRef returns the canonical provider/model identity of the runtime
// selected for a child. The resolver expands aliases and supplies the parent
// model when no child override is configured.
func (t *TaskTool) usageModelRef(modelRef, effort string) string {
	model, _ := t.effectiveIdentity(modelRef, effort)
	if model != "" {
		return model
	}
	return firstNonEmpty(modelRef, t.baseModel, t.subagentModel)
}

func (t *TaskTool) effectiveModelIdentity(modelRef string) string {
	if strings.TrimSpace(modelRef) != "" {
		return strings.TrimSpace(modelRef)
	}
	return strings.TrimSpace(t.baseModel)
}

func (t *TaskTool) effectiveEffortIdentity(effort string) string {
	if strings.TrimSpace(effort) != "" {
		return strings.TrimSpace(effort)
	}
	return strings.TrimSpace(t.baseEffort)
}

// buildSubReg returns the sub-agent's tool set: the named whitelist (minus
// unavailable sub-agent tools), or every parent tool except those tools.
func (t *TaskTool) buildSubReg(names []string, childDepth int) *tool.Registry {
	if t.environmentID != "" {
		return agent.SubagentToolRegistryWithOwnedJobs(t.parentReg, names, childDepth, t.maxDepth(), t.capabilityRuntime)
	}
	return agent.SubagentToolRegistryForDepthWithRuntime(t.parentReg, names, childDepth, t.maxDepth(), t.capabilityRuntime)
}

func (t *TaskTool) maxDepth() int {
	if t == nil {
		return agent.DefaultMaxSubagentDepth
	}
	if t.maxSubagentDepth == 0 {
		return agent.DefaultMaxSubagentDepth
	}
	return agent.NormalizeMaxSubagentDepth(t.maxSubagentDepth)
}

func (t *TaskTool) nextSubagentDepth(ctx context.Context) (int, error) {
	current := agent.SubagentDepth(ctx)
	next := current + 1
	maxDepth := t.maxDepth()
	if next > maxDepth {
		return 0, fmt.Errorf("subagent delegation depth limit reached (max_subagent_depth=%d)", maxDepth)
	}
	return next, nil
}

func (t *TaskTool) resolveSubSessionRuntime(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error) {
	prov, pricing, ctxWin := t.prov, t.pricing, t.contextWindow
	if t.resolveProvider != nil && (modelRef != "" || effort != "") {
		p, pr, cw, err := t.resolveProvider(modelRef, effort)
		if err != nil {
			return nil, nil, 0, err
		}
		prov, pricing, ctxWin = p, pr, cw
	}
	return prov, pricing, ctxWin, nil
}

func (t *TaskTool) runSubSession(ctx context.Context, prompt string, subReg *tool.Registry, sink event.Sink, maxSteps int, prov provider.Provider, pricing *provider.Pricing, ctxWin int, sess *sessionstore.Session, childDepth int, recoveryTaskID, modelRef string, mutationObserver *checkpoint.MutationObserver, entrance string, grant agent.ReviewReportGrant, observeRoot string) (string, error) {
	opts := t.steerable(t.subagentOptions(ctx, maxSteps, pricing, ctxWin, childDepth, recoveryTaskID, mutationObserver), sess)
	opts.ModelRef = modelRef
	opts.RequestEffort, _ = ctx.Value(childEffortKey{}).(string)
	opts.ObserveRoot = observeRoot
	opts.RequireReviewReportKind = grant.Delivery
	// Capture the pristine task before host framing is prepended: delivery
	// intent classification must judge the task, not the wrapper.
	opts.ClassifierTaskText = prompt
	prompt = agent.SubagentImageNote(ctx) + t.withWorkspaceContext(upstreamNote(ctx)+prompt) + "\n\n" + agent.CompleteSubtaskContract
	opts.ExpectCompletionReport, opts.HandoffEntrance = true, entrance
	// Child provider owns the final vision decision. Text-only providers
	// retain the attachment metadata but omit image parts during serialization.
	ctx = agent.WithUserImages(ctx, agent.SubagentImageCandidates(ctx))
	return agent.RunSubAgentWithSession(ctx, prov, subReg, sess, prompt, opts, sink)
}

func (t *TaskTool) runReadOnlySubSession(ctx context.Context, prompt string, subReg *tool.Registry, sink event.Sink, maxSteps int, prov provider.Provider, pricing *provider.Pricing, ctxWin int, sess *sessionstore.Session, childDepth int, recoveryTaskID, modelRef string, mutationObserver *checkpoint.MutationObserver, entrance string, grant agent.ReviewReportGrant) (string, error) {
	opts := t.steerable(t.subagentOptions(ctx, maxSteps, pricing, ctxWin, childDepth, recoveryTaskID, mutationObserver), sess)
	opts.RequireReviewReportKind = grant.Delivery
	ctx, prompt, opts = t.prepareSubSession(ctx, prompt, opts, modelRef, entrance)
	return agent.RunReadOnlySubAgentWithSession(ctx, prov, subReg, sess, prompt, opts, sink)
}

func subagentRecoveryTaskID(ctx context.Context, ref string) string {
	if ref = strings.TrimSpace(ref); ref != "" {
		return "subagent:" + ref
	}
	if callID, _, _, ok := agent.CallContext(ctx); ok && strings.TrimSpace(callID) != "" {
		return "subagent:" + strings.TrimSpace(callID)
	}
	return "subagent"
}

// WithRecoveryGate shares Auto Guard with spawned sub-agents.
func (t *TaskTool) WithRecoveryGate(g agent.RecoveryGate) *TaskTool {
	if t == nil {
		return nil
	}
	t.recoveryGate = g
	return t
}

// SetMutationObserver lets the parent agent hand its observer to the
// sub-agents this tool spawns.
func (t *TaskTool) SetMutationObserver(obs *checkpoint.MutationObserver) { t.WithMutationObserver(obs) }

// WithMutationObserver shares the host mutation observer with spawned sub-agents.
// Foreground children inherit the parent ownership turn; background children
// keep the turn that spawned them (set via OwnershipTurn at Begin).
func (t *TaskTool) WithMutationObserver(obs *checkpoint.MutationObserver) *TaskTool {
	if t == nil {
		return nil
	}
	t.mutationObserver = obs
	return t
}

func (t *TaskTool) withWorkspaceContext(prompt string) string {
	if t == nil {
		return prompt
	}
	ctx := subagentWorkspaceContext(t.workspaceRoot)
	if ctx == "" {
		return prompt
	}
	return ctx + "\n\n" + prompt
}

func subagentWorkspaceContext(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	// Wording note: avoid incidental action verbs ("resolve", "fix", …) in this
	// host framing — it is prepended to every sub-agent prompt and must never
	// read as task intent (see classifierTaskText, which also strips it).
	return `<workspace-context event="SubagentWorkspace">
Current workspace: ` + strconv.Quote(root) + `
File tools interpret relative paths against this workspace. For project inspection, prefer "." or relative paths unless the user explicitly named another absolute path.
</workspace-context>`
}

func FormatSubagentReference(run *SubagentRun) string {
	if run == nil || run.Ref == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Subagent reference: %s\n", run.Ref)
	if strings.TrimSpace(run.ForkedFrom) != "" {
		fmt.Fprintf(&b, "Forked from: %s\n", strings.TrimSpace(run.ForkedFrom))
		b.WriteString("The requested ref resolves to an ancestor conversation transcript, so the framework continues a copy owned by the current conversation. To continue this copied subagent transcript in a later call, pass ")
		b.WriteString(run.Ref)
		b.WriteString(" as `continue_from`. Start a fresh subagent when the next task is independent.")
		return b.String()
	}
	b.WriteString("To continue this same subagent transcript in a later call, pass this ref as `continue_from`. Start a fresh subagent when the next task is independent.")
	return b.String()
}

func FormatSubagentRunResult(answer string, run *SubagentRun, failed bool) string {
	answer = GuardSubagentHostDecisionText(answer)
	if run == nil || run.Ref == "" {
		return answer
	}
	if failed {
		if answer == "" {
			return "Subagent reference (failed): " + run.Ref
		}
		return "Subagent reference (failed): " + run.Ref + "\n\nFinal answer:\n" + answer
	}
	return FormatSubagentReference(run) + "\n\nFinal answer:\n" + answer
}

// GuardSubagentHostDecisionText appends a fixed boundary warning only when a
// child agent result appears to discuss host approval or user-owned decisions.
// The implementation lives in internal/contract/tool so the skill tools share the exact
// same phrase list and notice.
func GuardSubagentHostDecisionText(answer string) string {
	return tool.GuardSubagentHostDecisionText(answer)
}

// NestedSink returns a sink that forwards a sub-agent's tool activity to the
// parent stream, nested under the tool call carried by ctx, so a frontend shows
// it beneath that call (the same nesting `task` uses). Falls back to the given
// sink when ctx carries no call context. Used by subagent skills.
func NestedSink(ctx context.Context, fallback event.Sink) event.Sink {
	parentID, parent, _, ok := agent.CallContext(ctx)
	if !ok || parent == nil {
		return fallback
	}
	return subSinkFor(parentID, parent)
}

// reviewGrantFor issues the grant for one delegated run. Both halves come from
// the worker's own declaration, so no entry point can widen either by asking
// differently.
func reviewGrantFor(worker WorkerSpec, execution string) agent.ReviewReportGrant {
	return agent.IssueReviewGrant(worker.ReviewReport, worker.ReviewAuthority, execution)
}

// observeRootFor is where a writer child may observe its own effects. A
// foreground child runs while its parent waits on it; a background writer
// runs beside the parent, which registers as no writer, so it may observe only
// through the paths it declared, where a change is taken as its own.
func (t *TaskTool) observeRootFor(backgroundWriter bool, grant *writeclaim.WriteGrant) string {
	if backgroundWriter && (grant == nil || grant.Declared().WholeWorkspace || grant.Scope().Empty()) {
		return ""
	}
	return t.workspaceRoot
}
