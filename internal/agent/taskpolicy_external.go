package agent

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"unicode"

	"reasonix/internal/shellparse"
)

const maxExternalCommandDepth = 4

// isExternalActionTool recognizes push/publish/deploy-style actions after a
// proxy has resolved to its concrete target. Tool names are host metadata; the
// shell path uses static argv parsing so legal global options cannot hide the
// executable's real subcommand from a user constraint.
func isExternalActionTool(evidenceName, permName string, args json.RawMessage) bool {
	if externalActionToolName(evidenceName) || externalActionToolName(permName) {
		return true
	}
	return shellCommandHasExternalAction(bashCommandFromArgs(args))
}

func externalActionToolName(name string) bool {
	tokens := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(name)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, token := range tokens {
		switch token {
		case "push", "publish", "deploy":
			return true
		}
	}
	if !containsField(tokens, "release") {
		return false
	}
	if len(tokens) == 1 {
		return true
	}
	for _, token := range tokens {
		switch token {
		case "create", "delete", "edit", "publish", "upload", "update":
			return true
		}
	}
	return false
}

func shellCommandHasExternalAction(command string) bool {
	return shellCommandHasExternalActionAtDepth(command, 0)
}

func shellCommandHasExternalActionAtDepth(command string, depth int) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	if depth >= maxExternalCommandDepth {
		// Deep wrapper chains are statically opaque at this policy boundary.
		return true
	}
	segments, _, ok := shellparse.SplitTopLevel(command)
	if !ok {
		return opaqueCommandMayRunExternalAction(command)
	}
	for _, segment := range segments {
		parsed, err := shellparse.ParseStaticCommand(strings.TrimSpace(segment), shellparse.StaticCommandPolicy{
			AllowEnvAssignments: true,
			AllowStderrToStdout: true,
		})
		if err == nil {
			if commandFieldsRunExternalAction(parsed.Argv, depth) {
				return true
			}
			continue
		}
		if opaqueCommandMayRunExternalAction(segment) {
			return true
		}
	}
	return false
}

func opaqueCommandMayRunExternalAction(command string) bool {
	features, ok := shellparse.AnalyzeApprovalFeatures(command)
	if ok && len(features.CommandPrefix) > 0 {
		if commandFieldsRunExternalAction(features.CommandPrefix, 0) {
			return true
		}
		if (features.DynamicCommandName || features.Expansion) && externalActionCapableExecutable(features.CommandPrefix[0]) {
			// A dynamic subcommand such as `git "$verb"` cannot prove that the
			// explicit no-external constraint is preserved, so fail closed.
			return true
		}
	}
	lower := strings.ToLower(command)
	for _, action := range []string{"git push", "git publish", "npm publish", "gh release", "docker push", "kubectl apply"} {
		if strings.Contains(lower, action) {
			return true
		}
	}
	return false
}

func commandFieldsRunExternalAction(fields []string, depth int) bool {
	if len(fields) == 0 {
		return false
	}
	if depth >= maxExternalCommandDepth {
		return true
	}
	base := commandBase(fields[0])
	args := fields[1:]
	if handled, external := wrappedCommandRunsExternalAction(base, args, depth); handled {
		return external
	}
	return commandArgsRunExternalAction(base, lowerFields(args))
}

func wrappedCommandRunsExternalAction(base string, args []string, depth int) (bool, bool) {
	switch base {
	case "env":
		return true, commandFieldsRunExternalAction(wrappedCommandPayload(args), depth+1)
	case "command":
		if len(args) > 0 && (args[0] == "-v" || args[0] == "-V") {
			return true, false
		}
		return true, commandFieldsRunExternalAction(wrappedCommandPayload(args), depth+1)
	case "nohup":
		return true, commandFieldsRunExternalAction(wrappedCommandPayload(args), depth+1)
	case "sudo", "doas":
		return true, commandFieldsRunExternalAction(wrappedCommandPayload(args), depth+1)
	case "bash", "sh", "zsh":
		if nested := shellCommandString(args); nested != "" {
			return true, shellCommandHasExternalActionAtDepth(nested, depth+1)
		}
		return true, false
	case "npx", "pnpx", "bunx":
		return true, commandFieldsRunExternalAction(wrappedCommandPayload(args), depth+1)
	default:
		return false, false
	}
}

func commandArgsRunExternalAction(base string, args []string) bool {
	// Prefer conservative action-token matching over a table of every global option.
	// The gate is active only for an explicit no-external constraint, so rejecting
	// an ambiguous token is safer than letting a real action bypass it.
	switch base {
	case "git":
		return containsAnyField(args, "push", "publish")
	case "npm", "pnpm", "yarn", "cargo", "poetry", "uv":
		return containsAnyField(args, "push", "publish", "unpublish", "deploy", "release")
	case "gem":
		return containsAnyField(args, "push", "publish")
	case "dotnet":
		return containsField(args, "publish") || (containsField(args, "nuget") && containsField(args, "push"))
	case "docker":
		return containsAnyField(args, "push", "--push")
	case "kubectl":
		return containsField(args, "apply")
	case "gh":
		return containsField(args, "release") && containsAnyField(args, "create", "delete", "edit", "publish", "upload")
	case "vercel", "netlify", "flyctl", "railway", "firebase", "wrangler":
		return containsAnyField(args, "deploy", "publish")
	case "make", "just", "task":
		return containsAnyField(args, "push", "publish", "deploy", "release")
	}
	return false
}

func wrappedCommandPayload(args []string) []string {
	for i, arg := range args {
		if externalActionCapableExecutable(arg) {
			return args[i:]
		}
	}
	return nil
}

func externalActionCapableExecutable(field string) bool {
	switch commandBase(field) {
	case "git", "npm", "pnpm", "yarn", "cargo", "poetry", "uv", "gem", "dotnet",
		"docker", "kubectl", "gh", "vercel", "netlify", "flyctl", "railway", "firebase", "wrangler",
		"make", "just", "task", "env", "command", "nohup", "sudo", "doas", "bash", "sh", "zsh", "npx", "pnpx", "bunx":
		return true
	default:
		return false
	}
}

func shellCommandString(args []string) string {
	for i, arg := range args {
		lower := strings.ToLower(arg)
		if lower == "-c" || (strings.HasPrefix(lower, "-") && !strings.HasPrefix(lower, "--") && strings.Contains(lower[1:], "c")) {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
	}
	return ""
}

func commandBase(field string) string {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(field)))
	return strings.TrimSuffix(base, ".exe")
}

func lowerFields(fields []string) []string {
	out := make([]string, len(fields))
	for i, field := range fields {
		out[i] = strings.ToLower(field)
	}
	return out
}

func containsField(fields []string, want string) bool {
	for _, field := range fields {
		if strings.EqualFold(field, want) {
			return true
		}
	}
	return false
}

func containsAnyField(fields []string, wants ...string) bool {
	for _, want := range wants {
		if containsField(fields, want) {
			return true
		}
	}
	return false
}
