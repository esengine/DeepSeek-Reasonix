package acp

import (
	"strings"

	"reasonix/internal/ext/skill"
)

func skillInputHint(sk skill.Skill) string {
	if hint := strings.TrimSpace(sk.ArgumentHint); hint != "" {
		return hint
	}
	return "instructions"
}
