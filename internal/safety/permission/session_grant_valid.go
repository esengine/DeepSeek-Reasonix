package permission

import (
	"regexp"
	"strings"
)

var bareGrantTool = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.:-]{0,127}$`)

// ValidSessionGrantRule reports whether rule has a shape the host itself
// records for a session grant. A grant read back from disk is only as trusted as
// the file, so anything the host could not have written — a wildcard command, a
// prefix shorter or broader than SessionGrantRuleForScope derives — is dropped
// instead of widening what the conversation may do.
func ValidSessionGrantRule(rule string) bool {
	if rule == "" || rule != strings.TrimSpace(rule) || len(rule) > 512 || strings.ContainsAny(rule, "\n\r\x00") {
		return false
	}
	if rest, ok := strings.CutPrefix(rule, "Bash("); ok {
		inner, ok := strings.CutSuffix(rest, ")")
		if !ok {
			return false
		}
		cmd, ok := strings.CutSuffix(inner, ":*")
		return ok && BashCommandPrefix(cmd) == inner
	}
	if exact, ok := strings.CutPrefix(rule, "Bash="); ok {
		return strings.TrimSpace(exact) != ""
	}
	if tool, subject, ok := strings.Cut(rule, "="); ok {
		return bareGrantTool.MatchString(tool) && strings.TrimSpace(subject) != ""
	}
	return bareGrantTool.MatchString(rule)
}
