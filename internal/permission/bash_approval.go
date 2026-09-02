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
	if features.Expansion || features.Assignment || features.Redirection ||
		shellparse.ContainsUnquotedGlob(subject) || hasEnvWrapperAssignment(features.CommandPrefix) {
		return bashApprovalExactOnly
	}
	return bashApprovalReusable
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
	case "awk", "gawk", "mawk", "nawk":
		// awk has no inline-code flag: an inline program (no -f/--file) can
		// run shell commands (system(), "| getline"), so it is treated like
		// python -c; awk -f script.awk stays reusable like python script.py.
		return !awkUsesOnlyScriptFiles(args)
	default:
		return false
	}
}

func awkUsesOnlyScriptFiles(args []string) bool {
	hasFile := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-f" || arg == "--file":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return false
			}
			hasFile = true
			i++
		case strings.HasPrefix(arg, "-f") && len(arg) > 2:
			hasFile = true
		case strings.HasPrefix(arg, "--file="):
			if strings.TrimPrefix(arg, "--file=") == "" {
				return false
			}
			hasFile = true
		case arg == "-E" || arg == "--exec":
			return i+1 < len(args)
		case strings.HasPrefix(arg, "-E") && len(arg) > 2:
			return true
		case strings.HasPrefix(arg, "--exec="):
			return strings.TrimPrefix(arg, "--exec=") != ""
		case arg == "-e" || arg == "--source",
			strings.HasPrefix(arg, "-e") && len(arg) > 2,
			strings.HasPrefix(arg, "--source="):
			return false
		}
	}
	return hasFile
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
