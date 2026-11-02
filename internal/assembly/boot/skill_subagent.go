package boot

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/skill"
	"reasonix/internal/platform/environment"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/usecap"
	"reasonix/internal/runtime/writeclaim"
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
	runner     *delegation.Runner
	scheduler  *writeclaim.SubagentScheduler
	provider   provider.Provider
	entry      *config.ProviderEntry
	capRuntime *usecap.MCPCapabilityRuntime
	maxDepth   int
	maxSteps   int

	resolveProvider func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error)
	identity        func(modelRef, effort string) (string, string)
	inheritedFor    func(modelRef string) string
	runOptions      func(ctx context.Context, steps int, price *provider.Pricing, ctxWin, childDepth int) agent.Options
}

// systemPrompt is the child's whole prefix: its body, then the workspace facts
// every child needs and none can discover for free. One accessor, so a fresh
// run and a continued one hash the same prompt.
func (r *skillSubagents) systemPrompt(sk skill.Skill) string {
	body := strings.TrimSpace(sk.Body)
	if body == "" {
		body = delegation.DefaultReadOnlyTaskSystemPrompt
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

// stepsFor is the tighter of the default budget and the run's own cap; zero on
// either side means that side sets no limit.
func (r *skillSubagents) stepsFor(runOpts skill.SubagentRunOptions) int {
	steps := r.halfSteps()
	if runOpts.MaxSteps > 0 && (steps <= 0 || runOpts.MaxSteps < steps) {
		return runOpts.MaxSteps
	}
	return steps
}

// resolveModel picks the child's provider: the parent's unless the skill or
// config names one of its own, or the images the child is to look at need the
// vision role.
func (r *skillSubagents) runReadOnly(sctx context.Context, sk skill.Skill, task string, runOpts skill.SubagentRunOptions) (string, error) {
	return r.runWith(sctx, sk, task, runOpts, true)
}

func (r *skillSubagents) run(sctx context.Context, sk skill.Skill, task string, runOpts skill.SubagentRunOptions) (string, error) {
	return r.runWith(sctx, sk, task, runOpts, false)
}

func (r *skillSubagents) runWith(sctx context.Context, sk skill.Skill, task string, runOpts skill.SubagentRunOptions, readOnly bool) (string, error) {
	if strings.TrimSpace(runOpts.ContinueFrom) != "" || strings.TrimSpace(runOpts.ForkFrom) != "" {
		return "", fmt.Errorf("subagent skill %q does not support continue_from or fork_from", sk.Name)
	}
	if runOpts.MaxSteps > 0 {
		sctx = agent.WithRunStepLimit(sctx, r.stepsFor(runOpts), sk.Name+" rounds")
	}
	sk = skill.WithCodeGraphTools(sk, skill.CodeGraphReadTools(r.registry))
	out, err := r.runner.Run(sctx, delegation.RunSpec{
		Title:        sk.Name,
		Task:         task,
		Kind:         "skill",
		Name:         sk.Name,
		SystemPrompt: r.systemPrompt(sk),
		Tools:        sk.AllowedTools,
		Model:        subagentModelRef(r.cfg, sk),
		Effort:       subagentEffortRef(r.cfg, sk, r.inheritedFor),
		MaxSteps:     r.stepsFor(runOpts),
		ReadOnly:     readOnly || sk.ReadOnly,
	})
	return out.Answer, err
}
