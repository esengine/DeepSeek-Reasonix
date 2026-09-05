package boot

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/environment"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

// skillSubagents runs a skill inside its own sub-agent loop: an isolated turn
// with the skill body as system prompt, a tool set scoped to the skill's
// allowed tools minus recursive meta-tools, and an optional per-skill model.
// capRuntime is filled in after the capability runtime exists; nothing calls a
// runner before then, because the tools holding them only fire on a model request.
type skillSubagents struct {
	root     string
	cfg      *config.Config
	registry *tool.Registry
	// tasks is the one runner every delegated execution goes through. A skill
	// that kept its own would be a second owner of admission, of the child's
	// loop and of how a run ended, and the three drifted apart once already.
	tasks      *agent.TaskTool
	scheduler  *agent.SubagentScheduler
	provider   provider.Provider
	entry      *config.ProviderEntry
	capRuntime *agent.MCPCapabilityRuntime
	maxDepth   int
	maxSteps   int

	resolveProvider func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error)
	identity        func(modelRef, effort string) (string, string)
	runOptions      func(ctx context.Context, steps int, price *provider.Pricing, ctxWin, childDepth int) agent.Options
}

// systemPrompt is the child's whole prefix: its body, then the workspace facts
// every child needs and none can discover for free. One accessor, so a fresh
// run and a continued one hash the same prompt.
func (r *skillSubagents) systemPrompt(sk skill.Skill) string {
	body := strings.TrimSpace(sk.Body)
	if body == "" {
		body = agent.DefaultReadOnlyTaskSystemPrompt
	}
	return body + r.workspaceFacts()
}

// workspaceFacts is the slice of the parent's Environment section a child must
// be told rather than left to find out: unsaid, it reaches for `git diff` in a
// workspace that is no repository and burns a round finding out. Filesystem-
// derived and stable, so it stays in the child's cached prefix.
func (r *skillSubagents) workspaceFacts() string {
	vcs := environment.WorkspaceVCS(r.root)
	if vcs == "" {
		vcs = "none (not a repository)"
	}
	return "\n\n## Workspace\n\n- Version control: " + vcs + "\n"
}

// halfSteps gives a child half the parent's step budget, never below five:
// enough to finish a small job, not enough to spend the parent's turn.
func (r *skillSubagents) halfSteps() int {
	steps := r.maxSteps
	if steps > 0 {
		if steps /= 2; steps < 5 {
			steps = 5
		}
	}
	return steps
}

// resolveModel picks the child's provider: the parent's unless the skill or
// config names one of its own.
func (r *skillSubagents) resolveModel(sk skill.Skill) (provider.Provider, *provider.Pricing, int, string, string, error) {
	prov, price, ctxWin := r.provider, r.entry.Price, r.entry.ContextWindow
	modelRef := subagentModelRef(r.cfg, sk)
	effortRef := subagentEffortRef(r.cfg, sk)
	if modelRef == "" && effortRef == "" {
		return prov, price, ctxWin, modelRef, effortRef, nil
	}
	p, pr, cw, err := r.resolveProvider(modelRef, effortRef)
	if err != nil {
		return nil, nil, 0, "", "", err
	}
	return p, pr, cw, modelRef, effortRef, nil
}

func (r *skillSubagents) runReadOnly(sctx context.Context, sk skill.Skill, task string, runOpts skill.SubagentRunOptions) (string, error) {
	if strings.TrimSpace(runOpts.ContinueFrom) != "" || strings.TrimSpace(runOpts.ForkFrom) != "" {
		return "", fmt.Errorf("read_only_skill does not support continue_from/fork_from")
	}
	releaseSlot, err := r.scheduler.Acquire(sctx, agent.AcquireRequest{
		Writer: false,
		Nested: agent.SubagentDepth(sctx) > 0,
		Label:  sk.Name,
	})
	if err != nil {
		return "", err
	}
	defer releaseSlot()
	sk = skill.WithCodeGraphTools(sk, skill.CodeGraphReadTools(r.registry))
	prov, price, ctxWin, modelRef, effortRef, err := r.resolveModel(sk)
	if err != nil {
		return "", fmt.Errorf("read-only subagent skill %q profile: %w", sk.Name, err)
	}
	childDepth := agent.SubagentDepth(sctx) + 1
	if childDepth > r.maxDepth {
		return "", fmt.Errorf("subagent delegation depth limit reached (max_subagent_depth=%d)", r.maxDepth)
	}
	subReg := agent.ReadOnlySubagentToolRegistryForDepthWithRuntime(r.registry, sk.AllowedTools, childDepth, r.maxDepth, r.capRuntime)
	if subReg.Len() == 0 {
		return "", fmt.Errorf("read_only_skill: skill %q has no read-only tools available", sk.Name)
	}
	switch sk.Name {
	case "review", "security-review", "security_review":
		agent.AttachReviewReportTool(subReg)
	}
	// Custom and named built-in profiles fully control their system prompt
	// (no implicit concise/DefaultReadOnlyTaskSystemPrompt overlay).
	sysPrompt := r.systemPrompt(sk)
	runOptions := r.runOptions(sctx, r.halfSteps(), price, ctxWin, childDepth)
	usageModelRef, _ := r.identity(modelRef, effortRef)
	runOptions.ModelRef = usageModelRef
	// A verdict the parent must act on carries an identity, never a sentence,
	// so the typed report is required at every role setting. How much review a
	// change set owes is still the delivery contract's separate call.
	runOptions.RequireReviewReportKind = agent.ReviewReportKindForSkill(sk.Name)
	// Provider serializers decide whether these images are wire-visible from
	// the child model's own vision capability. Text-only children retain the
	// attachment metadata locally but never receive image parts on the wire.
	childCtx := agent.WithUserImages(sctx, agent.SubagentImageCandidates(sctx))
	return agent.RunReadOnlySubAgentWithSession(childCtx, prov, subReg, agent.NewSession(sysPrompt), task,
		runOptions, agent.NestedSink(sctx, event.Discard))
}

// run executes a subagent skill as what it is: one delegated execution. It owns
// the skill's own facts — the body, the workspace note, the tool ceiling, the
// verdict a reviewer owes — and nothing about how an execution is admitted,
// carried out or ended, which belong to the runner every other delegation uses.
func (r *skillSubagents) run(sctx context.Context, sk skill.Skill, task string, runOpts skill.SubagentRunOptions) (string, error) {
	spec, err := r.compile(sctx, sk, task, runOpts)
	if err != nil {
		return "", err
	}
	return r.tasks.RunProfileSpec(sctx, spec)
}

// compile turns a skill and one invocation into the shared execution spec. Every
// value here is one the skill layer alone knows; anything the runner can resolve
// for itself is left to it, so the two cannot come to disagree.
func (r *skillSubagents) compile(sctx context.Context, sk skill.Skill, task string, runOpts skill.SubagentRunOptions) (agent.ProfileExecSpec, error) {
	sk = skill.WithCodeGraphTools(sk, skill.CodeGraphReadTools(r.registry))
	spec := agent.ProfileExecSpec{
		Task: agent.TaskSpec{Objective: task, Description: sk.Name},
		Worker: agent.WorkerSpec{
			Kind: "skill", Name: sk.Name, Profile: sk.Name,
			// The body alone is not the prefix: the workspace facts a child
			// cannot discover for free are part of it, and letting the runner
			// resolve the profile again would silently drop them.
			SystemPrompt: r.systemPrompt(sk), UseProfilePrompt: true,
			Model:  subagentModelRef(r.cfg, sk),
			Effort: subagentEffortRef(r.cfg, sk),
			// A verdict the parent must act on carries an identity, never a
			// sentence, so the typed report is required at every role setting.
			ReviewReport: agent.ReviewReportKindForSkill(sk.Name),
		},
		Grant: agent.CapabilityGrant{ReadOnly: sk.ReadOnly, ProfileTools: sk.AllowedTools},
		Context: agent.ContextRequest{
			ContinueFrom: runOpts.ContinueFrom, ForkFrom: runOpts.ForkFrom,
			TopLevel: runOpts.HostInitiated,
		},
		Sched: agent.SchedulerPolicy{MaxSteps: r.halfSteps(), Nested: agent.SubagentDepth(sctx) > 0},
	}
	if !sk.ReadOnly {
		// Writer skills without declared paths claim the whole workspace, so
		// they serialize against fleet and task writers that declared disjoint
		// ones rather than racing them.
		whole, err := agent.WholeWorkspaceWriteClaim(r.root)
		if err != nil {
			return agent.ProfileExecSpec{}, fmt.Errorf("subagent skill %q write claim: %w", sk.Name, err)
		}
		spec.Grant.WritePaths = whole
	}
	return spec, nil
}
