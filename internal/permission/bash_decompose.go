package permission

import (
	"strings"

	"reasonix/internal/shellparse"
)

// DecomposeBashCommand splits a compound bash command line into its
// simple-command segments so each segment can be matched against the rule
// table independently. This is the mechanism Claude Code and comparable
// harnesses use to make prefix rules like `Bash(git push:*)` reusable across
// compound invocations without ever synthesizing a new prefix from a compound
// command.
//
// It splits on the shell control operators `;`, `&`, `&&`, `|`, `||`, and
// newlines. Quoting (single, double, backslash-escapes inside double quotes)
// and $(...) / <(...) / >(...) / `...` command / process substitutions are
// treated as opaque — operators inside them do NOT split the outer command.
// File-descriptor duplication like `2>&1` and combined redirects like
// `&>/dev/null` are recognized as redirection syntax rather than splitters.
//
// Known out-of-scope shapes — the parser refuses to decompose these to keep
// downstream matching safe, so callers fall back to whole-string matching:
//   - heredocs (`cat <<EOF … EOF`): the delimiter body isn't shell syntax,
//     but tokenizing it as one is wrong.
//   - leading operator (`&& ls`, `; ls`): malformed shell.
//   - unbalanced quotes and unsupported compound statements.
//
// Returns nil when the input has no control operator to split on, or when the
// parser encounters one of the above out-of-scope shapes. Redirect fragments
// (`2>/dev/null`, `> file`) are left attached to the simple command they
// annotate; permission matching later strips only the conservative safe subset.
//
// The only contract this function exposes is `[]string` of trimmed
// simple-command text, or `nil` for "fall back to exact match".
func DecomposeBashCommand(cmd string) []string {
	out, split, ok := shellparse.SplitTopLevel(cmd)
	if !ok || !split || len(out) < 2 {
		return nil
	}
	return out
}

// ExtractSubshellCommands returns the top-level inner commands hidden inside
// $(), backticks, <(), and >() process substitutions. It does NOT recurse —
// nesting is handled by the caller invoking DecideSubject recursively on each
// result. Returns nil when no subshell structures are found.
//
// Respects quoting:
//   - Single-quoted regions are skipped entirely ('$(cmd)' is literal)
//   - Double-quoted regions ARE scanned ("$(cmd)" expands at runtime)
//   - $((...)) arithmetic expansions are skipped
//   - Backslash escapes inside double quotes and backticks are handled
func ExtractSubshellCommands(subject string) []string {
	var result []string
	i := 0
	for i < len(subject) {
		ch := subject[i]

		// Single-quoted region — skip entirely, no expansion inside
		if ch == '\'' {
			i++
			for i < len(subject) && subject[i] != '\'' {
				i++
			}
			if i < len(subject) {
				i++ // skip closing quote
			}
			continue
		}

		// Double-quoted region — scan for $() inside
		if ch == '"' {
			i++
			for i < len(subject) && subject[i] != '"' {
				if subject[i] == '\\' && i+1 < len(subject) {
					i += 2 // skip escaped char
					continue
				}
				if subject[i] == '$' && i+1 < len(subject) && subject[i+1] == '(' {
					// Skip $(( arithmetic expansion
					if i+2 < len(subject) && subject[i+2] == '(' {
						depth := 0
						for j := i + 2; j < len(subject); j++ {
							if subject[j] == '(' {
								depth++
							} else if subject[j] == ')' {
								depth--
								if depth == 0 {
									i = j + 1
									break
								}
							}
						}
						continue
					}
					inner, end := extractParens(subject, i+2)
					if inner != "" {
						result = append(result, strings.TrimSpace(inner))
					}
					if end > i {
						i = end
					} else {
						i++
					}
					continue
				}
				i++
			}
			if i < len(subject) {
				i++ // skip closing quote
			}
			continue
		}

		// $(...) command substitution (not inside quotes)
		if ch == '$' && i+1 < len(subject) && subject[i+1] == '(' {
			// Skip $(( arithmetic expansion
			if i+2 < len(subject) && subject[i+2] == '(' {
				depth := 0
				for j := i + 2; j < len(subject); j++ {
					if subject[j] == '(' {
						depth++
					} else if subject[j] == ')' {
						depth--
						if depth == 0 {
							i = j + 1
							break
						}
					}
				}
				continue
			}
			inner, end := extractParens(subject, i+2)
			if inner != "" {
				result = append(result, strings.TrimSpace(inner))
			}
			if end > i {
				i = end
			} else {
				i++
			}
			continue
		}

		// Backtick command substitution
		if ch == '`' {
			j := i + 1
			for j < len(subject) && subject[j] != '`' {
				if subject[j] == '\\' && j+1 < len(subject) {
					j += 2
					continue
				}
				j++
			}
			if j < len(subject) {
				inner := subject[i+1 : j]
				if inner != "" {
					result = append(result, strings.TrimSpace(inner))
				}
				i = j + 1
			} else {
				i++
			}
			continue
		}

		// <() or >() process substitution
		if (ch == '<' || ch == '>') && i+1 < len(subject) && subject[i+1] == '(' {
			inner, end := extractParens(subject, i+2)
			if inner != "" {
				result = append(result, strings.TrimSpace(inner))
			}
			if end > i {
				i = end
			} else {
				i++
			}
			continue
		}

		i++
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// extractParens extracts the content inside balanced parentheses starting at
// position start (which should be right after the opening '('). Returns the
// inner content and the position after the closing ')'. Handles nesting and
// respects single-quoted and double-quoted regions.
func extractParens(s string, start int) (string, int) {
	depth := 1
	i := start
	for i < len(s) && depth > 0 {
		ch := s[i]
		if ch == '\'' {
			i++
			for i < len(s) && s[i] != '\'' {
				i++
			}
			if i < len(s) {
				i++
			}
			continue
		}
		if ch == '"' {
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				i++
			}
			if i < len(s) {
				i++
			}
			continue
		}
		if ch == '\\' && i+1 < len(s) {
			i += 2
			continue
		}
		switch {
		case ch == '(':
			depth++
		case ch == ')':
			depth--
			if depth == 0 {
				return s[start:i], i + 1
			}
		}
		i++
	}
	return "", start
}

// StripSubshells replaces $(), backtick, <(), and >() regions with a single
// space, returning the outer command skeleton. This skeleton can be evaluated
// through normal permission rules to preserve Ask/SessionAllow semantics for
// the outer command.
// Example: "git status $(touch /tmp/x) --verbose" → "git status --verbose"
func StripSubshells(subject string) string {
	var b strings.Builder
	i := 0
	lastEnd := 0
	for i < len(subject) {
		ch := subject[i]

		// Single-quoted — skip entirely
		if ch == '\'' {
			i++
			for i < len(subject) && subject[i] != '\'' {
				i++
			}
			if i < len(subject) {
				i++
			}
			continue
		}

		// Double-quoted — scan for $() inside
		if ch == '"' {
			i++
			for i < len(subject) && subject[i] != '"' {
				if subject[i] == '\\' && i+1 < len(subject) {
					i += 2
					continue
				}
				if subject[i] == '$' && i+1 < len(subject) && subject[i+1] == '(' {
					if i+2 < len(subject) && subject[i+2] == '(' {
						// arithmetic — skip
						depth := 0
						for j := i + 2; j < len(subject); j++ {
							if subject[j] == '(' {
								depth++
							}
							if subject[j] == ')' {
								depth--
								if depth == 0 {
									i = j + 1
									break
								}
							}
						}
						continue
					}
					_, end := extractParens(subject, i+2)
					if end > i {
						b.WriteString(subject[lastEnd:i])
						b.WriteByte(' ')
						lastEnd = end
						i = end
					} else {
						i++
					}
					continue
				}
				i++
			}
			if i < len(subject) {
				i++
			}
			continue
		}

		// $() outside quotes
		if ch == '$' && i+1 < len(subject) && subject[i+1] == '(' {
			if i+2 < len(subject) && subject[i+2] == '(' {
				depth := 0
				for j := i + 2; j < len(subject); j++ {
					if subject[j] == '(' {
						depth++
					}
					if subject[j] == ')' {
						depth--
						if depth == 0 {
							i = j + 1
							break
						}
					}
				}
				continue
			}
			_, end := extractParens(subject, i+2)
			if end > i {
				b.WriteString(subject[lastEnd:i])
				b.WriteByte(' ')
				lastEnd = end
				i = end
			} else {
				i++
			}
			continue
		}

		// Backtick
		if ch == '`' {
			j := i + 1
			for j < len(subject) && subject[j] != '`' {
				if subject[j] == '\\' && j+1 < len(subject) {
					j += 2
					continue
				}
				j++
			}
			if j < len(subject) {
				b.WriteString(subject[lastEnd:i])
				b.WriteByte(' ')
				lastEnd = j + 1
				i = j + 1
			} else {
				i++
			}
			continue
		}

		// <() or >()
		if (ch == '<' || ch == '>') && i+1 < len(subject) && subject[i+1] == '(' {
			_, end := extractParens(subject, i+2)
			if end > i {
				b.WriteString(subject[lastEnd:i])
				b.WriteByte(' ')
				lastEnd = end
				i = end
			} else {
				i++
			}
			continue
		}

		i++
	}
	b.WriteString(subject[lastEnd:])
	return strings.Join(strings.Fields(b.String()), " ")
}
