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
		DisabledNames: func() []string { return disabledSkillNames(cfg, root) }, MaxDepth: cfg.SkillMaxDepth(), Stderr: opts.Stderr,
	})
	a.store.ConfigureInvocationPolicy(nil)
	a.skills = a.store.List()
	a.all = skill.New(skill.Options{ProjectRoot: root, ReasonixHomeDir: home, CustomPaths: cfg.SkillCustomPaths(), PluginPaths: cfg.PluginPackageSkillOwners(), PluginAgentPaths: cfg.PluginPackageAgentOwners(), ExcludedPaths: cfg.SkillExcludedPaths(), MaxDepth: cfg.SkillMaxDepth(), Stderr: io.Discard})
	a.allSkills = a.all.List()
	return a
}

// disabledSkillNames is what discovery must not surface: the hand-written
// config list, plus what the durable switch turned off for this workspace,
// minus what it turned back on. It is called per discovery pass, so a switch
// flipped mid-session answers on the next turn rather than the next build.
func disabledSkillNames(cfg *config.Config, root string) []string {
	declared := cfg.DisabledSkillNames()
	resolver, err := config.DefaultActivationStore().SkillResolverFor(root)
	if err != nil {
		return declared
	}
	off, on := resolver.SkillSwitches()
	enabled := map[string]bool{}
	for _, name := range on {
		enabled[config.SkillNameKey(name)] = true
	}
	out := make([]string, 0, len(declared)+len(off))
	seen := map[string]bool{}
	for _, name := range append(declared, off...) {
		key := config.SkillNameKey(name)
		if key == "" || seen[key] || enabled[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}
