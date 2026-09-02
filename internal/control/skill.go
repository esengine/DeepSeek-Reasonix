package control

import (
	"sync/atomic"

	"reasonix/internal/skill"
)

// skillSet owns the session's discovered skills: the enabled subset surfaced to
// the model, the full set (including config-disabled ones) for management
// surfaces, and the optional reloadable stores that supersede the
// construction-time snapshots. It is the skills slice of the Capabilities concern
// (alongside mcpManager).
//
// No lock: every field but slashSeq and catalog is set once at construction and
// read thereafter — SetSkillEnabled persists a preference a rebuild picks up —
// and those two carry their own synchronisation.
type skillSet struct {
	enabled              []skill.Skill // discovered + enabled skills (the live store supersedes when set)
	all                  []skill.Skill // every discoverable skill, including config-disabled ones
	store                *skill.Store  // reloadable enabled-skill store; nil falls back to enabled
	allStore             *skill.Store  // reloadable all-skill store; nil falls back to all/enabled
	noImplicitInvocation bool          // the model may not reach a skill on its own; slash still does
	slashSeq             atomic.Uint64 // numbers the synthetic call a slash-invoked skill reports under
	// catalog is the listing owed to the next real turn, nil once delivered. It
	// rides the turn instead of the prefix: a per-project catalog in the prefix
	// diverges it, and one rewritten mid-session moves it under a live cache.
	catalog atomic.Pointer[string]
}

func newSkillSet(enabled, all []skill.Skill, store, allStore *skill.Store, noImplicit bool) skillSet {
	return skillSet{enabled: enabled, all: all, store: store, allStore: allStore, noImplicitInvocation: noImplicit}
}

// publishCatalog owes the listing to the next real turn. It is called with what
// the caller already discovered — never a fresh List(), which walks the disk —
// so a turn costs nothing when the catalog has not changed.
func (s *skillSet) publishCatalog(skills []skill.Skill) {
	if s.noImplicitInvocation {
		return
	}
	block := skill.IndexBlock(skills)
	if block == "" {
		return
	}
	s.catalog.Store(&block)
}

// drainCatalog returns the listing owed to this turn, once.
func (s *skillSet) drainCatalog() string {
	if block := s.catalog.Swap(nil); block != nil {
		return *block
	}
	return ""
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
