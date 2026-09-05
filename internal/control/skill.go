package control

import (
	"fmt"
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
	// delivered is the listing the model has, nil when it has none — the state
	// a new session and a completed fold are both in. It rides the turn, never
	// the prefix, which a per-project catalog would diverge.
	delivered atomic.Pointer[string]
}

func newSkillSet(enabled, all []skill.Skill, store, allStore *skill.Store, noImplicit bool) skillSet {
	return skillSet{enabled: enabled, all: all, store: store, allStore: allStore, noImplicitInvocation: noImplicit}
}

// owedCatalog returns the listing this turn owes and records it as delivered,
// empty when the model already has the current one. It asks the canonical
// registry rather than a flag, so a writer that never announced itself cannot
// leave the model's view stale — the reason this is a question and not a
// notification is benchmarks/catalog-detector.
func (s *skillSet) owedCatalog() string {
	if s.noImplicitInvocation {
		return ""
	}
	block := skill.IndexBlock(s.list())
	if block == "" {
		return ""
	}
	if prev := s.delivered.Load(); prev != nil && *prev == block {
		return ""
	}
	s.delivered.Store(&block)
	return block
}

// forgetDeliveredCatalog returns the listing to the unknown state: what the
// model was sent is no longer in the context it samples from.
func (s *skillSet) forgetDeliveredCatalog() {
	s.delivered.Store(nil)
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

// CreateSkill writes a new skill file at the given scope and returns its path.
// The live store makes it usable by name at once, and the next turn's listing
// carries it without this having to say so. A config activation change still
// needs a rebuild: the store's disabled set is bound at construction.
func (c *Controller) CreateSkill(name string, scope skill.Scope, content string) (string, error) {
	w := c.skills.writer()
	if w == nil {
		return "", fmt.Errorf("no writable skill store in this session")
	}
	return w.CreateWithContent(name, scope, content)
}

// UpdateSkill overwrites an existing user-authored skill file in place. See
// skill.Store.UpdateContent for the builtin-refusal and scope-match rules.
func (c *Controller) UpdateSkill(name string, scope skill.Scope, content string) error {
	w := c.skills.writer()
	if w == nil {
		return fmt.Errorf("no writable skill store in this session")
	}
	return w.UpdateContent(name, scope, content)
}

// DeleteSkill removes a user-authored skill file at the given scope. See
// skill.Store.Delete for the builtin-refusal and scope-match rules.
func (c *Controller) DeleteSkill(name string, scope skill.Scope) error {
	w := c.skills.writer()
	if w == nil {
		return fmt.Errorf("no writable skill store in this session")
	}
	return w.Delete(name, scope)
}
