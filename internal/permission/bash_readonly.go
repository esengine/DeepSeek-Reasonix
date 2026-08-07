package permission

import (
	"encoding/json"
	"slices"
	"strings"

	"reasonix/internal/shellsafe"
)

// BashCommandIsReadOnly reports whether a bash tool call is a known foreground
// read-only command. Capability-restricted runners use this directly instead of
// depending on Plan mode: Plan is a collaboration workflow, while this check is
// an execution permission boundary.
func BashCommandIsReadOnly(args json.RawMessage) bool {
	var p struct {
		Command                     string `json:"command"`
		RunInBackground             bool   `json:"run_in_background"`
		PreserveBackgroundProcesses bool   `json:"preserve_background_processes"`
	}
	if err := json.Unmarshal(args, &p); err != nil || strings.TrimSpace(p.Command) == "" {
		return false
	}
	if p.RunInBackground || p.PreserveBackgroundProcesses {
		return false
	}
	return isReadOnlyBashSubject(p.Command)
}

// isReadOnlyBashSubject returns true when a bash command is a known read-only
// operation. The subject is the JSON arg value extracted by Subject() — for bash
// it is the raw command string. Command membership comes from the shared
// shellsafe tables (the shared command-classification source, #5341); the
// argument rigor below is permission-specific.
func isReadOnlyBashSubject(subject string) bool {
	if normalized, ok := normalizeBashSafeRedirectsForMatch(subject); ok {
		subject = normalized
	}
	base, sub, fields, ok := shellsafe.ClassifyReadOnlyCommand(subject)
	if !ok {
		return false
	}
	if sub == "" {
		return !hasUnsafeReadOnlyArgs(base, fields[1:])
	}
	return !hasUnsafePrefixArgs(base, sub, fields[2:])
}

// containsShellSyntax delegates to the shared classifier; retained for the other
// permission call sites (permission.go).
func containsShellSyntax(cmd string) bool {
	return shellsafe.ContainsShellSyntax(cmd)
}

func hasUnsafeReadOnlyArgs(base string, args []string) bool {
	switch base {
	case "find":
		return hasAnyArg(args, "-exec", "-execdir", "-delete", "-ok", "-okdir", "-fls", "-fprint", "-fprint0", "-fprintf")
	case "sed":
		for _, arg := range args {
			if strings.HasPrefix(arg, "-i") || strings.HasPrefix(arg, "--in-place") {
				return true
			}
		}
	case "sort":
		return hasArgWithPrefix(args, "-o") || hasAnyArg(args, "--output") || hasArgWithPrefix(args, "--output=")
	}
	return false
}

func hasUnsafePrefixArgs(base, subcmd string, args []string) bool {
	switch base {
	case "git":
		switch subcmd {
		case "diff", "show", "log":
			return hasAnyArg(args, "--output") || hasArgWithPrefix(args, "--output=")
		case "tag":
			// Bare `git tag` lists; with a name it creates one, and -d deletes.
			return !gitTagIsListing(args)
		}
	case "go":
		if subcmd == "env" {
			return hasAnyArg(args, "-w", "-u")
		}
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

func hasArgWithPrefix(args []string, prefix string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}

func hasAnyArg(args []string, unsafe ...string) bool {
	for _, arg := range args {
		if slices.Contains(unsafe, arg) {
			return true
		}
	}
	return false
}

// dangerousBashPatterns are glob-like patterns that match destructive
// commands. Used only for a UI warning — the deny list is the actual
// enforcement mechanism.
var dangerousBashPatterns = []struct {
	pattern string
	label   string
}{
	{"rm -rf*", "recursive delete"},
	{"rm -r *", "recursive delete"},
	{"rm -fr*", "recursive delete"},
	{"git push*--force*", "force push"},
	{"git push*-f*", "force push"},
	{"git reset --hard*", "hard reset"},
	{"git clean -f*", "force clean"},
	{"git restore*", "discards uncommitted changes"},
	{"git checkout -- *", "discards uncommitted changes"},
	{"git checkout .*", "discards uncommitted changes"},
	{"git stash drop*", "drops stashed changes"},
	{"git stash clear*", "drops stashed changes"},
	{"chmod 777*", "world-writable"},
	{"chmod -R 777*", "world-writable recursive"},
	{"chown *", "ownership change"},
	{"sudo *", "superuser"},
	{"mkfs*", "filesystem format"},
	{"dd if=*", "raw device write"},
	{"fdisk*", "partition table"},
	{"> /dev/*", "device overwrite"},
}

// BashDangerWarning returns a short label if subject matches a known
// dangerous pattern, or "" when the command looks safe. This is a visual
// hint only — the Policy rules are the authority.
func BashDangerWarning(subject string) string {
	s := strings.TrimSpace(subject)
	for _, d := range dangerousBashPatterns {
		if matchGlob(d.pattern, s) {
			return d.label
		}
	}
	return ""
}
