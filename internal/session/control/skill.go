package control

import (
	"fmt"
	"sync/atomic"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/skill"
)

// skillSet owns the session's discovered skills: the enabled subset surfaced to
// the model, the full set (including config-disabled ones) for management
// surfaces, and the optional reloadable stores that supersede the
// construction-time snapshots. It is the skills slice of the Capabilities concern
// (alongside mcpManager).
//
// No lock: every field but slashSeq and catalog is set once at construction and
// read thereafter — SetSkillEnabled writes the activation store, which the live
// store consults per call — and those two carry their own synchronisation.
type skillSet struct {
	enabled              []skill.Skill // discovered + enabled skills (the live store supersedes when set)
	all                  []skill.Skill // every discoverable skill, including config-disabled ones
	store                *skill.Store  // reloadable enabled-skill store; nil falls back to enabled
	allStore             *skill.Store  // reloadable all-skill store; nil falls back to all/enabled
	noImplicitInvocation bool          // the model may not reach a skill on its own; slash still does
	slashSeq             atomic.Uint64 // numbers the synthetic call a slash-invoked skill reports under
	// catalog is what the model has of the listing. It rides the turn, never
	// the prefix, which a per-project catalog would diverge.
	catalog projectionDebt
	// pathHits is the workspace files the host has seen the session touch; a
	// skill that declares `paths:` is eligible once one of them matches.
	pathHits *skill.PathHits
}

func newSkillSet(enabled, all []skill.Skill, store, allStore *skill.Store, noImplicit bool, workspaceRoot string) skillSet {
	hits := store.PathHits()
	if hits == nil {
		hits = skill.NewPathHits(workspaceRoot)
	}
	return skillSet{enabled: enabled, all: all, store: store, allStore: allStore, noImplicitInvocation: noImplicit, pathHits: hits}
}

// skillsAllOffBlock replaces the listing when every skill is switched off: an
// absence the model must be told about, since the listing it already has would
// otherwise keep standing as current.
const skillsAllOffBlock = "# Skills\n\nEvery skill this project had is switched off. The listing you were sent earlier no longer holds, and `run_skill` has nothing to reach."

// skillsNoneEligibleBlock replaces the listing when skills exist but none is
// eligible for the files this session has touched.
const skillsNoneEligibleBlock = "# Skills\n\nNone of this project's skills apply to the files this session has touched. The listing you were sent earlier no longer holds."

// owedCatalog returns the listing this turn owes, empty when the model already
// has the current one. It asks the canonical registry rather than a flag, so a
// writer that never announced itself cannot leave the model's view stale; the
// cost of asking is measured at benchmarks/catalog-detector.
func (s *skillSet) owedCatalog() string {
	if s.noImplicitInvocation {
		return ""
	}
	listed := s.list()
	block := skill.IndexBlock(s.pathHits.Visible(listed))
	if block == "" && s.catalog.sent() {
		// Silence would leave the listing the model already has standing as
		// current. Every skill being switched off is a fact, not an absence.
		block = skillsAllOffBlock
		if skill.IndexBlock(listed) != "" {
			block = skillsNoneEligibleBlock
		}
	}
	return s.catalog.owed(block)
}

// forgetDeliveredCatalog returns the listing to the unknown state: what the
// model was sent is no longer in the context it samples from.
func (s *skillSet) forgetDeliveredCatalog() {
	s.catalog.forget()
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
	sk = sk.Complete()
	if s.store != nil {
		return s.store.Prepare(sk)
	}
	return sk
}

// forModel is the skill as the model may use it now, by the slash name its
// entry was registered under: re-read from the live store when there is one,
// and refused when its author reserved it for the user.
func (s *skillSet) forModel(sk skill.Skill) (skill.Skill, error) {
	if s.store != nil {
		return s.store.ForModel(sk.SlashName())
	}
	if err := s.store.ValidateInvocation(sk); err != nil {
		return skill.Skill{}, err
	}
	return sk.Complete(), nil
}

// modelGate judges the model's slash entries once per call; a session with no
// live store has nothing that can change, so its entries were judged at build.
func (s *skillSet) modelGate() func() func(string) error {
	if s.store != nil {
		return s.store.ModelGate()
	}
	return nil
}

func (s *skillSet) renderInvocation(sk skill.Skill, args string) string {
	return skill.RenderInvocation(s.prepare(sk), args)
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

// Skills scans the live Store, so a skill installed this session is listed.
func (c *Controller) Skills() []skill.Skill {
	return c.skills.list()
}

// ImplicitSkillInvocationEnabled reports whether skills are exposed to the
// model for automatic discovery and invocation. Explicit /skill handling is
// independent of this model-facing capability.
func (c *Controller) ImplicitSkillInvocationEnabled() bool {
	return c != nil && !c.skills.noImplicitInvocation
}

// SlashSkills returns the user-visible skill directory. Plugin skills use
// package-qualified names while Skills keeps bare model/run_skill identifiers.
func (c *Controller) SlashSkills() []skill.Skill {
	return c.skills.slashList()
}

// AllSkills returns every discoverable skill, including disabled ones, for
// management surfaces that need to re-enable a hidden skill.
func (c *Controller) AllSkills() []skill.Skill {
	return c.skills.listAll()
}

// DisabledSkills returns all discoverable skills that are off in this project.
func (c *Controller) DisabledSkills() []skill.Skill {
	resolve := c.SkillActivation()
	var out []skill.Skill
	for _, sk := range c.AllSkills() {
		if !resolve(sk.Name) {
			out = append(out, sk)
		}
	}
	return out
}

// SkillEnabled reports whether a skill is on in this project.
func (c *Controller) SkillEnabled(name string) bool {
	return c.SkillActivation()(name)
}

// SkillActivation resolves several names against one config and one store read.
// skills.disabled_skills stays readable as the declared default, so a
// hand-written config keeps working even though the switch no longer writes it.
func (c *Controller) SkillActivation() func(string) bool {
	declared := func(string) bool { return true }
	if cfg, err := config.Load(); err == nil {
		declared = func(name string) bool { return !cfg.IsSkillDisabled(name) }
	}
	resolver, err := config.DefaultActivationStore().SkillResolverFor(c.workspaceRoot)
	if err != nil {
		return declared
	}
	return func(name string) bool { return resolver.Enabled(name, declared(name)) }
}

// SkillOverrideScope reports where the decision governing name lives, so the
// settings surface can show whether this project set it for itself.
func (c *Controller) SkillOverrideScope(name string) (config.ActivationScope, bool) {
	scope, found, err := config.DefaultActivationStore().SkillOverrideScope(name, c.workspaceRoot)
	if err != nil {
		return config.ActivationGlobal, false
	}
	return scope, found
}

// SetSkillEnabled persists a skill's switch at scope. The caller should rebuild
// the controller for the prompt/tool registry to reflect it immediately.
// Flipping a skill the project inherits writes a project row: the user answered
// for this folder, and two projects may hold different skills of one name.
func (c *Controller) SetSkillEnabled(name string, scope config.ActivationScope, enabled bool) error {
	canonical, err := c.canonicalSkillName(name)
	if err != nil {
		return err
	}
	return config.DefaultActivationStore().SetSkillEnabled(canonical, c.workspaceRoot, scope, enabled)
}

// ClearSkillOverride drops this project's exception for name.
func (c *Controller) ClearSkillOverride(name string, scope config.ActivationScope) error {
	canonical, err := c.canonicalSkillName(name)
	if err != nil {
		return err
	}
	return config.DefaultActivationStore().ClearSkill(canonical, c.workspaceRoot, scope)
}

func (c *Controller) canonicalSkillName(name string) (string, error) {
	for _, sk := range c.AllSkills() {
		if config.SkillNameKey(sk.Name) == config.SkillNameKey(name) {
			return sk.Name, nil
		}
	}
	return "", &skill.NotFoundError{Name: name}
}

// TouchedPaths lists the workspace files this session's completed tool calls
// named, sorted: the facts a skill's `paths:` globs are judged against. It is a
// read-only diagnostic view of the set.
func (c *Controller) TouchedPaths() []string { return c.skills.pathHits.Seen() }
