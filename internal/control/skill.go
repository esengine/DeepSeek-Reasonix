package control

import "reasonix/internal/skill"

// skillSet owns the session's discovered skills: the enabled subset surfaced to
// the model, the full set (including config-disabled ones) for management
// surfaces, and the optional reloadable stores that supersede the
// construction-time snapshots. It is the skills slice of the Capabilities concern
// (alongside mcpManager).
//
// No lock: every field is set once at construction and only read thereafter.
// Skills are discovered at boot and never mutated in place — SetSkillEnabled
// persists a config preference and relies on a controller rebuild to take effect.
type skillSet struct {
	enabled  []skill.Skill // discovered + enabled skills (the live store supersedes when set)
	all      []skill.Skill // every discoverable skill, including config-disabled ones
	store    *skill.Store  // reloadable enabled-skill store; nil falls back to enabled
	allStore *skill.Store  // reloadable all-skill store; nil falls back to all/enabled
}

func newSkillSet(enabled, all []skill.Skill, store, allStore *skill.Store) skillSet {
	return skillSet{enabled: enabled, all: all, store: store, allStore: allStore}
}

// list returns the enabled skills, preferring the live store.
func (s *skillSet) list() []skill.Skill {
	if s.store != nil {
		return s.store.List()
	}
	return s.enabled
}

func (s *skillSet) slashList() []skill.Skill {
	if s.store != nil {
		return s.store.SlashList()
	}
	return skill.VisibleSlashSkills(s.enabled)
}

// listAll returns every discoverable skill (including disabled), preferring the
// live store, for management surfaces that re-enable a hidden skill.
func (s *skillSet) listAll() []skill.Skill {
	if s.allStore != nil {
		return s.allStore.List()
	}
	if len(s.all) > 0 {
		return s.all
	}
	return s.enabled
}

func (s *skillSet) bySlashName(name string) (skill.Skill, bool) {
	if s.store != nil {
		return s.store.ReadSlash(name)
	}
	return skill.ResolveSlashSkill(s.enabled, name)
}

func (s *skillSet) prepare(sk skill.Skill) skill.Skill {
	if s.store != nil {
		return s.store.Prepare(sk)
	}
	return sk
}

func (s *skillSet) render(sk skill.Skill, args string) string {
	if s.store != nil {
		return s.store.Render(sk, args)
	}
	return skill.Render(sk, args)
}

// discovered returns the construction-time enabled snapshot (not the live store),
// for the /skills listing which reflects what was discovered at boot.
func (s *skillSet) discovered() []skill.Skill {
	return s.enabled
}

// writer returns the live store to use for authoring (create/delete), preferring
// allStore since management surfaces must resolve disabled and builtin skills
// too (e.g. a create-time name-collision check). nil when this session has no
// reloadable store (e.g. a construction-time-only test snapshot).
func (s *skillSet) writer() *skill.Store {
	if s.allStore != nil {
		return s.allStore
	}
	return s.store
}
