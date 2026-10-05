package config

// SkillOverrideFor builds the override identity for a skill name at scope. A
// skill is switched by the name the user invokes, so no source disambiguator
// is stored: the decision follows whichever file answers to that name here.
func SkillOverrideFor(name, root string, scope ActivationScope) ActivationOverride {
	override := ActivationOverride{
		Kind:  CapabilitySkill,
		Scope: scope,
		Name:  SkillNameKey(name),
	}
	return placeProjectRow(override, root)
}

// SkillResolver answers many names against one loaded file and one workspace
// identity, so listing every skill costs a single read and a single path probe
// rather than one of each per name.
type SkillResolver struct {
	overrides []ActivationOverride
	keys      []string
}

// SkillResolverFor snapshots the store for root.
func (s *ActivationStore) SkillResolverFor(root string) (SkillResolver, error) {
	if s == nil {
		return SkillResolver{}, nil
	}
	file, err := s.Load()
	if err != nil {
		return SkillResolver{}, err
	}
	return SkillResolver{overrides: file.Overrides, keys: projectKeys(root)}, nil
}

// Enabled reports whether name is on, with declared used when nothing
// overrides it — the skill's own frontmatter or a hand-written config.
func (r SkillResolver) Enabled(name string, declared bool) bool {
	if row, ok := r.find(name); ok {
		return row.Enabled
	}
	return declared
}

// Scope reports where the decision governing name lives, and whether one
// exists at all.
func (r SkillResolver) Scope(name string) (ActivationScope, bool) {
	if row, ok := r.find(name); ok {
		return row.Scope, true
	}
	return ActivationGlobal, false
}

// find applies the project layer before the global one.
func (r SkillResolver) find(name string) (ActivationOverride, bool) {
	key := SkillNameKey(name)
	if key == "" {
		return ActivationOverride{}, false
	}
	for _, storageKey := range r.keys {
		probe := ActivationOverride{Kind: CapabilitySkill, Scope: ActivationProject, Key: storageKey, Name: key}
		if row, ok := findOverride(r.overrides, overrideKey(probe)); ok {
			return row, true
		}
	}
	probe := ActivationOverride{Kind: CapabilitySkill, Scope: ActivationGlobal, Name: key}
	return findOverride(r.overrides, overrideKey(probe))
}

// SkillSwitches returns the names this resolver decides for: off are the ones
// switched off, on the ones switched on against a declared default. Discovery
// needs the pair rather than a per-name question, because it does not yet know
// which names exist when it is configured.
func (r SkillResolver) SkillSwitches() (off, on []string) {
	seen := map[string]bool{}
	for _, storageKey := range append(append([]string{}, r.keys...), "") {
		for _, row := range r.overrides {
			if row.Kind != CapabilitySkill || row.Name == "" || seen[row.Name] {
				continue
			}
			// A project row answers for its own workspace only; a global row
			// answers where no project row does, which is what the empty key
			// pass covers.
			if (storageKey == "" && row.Scope != ActivationGlobal) || (storageKey != "" && row.Key != storageKey) {
				continue
			}
			seen[row.Name] = true
			if row.Enabled {
				on = append(on, row.Name)
			} else {
				off = append(off, row.Name)
			}
		}
	}
	return off, on
}

// SkillEnabled resolves whether name is on in root. declared is what the skill
// itself asks for when nothing overrides it.
func (s *ActivationStore) SkillEnabled(name, root string, declared bool) (bool, error) {
	resolver, err := s.SkillResolverFor(root)
	if err != nil {
		return declared, err
	}
	return resolver.Enabled(name, declared), nil
}

// SetSkillEnabled records a durable decision for name at scope.
func (s *ActivationStore) SetSkillEnabled(name, root string, scope ActivationScope, enabled bool) error {
	override := SkillOverrideFor(name, root, scope)
	if override.Name == "" {
		return nil
	}
	override.Enabled = enabled
	return s.SetOverride(override)
}

// ClearSkill removes name's override at scope, restoring what it inherits.
func (s *ActivationStore) ClearSkill(name, root string, scope ActivationScope) error {
	return s.ClearOverride(SkillOverrideFor(name, root, scope))
}

// SkillOverrideScope reports where the decision that currently governs name in
// root lives, so the settings surface can say whether it is a local exception.
func (s *ActivationStore) SkillOverrideScope(name, root string) (ActivationScope, bool, error) {
	resolver, err := s.SkillResolverFor(root)
	if err != nil {
		return ActivationGlobal, false, err
	}
	scope, found := resolver.Scope(name)
	return scope, found, nil
}
