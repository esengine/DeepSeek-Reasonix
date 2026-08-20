package permission

import (
	"fmt"
	"strings"
)

// Problem is one validation finding for a permission rule. Severity is "error"
// (the rule is rejected by config.AddPermissionRule) or "warning" (accepted,
// but surfaced in diagnostics because it is likely a mistake). Code is stable
// and consumed by the capability diagnostics report.
type Problem struct {
	Severity string
	Code     string
	Message  string
}

// Stable diagnostic codes for permission-rule problems.
const (
	CodeInvalidRule        = "permission.invalid_rule"
	CodeEmptySpecifier     = "permission.empty_specifier"
	CodeMalformedSpecifier = "permission.malformed_specifier"
	CodePaddedSpecifier    = "permission.padded_specifier"
	CodeUnknownTool        = "permission.unknown_tool"
	CodeOutsideWorkspace   = "permission.outside_workspace"
)

// ValidateRule reports problems with one config-style rule string. known, when
// non-nil, reports whether a canonical tool ID is a real tool (built-in, MCP,
// or session); nil skips the unknown-tool check so callers without a registry
// (plain config edits) cannot reject valid MCP/session rules. The Bash/Edit
// aliases and the file_mutation group are always accepted.
func ValidateRule(rule string, known func(string) bool) []Problem {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return []Problem{{Severity: "error", Code: CodeInvalidRule, Message: "empty permission rule"}}
	}
	parsed, ok := ParseRule(rule)
	if !ok {
		return []Problem{{Severity: "error", Code: CodeInvalidRule,
			Message: fmt.Sprintf("cannot parse %q as a permission rule", rule)}}
	}

	var out []Problem
	if strings.TrimSpace(parsed.Subject) == "" && strings.ContainsAny(rule, "()=") {
		// "Tool()", "Tool( )" and "Tool=" all collapse to a bare rule; a paren or
		// equals sign with nothing inside is almost always a typo for the bare form.
		out = append(out, Problem{Severity: "error", Code: CodeEmptySpecifier,
			Message: fmt.Sprintf("rule %q has an empty specifier and matches every call to %s; write %q or %s(glob)", rule, parsed.Tool, parsed.Tool, parsed.Tool)})
	}
	if strings.Contains(rule, "(") && !strings.HasSuffix(rule, ")") {
		// ParseRule only consumes "Tool(...)" when the string ends with ")"; a
		// paren elsewhere lands in the tool name, which never matches.
		out = append(out, Problem{Severity: "error", Code: CodeMalformedSpecifier,
			Message: fmt.Sprintf("rule %q has an unbalanced specifier; expected %s or %s(glob)", rule, parsed.Tool, parsed.Tool)})
	}
	if subject := parsed.Subject; strings.TrimSpace(subject) != "" && subject != strings.TrimSpace(subject) {
		// The engine matches the raw subject string, so a leading or trailing
		// space makes the rule dead: no real call's subject carries it.
		out = append(out, Problem{Severity: "warning", Code: CodePaddedSpecifier,
			Message: fmt.Sprintf("specifier %q has leading or trailing whitespace and will never match; remove the spaces", subject)})
	}
	if known != nil {
		canonical := canonicalRuleTool(parsed.Tool)
		if canonical != "file_mutation" && !known(canonical) {
			out = append(out, Problem{Severity: "warning", Code: CodeUnknownTool,
				Message: fmt.Sprintf("tool %q is not a built-in tool name; MCP and session tool names are valid, but verify the spelling", parsed.Tool)})
		}
	}
	if parsed.Subject != "" && pathSubjectTool(parsed.Tool) && subjectEscapesWorkspace(parsed.Subject) {
		out = append(out, Problem{Severity: "warning", Code: CodeOutsideWorkspace,
			Message: fmt.Sprintf("specifier %q is an absolute or workspace-escaping path; relative workspace globs like src/** match more robustly", parsed.Subject)})
	}
	return out
}

// RuleError returns the first error-severity finding's message for rule, or
// ("", false) when the rule is valid or only has warnings. Config saves use it
// to reject structural mistakes without blocking MCP/session tool names.
func RuleError(rule string, known func(string) bool) (string, bool) {
	for _, p := range ValidateRule(rule, known) {
		if p.Severity == "error" {
			return p.Message, true
		}
	}
	return "", false
}

// BareRule reports the configured rule with an empty subject (bare tool name)
// that decides a bare tool call. Diagnostics use it to show which configured
// rule fires for a tool when no concrete subject is available; MatchedRule
// covers concrete calls with subjects.
func (p Policy) BareRule(toolName string, decision Decision) (string, bool) {
	var rules []Rule
	switch decision {
	case Allow:
		rules = p.Allow
	case Ask:
		rules = p.Ask
	case Deny:
		rules = p.Deny
	default:
		return "", false
	}
	if rule, ok := firstMatchingRule(rules, toolName, "", canonicalRuleTool(toolName) == "bash"); ok {
		return ruleConfigString(rule), true
	}
	return "", false
}

// pathSubjectTool reports whether a tool's subject is a filesystem path rather
// than a command (bash) or a search pattern (grep/glob), so the out-of-workspace
// check only fires where a path glob really means a path.
func pathSubjectTool(tool string) bool {
	if canonicalRuleTool(tool) == "file_mutation" {
		return true
	}
	switch tool {
	case "read_file", "ls":
		return true
	}
	return false
}

// subjectEscapesWorkspace reports whether a path specifier is absolute or walks
// out of the workspace (leading ".."). Shell commands are excluded by the
// caller because "/" is common inside them.
func subjectEscapesWorkspace(subject string) bool {
	if strings.HasPrefix(subject, "/") || strings.HasPrefix(subject, `\`) {
		return true
	}
	if len(subject) >= 2 && isDriveLetter(subject[0]) && subject[1] == ':' {
		return true
	}
	if strings.HasPrefix(subject, "..") && (len(subject) == 2 || subject[2] == '/' || subject[2] == '\\') {
		return true
	}
	return false
}

func isDriveLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
