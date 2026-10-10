package permission

import (
	"slices"
	"strings"

	"reasonix/internal/base/shellparse"
	"reasonix/internal/safety/shellsafe"
)

type bashApprovalClass uint8

const (
	bashApprovalReusable bashApprovalClass = iota
	bashApprovalExactOnly
	bashApprovalRequireHuman
)

// BashApprovalBlocker names which shape forced a command to a human. Hosts use
// it to say what they actually stopped: a caller told "inline interpreter code
// is blocked" about `env | grep` will rewrite toward the wrong thing.
type BashApprovalBlocker uint8

const (
	BashApprovalBlockerNone BashApprovalBlocker = iota
	// BashApprovalBlockerUnparsable is a command static analysis could not read
	// at all, so nothing about it can be trusted.
	BashApprovalBlockerUnparsable
	// BashApprovalBlockerNestedExecution is command substitution or process
	// substitution: $(...), `...`, <(...).
	BashApprovalBlockerNestedExecution
	BashApprovalBlockerDynamicName
	// BashApprovalBlockerInlineCode is an interpreter running code from an
	// argument: python -c, node -e, bash -c.
	BashApprovalBlockerInlineCode
	// BashApprovalBlockerIndirectExecution is a wrapper that runs some other
	// command the argv does not name: eval, source, xargs, find -exec.
	BashApprovalBlockerIndirectExecution
	// BashApprovalBlockerHereDocBody is a here-document the parser reads but an
	// approval cannot: its body is file content, not arguments.
	BashApprovalBlockerHereDocBody
)

// BashSubjectRequiresExplicitApproval reports whether subject can execute a
// nested or indirect command and therefore needs a human in Ask/Auto. Exact
// command rules are handled separately by Policy before this classification.
func BashSubjectRequiresExplicitApproval(subject string) bool {
	return BashSubjectApprovalBlocker(subject) != BashApprovalBlockerNone
}

// BashSubjectApprovalBlocker returns why subject needs a human, or None when it
// does not. The first blocking segment wins, matching the classification order.
func BashSubjectApprovalBlocker(subject string) BashApprovalBlocker {
	if strings.TrimSpace(subject) == "" {
		return BashApprovalBlockerNone
	}
	segments, _, ok := shellparse.SplitTopLevel(subject)
	if !ok {
		return segmentApprovalBlocker(subject)
	}
	if len(segments) == 0 {
		return BashApprovalBlockerUnparsable
	}
	for _, segment := range segments {
		if blocker := segmentApprovalBlocker(segment); blocker != BashApprovalBlockerNone {
			return blocker
		}
	}
	return BashApprovalBlockerNone
}

func segmentApprovalBlocker(subject string) BashApprovalBlocker {
	if normalized, ok := normalizeBashSafeRedirectsForMatch(subject); ok {
		subject = normalized
	}
	features, ok := shellparse.AnalyzeApprovalFeatures(subject)
	if !ok {
		// `cd x && python3 - <<EOF` hides the same inline code a bare
		// `python3 - <<EOF` does; the redirect is visible at any nesting.
		if hasHereDocFedInterpreter(subject) {
			return BashApprovalBlockerInlineCode
		}
		// A compound statement is not a simple call, but the parser still names
		// every command it runs; all-read-only leaves leave nothing to rule on.
		if compoundIsReadOnly(subject) {
			return BashApprovalBlockerNone
		}
		if hasHereDoc(subject) {
			return BashApprovalBlockerHereDocBody
		}
		return BashApprovalBlockerUnparsable
	}
	if features.NestedExecution {
		return BashApprovalBlockerNestedExecution
	}
	if features.DynamicCommandName {
		return BashApprovalBlockerDynamicName
	}
	if len(features.CommandPrefix) > 0 {
		if blocker := indirectExecutionBlocker(features.CommandPrefix); blocker != BashApprovalBlockerNone {
			return blocker
		}
		if inv, ok := shellparse.PeelWrappers(subject); ok && inv.Opaque {
			return BashApprovalBlockerIndirectExecution
		}
		// `python3 - <<EOF` carries its program in the call exactly like -c does;
		// the parser reports the here-document, so this is the same fact, not a
		// guess about what the interpreter will read.
		if features.StdinHereDoc && isInterpreter(features.CommandPrefix[0]) {
			return BashApprovalBlockerInlineCode
		}
	}
	return BashApprovalBlockerNone
}

// isInterpreter reports whether a program executes source handed to it rather
// than a fixed operation. Kept beside indirectExecutionBlocker, which already
// enumerates the same programs for their inline-code flags.
func isInterpreter(program string) bool {
	switch shellsafe.ExecutableBase(program) {
	case "python", "python3", "py", "pypy", "pypy3",
		"node", "bun", "deno",
		"perl", "ruby", "lua", "luajit", "r", "rscript", "osascript", "php",
		"bash", "dash", "fish", "ksh", "sh", "zsh", "powershell", "pwsh":
		return true
	}
	return false
}

func bashSubjectRequiresExactRule(subject string) bool {
	return classifyBashApproval(subject) != bashApprovalReusable
}

func classifyBashApproval(subject string) bashApprovalClass {
	if strings.TrimSpace(subject) == "" {
		return bashApprovalReusable
	}
	segments, _, ok := shellparse.SplitTopLevel(subject)
	if !ok {
		return classifyBashSegmentApproval(subject)
	}
	if len(segments) == 0 {
		return bashApprovalRequireHuman
	}
	class := bashApprovalReusable
	for _, segment := range segments {
		segmentClass := classifyBashSegmentApproval(segment)
		if segmentClass > class {
			class = segmentClass
		}
		if class == bashApprovalRequireHuman {
			break
		}
	}
	return class
}

func classifyBashSegmentApproval(subject string) bashApprovalClass {
	if segmentApprovalBlocker(subject) != BashApprovalBlockerNone {
		return bashApprovalRequireHuman
	}
	if normalized, ok := normalizeBashSafeRedirectsForMatch(subject); ok {
		subject = normalized
	}
	features, ok := shellparse.AnalyzeApprovalFeatures(subject)
	if !ok {
		if compoundIsReadOnly(subject) {
			// Approvable, but never as a reusable prefix: its globs and loop
			// variables make the next command with the same prefix a different
			// command.
			return bashApprovalExactOnly
		}
		return bashApprovalRequireHuman
	}
	if features.Expansion || features.Assignment || features.Redirection ||
		shellparse.ContainsUnquotedGlob(subject) || hasEnvWrapperAssignment(features.CommandPrefix) {
		return bashApprovalExactOnly
	}
	return bashApprovalReusable
}

// indirectExecutionBlocker separates code carried in an argument from wrappers
// that run a command the argv does not name. Both need a human; only the first
// is fixed by writing the code to a file.
func indirectExecutionBlocker(fields []string) BashApprovalBlocker {
	if len(fields) == 0 {
		return BashApprovalBlockerIndirectExecution
	}
	base := shellsafe.ExecutableBase(fields[0])
	args := fields[1:]

	inline := func(ok bool) BashApprovalBlocker {
		if ok {
			return BashApprovalBlockerInlineCode
		}
		return BashApprovalBlockerNone
	}

	switch base {
	case "eval", "source", ".", "xargs":
		return BashApprovalBlockerIndirectExecution
	case "env":
		for len(args) > 0 && isEnvironmentAssignment(args[0]) {
			args = args[1:]
		}
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			return BashApprovalBlockerIndirectExecution
		}
		return indirectExecutionBlocker(args)
	case "builtin", "exec", "nohup", "sudo":
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			return BashApprovalBlockerIndirectExecution
		}
		return indirectExecutionBlocker(args)
	case "command":
		return commandBuiltinBlocker(args)
	case "find":
		if hasAnyFoldedArg(args, "-exec", "-execdir", "-ok", "-okdir") {
			return BashApprovalBlockerIndirectExecution
		}
		return BashApprovalBlockerNone
	default:
		return inline(shellsafe.ArgvCarriesInlineCode(fields))
	}
}

// `command -v`/`-V` report how a name would resolve and run nothing, which
// makes them readers like `which`. Every other form hands the call to the
// program it names.
func commandBuiltinBlocker(args []string) BashApprovalBlocker {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		flag := args[0]
		args = args[1:]
		if flag == "--" {
			break
		}
		if strings.ContainsAny(flag, "vV") {
			return BashApprovalBlockerNone
		}
	}
	if len(args) == 0 {
		return BashApprovalBlockerIndirectExecution
	}
	return indirectExecutionBlocker(args)
}

func hasEnvWrapperAssignment(fields []string) bool {
	if len(fields) < 2 || shellsafe.ExecutableBase(fields[0]) != "env" {
		return false
	}
	for _, arg := range fields[1:] {
		if isEnvironmentAssignment(arg) {
			return true
		}
		if !strings.HasPrefix(arg, "-") {
			return false
		}
	}
	return false
}

func hasAnyFoldedArg(args []string, candidates ...string) bool {
	for _, arg := range args {
		lower := strings.ToLower(arg)
		for _, candidate := range candidates {
			candidate = strings.ToLower(candidate)
			if lower == candidate {
				return true
			}
			if strings.HasPrefix(candidate, "--") && (strings.HasPrefix(lower, candidate+"=") || strings.HasPrefix(lower, candidate+":")) {
				return true
			}
			if strings.HasPrefix(candidate, "-") && !strings.HasPrefix(candidate, "--") && len(candidate) == 2 && strings.HasPrefix(lower, candidate) && !strings.HasPrefix(lower, "--") {
				return true
			}
			if strings.HasPrefix(candidate, "/") && len(candidate) == 2 && strings.HasPrefix(lower, candidate) {
				return true
			}
			if len(candidate) > 2 && strings.HasPrefix(candidate, "-") && strings.HasPrefix(lower, candidate+":") {
				return true
			}
		}
	}
	return false
}

func isEnvironmentAssignment(arg string) bool {
	name, _, ok := strings.Cut(arg, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		letter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		digit := i > 0 && r >= '0' && r <= '9'
		if !letter && !digit && r != '_' {
			return false
		}
	}
	return true
}

// compoundIsReadOnly reports whether every command a compound statement would
// run is read-only. It fails closed: the parser must be able to name each
// program, and any substitution, redirect, or here-document rules the whole
// statement out before its leaves are examined.
func compoundIsReadOnly(subject string) bool {
	leaves, ok := shellparse.CompoundLeafCommands(subject)
	if !ok {
		return false
	}
	readOnly, _ := leavesProof(leaves)
	return readOnly
}

// hasHereDocFedInterpreter reports whether any command in the statement reads
// its source from a here-document.
func hasHereDocFedInterpreter(subject string) bool {
	return slices.ContainsFunc(shellparse.StdinHereDocPrograms(subject), isInterpreter)
}

// hasHereDoc reports whether the statement carries a here-document at all. The
// parser reads one fine; an approval cannot, because the body is content rather
// than arguments — which is a different gap from a statement nothing can read.
func hasHereDoc(subject string) bool {
	file, err := shellparse.ParseBash(subject)
	return err == nil && shellparse.HasHereDoc(file)
}
