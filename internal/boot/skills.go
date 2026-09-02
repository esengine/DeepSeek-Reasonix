package boot

import (
	"io"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/skill"
)

// skillAssembly is the discovered skill surface for one build: the policy-gated
// store the model sees, and the unfiltered one management commands list from.
type skillAssembly struct {
	store     *skill.Store
	all       *skill.Store
	skills    []skill.Skill
	allSkills []skill.Skill
	sysPrompt string
}

// buildSkillAssembly discovers skills. Their index does not enter the prompt:
// a per-project catalog in the prefix diverges it, so the controller owes the
// listing to the turn instead. Rediscovery is skipped on no-op/UI rebuilds.
func buildSkillAssembly(opts Options, cfg *config.Config, root string, implicit bool, sysPrompt string) skillAssembly {
	a := skillAssembly{sysPrompt: sysPrompt}
	home := opts.roots().Home()
	if opts.ReuseAssembly != nil && shouldReuseDiscovery(opts.PreviousPlan) &&
		opts.ReuseAssembly.ImplicitSkillInvocation == implicit {
		a.skills = opts.ReuseAssembly.Skills
		a.allSkills = a.skills
		a.store = skill.New(skill.Options{ProjectRoot: root, ReasonixHomeDir: home, Stderr: io.Discard})
		a.all = a.store
		if s := strings.TrimSpace(opts.ReuseAssembly.SystemPrompt); s != "" {
			a.sysPrompt = s
		}
		return a
	}
	a.store = skill.New(skill.Options{
		ProjectRoot: root, ReasonixHomeDir: home, CustomPaths: cfg.SkillCustomPaths(), PluginPaths: cfg.PluginPackageSkillOwners(),
		PluginAgentPaths: cfg.PluginPackageAgentOwners(), ExcludedPaths: cfg.SkillExcludedPaths(),
		DisabledNames: cfg.DisabledSkillNames(), MaxDepth: cfg.SkillMaxDepth(), Stderr: opts.Stderr,
	})
	a.store.ConfigureInvocationPolicy(nil)
	a.skills = a.store.List()
	a.all = skill.New(skill.Options{ProjectRoot: root, ReasonixHomeDir: home, CustomPaths: cfg.SkillCustomPaths(), PluginPaths: cfg.PluginPackageSkillOwners(), PluginAgentPaths: cfg.PluginPackageAgentOwners(), ExcludedPaths: cfg.SkillExcludedPaths(), MaxDepth: cfg.SkillMaxDepth(), Stderr: io.Discard})
	a.allSkills = a.all.List()
	return a
}
