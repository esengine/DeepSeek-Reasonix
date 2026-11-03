package skill

import (
	"reasonix/internal/contract/config"
	"slices"
)

func (s *Store) discover(scan func(discoveryRoot) []Skill) []Skill {
	if s != nil && s.snapshot != nil {
		return s.snapshotSkills()
	}
	if s == nil || s.disableDiscovery {
		return nil
	}
	if s.discoverySource != nil {
		current := s.discoverySource()
		if current == nil {
			return nil
		}
		current.admits = s.admits
		current.stderr, current.said = s.stderr, s.said
		return current.discover(current.discoverRoot)
	}
	var out []Skill
	disabled := s.disabledSet()
	for _, r := range s.roots() {
		if r.Status != StatusOK {
			continue
		}
		for _, sk := range scan(r) {
			if disabled[config.SkillNameKey(sk.Name)] {
				continue
			}
			if len(r.plugins) == 0 {
				out = append(out, sk)
				continue
			}
			for _, plugin := range r.plugins {
				owned := sk
				owned.Plugin = plugin
				if r.forceSubagent {
					owned.SlashPrefix = plugin + ":agent"
				}
				out = append(out, owned)
			}
		}
	}
	if !s.disableBuiltins {
		for _, sk := range builtinSkills() {
			if !disabled[config.SkillNameKey(sk.Name)] {
				out = append(out, sk)
			}
		}
	}
	if s.admits != nil {
		out = slices.DeleteFunc(out, func(sk Skill) bool { return !s.admits(sk) })
		for i := range out {
			out[i].confinedSupplements = true
		}
	}
	return out
}

// Restrict applies the session ceiling before any discovery or invocation.
func (s *Store) Restrict(admits func(Skill) bool) {
	s.admits = admits
}

func (s *Store) ConfigureDiscoverySource(source func() *Store) {
	s.discoverySource = source
}
