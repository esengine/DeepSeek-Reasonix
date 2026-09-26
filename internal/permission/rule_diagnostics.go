package permission

import (
	"strings"
	"unicode"
)

// UnmatchableRule is a configured rule that no tool call can satisfy, paired
// with the rewrite that expresses what the author meant.
type UnmatchableRule struct {
	// List is "allow", "ask" or "deny" — which setting carried the rule.
	List string
	// Rule is the entry exactly as configured.
	Rule string
	// Suggestion is the Bash(...) form that would match the command.
	Suggestion string
}

// UnmatchableRules reports configured rules whose tool name contains
// whitespace. ParseRule treats only an empty tool name as malformed, so an
// entry like "git push --force" parses and installs a rule keyed on a tool
// nothing answers to: a deny the author believes is enforced never fires.
// Whitespace is an exact test, not a guess — a tool name is an identifier and
// never contains any, which TestNoBuiltinToolNameHasWhitespace pins — so this
// cannot accuse a plugin or MCP tool that merely is not registered yet. Both
// sides ask unicode.IsSpace so the check and its invariant cannot drift apart.
func UnmatchableRules(allow, ask, deny []string) []UnmatchableRule {
	var out []UnmatchableRule
	for _, group := range []struct {
		list  string
		rules []string
	}{{"allow", allow}, {"ask", ask}, {"deny", deny}} {
		for _, raw := range group.rules {
			rule, ok := ParseRule(raw)
			if !ok || strings.IndexFunc(rule.Tool, unicode.IsSpace) < 0 {
				continue
			}
			out = append(out, UnmatchableRule{
				List:       group.list,
				Rule:       strings.TrimSpace(raw),
				Suggestion: bashRuleSuggestion(rule),
			})
		}
	}
	return out
}

// bashRuleSuggestion renders the rule the author probably wanted. A bare
// command becomes a prefix rule so its arguments are covered; one that already
// carries a subject keeps it verbatim rather than guessing a second time.
func bashRuleSuggestion(rule Rule) string {
	command := strings.TrimSpace(rule.Tool)
	if subject := strings.TrimSpace(rule.Subject); subject != "" {
		return "Bash(" + command + " " + subject + ")"
	}
	return "Bash(" + command + ":*)"
}
