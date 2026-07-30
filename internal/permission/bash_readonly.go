package permission

import (
	"encoding/json"
	"strings"

	"reasonix/internal/shellparse"
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
	base, sub, ok := shellsafe.CommandIsReadOnly(subject)
	if !ok {
		return false
	}
	fields, malformed := shellparse.StaticFields(subject)
	if malformed != "" {
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
		}
	case "go":
		if subcmd == "env" {
			return hasAnyArg(args, "-w", "-u")
		}
	}
	return false
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
		for _, candidate := range unsafe {
			if arg == candidate {
				return true
			}
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
