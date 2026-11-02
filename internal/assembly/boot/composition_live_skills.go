package boot

import (
	"io"

	"reasonix/internal/ext/skill"
)

func (b *builder) liveSkillSource() func() *skill.Store {
	return func() *skill.Store {
		cfg, err := b.roots.LoadForRootReadOnly(b.root)
		if err != nil {
			return nil
		}
		return skill.New(skill.Options{
			ProjectRoot: b.root, ReasonixHomeDir: b.roots.Home(), CustomPaths: cfg.SkillCustomPaths(),
			PluginPaths: cfg.PluginPackageSkillOwners(), PluginAgentPaths: cfg.PluginPackageAgentOwners(),
			ExcludedPaths: cfg.SkillExcludedPaths(), DisabledNames: func() []string { return disabledSkillNames(cfg, b.root) },
			MaxDepth: cfg.SkillMaxDepth(), Stderr: io.Discard, SuppressWarnings: cfg.Skills.SuppressWarnings,
		})
	}
}
