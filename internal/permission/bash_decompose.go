package permission

import "reasonix/internal/shellparse"

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
