package shellsafe

import (
	"slices"
	"strings"
)

// ArgsMakeReadOnlyCommandWrite reports whether a command whose name is in the
// read-only tables writes anyway because of its arguments — `find -delete`,
// `sort -o`, `git tag v1.0`. Table membership answers "can this program ever
// read"; this answers "is this call a read". Permission and evidence must ask
// both and agree, so the rule lives here rather than once in each.
func ArgsMakeReadOnlyCommandWrite(base, sub string, fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	base = strings.ToLower(base)
	args := fields[1:]
	if sub != "" {
		if len(args) == 0 {
			return false
		}
		args = args[1:]
	}
	if rule, ok := argRules[base]; ok {
		return rule(sub, args)
	}
	return false
}

// gitTagIsListing reports whether a `git tag` invocation only lists tags. A
// bare `git tag` lists; a name creates one and -d deletes, so anything that
// isn't an explicit listing form writes the ref namespace.
func gitTagIsListing(args []string) bool {
	listing := false
	var operands []string
	for _, arg := range args {
		switch {
		case arg == "-l" || arg == "--list":
			listing = true
		case arg == "-d" || arg == "--delete" || arg == "-a" || arg == "-s" || arg == "-f" || arg == "--force" || arg == "-m" || arg == "-F":
			return false
		case strings.HasPrefix(arg, "-"):
			// Remaining flags (-n, --sort=, --format=, --contains, …) are output
			// shaping; unknown ones fail closed below only when paired with an
			// operand, which is the create/delete form.
			continue
		default:
			operands = append(operands, arg)
		}
	}
	if listing {
		return true // operands are shell patterns for the listing filter
	}
	return len(operands) == 0
}

func hasAnyArg(args []string, unsafe ...string) bool {
	for _, arg := range args {
		if slices.Contains(unsafe, arg) {
			return true
		}
	}
	return false
}

// envRunsAProgram reports whether an env invocation ends in a program rather
// than printing the environment. Everything env can be handed before that is an
// option or a NAME=VALUE assignment, so the first bare word is the program —
// and env can no more vouch for it than a shell can. Option arguments that take
// a separate value (-u VAR) read as that bare word and fail closed.
func envRunsAProgram(args []string) bool {
	if hasShortFlag(args, 'S') || hasLongOpt(args, "--split-string", 3) {
		return true
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
			continue
		}
		return true
	}
	return false
}
