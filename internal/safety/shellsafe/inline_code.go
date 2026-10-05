package shellsafe

import "strings"

// ArgvCarriesInlineCode reports whether an argv runs a program written in the
// call itself — `python -c`, `node -e`, `bash -c`, `deno eval`. Permission and
// evidence must agree on it, so it lives here rather than once in each, where
// the two copies had already drifted over the shells and smaller interpreters.
func ArgvCarriesInlineCode(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	carries, known := inlineCodeFlagsFor(ExecutableBase(argv[0]))
	return known && carries(argv[1:])
}

// IsInlineCodeInterpreter reports whether a program runs source handed to it
// rather than a fixed operation. It answers the same table as
// ArgvCarriesInlineCode, for callers holding a fact other than a flag — a
// here-doc on the interpreter's stdin is `-c` by another spelling.
func IsInlineCodeInterpreter(program string) bool {
	_, known := inlineCodeFlagsFor(ExecutableBase(program))
	return known
}

// ArgvTakesProgramFromStdin reports whether an interpreter argv will read its
// program from standard input rather than from a file it names. Handed a
// here-doc that is `-c` by another spelling; given a script or module operand
// the source stays a file the host can read and the body is only its input.
func ArgvTakesProgramFromStdin(argv []string) bool {
	if len(argv) == 0 || !IsInlineCodeInterpreter(argv[0]) {
		return false
	}
	for _, arg := range argv[1:] {
		// A bare "-" names stdin itself; anything else without a dash is the
		// program, and then the body is what that program reads.
		if arg == "-" || strings.HasPrefix(arg, "-") {
			continue
		}
		return false
	}
	return true
}

// inlineCodeFlagsFor is the one table: which programs take handed-in source,
// and how each spells the flag that carries it. Membership and spelling come
// from the same place so a program cannot be known to one caller and unknown to
// the next.
func inlineCodeFlagsFor(base string) (func(args []string) bool, bool) {
	switch base {
	case "bash", "dash", "fish", "ksh", "sh", "zsh":
		return hasShellCommandFlag, true
	case "powershell", "pwsh":
		return inlineFlags("-c", "-command", "-e", "-enc", "-encodedcommand"), true
	case "cmd":
		return inlineFlags("/c", "/k"), true
	case "node", "bun":
		return inlineFlags("-e", "--eval", "-p", "--print"), true
	case "deno":
		return inlineFlags("eval"), true
	case "python", "python3", "py", "pypy", "pypy3":
		return inlineFlags("-c"), true
	case "perl", "ruby", "lua", "luajit", "r", "rscript", "osascript":
		return inlineFlags("-e"), true
	case "php":
		return inlineFlags("-r"), true
	}
	return nil, false
}

func inlineFlags(candidates ...string) func(args []string) bool {
	return func(args []string) bool { return hasInlineFlag(args, candidates...) }
}

// ExecutableBase strips directory and .exe so a path and a bare name classify
// the same. It is exported because every caller of the rules here needs it.
func ExecutableBase(command string) string {
	if i := strings.LastIndexAny(command, `/\`); i >= 0 {
		command = command[i+1:]
	}
	return strings.TrimSuffix(strings.ToLower(command), ".exe")
}

// hasShellCommandFlag reports a POSIX shell's -c, including bundled forms such
// as `sh -lc`. A bare `--` ends option parsing, so nothing after it counts.
func hasShellCommandFlag(args []string) bool {
	for _, arg := range args {
		lower := strings.ToLower(arg)
		switch {
		case lower == "--":
			return false
		case lower == "--command":
			return true
		case strings.HasPrefix(lower, "-") && !strings.HasPrefix(lower, "--") && strings.Contains(lower[1:], "c"):
			return true
		}
	}
	return false
}

func hasInlineFlag(args []string, candidates ...string) bool {
	for _, arg := range args {
		lower := strings.ToLower(arg)
		for _, candidate := range candidates {
			if inlineFlagMatches(lower, candidate) {
				return true
			}
		}
	}
	return false
}

// inlineFlagMatches accepts the spellings a flag actually takes: exact, an
// attached value (--eval=…, -enc:…), and a bundled short form (-pe).
func inlineFlagMatches(arg, candidate string) bool {
	switch {
	case arg == candidate:
		return true
	case strings.HasPrefix(candidate, "--"):
		return strings.HasPrefix(arg, candidate+"=") || strings.HasPrefix(arg, candidate+":")
	case strings.HasPrefix(candidate, "/"):
		return len(candidate) == 2 && strings.HasPrefix(arg, candidate)
	case strings.HasPrefix(candidate, "-") && len(candidate) == 2:
		return strings.HasPrefix(arg, candidate) && !strings.HasPrefix(arg, "--")
	case strings.HasPrefix(candidate, "-"):
		return strings.HasPrefix(arg, candidate+":")
	}
	return false
}
