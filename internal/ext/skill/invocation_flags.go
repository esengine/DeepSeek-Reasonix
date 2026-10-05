package skill

import (
	"errors"
	"fmt"
	"strings"
)

// ErrModelInvocationDisabled marks a model-side call to a skill whose author
// reserved it for the user (`disable-model-invocation: true`).
var ErrModelInvocationDisabled = errors.New("skill is reserved for the user")

// InvocationFlags is who may start a skill, as its author declared it.
type InvocationFlags struct {
	// DisableModelInvocation keeps the skill out of every model-facing surface
	// and refuses the model's own calls; only the user's /<name> reaches it.
	DisableModelInvocation bool
	// DisableUserInvocation hides the skill from the slash surface and refuses
	// a typed /<name>; the model still reaches it.
	DisableUserInvocation bool
	ArgumentHint          string // completion hint for /<name>
	// Invalid lists declarations that could not be read as written, so doctor
	// can report what the parser had to decide on the author's behalf.
	Invalid []string
}

// parseInvocationFlags fails closed: a value that is neither a boolean nor
// absent on disable-model-invocation restricts the skill rather than freeing it.
func parseInvocationFlags(fm map[string]string) InvocationFlags {
	f := InvocationFlags{ArgumentHint: strings.TrimSpace(fm[skillFrontmatterArgumentHint])}
	if raw := strings.TrimSpace(fm[skillFrontmatterDisableModel]); raw != "" {
		v, ok := parseStrictBool(raw)
		f.DisableModelInvocation = v || !ok
		if !ok {
			f.Invalid = append(f.Invalid, skillFrontmatterDisableModel+": "+raw+" (not a boolean; treated as true)")
		}
	}
	if raw := strings.TrimSpace(fm[skillFrontmatterUserInvocable]); raw != "" {
		v, ok := parseStrictBool(raw)
		f.DisableUserInvocation = ok && !v
		if !ok {
			f.Invalid = append(f.Invalid, skillFrontmatterUserInvocable+": "+raw+" (not a boolean; treated as true)")
		}
	}
	return f
}

func parseStrictBool(raw string) (value, ok bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "yes", "1", "on":
		return true, true
	case "false", "no", "0", "off":
		return false, true
	default:
		return false, false
	}
}

// scanFrontmatterLines reads top-level `key: value` lines from a block YAML
// rejected as a whole, so a restriction written beside a malformed sibling still applies.
func scanFrontmatterLines(raw string) map[string]string {
	out := map[string]string{}
	for line := range strings.SplitSeq(raw, "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if n := len(value); n >= 2 && (value[0] == '"' || value[0] == '\'') && value[n-1] == value[0] {
			value = value[1 : n-1]
		}
		key = strings.ToLower(strings.TrimSpace(key))
		if key == skillFrontmatterDisableModel {
			if v, ok := parseStrictBool(out[key]); out[key] != "" && (v || !ok) {
				continue
			}
		}
		out[key] = value
	}
	return out
}

func (s Skill) modelCallError() error {
	if !s.DisableModelInvocation {
		return nil
	}
	return fmt.Errorf("%w: %q runs only when the user types /%s; do not call it yourself", ErrModelInvocationDisabled, s.Name, s.SlashName())
}

// ModelInvocable drops the skills whose author reserved them for the user.
func ModelInvocable(skills []Skill) []Skill {
	out := make([]Skill, 0, len(skills))
	for _, sk := range skills {
		if !sk.DisableModelInvocation {
			out = append(out, sk)
		}
	}
	return out
}

// ModelGate returns a per-call judge for the model's slash entries. One call
// takes one snapshot of the registry and judges every entry against it, so
// the cost of a listing does not grow with the number of entries.
func (s *Store) ModelGate() func() func(slashName string) error {
	return func() func(string) error {
		skills := s.discoveredSkills()
		return func(name string) error {
			_, err := s.gateSkill(skills, name)
			return err
		}
	}
}

// ForModel re-reads one skill by its slash name, which is the name the entry
// was registered under, and applies the gates the model's tools apply.
func (s *Store) ForModel(slashName string) (Skill, error) {
	return s.gateSkill(s.discoveredSkills(), slashName)
}

func (s *Store) gateSkill(skills []Skill, slashName string) (Skill, error) {
	sk, ok := resolveSlashSkill(skills, slashName)
	if !ok {
		return Skill{}, fmt.Errorf("unknown skill %q", slashName)
	}
	if err := s.ValidateInvocation(sk); err != nil {
		return Skill{}, err
	}
	return s.Prepare(sk), nil
}
