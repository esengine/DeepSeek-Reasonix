// Package permrule is the syntax of a permission rule: the form a rule is
// written in and stored under in configuration. What a rule decides is
// internal/safety/permission's; this only says what a rule is.
package permrule

import "strings"

// Rule matches tool calls. Tool is the tool name; Subject, when non-empty,
// constrains the call's subject. A glob Subject matches by wildcard; a Literal
// Subject matches by exact string equality. An empty Subject matches every
// call to Tool.
type Rule struct {
	Tool    string
	Subject string
	// Literal matches Subject by exact equality rather than as a glob, so a
	// remembered concrete command keeps any '*'/'?' as ordinary characters
	// instead of turning them into wildcards.
	Literal bool
}

// Parse reads "ToolName", "ToolName(glob)", or the legacy "ToolName=literal"
// form, trimming surrounding whitespace. The "=literal" form (taken when the
// '=' precedes any '(') matches the rest verbatim. ok is false for a malformed
// entry (empty tool name), so a caller can warn rather than install a rule
// that matches nothing.
func Parse(s string) (Rule, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Rule{}, false
	}
	if eq := strings.IndexByte(s, '='); eq > 0 {
		if paren := strings.IndexByte(s, '('); paren < 0 || eq < paren {
			tool := strings.TrimSpace(s[:eq])
			if tool == "" {
				return Rule{}, false
			}
			return Rule{Tool: tool, Subject: s[eq+1:], Literal: true}, true
		}
	}
	if i := strings.IndexByte(s, '('); i >= 0 && strings.HasSuffix(s, ")") {
		tool := strings.TrimSpace(s[:i])
		if tool == "" {
			return Rule{}, false
		}
		return Rule{Tool: tool, Subject: s[i+1 : len(s)-1]}, true
	}
	return Rule{Tool: s}, true
}
