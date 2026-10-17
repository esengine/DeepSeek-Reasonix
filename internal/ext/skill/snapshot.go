package skill

import (
	"fmt"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/pluginpkg"
	"slices"
	"sort"
)

func (s *Store) Freeze(skills []Skill) *Store {
	copy := *s
	items := slices.Clone(skills)
	for i := range items {
		items[i] = items[i].Complete()
	}
	copy.snapshot = &items
	return &copy
}

func (s *Store) CompositionSkills() []Skill {
	byName := map[string]Skill{}
	for _, sk := range s.discoveredSkills() {
		if _, ok := byName[sk.SlashName()]; !ok {
			byName[sk.SlashName()] = sk
		}
	}
	out := make([]Skill, 0, len(byName))
	for _, sk := range byName {
		out = append(out, sk)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SlashName() < out[j].SlashName() })
	return out
}

func (s *Store) ValidateSnapshot() error {
	if s == nil || s.snapshot == nil {
		return nil
	}
	disabled := s.disabledSet()
	installed, _ := pluginpkg.LoadInstalled(s.reasonixHomeDir)
	for _, sk := range *s.snapshot {
		if disabled[config.SkillNameKey(sk.Name)] {
			return fmt.Errorf("%w: skill %s was disabled", ErrInvocationUnavailable, sk.SlashName())
		}
		if sk.Plugin != "" && !slices.ContainsFunc(installed, func(p pluginpkg.InstalledPackage) bool { return p.Installed.Name == sk.Plugin }) {
			return fmt.Errorf("%w: skill package %s was disabled", ErrInvocationUnavailable, sk.Plugin)
		}
	}
	return nil
}

func (s *Store) snapshotSkills() []Skill {
	if err := s.ValidateSnapshot(); err != nil {
		return nil
	}
	return slices.Clone(*s.snapshot)
}
