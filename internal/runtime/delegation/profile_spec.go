package delegation

import (
	"strings"

	"reasonix/internal/ext/skill"
)

type ProfileDefinition struct {
	Name                   string
	Body                   string
	AllowedTools           []string
	Model                  string
	Effort                 string
	ReadOnly               bool
	Invocation             string
	DisableModelInvocation bool
	NamedBuiltin           bool
}

type ProfileLookup func(name string) (ProfileDefinition, bool)

func ProfileFromSkill(sk skill.Skill) ProfileDefinition {
	return ProfileDefinition{
		Name:                   sk.Name,
		Body:                   sk.Body,
		AllowedTools:           sk.AllowedTools,
		Model:                  sk.Model,
		Effort:                 sk.Effort,
		ReadOnly:               sk.ReadOnly,
		Invocation:             sk.Invocation,
		DisableModelInvocation: sk.DisableModelInvocation,
		NamedBuiltin:           NamedBuiltinProfile(sk.Name),
	}
}

func NamedBuiltinProfile(name string) bool {
	switch strings.TrimSpace(name) {
	case "explore", "locate", "research", "review", "security-review", "security_review":
		return true
	default:
		return false
	}
}
