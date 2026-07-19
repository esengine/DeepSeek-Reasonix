package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/jobs"
	"reasonix/internal/permission"
	"reasonix/internal/planmode"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
	"reasonix/internal/workspacelease"
)

// DefaultTaskSystemPrompt steers a sub-agent toward focused, terse delivery —
// it doesn't see the parent's conversation so it must self-contain.
const DefaultTaskSystemPrompt = `You are a sub-agent invoked by a parent coding agent to carry out one focused task.
Use the provided tools to investigate or act. Return a single final answer that is concise
and self-contained — the parent will see only that answer, not your tool calls or reasoning.
If you need to ask for clarification, fail with a precise question instead of guessing.`

// DefaultReadOnlyTaskSystemPrompt steers read-only sub-agents toward isolated
// research. They never receive writer tools, persisted transcript controls, or
// background process controls, so their final answer is the only handoff.
const DefaultReadOnlyTaskSystemPrompt = `You are a read-only research sub-agent invoked by a parent coding agent.
Use only the provided read-only tools to inspect code, docs, history, and safe shell output.
Do not attempt to write files, install capabilities, mutate memory, control long-lived
processes, or delegate to writer-capable agents. If a read-only delegation tool is
available and genuinely useful, you may use it within the configured depth limit.
Return a concise, self-contained final answer with the evidence the parent needs.`

const subagentStartContext = `<subagent-context event="SubagentStart">
Before acting, check the available skills and tools. If a relevant skill is available, invoke it before continuing. Delegate to another sub-agent only when the task genuinely benefits from isolated context and the delegation tool is available.
</subagent-context>`

// read_skill is deliberately not listed: it renders playbook text inline and
// cannot recurse, so depth-capped sub-agents keep it and can still read
// playbooks even when they can no longer delegate.
var subagentRecursiveTools = []string{
	"task",
	"read_only_task",
	"run_skill",
	"read_only_skill",
	"explore",
	"research",
	"review",
	"security_review",
}

var subagentAlwaysHiddenTools = []string{
	"parallel_tasks",
	"fleet",
	"install_skill",
	"install_source",
}

var subagentJobTools = []string{
	"wait",
	"bash_output",
	"kill_shell",
}

var readOnlySubagentWorkflowTools = []string{
	"connect_tool_source",
}

const subagentToolBoundarySummary = "Recursive agent/skill tools are exposed only while max_subagent_depth leaves another delegation layer; unsupported background job tools (parallel_tasks, wait, bash_output, kill_shell) are excluded; bash is exposed as foreground-only inside subagents."

// maxConcurrentBackgroundTasks is the legacy writer-background fallback used
// only when a TaskTool has no session scheduler (tests). Production boots
// inject MaxParallelWriters via SubagentScheduler.
const maxConcurrentBackgroundTasks = DefaultMaxParallelWriters

// AlwaysHiddenSubagentTools returns the tool names excluded from every
// subagent's registry regardless of an explicit allowlist or delegation
// depth (unlike subagentRecursiveTools, which depends on remaining depth).
// That covers both subagentAlwaysHiddenTools and subagentJobTools —
// SubagentToolRegistryForDepth and its read-only variant strip the job tools
// unconditionally too. Host UIs offering a tool picker for a subagent
// profile's allowed-tools should exclude these from the offered choices —
// selecting them would be silently ignored at runtime.
func AlwaysHiddenSubagentTools() []string {
	names := append([]string(nil), subagentAlwaysHiddenTools...)
	return append(names, subagentJobTools...)
}

// SubagentMetaTools returns the tool names that spawned agents should not inherit
// from the parent registry unless a future call site deliberately opts into a
// different boundary. They can spawn or author more agent work, so excluding them
// preserves one layer of delegation without adding a spawn-count cap.
// read_skill stays listed here so the guardian and planner surfaces, which
// exclude these names, keep their provider-visible tool sets byte-identical —
// only the sub-agent depth cap deliberately stopped stripping it.
func SubagentMetaTools() []string {
	out := append([]string(nil), subagentRecursiveTools...)
	out = append(out, "read_skill")
	out = append(out, subagentAlwaysHiddenTools...)
	return out
}

// SubagentToolRegistry returns the tool set exposed inside spawned sub-agents:
// the requested whitelist (or every parent tool), minus meta tools that would
// spawn more agent work and job tools whose runtime manager is not injected into
// sub-agents. When bash is present, it is wrapped to advertise and allow only
// foreground execution.
func SubagentToolRegistry(parent *tool.Registry, names []string) *tool.Registry {
	return SubagentToolRegistryForDepth(parent, names, 1, 1)
}

// SubagentToolRegistryForDepth returns the writer-capable tool set for a spawned
// subagent at childDepth. Recursive delegation tools are available only when the
// child still has room to spawn one more subagent.
func SubagentToolRegistryForDepth(parent *tool.Registry, names []string, childDepth, maxDepth int) *tool.Registry {
	exclude := append([]string(nil), subagentAlwaysHiddenTools...)
	if childDepth >= NormalizeMaxSubagentDepth(maxDepth) {
		exclude = append(exclude, subagentRecursiveTools...)
	}
	exclude = append(exclude, subagentJobTools...)
	sub := FilterRegistry(parent, names, exclude...)
	if bash, ok := sub.Get("bash"); ok {
		sub.Add(foregroundOnlyBash{inner: bash})
	}
	return sub
}

type foregroundOnlyBash struct {
	inner tool.Tool
}

func (b foregroundOnlyBash) Name() string { return "bash" }

func (b foregroundOnlyBash) Description() string {
	desc := strings.TrimSpace(b.inner.Description())
	if desc == "" {
		desc = "Execute a command in the shell and return combined stdout/stderr."
	}
	desc = strings.Replace(desc, "Execute a command in the shell", "Execute a foreground command in the shell", 1)
	return desc + " Background execution is unavailable inside subagents."
}

func (foregroundOnlyBash) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute in the foreground"}},"required":["command"]}`)
}

func (b foregroundOnlyBash) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		RunInBackground bool `json:"run_in_background"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.RunInBackground {
		return "", fmt.Errorf("background bash is unavailable in subagents; run a foreground command or ask the parent agent to start a background job")
	}
	return b.inner.Execute(ctx, args)
}

func (b foregroundOnlyBash) ReadOnly() bool { return b.inner.ReadOnly() }

type readOnlyBash struct {
	inner tool.Tool
}

func (b readOnlyBash) Name() string { return "bash" }

func (b readOnlyBash) Description() string {
	desc := strings.TrimSpace(b.inner.Description())
	if desc == "" {
		desc = "Execute a command in the shell and return combined stdout/stderr."
	}
	desc = strings.Replace(desc, "Execute a command in the shell", "Execute a foreground read-only command in the shell", 1)
	return desc + " Only permission-classified read-only commands are allowed; shell operators, background execution, process preservation, and write-capable arguments are blocked."
}

func (readOnlyBash) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Read-only shell command to execute in the foreground. Must match the permission-layer read-only command policy."}},"required":["command"]}`)
}

func (b readOnlyBash) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	if !permission.BashCommandIsReadOnly(args) {
		return "blocked: read-only subagents can run only permission-classified foreground read-only commands", nil
	}
	return b.inner.Execute(ctx, args)
}

func (readOnlyBash) ReadOnly() bool { return true }

// TaskTool spawns a sub-agent in its own session for a focused sub-task. The
// sub-agent runs with a filtered tool whitelist and the same step budget shape
// as the parent (see Execute); its tool calls are forwarded to the parent's
// event stream nested under this call, while only its final assistant message is
// returned to the parent model. Use cases: keep noisy tool sequences (multi-file
// exploration, repeated grep / read_file) out of the parent's context budget, or
// parallel research across independent areas (the parallel-dispatch path picks
// these up only when readOnly, which task is not).
type TaskTool struct {
	prov                provider.Provider
	pricing             *provider.Pricing
	parentReg           *tool.Registry
	maxSteps            int
	contextWindow       int
	softCompactRatio    float64
	toolResultSnipRatio float64
	compactRatio        float64
	compactForceRatio   float64
	recentKeep          int
	temperature         float64
	archiveDir          string
	keepPolicy          KeepPolicy
	sysPrompt           string
	gate                Gate
	subagentModel       string
	subagentEffort      string
	resolveProvider     func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error)
	transcripts         *SubagentStore
	workspaceRoot       string
	baseModel           string
	baseEffort          string
	identityProfile     func(modelRef, effort string) (string, string)
	maxSubagentDepth    int
	deliveryProfile     bool
	workspaceLease      *workspacelease.Owner
	// scheduler is the session-scoped concurrency + write-claim controller.
	// nil falls back to the legacy jobs.ReserveStart cap for background tasks.
	scheduler *SubagentScheduler
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
}

// NewTaskTool wires a task tool to the parent agent's environment so its
// sub-agents can use the same provider and tools. sysPrompt is the system
// prompt every sub-agent starts with; pass "" for DefaultTaskSystemPrompt. gate
// is the permission gate sub-agents inherit — pass the headless variant so
// deny rules still bite while autonomous sub-agents are never blocked on an
// interactive prompt (there is no UI to answer one).
func NewTaskTool(prov provider.Provider, pricing *provider.Pricing, parentReg *tool.Registry,
	maxSteps, contextWindow, recentKeep int, softCompactRatio, toolResultSnipRatio, compactRatio, compactForceRatio, temperature float64, archiveDir, sysPrompt string, gate Gate,
	keepPolicy KeepPolicy, subagentModel, subagentEffort string, resolveProvider func(string, string) (provider.Provider, *provider.Pricing, int, error)) *TaskTool {
	if sysPrompt == "" {
		sysPrompt = DefaultTaskSystemPrompt
	}
	return &TaskTool{
		prov:                prov,
		pricing:             pricing,
		parentReg:           parentReg,
		maxSteps:            maxSteps,
		contextWindow:       contextWindow,
		recentKeep:          recentKeep,
		softCompactRatio:    softCompactRatio,
		toolResultSnipRatio: toolResultSnipRatio,
		compactRatio:        compactRatio,
		compactForceRatio:   compactForceRatio,
		temperature:         temperature,
		archiveDir:          archiveDir,
		keepPolicy:          keepPolicy,
		sysPrompt:           sysPrompt,
		gate:                gate,
		subagentModel:       subagentModel,
		subagentEffort:      subagentEffort,
		resolveProvider:     resolveProvider,
		maxSubagentDepth:    DefaultMaxSubagentDepth,
	}
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
	t.maxSubagentDepth = NormalizeMaxSubagentDepth(depth)
	return t
}

// WithDeliveryProfile propagates the parent's runtime delivery contract into
// writer-capable sub-agents. Read-only sub-agents may receive the flag too, but
// the mutation gate remains dormant for them.
func (t *TaskTool) WithDeliveryProfile(enabled bool) *TaskTool {
	t.deliveryProfile = enabled
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
func (t *TaskTool) WithScheduler(s *SubagentScheduler) *TaskTool {
	t.scheduler = s
	return t
}

// Scheduler returns the attached session scheduler (may be nil in unit tests).
func (t *TaskTool) Scheduler() *SubagentScheduler {
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

func (t *TaskTool) Name() string { return "task" }

func (t *TaskTool) Description() string {
	return "Spawn a sub-agent for a focused sub-task. Optional profile selects a runAs=subagent Skill whose body becomes the full system prompt (no implicit concise default). Optional write_paths declare non-overlapping write targets so background writers may run in parallel; omitting write_paths on a writer claims the whole workspace and serializes writers. The sub-agent runs in its own session with a filtered tool list (defaults to every parent tool, then applies the subagent boundary: " + subagentToolBoundarySummary + "). Only its final answer is returned."
}

func (t *TaskTool) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "prompt":{"type":"string","description":"What the sub-agent should accomplish. Be specific about the deliverable — the sub-agent does not see this conversation."},
  "description":{"type":"string","description":"Short label for the sub-task (3-7 words). Surfaced in the dispatch line so the user sees what's running."},
  "profile":{"type":"string","description":"Optional runAs=subagent profile name. Resolved at runtime from the Skill store; explicit names may invoke invocation=manual profiles. The profile body becomes the full system prompt."},
  "write_paths":{"type":"array","items":{"type":"string"},"description":"Optional workspace-relative or absolute file/directory paths this writer may modify. Globs and workspace escapes are rejected. Writers without write_paths claim the whole workspace (serializing against every other writer claim). Non-overlapping paths allow parallel writers up to max_parallel_writers. In fleet, multiple whole-workspace claims fail preflight before any task starts."},
  "tools":{"type":"array","items":{"type":"string"},"description":"Optional tool whitelist. When profile sets allowed-tools, this list is intersected (call args cannot expand profile permissions). ` + subagentToolBoundarySummary + `"},
  "max_steps":{"type":"integer","description":"Optional cap on tool-call rounds. Defaults to half the parent's cap (min 5).","minimum":1},
  "run_in_background":{"type":"boolean","description":"Run the sub-agent asynchronously: returns a job id immediately and keeps working across turns. Collect its final answer with wait, and you'll be notified when it finishes. Use for long, independent sub-tasks you don't need to block on right now."},
  "model":{"type":"string","description":"Optional model override for the sub-agent (a configured provider/model name). Precedence: persistent profile config, this argument, profile frontmatter, global subagent default, parent model."},
  "effort":{"type":"string","description":"Optional reasoning effort for the sub-agent (e.g. high, max). Same precedence as model."},
  "continue_from":{"type":"string","description":"Continue a prior compatible subagent transcript in the current conversation context. Pass only the 'sa_...' value from the prior result's 'Subagent reference: ...' line. If the ref belongs to an ancestor conversation, the framework continues a current-conversation copy."}
},
"required":["prompt"]
}`)
}

// ReadOnly is false: a sub-agent can invoke any whitelisted tool, including
// writers. Conservative classification keeps the parallel-dispatch path from
// running two sub-agents at once and letting their writes race.
func (t *TaskTool) ReadOnly() bool { return false }

// ResolveProfile extracts model/effort from task args (and optional profile
// overrides) for dispatch-line display. Runtime execution re-resolves with the
// full precedence chain.
func (t *TaskTool) ResolveProfile(args json.RawMessage) *event.Profile {
	var p struct {
		Model   string `json:"model"`
		Effort  string `json:"effort"`
		Profile string `json:"profile"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil
	}
	profileModel, profileEffort := "", ""
	configModel, configEffort := "", ""
	if name := strings.TrimSpace(p.Profile); name != "" {
		if def, err := ResolveProfileDefinition(t.profileLookup, name); err == nil {
			profileModel, profileEffort = def.Model, def.Effort
		}
		if t.profileConfigModel != nil {
			configModel = t.profileConfigModel(name)
		}
		if t.profileConfigEffort != nil {
			configEffort = t.profileConfigEffort(name)
		}
	}
	model, effort := ResolveModelEffort(
		configModel, configEffort,
		p.Model, p.Effort,
		profileModel, profileEffort,
		t.subagentModel, t.subagentEffort,
	)
	if model == "" && effort == "" {
		return nil
	}
	return &event.Profile{Model: model, Effort: effort}
}

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

func (r *ReadOnlyTaskTool) ResolveProfile(args json.RawMessage) *event.Profile {
	if r == nil || r.task == nil {
		return nil
	}
	return r.task.ResolveProfile(args)
}

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
	if strings.TrimSpace(p.Prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}

	// Ordinary read_only_task keeps the concise default system prompt and does
	// not accept profile/write_paths (use fleet with read_only for those).
	releaseSlot, err := r.task.acquireSlot(ctx, AcquireRequest{
		Writer: false,
		Nested: SubagentDepth(ctx) > 0,
		Label:  firstNonEmpty(p.Description, "read_only_task"),
	})
	if err != nil {
		return "", err
	}
	defer releaseSlot()

	maxSteps := r.task.childMaxSteps(p.MaxSteps)

	childDepth, err := r.task.nextSubagentDepth(ctx)
	if err != nil {
		return "", err
	}
	subReg := ReadOnlySubagentToolRegistryForDepth(r.task.parentReg, p.Tools, childDepth, r.task.maxDepth())
	if subReg.Len() == 0 {
		return "", fmt.Errorf("read_only_task has no read-only tools available")
	}
	modelRef, effortRef := r.task.effectiveProfile(p.Model, p.Effort)
	prov, pricing, ctxWin, err := r.task.resolveSubSessionRuntime(modelRef, effortRef)
	if err != nil {
		return "", fmt.Errorf("read-only sub-agent profile: %w", err)
	}
	answer, err := r.task.runReadOnlySubSession(ctx, p.Prompt, subReg, subSink(ctx), maxSteps, prov, pricing, ctxWin, NewSession(DefaultReadOnlyTaskSystemPrompt), childDepth)
	if err != nil {
		return "", err
	}
	return GuardSubagentHostDecisionText(answer), nil
}

// childMaxSteps resolves a sub-agent's step budget. An explicit request wins.
// Otherwise mirror the parent: a finite parent caps the child at half its
// budget (min 5) so a delegated sub-task stays shorter than the whole turn; an
// unbounded parent yields an unbounded child (it shares the parent's ctx, so
// cancelling the turn stops it, and it compacts its own context — the same
// bounds the parent has). Shared by task, read_only_task, and parallel_tasks
// children so the default cannot drift per call site.
func (t *TaskTool) childMaxSteps(requested int) int {
	if requested > 0 {
		return requested
	}
	if t.maxSteps <= 0 {
		return 0
	}
	half := t.maxSteps / 2
	if half < 5 {
		half = 5
	}
	return half
}

func (t *TaskTool) effectiveProfile(model, effort string) (string, string) {
	model = strings.TrimSpace(model)
	effort = strings.TrimSpace(effort)
	if model == "" {
		model = strings.TrimSpace(t.subagentModel)
	}
	if effort == "" {
		effort = strings.TrimSpace(t.subagentEffort)
	}
	return model, effort
}

func (t *TaskTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
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
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}

	spec, err := t.buildTaskSpec(ctx, p.Prompt, p.Description, p.Profile, p.WritePaths, p.Tools, p.MaxSteps, p.Model, p.Effort, p.ContinueFrom, p.ForkFrom, p.RunInBackground, false)
	if err != nil {
		return "", err
	}
	return t.RunProfileSpec(ctx, spec)
}

// buildTaskSpec resolves profile, tools, model/effort, and write claims for a
// single task/fleet item. forceReadOnly forces the read-only registry.
func (t *TaskTool) buildTaskSpec(ctx context.Context, prompt, description, profile string, writePaths, tools []string, maxSteps int, model, effort, continueFrom, forkFrom string, background, forceReadOnly bool) (ProfileExecSpec, error) {
	spec := ProfileExecSpec{
		Kind:            "task",
		Name:            "task",
		Prompt:          prompt,
		Description:     description,
		CallTools:       tools,
		MaxSteps:        maxSteps,
		ContinueFrom:    strings.TrimSpace(continueFrom),
		ForkFrom:        strings.TrimSpace(forkFrom),
		RunInBackground: background,
		Nested:          SubagentDepth(ctx) > 0,
		SystemPrompt:    t.sysPrompt,
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
		spec.Profile = def.Name
		spec.Name = def.Name
		spec.Kind = "skill"
		spec.SystemPrompt = def.Body
		spec.UseProfilePrompt = true
		profileTools = def.AllowedTools
		profileModel, profileEffort = def.Model, def.Effort
		if def.ReadOnly {
			readOnly = true
		}
	}
	spec.ReadOnly = readOnly
	spec.ProfileTools = profileTools

	configModel, configEffort := "", ""
	if profile != "" {
		if t.profileConfigModel != nil {
			configModel = t.profileConfigModel(profile)
		}
		if t.profileConfigEffort != nil {
			configEffort = t.profileConfigEffort(profile)
		}
	}
	spec.Model, spec.Effort = ResolveModelEffort(
		configModel, configEffort,
		model, effort,
		profileModel, profileEffort,
		t.subagentModel, t.subagentEffort,
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
		spec.WritePaths = claims
		if requireClaim && claims.Empty() {
			return ProfileExecSpec{}, fmt.Errorf("writer claim resolved empty")
		}
	} else if len(writePaths) > 0 {
		return ProfileExecSpec{}, fmt.Errorf("write_paths is not valid for read-only tasks")
	}
	return spec, nil
}

func (t *TaskTool) resolveWriterClaims(writePaths []string, requireClaim bool) (WritePathSet, error) {
	if len(writePaths) > 0 {
		return NormalizeWritePaths(t.workspaceRoot, writePaths)
	}
	if !requireClaim {
		return WritePathSet{}, nil
	}
	return WholeWorkspaceWriteClaim(t.workspaceRoot)
}

// RunProfileSpec executes a unified profile/task specification. Shared by task,
// fleet items, and boot-wired skill runners so prompt, tools, claims, and
// scheduling cannot drift across entry points.
func (t *TaskTool) RunProfileSpec(ctx context.Context, spec ProfileExecSpec) (string, error) {
	if t == nil {
		return "", fmt.Errorf("task tool is not configured")
	}
	if strings.TrimSpace(spec.Prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}
	if strings.TrimSpace(spec.SystemPrompt) == "" {
		if spec.UseProfilePrompt {
			return "", fmt.Errorf("profile system prompt is empty")
		}
		spec.SystemPrompt = t.sysPrompt
	}

	maxSteps := t.childMaxSteps(spec.MaxSteps)
	childDepth, err := t.nextSubagentDepth(ctx)
	if err != nil {
		return "", err
	}

	toolNames, err := IntersectToolLists(t.parentReg, spec.ProfileTools, spec.CallTools)
	if err != nil {
		return "", err
	}
	var subReg *tool.Registry
	if spec.ReadOnly {
		subReg = ReadOnlySubagentToolRegistryForDepth(t.parentReg, toolNames, childDepth, t.maxDepth())
		if subReg.Len() == 0 {
			return "", fmt.Errorf("no read-only tools available for this sub-agent")
		}
	} else {
		subReg = t.buildSubReg(toolNames, childDepth)
		// Explicit paths are an execution boundary and rebind/drop tools that
		// cannot honor it. A synthesized whole-workspace claim is a scheduling
		// boundary for omitted write_paths; it preserves the legacy registry and
		// the parent session's existing sandbox/permission boundaries.
		if !spec.WritePaths.Empty() && !spec.WritePaths.WholeWorkspace {
			keepBash := t.bashCanEnforceWriteRoots()
			bound, removed := BindWritePaths(subReg, spec.WritePaths, t.workspaceRoot, keepBash)
			subReg = bound
			if len(removed) > 0 && subReg.Len() == 0 {
				return "", fmt.Errorf("no path-bound write tools available after dropping unbound writers: %s", strings.Join(removed, ", "))
			}
		}
	}

	modelRef, effortRef := spec.Model, spec.Effort
	parentID, parent, _, _ := CallContext(ctx)
	run, err := t.prepareTranscriptRunWithPrompt(subReg, modelRef, effortRef, ParentSession(ctx), parentID, spec.ContinueFrom, spec.ForkFrom, spec.SystemPrompt, spec.Kind, spec.Name)
	if err != nil {
		return "", err
	}
	prov, pricing, ctxWin, err := t.resolveSubSessionRuntime(modelRef, effortRef)
	if err != nil {
		run.Release()
		return "", fmt.Errorf("sub-agent profile: %w", err)
	}

	isWriter := !spec.ReadOnly
	acquireReq := AcquireRequest{
		Writer:     isWriter,
		WritePaths: spec.WritePaths,
		Nested:     spec.Nested,
		Label:      firstNonEmpty(spec.Description, spec.Name, "task"),
	}
	// Defensive fallback for callers that manually construct a background spec
	// instead of going through buildTaskSpec.
	if isWriter && spec.WritePaths.Empty() && spec.RunInBackground {
		whole, werr := WholeWorkspaceWriteClaim(t.workspaceRoot)
		if werr != nil {
			run.Release()
			return "", werr
		}
		acquireReq.WritePaths = whole
		spec.WritePaths = whole
	}

	runSession := func(runCtx context.Context, sink event.Sink) (string, error) {
		if spec.ReadOnly {
			return t.runReadOnlySubSession(runCtx, spec.Prompt, subReg, sink, maxSteps, prov, pricing, ctxWin, run.Session, childDepth)
		}
		return t.runSubSession(runCtx, spec.Prompt, subReg, sink, maxSteps, prov, pricing, ctxWin, run.Session, childDepth)
	}

	if spec.RunInBackground {
		jm, ok := jobs.FromContext(ctx)
		if !ok {
			run.Release()
			return "", fmt.Errorf("background execution is not available in this context")
		}
		// Legacy hard-cap remains only when no scheduler is attached. With a
		// scheduler, return the job immediately and queue for a slot inside the
		// job so the parent turn is not blocked at concurrency limits.
		var releaseStart func()
		if t.scheduler == nil {
			var running int
			var okReserve bool
			releaseStart, running, okReserve = jm.ReserveStartForSession(jobs.SessionFromContext(ctx), "task", maxConcurrentBackgroundTasks)
			if !okReserve {
				run.Release()
				return "", fmt.Errorf("%d background tasks are already running for this session (limit %d); collect their results with wait — or run this sub-task in the foreground — before starting more", running, maxConcurrentBackgroundTasks)
			}
			defer releaseStart()
		} else {
			releaseStart = func() {}
		}
		nested := subSinkFor(parentID, parent)
		label := firstNonEmpty(spec.Description, spec.Name, "task")
		if t.transcripts != nil && run != nil && run.Ref != "" {
			if err := t.transcripts.MarkRunning(run); err != nil {
				releaseStart()
				run.Release()
				return "", err
			}
		}
		parentSession := ParentSession(ctx)
		backgroundEvidence := evidence.NewLedger()
		// Capture acquire request by value for the job goroutine.
		slotReq := acquireReq
		job := jm.StartForSession(jobs.SessionFromContext(ctx), "task", label, func(jobCtx context.Context, _ io.Writer) (result string, err error) {
			jobCtx = WithParentSession(jobCtx, parentSession)
			jobCtx = evidence.WithLedger(jobCtx, backgroundEvidence)
			defer run.Release()
			defer func() { jobs.PublishEvidence(jobCtx, backgroundEvidence.Summary()) }()
			defer func() {
				if r := recover(); r != nil {
					panicErr := fmt.Errorf("internal error: panic: %v\n%s", r, debug.Stack())
					result = FormatSubagentRunResult("", run, true)
					err = errors.Join(panicErr, t.transcripts.SaveFailed(run))
				}
			}()
			// Queue for a concurrency/write slot here — not before Start —
			// so the parent tool call returns a job id immediately.
			releaseSlot, slotErr := t.acquireSlot(jobCtx, slotReq)
			if slotErr != nil {
				return FormatSubagentRunResult("", run, true), errors.Join(slotErr, t.transcripts.SaveFailed(run))
			}
			defer releaseSlot()
			answer, err := runSession(jobCtx, nested)
			if err != nil {
				return FormatSubagentRunResult("", run, true), errors.Join(err, t.transcripts.SaveFailed(run))
			}
			if err := t.transcripts.SaveCompleted(run); err != nil {
				return FormatSubagentRunResult("", run, true), errors.Join(err, t.transcripts.SaveFailed(run))
			}
			return FormatSubagentRunResult(answer, run, false), nil
		})
		releaseStart()
		queuedNote := ""
		if t.scheduler != nil {
			queuedNote = " It may wait in the session queue until a concurrency/write slot is free."
		}
		if run != nil && run.Ref != "" {
			return fmt.Sprintf("Started background task %q (%s).%s\n%s\nIt runs across turns; collect its final answer with wait (or wait will return it once done), and you'll be notified when it finishes.", job.ID, label, queuedNote, FormatSubagentReference(run)), nil
		}
		return fmt.Sprintf("Started background task %q (%s).%s It runs across turns; collect its final answer with wait (or wait will return it once done), and you'll be notified when it finishes.", job.ID, label, queuedNote), nil
	}

	// Foreground: acquire a slot (queue if needed), then run synchronously.
	releaseSlot, err := t.acquireSlot(ctx, acquireReq)
	if err != nil {
		run.Release()
		return "", err
	}
	defer releaseSlot()
	defer run.Release()
	answer, err := runSession(ctx, subSink(ctx))
	if err != nil {
		return "", errors.Join(err, t.transcripts.SaveFailed(run))
	}
	if t.transcripts != nil && run.Ref != "" {
		if err := t.transcripts.SaveCompleted(run); err != nil {
			return "", errors.Join(err, t.transcripts.SaveFailed(run))
		}
		return FormatSubagentRunResult(answer, run, false), nil
	}
	return GuardSubagentHostDecisionText(answer), nil
}

func (t *TaskTool) acquireSlot(ctx context.Context, req AcquireRequest) (func(), error) {
	noop := func() {}
	if t.scheduler == nil {
		return noop, nil
	}
	return t.scheduler.Acquire(ctx, req)
}

func (t *TaskTool) bashCanEnforceWriteRoots() bool {
	if t != nil && t.bashSandboxEnforced != nil {
		return t.bashSandboxEnforced()
	}
	return false
}

func (t *TaskTool) prepareTranscriptRunWithPrompt(subReg *tool.Registry, modelRef, effortRef, parentSession, parentID, continueFrom, legacyForkFrom, systemPrompt, kind, name string) (*SubagentRun, error) {
	continueFrom = strings.TrimSpace(continueFrom)
	legacyForkFrom = strings.TrimSpace(legacyForkFrom)
	parentSession = strings.TrimSpace(parentSession)
	if continueFrom != "" && legacyForkFrom != "" {
		return nil, fmt.Errorf("continue_from and fork_from are mutually exclusive; pass only continue_from")
	}
	if t.transcripts == nil {
		return nil, fmt.Errorf("subagent transcript store is required")
	}
	if systemPrompt == "" {
		systemPrompt = t.sysPrompt
	}
	if kind == "" {
		kind = "task"
	}
	if name == "" {
		name = "task"
	}
	if parentSession == "" {
		if continueFrom != "" || legacyForkFrom != "" {
			return nil, fmt.Errorf("subagent continuation requires a persisted session; none is active in this run")
		}
		return EphemeralSubagentRun(systemPrompt), nil
	}
	identityModel, identityEffort := t.effectiveIdentity(modelRef, effortRef)
	spec := SubagentSpec{
		Kind:             kind,
		Name:             name,
		WorkspaceRoot:    t.workspaceRoot,
		ParentSession:    parentSession,
		ParentToolCallID: parentID,
		SystemPrompt:     systemPrompt,
		Registry:         subReg,
		Model:            identityModel,
		Effort:           identityEffort,
	}
	if continueFrom != "" {
		return t.transcripts.PrepareContinue(continueFrom, spec)
	}
	if legacyForkFrom != "" {
		return t.transcripts.PrepareLegacyForkFrom(legacyForkFrom, spec)
	}
	return t.transcripts.PrepareFresh(spec)
}

func (t *TaskTool) effectiveIdentity(modelRef, effort string) (string, string) {
	if t.identityProfile != nil {
		model, eff := t.identityProfile(modelRef, effort)
		return strings.TrimSpace(model), strings.TrimSpace(eff)
	}
	return t.effectiveModelIdentity(modelRef), t.effectiveEffortIdentity(effort)
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
	return SubagentToolRegistryForDepth(t.parentReg, names, childDepth, t.maxDepth())
}

func (t *TaskTool) maxDepth() int {
	if t == nil {
		return DefaultMaxSubagentDepth
	}
	if t.maxSubagentDepth == 0 {
		return DefaultMaxSubagentDepth
	}
	return NormalizeMaxSubagentDepth(t.maxSubagentDepth)
}

func (t *TaskTool) nextSubagentDepth(ctx context.Context) (int, error) {
	current := SubagentDepth(ctx)
	next := current + 1
	maxDepth := t.maxDepth()
	if next > maxDepth {
		return 0, fmt.Errorf("subagent delegation depth limit reached (max_subagent_depth=%d)", maxDepth)
	}
	return next, nil
}

// FilterRegistry builds a sub-registry from parent: the named whitelist (empty =
// every parent tool), minus any excluded names. Used to scope what a spawned
// sub-agent — a `task` sub-agent or a subagent skill — may call, e.g. excluding
// `task` to bar recursive nesting, or restricting to a skill's allowed-tools.
func FilterRegistry(parent *tool.Registry, names []string, exclude ...string) *tool.Registry {
	sub := tool.NewRegistry()
	if parent == nil {
		return sub
	}
	ex := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		ex[e] = true
	}
	src := names
	if len(src) == 0 {
		src = parent.Names()
	} else {
		src = expandToolPatterns(parent, src)
	}
	for _, name := range src {
		if ex[name] {
			continue
		}
		if tl, ok := parent.Get(name); ok {
			sub.Add(tl)
		}
	}
	return sub
}

var plannerNonResearchTools = []string{
	"ask",
	"bash_output",
	"complete_step",
	"slash_command",
	"todo_write",
	"wait",
}

// PlannerToolRegistry returns the tool set exposed to the two-model planner:
// read-only research tools only. It deliberately excludes workflow/meta tools
// that are technically read-only but can prompt the user, update visible task
// state, wait on jobs, or expand commands instead of inspecting context.
func PlannerToolRegistry(parent *tool.Registry) *tool.Registry {
	exclude := append(SubagentMetaTools(), plannerNonResearchTools...)
	return FilterReadOnlyRegistry(parent, exclude...)
}

// ReadOnlySubagentToolRegistry returns the tool set exposed to read-only
// sub-agents: read-only research tools plus a bash wrapper that enforces the
// permission-layer read-only command policy at execution time. Workflow/meta tools are
// excluded even when their Tool.ReadOnly contract is true.
func ReadOnlySubagentToolRegistry(parent *tool.Registry, names []string) *tool.Registry {
	return ReadOnlySubagentToolRegistryForDepth(parent, names, 1, 1)
}

// ReadOnlySubagentToolRegistryForDepth returns the tool set exposed to read-only
// subagents. It permits only read-only delegation tools while another depth
// layer is available.
func ReadOnlySubagentToolRegistryForDepth(parent *tool.Registry, names []string, childDepth, maxDepth int) *tool.Registry {
	exclude := append([]string(nil), subagentAlwaysHiddenTools...)
	if childDepth >= NormalizeMaxSubagentDepth(maxDepth) {
		exclude = append(exclude, subagentRecursiveTools...)
	} else {
		exclude = append(exclude, "task", "run_skill", "explore", "research", "review", "security_review")
	}
	exclude = append(exclude, subagentJobTools...)
	exclude = append(exclude, plannerNonResearchTools...)
	exclude = append(exclude, readOnlySubagentWorkflowTools...)
	ex := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		ex[e] = true
	}
	sub := tool.NewRegistry()
	if parent == nil {
		return sub
	}
	src := names
	if len(src) == 0 {
		src = parent.Names()
	} else {
		src = expandToolPatterns(parent, src)
	}
	for _, name := range src {
		if ex[name] {
			continue
		}
		tl, ok := parent.Get(name)
		if !ok {
			continue
		}
		if name == "bash" {
			sub.Add(readOnlyBash{inner: tl})
			continue
		}
		if !tl.ReadOnly() {
			continue
		}
		if u, ok := tl.(tool.PlanModeUntrustedReadOnly); ok && u.PlanModeUntrustedReadOnly() {
			continue
		}
		sub.Add(tl)
	}
	return sub
}

// expandToolPatterns resolves explicit wildcard allowlist entries from imported
// agent profiles against the current registry. Expansion is deterministic and
// session-local, so optional MCP tools only enter a child after connection.
func expandToolPatterns(parent *tool.Registry, names []string) []string {
	if parent == nil {
		return nil
	}
	available := parent.Names()
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, name := range names {
		if !strings.ContainsAny(name, "*?[") {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
			continue
		}
		for _, candidate := range available {
			matched, err := filepath.Match(name, candidate)
			if err == nil && matched && !seen[candidate] {
				seen[candidate] = true
				out = append(out, candidate)
			}
		}
	}
	return out
}

// FilterReadOnlyRegistry builds a sub-registry containing only tools whose
// ReadOnly contract is true, minus explicit exclusions.
func FilterReadOnlyRegistry(parent *tool.Registry, exclude ...string) *tool.Registry {
	ex := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		ex[e] = true
	}
	sub := tool.NewRegistry()
	if parent == nil {
		return sub
	}
	for _, name := range parent.Names() {
		if ex[name] {
			continue
		}
		tl, ok := parent.Get(name)
		if !ok || !tl.ReadOnly() {
			continue
		}
		if u, ok := tl.(tool.PlanModeUntrustedReadOnly); ok && u.PlanModeUntrustedReadOnly() {
			continue
		}
		sub.Add(tl)
	}
	return sub
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

func (t *TaskTool) runSubSession(ctx context.Context, prompt string, subReg *tool.Registry, sink event.Sink, maxSteps int, prov provider.Provider, pricing *provider.Pricing, ctxWin int, sess *Session, childDepth int) (string, error) {
	opts := t.subagentOptions(ctx, maxSteps, pricing, ctxWin, childDepth)
	// Capture the pristine task before host framing is prepended: delivery
	// intent classification must judge the task, not the wrapper.
	opts.ClassifierTaskText = prompt
	prompt = t.withWorkspaceContext(prompt)
	return RunSubAgentWithSession(ctx, prov, subReg, sess, prompt, opts, sink)
}

func (t *TaskTool) runReadOnlySubSession(ctx context.Context, prompt string, subReg *tool.Registry, sink event.Sink, maxSteps int, prov provider.Provider, pricing *provider.Pricing, ctxWin int, sess *Session, childDepth int) (string, error) {
	opts := t.subagentOptions(ctx, maxSteps, pricing, ctxWin, childDepth)
	// Capture the pristine task before host framing is prepended: delivery
	// intent classification must judge the task, not the wrapper.
	opts.ClassifierTaskText = prompt
	prompt = t.withWorkspaceContext(prompt)
	return RunReadOnlySubAgentWithSession(ctx, prov, subReg, sess, prompt, opts, sink)
}

// subagentOptions is the single construction point for the run options every
// sub-agent spawned through this tool shares (task, read_only_task, and
// parallel_tasks children). Compaction, language preferences, and depth limits
// must stay uniform across those paths — add new fields here, not at call sites.
func (t *TaskTool) subagentOptions(ctx context.Context, maxSteps int, pricing *provider.Pricing, ctxWin, childDepth int) Options {
	return Options{
		MaxSteps:            maxSteps,
		Temperature:         t.temperature,
		Pricing:             pricing,
		UsageSource:         event.UsageSourceSubagent,
		Gate:                t.gate,
		ContextWindow:       ctxWin,
		RecentKeep:          t.recentKeep,
		SoftCompactRatio:    t.softCompactRatio,
		ToolResultSnipRatio: t.toolResultSnipRatio,
		CompactRatio:        t.compactRatio,
		CompactForceRatio:   t.compactForceRatio,
		ArchiveDir:          t.archiveDir,
		KeepPolicy:          t.keepPolicy,
		ResponseLanguage:    ResponseLanguageFromContext(ctx),
		ReasoningLanguage:   ReasoningLanguageFromContext(ctx),
		SubagentDepth:       childDepth,
		MaxSubagentDepth:    t.maxDepth(),
		DeliveryProfile:     t.deliveryProfile,
		WorkspaceLease:      t.workspaceLease,
	}
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
// The implementation lives in internal/tool so the skill tools share the exact
// same phrase list and notice.
func GuardSubagentHostDecisionText(answer string) string {
	return tool.GuardSubagentHostDecisionText(answer)
}

// maxReviewReportNudges bounds the in-session completion nudges sent to a
// review subagent that finished without submitting review_report. Each nudge is
// one cheap continuation request on the same (cached) subagent session — far
// cheaper than discarding the run and re-reviewing from scratch.
const maxReviewReportNudges = 2

// reviewReportTaskContract is appended to the task prompt of a review subagent
// whose run must end with a typed report. The skill body describes how to
// review; this states the non-negotiable submission protocol.
func reviewReportTaskContract(kind evidence.ReviewKind) string {
	return fmt.Sprintf(`<review-report-contract event="SubagentReviewReport">
Before your final answer you MUST call the review_report tool exactly once with kind=%q, your verdict (pass | warn | block), reviewed_paths listing only files you actually read this run, and your findings. The host discards a review run that ends without a successful review_report call — your prose summary alone does not count.
</review-report-contract>`, string(kind))
}

// reviewReportNudgePrompt asks an already-finished review subagent to submit
// the missing typed report without redoing the review.
func reviewReportNudgePrompt(kind evidence.ReviewKind) string {
	return fmt.Sprintf("You finished the review without calling the review_report tool, so the host cannot accept the run yet. Do not redo the review. Call review_report now with kind=%q, your verdict (pass | warn | block), reviewed_paths listing only the files you actually read in this conversation, and the findings you already reported. Then restate your final verdict in one sentence.", string(kind))
}

// RunSubAgentWithSession continues an existing sub-agent session with prompt and
// returns the latest final assistant answer. Fresh sub-agents pass a newly-created
// session; continued sub-agents pass a loaded transcript session.
func RunSubAgentWithSession(ctx context.Context, prov provider.Provider, reg *tool.Registry, sess *Session, prompt string, opts Options, sink event.Sink) (string, error) {
	if sess == nil {
		return "", fmt.Errorf("sub-agent session is nil")
	}
	if opts.SubagentDepth > 0 {
		ctx = WithSubagentDepth(ctx, opts.SubagentDepth)
	}
	// Callers that wrap the prompt themselves (runSubSession) set
	// ClassifierTaskText before wrapping; for everyone else the prompt is
	// still pristine here, so capture it before host framing is prepended.
	if strings.TrimSpace(opts.ClassifierTaskText) == "" {
		opts.ClassifierTaskText = prompt
	}
	planWorkflow := PlanModeFromContext(ctx)
	if opts.SubagentDepth > 0 && isFreshSubagentSession(sess) {
		prompt = subagentStartContext + "\n\n" + prompt
	}
	if planWorkflow && !strings.Contains(prompt, planmode.Marker) {
		prompt = planmode.Marker + "\n\n" + prompt
	}
	if kind := opts.RequireReviewReportKind; kind != "" {
		prompt = prompt + "\n\n" + reviewReportTaskContract(kind)
	}
	sub := New(prov, reg, sess, opts, sink)
	sub.SetPlanMode(planWorkflow)
	if err := sub.Run(ctx, prompt); err != nil {
		// Still merge any partial child evidence so parent gates see real writes.
		mergeChildEvidence(ctx, sub)
		if answer, ok := salvageReadinessExhaustedAnswer(sub, sess, opts, err); ok {
			return answer, nil
		}
		return "", fmt.Errorf("sub-agent: %w", err)
	}
	// Review/security subagents must hand back a typed report the parent's
	// delivery gate can verify; prose alone would leave the gate demanding a
	// review forever with no way to tell why it never arrives. A run that
	// finished without the report gets bounded completion nudges on the same
	// session (evidence preserved, so review_report can still cite the reads it
	// already earned) before the whole run is declared failed.
	if kind := opts.RequireReviewReportKind; kind != "" {
		nudges := 0
		for !sub.HasSuccessfulReviewReport(kind) && nudges < maxReviewReportNudges {
			nudges++
			sub.preserveEvidenceOnce = true
			if err := sub.Run(ctx, reviewReportNudgePrompt(kind)); err != nil {
				mergeChildEvidence(ctx, sub)
				return "", fmt.Errorf("sub-agent: %w", err)
			}
		}
		if !sub.HasSuccessfulReviewReport(kind) {
			mergeChildEvidence(ctx, sub)
			dumpRef := dumpFailedSubagentSession(opts.ArchiveDir, string(kind), sess)
			return "", fmt.Errorf("%s subagent finished without submitting review_report (kind=%s) even after %d host nudges; the report must be submitted by the review subagent itself (the parent has no review_report tool) — re-run the review skill%s", kind, kind, nudges, dumpRef)
		}
	}
	mergeChildEvidence(ctx, sub)
	if answer := latestAssistantAnswer(sess); answer != "" {
		return answer, nil
	}
	return "", fmt.Errorf("sub-agent finished without producing a final answer")
}

// readOnlyAgentConstruction is the single pairing every strictly read-only
// loop shares: the permanent ReadOnlyExecution flag plus the final registry
// filter. Batch children (RunReadOnlySubAgentWithSession) and the interactive
// two-model planner (NewReadOnlyAgent) both build through it, so a missed call
// site cannot set only half the boundary.
func readOnlyAgentConstruction(reg *tool.Registry, opts Options) (*tool.Registry, Options) {
	opts.ReadOnlyExecution = true
	return strictReadOnlyExecutionRegistry(reg), opts
}

// NewReadOnlyAgent constructs a long-lived, strictly read-only agent (the
// two-model planner) through the shared construction boundary.
func NewReadOnlyAgent(prov provider.Provider, reg *tool.Registry, sess *Session, opts Options, sink event.Sink) *Agent {
	reg, opts = readOnlyAgentConstruction(reg, opts)
	return New(prov, reg, sess, opts, sink)
}

// RunReadOnlySubAgentWithSession is the construction boundary for every
// strictly read-only child loop. Registry filtering limits the visible surface;
// this permanent execution flag also re-checks targets resolved dynamically by
// proxy tools such as use_capability.
func RunReadOnlySubAgentWithSession(ctx context.Context, prov provider.Provider, reg *tool.Registry, sess *Session, prompt string, opts Options, sink event.Sink) (string, error) {
	reg, opts = readOnlyAgentConstruction(reg, opts)
	return RunSubAgentWithSession(ctx, prov, reg, sess, prompt, opts, sink)
}

// strictReadOnlyExecutionRegistry is the final construction-time filter shared
// by every strict child. Callers still apply role-specific filtering (review,
// planner, profile allowlists), while this layer guarantees that a missed call
// site cannot expose writers, destructive MCP tools, untrusted readers, or an
// untrusted host-starting target to the model.
func strictReadOnlyExecutionRegistry(reg *tool.Registry) *tool.Registry {
	filtered := tool.NewRegistry()
	if reg == nil {
		return filtered
	}
	for _, name := range reg.Names() {
		target, ok := reg.Get(name)
		if !ok || !target.ReadOnly() || planModeUntrustedReadOnly(target) || mcpDestructiveHint(target) {
			continue
		}
		// An installed MCP reader needs a positive trust authority (receipts),
		// not a server hint carried through the no-TrustManager compatibility
		// path: strict children fail closed without a trust store.
		if isInstalledMCPTool(target) {
			if authority, ok := target.(tool.ReadOnlyExecutionTrustAuthority); !ok || !authority.ReadOnlyExecutionTrustAuthority() {
				continue
			}
		}
		if mutation, ok := target.(tool.ReadOnlyExecutionHostMutation); ok && mutation.ReadOnlyExecutionHostMutation() && !readOnlyExecutionAllowsTrustedMCPStartup(target) {
			continue
		}
		filtered.Add(target)
	}
	return filtered
}

// latestAssistantAnswer walks the session backwards for the last assistant
// message with content — that's the sub-agent's final answer. Intermediate
// assistant messages with tool_calls but no text don't count.
func latestAssistantAnswer(sess *Session) string {
	if sess == nil {
		return ""
	}
	for i := len(sess.Messages) - 1; i >= 0; i-- {
		m := sess.Messages[i]
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			return m.Content
		}
	}
	return ""
}

// salvageReadinessExhaustedAnswer degrades a sub-agent's readiness exhaustion
// from a hard failure to an explicitly unverified result. The gate exists to
// stop unverified *claims*, not to discard finished *work*: when the child has
// a real successful mutation on disk and a visible answer, failing the whole
// run makes the parent believe the work is broken and spawn repair tasks for
// changes that already landed — the failure cascade users see as a wall of
// "background task failed" notices. The child's receipts were already merged
// into the parent ledger, so the parent's own delivery gates still require
// verification and review of those writes before it can final-answer.
//
// Salvage is refused when the child produced no successful mutation (an
// unbacked "done" claim must keep failing, e.g. a spoofed or lazy run) and for
// report-required review sub-agents, whose contract is the typed review_report
// rather than prose.
func salvageReadinessExhaustedAnswer(sub *Agent, sess *Session, opts Options, err error) (string, bool) {
	var readinessErr *FinalReadinessError
	if !errors.As(err, &readinessErr) {
		return "", false
	}
	if opts.RequireReviewReportKind != "" {
		return "", false
	}
	if sub == nil || !sub.EvidenceSummary().HasMutation() {
		return "", false
	}
	answer := latestAssistantAnswer(sess)
	if answer == "" {
		return "", false
	}
	return "[unverified] The sub-agent finished its work but exhausted the host delivery sign-off checks before reporting (" +
		readinessErr.Reason +
		"). Its successful writes are already on disk and its receipts were merged into this turn's evidence. " +
		"Inspect the diff and run the relevant checks before relying on the result below; do not re-run or \"fix\" the same work without first checking what already changed.\n\nSub-agent answer:\n" +
		answer, true
}

// dumpFailedSubagentSession best-effort persists a failed report-required
// subagent transcript for post-hoc diagnosis (read-only skill subagents are
// otherwise ephemeral, so a protocol failure leaves no trace). Returns a
// human-readable suffix naming the dump, or "" when disabled/failed.
func dumpFailedSubagentSession(archiveDir, kind string, sess *Session) string {
	if strings.TrimSpace(archiveDir) == "" || sess == nil {
		return ""
	}
	dir := filepath.Join(archiveDir, "subagent-report-failures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%d.jsonl", kind, time.Now().UnixNano()))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return ""
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, m := range sess.Messages {
		if err := enc.Encode(m); err != nil {
			return ""
		}
	}
	return "; transcript dumped to " + path
}

// mergeChildEvidence folds a sub-agent's real receipts into the parent ledger
// carried on ctx. Meta tools themselves are never mutations.
func mergeChildEvidence(ctx context.Context, sub *Agent) {
	if sub == nil {
		return
	}
	parent, ok := evidence.FromContext(ctx)
	if !ok || parent == nil {
		return
	}
	parent.MergeChild(sub.EvidenceSummary())
}

// EvidenceSummary exports this agent's turn-scoped receipts for parent merge.
func (a *Agent) EvidenceSummary() evidence.ChildEvidenceSummary {
	if a == nil || a.evidence == nil {
		return evidence.ChildEvidenceSummary{}
	}
	return a.evidence.Summary()
}

func isFreshSubagentSession(sess *Session) bool {
	if sess == nil {
		return false
	}
	snap := sess.Snapshot()
	return len(snap) == 1 && snap[0].Role == provider.RoleSystem
}

// NestedSink returns a sink that forwards a sub-agent's tool activity to the
// parent stream, nested under the tool call carried by ctx, so a frontend shows
// it beneath that call (the same nesting `task` uses). Falls back to the given
// sink when ctx carries no call context. Used by subagent skills.
func NestedSink(ctx context.Context, fallback event.Sink) event.Sink {
	parentID, parent, _, ok := CallContext(ctx)
	if !ok || parent == nil {
		return fallback
	}
	return subSinkFor(parentID, parent)
}

// subSink forwards a sub-agent's tool dispatch/result events and billable usage
// to the parent's event stream. Only tool activity is nested visually; the
// sub-agent's text/reasoning stays isolated and only its final answer is returned.
//
// The sub-agent's own turn/text/reasoning events are dropped — forwarding them
// would make the parent transcript noisy and could imply they belong to the
// parent model context, which they do not.
//
// Usage events are observability only, so forwarding them preserves billing
// totals without polluting the parent provider-visible prefix.
//
// Tool events are tagged with the parent task call's ID so a frontend nests them
// under it. The forwarded call IDs are namespaced with the parent ID so a
// sub-agent call can never collide with a parent call in the frontend's
// dispatch→result matching. Falls back to Discard when there's no parent stream
// (the headless run loop, or a direct Execute in tests).
func subSink(ctx context.Context) event.Sink {
	parentID, parent, _, ok := CallContext(ctx)
	if !ok || parent == nil {
		return event.Discard
	}
	return subSinkFor(parentID, parent)
}

// subSinkFor builds the nesting sink from an already-captured parent ID + stream,
// for the background path where the job runs under a context that no longer
// carries the call context. Falls back to Discard when there's no parent stream.
func subSinkFor(parentID string, parent event.Sink) event.Sink {
	if parent == nil {
		return event.Discard
	}
	return event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.ToolDispatch, event.ToolResult:
			e.Tool.ParentID = parentID
			e.Tool.ID = parentID + "/" + e.Tool.ID
			parent.Emit(e)
		case event.Usage:
			if e.UsageSource == "" {
				e.UsageSource = event.UsageSourceSubagent
			}
			parent.Emit(e)
		}
	})
}
