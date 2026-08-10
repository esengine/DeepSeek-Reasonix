package permission

import (
	"strings"

	"reasonix/internal/shellparse"
)

type bashApprovalClass uint8

const (
	bashApprovalReusable bashApprovalClass = iota
	bashApprovalExactOnly
	bashApprovalRequireHuman
)

// BashSubjectRequiresExplicitApproval reports whether subject can execute a
// nested or indirect command and therefore needs a human in Ask/Auto. Exact
// command rules are handled separately by Policy before this classification.
func BashSubjectRequiresExplicitApproval(subject string) bool {
	return classifyBashApproval(subject) == bashApprovalRequireHuman
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
	if normalized, ok := normalizeBashSafeRedirectsForMatch(subject); ok {
		subject = normalized
	}
	features, ok := shellparse.AnalyzeApprovalFeatures(subject)
	if !ok || features.NestedExecution || features.DynamicCommandName {
		return bashApprovalRequireHuman
	}
	if len(features.CommandPrefix) > 0 && isIndirectExecution(features.CommandPrefix) {
		return bashApprovalRequireHuman
	}
	// Destructive git/file forms are exact-looking shell but must not auto-run
	// under Auto: users report silent `git checkout` discarding work (#7784).
	if bashSubjectIsHighRiskMutation(subject) {
		return bashApprovalRequireHuman
	}
	if features.Expansion || features.Assignment || features.Redirection ||
		shellparse.ContainsUnquotedGlob(subject) || hasEnvWrapperAssignment(features.CommandPrefix) {
		return bashApprovalExactOnly
	}
	return bashApprovalReusable
}

// bashSubjectIsHighRiskMutation mirrors the recovery high-risk git classifier
// without importing recovery (that package depends on agent → config →
// permission). Keep destructive worktree/index rewrites behind a human prompt
// even when the shell shape is "exact-only reusable" under Auto.
func bashSubjectIsHighRiskMutation(subject string) bool {
	features, ok := shellparse.AnalyzeApprovalFeatures(subject)
	if !ok || len(features.CommandPrefix) == 0 {
		return false
	}
	fields := features.CommandPrefix
	// Strip env-style wrappers so `env git checkout` still matches.
	for len(fields) > 0 {
		base := executableBase(fields[0])
		switch base {
		case "env":
			fields = fields[1:]
			for len(fields) > 0 && isEnvironmentAssignment(fields[0]) {
				fields = fields[1:]
			}
			continue
		case "command", "builtin", "exec", "nohup", "sudo":
			fields = fields[1:]
			for len(fields) > 0 && strings.HasPrefix(fields[0], "-") {
				fields = fields[1:]
			}
			continue
		}
		break
	}
	if len(fields) == 0 || executableBase(fields[0]) != "git" {
		return false
	}
	args := fields[1:]
	if containsFoldedArg(args, "push", "clean", "prune", "filter-branch", "filter-repo") {
		return true
	}
	if containsFoldedArg(args, "reset") && containsFoldedArg(args, "--hard", "--merge", "--keep") {
		return true
	}
	if containsFoldedArg(args, "checkout") {
		return true
	}
	if containsFoldedArg(args, "switch") && containsFoldedArg(args, "--discard-changes") {
		return true
	}
	if containsFoldedArg(args, "restore") && (!containsFoldedArg(args, "--staged") || containsFoldedArg(args, "--worktree")) {
		return true
	}
	if containsFoldedArg(args, "branch") && containsFoldedArg(args, "-d", "--delete", "-f", "--force") {
		return true
	}
	if containsFoldedArg(args, "stash") && containsFoldedArg(args, "clear", "drop") {
		return true
	}
	return false
}

func containsFoldedArg(args []string, needles ...string) bool {
	for _, arg := range args {
		low := strings.ToLower(strings.TrimSpace(arg))
		for _, needle := range needles {
			if low == strings.ToLower(needle) {
				return true
			}
		}
	}
	return false
}

func isIndirectExecution(fields []string) bool {
	if len(fields) == 0 {
		return true
	}
	base := executableBase(fields[0])
	args := fields[1:]

	switch base {
	case "eval", "source", ".", "xargs":
		return true
	case "env":
		for len(args) > 0 && isEnvironmentAssignment(args[0]) {
			args = args[1:]
		}
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			return true
		}
		return isIndirectExecution(args)
	case "builtin", "command", "exec", "nohup", "sudo":
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			return true
		}
		return isIndirectExecution(args)
	case "bash", "dash", "fish", "ksh", "sh", "zsh":
		return hasShellCommandFlag(args)
	case "powershell", "pwsh":
		return hasAnyFoldedArg(args, "-c", "-command", "-e", "-enc", "-encodedcommand")
	case "cmd":
		return hasAnyFoldedArg(args, "/c", "/k")
	case "node", "bun":
		return hasAnyFoldedArg(args, "-e", "--eval", "-p", "--print")
	case "deno":
		return hasAnyFoldedArg(args, "eval")
	case "python", "python3", "py", "pypy", "pypy3":
		return hasAnyFoldedArg(args, "-c")
	case "perl", "ruby", "lua", "luajit", "r", "rscript", "osascript":
		return hasAnyFoldedArg(args, "-e")
	case "php":
		return hasAnyFoldedArg(args, "-r")
	case "find":
		return hasAnyFoldedArg(args, "-exec", "-execdir", "-ok", "-okdir")
	default:
		return false
	}
}

func hasEnvWrapperAssignment(fields []string) bool {
	if len(fields) < 2 || executableBase(fields[0]) != "env" {
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

func executableBase(command string) string {
	if i := strings.LastIndexAny(command, `/\\`); i >= 0 {
		command = command[i+1:]
	}
	command = strings.ToLower(command)
	return strings.TrimSuffix(command, ".exe")
}

func hasShellCommandFlag(args []string) bool {
	for _, arg := range args {
		lower := strings.ToLower(arg)
		if lower == "--" {
			return false
		}
		if lower == "--command" {
			return true
		}
		if strings.HasPrefix(lower, "-") && !strings.HasPrefix(lower, "--") && strings.Contains(lower[1:], "c") {
			return true
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
