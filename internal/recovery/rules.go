package recovery

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/shellparse"
	"reasonix/internal/shellsafe"
)

// QualifyingFailure reports whether an observation should arm the checkpoint.
// User rejections, host policy blocks, cancels, provider errors, and empty
// search results never qualify.
func QualifyingFailure(obs Observation) bool {
	if obs.Success || obs.Blocked || obs.UserRejected || obs.ProviderError || obs.Cancelled || obs.EmptySearch {
		return false
	}
	// Mutating tool failure always qualifies.
	if obs.Mutates {
		return true
	}
	// Host-recognized verification command non-zero exit.
	if obs.Verification {
		return true
	}
	// File/shell/MCP tools that can change state but reported non-readonly.
	if !obs.ReadOnly && strings.TrimSpace(obs.Tool) != "" {
		return true
	}
	return false
}

// ClassifyFailure identifies the owning recovery policy without treating an
// execution reliability problem as a permission or user-decision boundary.
// The classifier is deliberately narrow: permission/sandbox/user blocks are
// filtered by QualifyingFailure before this is called.
func ClassifyFailure(obs Observation) FailureClass {
	if transientFailureText(obs.ErrSummary) || transientFailureText(obs.Output) {
		return FailureClassTransient
	}
	if obs.Verification {
		return FailureClassVerification
	}
	if obs.Mutates {
		return FailureClassMutation
	}
	return FailureClassExecution
}

func transientFailureText(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	for _, marker := range []string{
		"command timed out",
		"timed out after",
		"timed out (>",
		"context deadline exceeded",
		"deadline exceeded",
		"execution timeout",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// IsVerificationCall reports whether the host recognizes the call as a
// verification command (test/lint/build/typecheck/compile).
func IsVerificationCall(tool string, args json.RawMessage, readOnly bool) bool {
	tool = strings.TrimSpace(tool)
	if tool == "bash" {
		return evidence.IsDeliveryVerificationCommand(commandFromArgs(args))
	}
	// Project-check style tools are verification even when not bash.
	switch tool {
	case "complete_step":
		return false
	}
	_ = readOnly
	return false
}

// IsSafeVerificationRetry reports whether proposal is a first safe retry of the
// same host-proven verification command that failed.
// Callers must also consult the runtime safe-retry budget (safeRetryUsed /
// SafeRetryLeft); a spent budget never qualifies.
func IsSafeVerificationRetry(failure *FailureEvent, proposal Proposal) bool {
	if failure == nil || !failure.Verification {
		return false
	}
	if failure.SafeRetryLeft <= 0 {
		// evidenceCopy sets SafeRetryLeft from runtime truth; 0 means spent.
		return false
	}
	if !proposal.Verification || proposal.HighRisk || proposal.ExpandedScope || proposal.StrategyChanged {
		return false
	}
	if strings.TrimSpace(proposal.Tool) != strings.TrimSpace(failure.Tool) {
		return false
	}
	// Same normalized command / subject for verification retries.
	if normalizeCommand(proposal.Subject) != "" && normalizeCommand(failure.Subject) != "" {
		return normalizeCommand(proposal.Subject) == normalizeCommand(failure.Subject)
	}
	return CallFingerprint(proposal.Tool, proposal.Subject, "", proposal.Args) ==
		CallFingerprint(failure.Tool, failure.Subject, "", failure.Args)
}

// IsHighRiskMutation preserves the legacy execution-risk classifier for event
// compatibility and focused policy tests. Auto no longer turns this result into
// a human confirmation; permission, sandbox, and tool policy own that boundary.
func IsHighRiskMutation(proposal Proposal) bool {
	return riskBoundaryForProposal(proposal).highRisk
}

// TaskGrantKey returns the legacy semantic key used by persisted recovery cards.
// New Auto decisions do not create execution-risk grants. Keys remain narrower
// than a command name but broader than raw command bytes:
// for example, ordinary pushes to the same Git remote destination share a key,
// while a different ref, force push, or arbitrary HTTP/API mutation never does.
func TaskGrantKey(proposal Proposal) string {
	return riskBoundaryForProposal(proposal).taskGrantKey
}

type riskBoundary struct {
	highRisk         bool
	taskGrantKey     string
	taskGrantDisplay string
}

func riskBoundaryForProposal(proposal Proposal) riskBoundary {
	if proposal.HighRisk {
		// Caller-supplied risk has no host-proven semantic scope, so it is never
		// eligible for a reusable task grant.
		return riskBoundary{highRisk: true}
	}
	tool := strings.TrimSpace(proposal.Tool)
	if strings.HasPrefix(tool, "mcp__") || strings.Contains(tool, "mcp") {
		// MCP already has a richer policy/destructive-hint gate. Duplicating that
		// prompt here would create two human decisions for one call.
		return riskBoundary{}
	}
	if tool == "bash" {
		cmd := commandFromArgs(proposal.Args)
		// Host-recognized test/build commands may create project-local artifacts,
		// but are already bounded by the verification classifier. Deterministic
		// destructive forms still trip commandFieldsHighRisk below.
		return bashRiskBoundary(cmd, proposal.Mutates && !proposal.Verification)
	}
	// Workspace file tools remain on Auto's fast path, including dependency,
	// configuration, and workflow files. Sandbox and explicit approval policy
	// still own writes outside the workspace; this layer only adds hard-boundary
	// confirmation for commands the host can classify deterministically.
	return riskBoundary{}
}

// ClassifyEmptySearch reports whether a successful read-only search produced
// no matches. Callers set Observation.EmptySearch from this.
func ClassifyEmptySearch(tool string, success bool, readOnly bool, output string) bool {
	if !success || !readOnly {
		return false
	}
	switch strings.TrimSpace(tool) {
	case "grep", "glob", "ls", "code_index", "codeindex":
		// fall through
	default:
		return false
	}
	out := strings.TrimSpace(output)
	if out == "" {
		return true
	}
	lower := strings.ToLower(out)
	for _, marker := range []string{
		"no matches",
		"no files found",
		"0 matches",
		"not found",
		"no results",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// IsDiagnosticSuccess reports a successful read-only diagnostic that must not
// clear the active failure event (ls/rg/grep/read_file, etc.).
func IsDiagnosticSuccess(obs Observation) bool {
	if !obs.Success || obs.Mutates || obs.Verification {
		return false
	}
	switch strings.TrimSpace(obs.Tool) {
	case "bash":
		cmd := commandFromArgs(obs.Args)
		base, _, readOnly := shellsafe.CommandIsReadOnly(cmd)
		if !readOnly {
			return false
		}
		switch strings.ToLower(filepath.Base(base)) {
		case "ls", "rg", "grep", "find", "cat", "head", "tail", "wc", "file", "stat", "pwd", "which", "type":
			return true
		}
		return true // other host-proven read-only bash diagnostics
	case "read_file", "grep", "glob", "ls", "code_index", "codeindex":
		return obs.ReadOnly
	default:
		return false
	}
}

func commandFromArgs(args json.RawMessage) string {
	if len(args) == 0 {
		return ""
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(args, &fields); err != nil {
		return ""
	}
	raw, ok := fields["command"]
	if !ok {
		return ""
	}
	var cmd string
	if err := json.Unmarshal(raw, &cmd); err != nil {
		return ""
	}
	return strings.TrimSpace(cmd)
}

func pathsFromArgs(args json.RawMessage) []string {
	if len(args) == 0 {
		return nil
	}
	var fields map[string]any
	if err := json.Unmarshal(args, &fields); err != nil {
		return nil
	}
	var paths []string
	for _, key := range []string{
		"path", "file_path", "file", "target", "destination",
		"source_path", "destination_path", "old_path", "new_path",
	} {
		if v, ok := fields[key].(string); ok && strings.TrimSpace(v) != "" {
			paths = append(paths, strings.TrimSpace(v))
		}
	}
	return uniqueStrings(paths)
}

func normalizeCommand(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func bashRiskBoundary(command string, enforceMutationAllowlist bool) riskBoundary {
	command = strings.TrimSpace(command)
	if command == "" {
		return riskBoundary{highRisk: true}
	}
	lower := strings.ToLower(command)
	// Fast markers cover destructive redirection and commands whose static
	// tokenization may be obscured by shell punctuation. Project-local installs
	// and version-controlled configuration edits intentionally stay automatic.
	riskMarkers := []string{
		"rm -", "rmdir", "unlink ", "shred ",
		"git reset --hard", "git clean",
		"chmod ", "chown ", "mkfs", "dd if=",
		"> /", ">> /",
	}
	for _, m := range riskMarkers {
		if strings.Contains(lower, m) {
			return riskBoundary{highRisk: true}
		}
	}
	segments, _, ok := shellparse.SplitTopLevel(command)
	if !ok {
		return riskBoundary{highRisk: true}
	}
	var grant taskGrantBoundary
	for _, segment := range segments {
		fields, malformed := shellparse.StaticFields(segment)
		if malformed != "" || len(fields) == 0 {
			return riskBoundary{highRisk: true}
		}
		if commandFieldsHighRisk(fields) {
			if len(segments) == 1 {
				grant = commandFieldsTaskGrantBoundary(fields)
			}
			return riskBoundary{
				highRisk:         true,
				taskGrantKey:     grant.key,
				taskGrantDisplay: grant.display,
			}
		}
		if enforceMutationAllowlist && !commandFieldsKnownSafeMutation(fields) {
			// The host knows this call can mutate, but this policy cannot prove it is
			// a reversible workspace operation. Fail closed instead of letting an
			// unlisted shell or PowerShell command silently widen Auto.
			return riskBoundary{highRisk: true}
		}
	}
	return riskBoundary{}
}

func commandFieldsHighRisk(fields []string) bool {
	if len(fields) == 0 {
		return true
	}
	base := strings.ToLower(filepath.Base(fields[0]))
	rawArgs := fields[1:]
	args := lowerFields(rawArgs)
	switch base {
	case "sudo", "doas", "pkexec", "xargs":
		// Privilege escalation and dynamic command dispatch are high risk even
		// when the wrapped command itself is not statically recoverable here.
		return true
	case "env":
		wrapped, ok := unwrapEnvCommand(rawArgs)
		return !ok || commandFieldsHighRisk(wrapped)
	case "command":
		wrapped, ok := unwrapCommandBuiltin(rawArgs)
		return !ok || (len(wrapped) > 0 && commandFieldsHighRisk(wrapped))
	case "nohup":
		return commandFieldsHighRisk(trimLeadingOptions(rawArgs))
	case "rm", "rmdir", "unlink", "shred", "dd", "mkfs", "chmod", "chown",
		"docker", "kubectl", "terraform":
		return true
	case "remove-item", "clear-content", "set-content", "add-content", "move-item", "copy-item",
		"new-item", "rename-item", "invoke-restmethod", "invoke-webrequest", "start-process",
		"stop-process", "restart-computer", "stop-computer", "format-volume", "clear-disk",
		"initialize-disk", "powershell", "powershell.exe", "pwsh", "pwsh.exe", "cmd", "cmd.exe",
		"del", "erase", "rd", "format", "diskpart":
		// Reasonix runs the bash tool through PowerShell on Windows. Bash AST still
		// gives us useful static words for simple native commands, but these verbs
		// are not reversible workspace operations and must never fall through.
		return true
	case "find":
		return containsAny(args, "-delete", "-exec", "-execdir", "-ok", "-okdir")
	case "git":
		return gitCommandHighRisk(args)
	case "curl":
		return curlCommandHighRisk(rawArgs)
	case "wget":
		return wgetCommandHighRisk(args)
	case "gh":
		return ghCommandHighRisk(args)
	case "http", "https", "xh":
		return httpCommandHighRisk(args)
	case "aws", "gcloud", "az", "oci", "doctl", "heroku", "vercel", "netlify",
		"flyctl", "railway", "firebase", "wrangler", "cloudflared", "ssh", "scp",
		"sftp", "rsync", "psql", "mysql", "redis-cli", "mongosh":
		// These tools can mutate remote services or hosts, and their command
		// languages are too broad for this layer to prove a call read-only. Keep
		// them behind Auto's explicit external-action boundary.
		return true
	case "npm":
		return containsAny(args, "publish", "unpublish", "link", "unlink", "config") || hasGlobalFlag(args)
	case "pnpm":
		return containsAny(args, "publish", "deploy", "link", "unlink", "setup") || hasGlobalFlag(args) ||
			(containsAny(args, "env") && containsAny(args, "use", "remove") && containsAny(args, "--global"))
	case "yarn":
		return containsAny(args, "publish", "link", "unlink") || hasGlobalFlag(args) ||
			(containsAny(args, "global") && containsAny(args, "add", "remove", "upgrade"))
	case "pip", "pip3", "pipx":
		// Python installers mutate the active interpreter environment unless the
		// host can prove a project-local target, which this command layer cannot.
		return containsAny(args, "install", "uninstall", "inject", "upgrade")
	case "brew", "apt", "apt-get", "dnf", "yum", "apk", "pacman":
		return containsAny(args, "install", "add", "remove", "uninstall", "upgrade", "update")
	case "go":
		if containsAny(args, "install", "clean") {
			return true
		}
		if containsAny(args, "env") && containsAny(args, "-w", "-u") {
			return true
		}
		return false
	case "cargo":
		return containsAny(args, "install", "uninstall", "publish", "yank", "login", "logout")
	case "composer":
		return (containsAny(args, "config") && hasGlobalFlag(args)) ||
			(containsAny(args, "global") && containsAny(args, "require", "remove", "update", "install", "config", "exec"))
	case "poetry":
		return containsAny(args, "publish", "config", "self")
	case "uv":
		return containsAny(args, "publish", "tool")
	case "dotnet":
		return containsAny(args, "push", "delete") || hasGlobalFlag(args)
	case "gem", "bundle", "bundler":
		return containsAny(args, "install", "uninstall", "update", "add", "remove", "push", "yank", "publish")
	}
	return false
}

func gitCommandHighRisk(args []string) bool {
	if containsAny(args, "push", "clean", "prune", "filter-branch", "filter-repo") {
		return true
	}
	if containsAny(args, "gc") {
		return true
	}
	if containsAny(args, "reset") && containsAny(args, "--hard", "--merge", "--keep") {
		return true
	}
	if containsAny(args, "checkout") {
		// `git checkout .` and `git checkout path` discard worktree contents even
		// without -f/--. Prefer the unambiguous switch command for safe branch
		// changes; keep all checkout forms behind confirmation.
		return true
	}
	if containsAny(args, "switch") && containsAny(args, "--discard-changes") {
		return true
	}
	if containsAny(args, "restore") && (!containsAny(args, "--staged") || containsAny(args, "--worktree")) {
		// Restoring only the index is reversible from the worktree; restoring the
		// worktree can discard the user's uncommitted contents.
		return true
	}
	if containsAny(args, "branch") && containsAny(args, "-d", "--delete", "-f", "--force") {
		return true
	}
	if containsAny(args, "tag") && containsAny(args, "-d", "--delete", "-f", "--force") {
		return true
	}
	if containsAny(args, "stash") && containsAny(args, "clear", "drop") {
		return true
	}
	if containsAny(args, "reflog") && containsAny(args, "expire", "delete") {
		return true
	}
	if containsAny(args, "worktree") && containsAny(args, "remove", "prune") {
		return true
	}
	if containsAny(args, "update-ref") && containsAny(args, "-d", "--delete", "--stdin") {
		return true
	}
	if containsAny(args, "remote") && containsAny(args, "add", "remove", "rm", "rename", "set-url", "set-head", "set-branches", "prune", "update") {
		return true
	}
	// Repository-local git config is not version-controlled workspace config and
	// can redirect hooks, credentials, or future pushes. Read-only config probes
	// are the only fast path.
	if containsAny(args, "config") {
		if containsAny(args, "--unset", "--unset-all", "--add", "--replace-all", "--rename-section", "--remove-section", "--edit", "-e") {
			return true
		}
		return !containsAny(args, "--get", "--get-all", "--get-regexp", "--get-urlmatch", "--list", "-l", "--name-only")
	}
	return false
}

func curlCommandHighRisk(args []string) bool {
	method := ""
	for i, arg := range args {
		lower := strings.ToLower(arg)
		switch {
		case arg == "-X" || lower == "--request":
			if i+1 >= len(args) {
				return true
			}
			method = strings.ToUpper(args[i+1])
		case strings.HasPrefix(arg, "-X") && len(arg) > 2:
			method = strings.ToUpper(arg[2:])
		case strings.HasPrefix(lower, "--request="):
			method = strings.ToUpper(arg[len("--request="):])
		case arg == "-d" || lower == "--data" || lower == "--data-ascii" || lower == "--data-binary" ||
			lower == "--data-raw" || lower == "--data-urlencode" || lower == "--json" ||
			arg == "-F" || lower == "--form" || lower == "--form-string" ||
			arg == "-T" || lower == "--upload-file":
			return true
		case strings.HasPrefix(arg, "-d") && len(arg) > 2,
			strings.HasPrefix(arg, "-F") && len(arg) > 2,
			strings.HasPrefix(arg, "-T") && len(arg) > 2,
			strings.HasPrefix(lower, "--data="), strings.HasPrefix(lower, "--data-ascii="),
			strings.HasPrefix(lower, "--data-binary="), strings.HasPrefix(lower, "--data-raw="),
			strings.HasPrefix(lower, "--data-urlencode="), strings.HasPrefix(lower, "--json="),
			strings.HasPrefix(lower, "--form="), strings.HasPrefix(lower, "--form-string="),
			strings.HasPrefix(lower, "--upload-file="):
			return true
		}
	}
	return method != "" && method != "GET" && method != "HEAD" && method != "OPTIONS"
}

func wgetCommandHighRisk(args []string) bool {
	for i, arg := range args {
		switch {
		case arg == "--post-data" || arg == "--post-file" || strings.HasPrefix(arg, "--post-data=") || strings.HasPrefix(arg, "--post-file="):
			return true
		case arg == "--method":
			if i+1 >= len(args) {
				return true
			}
			method := strings.ToUpper(args[i+1])
			return method != "GET" && method != "HEAD" && method != "OPTIONS"
		case strings.HasPrefix(arg, "--method="):
			method := strings.ToUpper(strings.TrimPrefix(arg, "--method="))
			return method != "GET" && method != "HEAD" && method != "OPTIONS"
		}
	}
	return false
}

func ghCommandHighRisk(args []string) bool {
	group, rest := ghCommandGroup(args)
	switch group {
	case "api":
		return ghAPICommandHighRisk(rest)
	case "pr":
		return containsAny(rest, "create", "close", "comment", "edit", "merge", "ready", "reopen", "review")
	case "issue":
		return containsAny(rest, "create", "close", "comment", "delete", "edit", "reopen", "transfer", "pin", "unpin", "lock", "unlock")
	case "repo":
		return containsAny(rest, "create", "delete", "archive", "edit", "fork", "rename", "sync")
	case "release":
		return containsAny(rest, "create", "delete", "edit", "upload")
	case "workflow":
		return containsAny(rest, "run", "enable", "disable")
	case "run":
		return containsAny(rest, "cancel", "delete", "rerun")
	case "secret", "variable":
		return containsAny(rest, "set", "delete")
	case "label":
		return containsAny(rest, "create", "delete", "edit", "clone")
	case "gist":
		return containsAny(rest, "create", "delete", "edit")
	case "ssh-key", "gpg-key":
		return containsAny(rest, "add", "delete")
	case "cache":
		return containsAny(rest, "delete")
	case "auth":
		return containsAny(rest, "login", "logout", "refresh", "setup-git", "switch")
	case "alias":
		return containsAny(rest, "set", "delete")
	case "config":
		return containsAny(rest, "set", "clear")
	case "extension":
		return containsAny(rest, "install", "remove", "upgrade", "create")
	case "project", "codespace":
		return !containsAny(rest, "list", "view", "status", "logs")
	}
	return false
}

func ghCommandGroup(args []string) (string, []string) {
	groups := map[string]struct{}{
		"api": {}, "pr": {}, "issue": {}, "repo": {}, "release": {}, "workflow": {}, "run": {},
		"secret": {}, "variable": {}, "label": {}, "gist": {}, "ssh-key": {}, "gpg-key": {},
		"cache": {}, "auth": {}, "alias": {}, "config": {}, "extension": {}, "project": {}, "codespace": {},
	}
	for i, arg := range args {
		if _, ok := groups[arg]; ok {
			return arg, args[i+1:]
		}
	}
	return "", nil
}

func ghAPICommandHighRisk(args []string) bool {
	method := ""
	hasBody := false
	for i, arg := range args {
		switch {
		case arg == "-x" || arg == "--method":
			if i+1 >= len(args) {
				return true
			}
			method = strings.ToUpper(args[i+1])
		case strings.HasPrefix(arg, "-x") && len(arg) > 2:
			method = strings.ToUpper(arg[2:])
		case strings.HasPrefix(arg, "--method="):
			method = strings.ToUpper(strings.TrimPrefix(arg, "--method="))
		case arg == "-f" || arg == "--raw-field" || arg == "--field" || arg == "--input":
			hasBody = true
		case strings.HasPrefix(arg, "-f") && len(arg) > 2:
			hasBody = true
		case strings.HasPrefix(arg, "--raw-field=") || strings.HasPrefix(arg, "--field=") || strings.HasPrefix(arg, "--input="):
			hasBody = true
		}
	}
	if method == "" {
		return hasBody // gh api switches its default from GET to POST when fields/input are supplied.
	}
	return method != "GET" && method != "HEAD" && method != "OPTIONS"
}

func commandFieldsKnownSafeMutation(fields []string) bool {
	if len(fields) == 0 || commandFieldsHighRisk(fields) {
		return false
	}
	base := strings.ToLower(filepath.Base(fields[0]))
	rawArgs := fields[1:]
	args := lowerFields(rawArgs)
	switch base {
	case "env":
		wrapped, ok := unwrapEnvCommand(rawArgs)
		return ok && commandFieldsKnownSafeMutation(wrapped)
	case "command":
		wrapped, ok := unwrapCommandBuiltin(rawArgs)
		return ok && (len(wrapped) == 0 || commandFieldsKnownSafeMutation(wrapped))
	case "nohup":
		wrapped := trimLeadingOptions(rawArgs)
		return len(wrapped) > 0 && commandFieldsKnownSafeMutation(wrapped)
	case "git":
		return gitCommandKnownSafe(args)
	case "curl":
		return !curlCommandHighRisk(rawArgs)
	case "wget":
		return !wgetCommandHighRisk(args)
	case "gh":
		return !ghCommandHighRisk(args)
	case "http", "https", "xh":
		return !httpCommandHighRisk(args)
	case "sed", "gofmt", "goimports", "rustfmt", "prettier", "biome", "eslint", "black", "ruff",
		"cp", "mv", "mkdir", "touch", "ln":
		// These are deterministic workspace-editing families. The ordinary
		// permission/sandbox layer still owns path confinement.
		return true
	case "npm":
		return containsAny(args, "install", "add", "remove", "uninstall", "update", "dedupe") && !hasGlobalFlag(args)
	case "pnpm":
		return containsAny(args, "install", "add", "remove", "update", "dedupe", "import") && !hasGlobalFlag(args)
	case "yarn":
		return containsAny(args, "install", "add", "remove", "up", "upgrade", "dedupe") && !hasGlobalFlag(args) && !containsAny(args, "global")
	case "go":
		return containsAny(args, "get", "mod", "work", "fmt", "build", "test") && !containsAny(args, "install", "clean")
	case "cargo":
		return containsAny(args, "add", "remove", "update", "build", "check", "test", "fmt", "fix", "clippy")
	case "composer":
		return containsAny(args, "require", "remove", "update", "install", "dump-autoload") && !hasGlobalFlag(args) && !containsAny(args, "global")
	case "poetry":
		return containsAny(args, "add", "remove", "install", "update", "lock", "sync")
	case "uv":
		return containsAny(args, "add", "remove", "sync", "lock")
	case "dotnet":
		return containsAny(args, "add", "remove", "restore", "build", "test", "format") && !hasGlobalFlag(args)
	}
	// A coarse host mutation bit must not turn a statically proven read-only
	// diagnostic into a confirmation. Destructive argument forms were rejected
	// before reaching this point.
	if _, _, readOnly := shellsafe.CommandIsReadOnly(strings.Join(fields, " ")); readOnly {
		return true
	}
	return false
}

func gitCommandKnownSafe(args []string) bool {
	sub := gitSubcommand(args)
	switch sub {
	case "add", "commit", "status", "diff", "log", "show", "rev-parse", "rev-list", "describe",
		"blame", "grep", "ls-files", "ls-tree", "cat-file", "for-each-ref", "name-rev", "shortlog",
		"whatchanged", "cherry", "fetch", "pull", "clone", "init", "merge", "rebase", "cherry-pick",
		"revert", "apply", "am", "switch", "reset", "branch", "tag", "stash", "restore", "worktree",
		"remote", "config", "reflog":
		return true
	default:
		return false
	}
}

func gitSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-c" || arg == "--git-dir" || arg == "--work-tree" || arg == "--namespace":
			i++
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			return strings.ToLower(arg)
		}
	}
	return ""
}

type taskGrantBoundary struct {
	key     string
	display string
}

func commandFieldsTaskGrantBoundary(fields []string) taskGrantBoundary {
	if len(fields) == 0 {
		return taskGrantBoundary{}
	}
	base := strings.ToLower(filepath.Base(fields[0]))
	rawArgs := fields[1:]
	switch base {
	case "env":
		wrapped, ok := unwrapEnvCommand(rawArgs)
		if ok {
			return commandFieldsTaskGrantBoundary(wrapped)
		}
	case "command":
		wrapped, ok := unwrapCommandBuiltin(rawArgs)
		if ok {
			return commandFieldsTaskGrantBoundary(wrapped)
		}
	case "git":
		return gitPushTaskGrantBoundary(rawArgs)
	case "gh":
		return ghTaskGrantBoundary(rawArgs)
	}
	return taskGrantBoundary{}
}

func gitPushTaskGrantBoundary(args []string) taskGrantBoundary {
	lower := lowerFields(args)
	if gitSubcommand(lower) != "push" || containsAny(lower,
		"-f", "--force", "--mirror", "--delete", "--prune", "--all", "--tags", "--follow-tags",
	) {
		return taskGrantBoundary{}
	}
	for _, arg := range lower {
		if strings.HasPrefix(arg, "--force") || strings.HasPrefix(arg, ":") || strings.HasPrefix(arg, "+") {
			return taskGrantBoundary{}
		}
	}
	pushAt := -1
	for i, arg := range lower {
		if arg == "push" {
			pushAt = i
			break
		}
	}
	if pushAt != 0 {
		// Global options such as -C/--git-dir can redirect an otherwise identical
		// command to another repository. Keep those forms one-shot because the
		// displayed remote alias would no longer identify the same target context.
		return taskGrantBoundary{}
	}
	var positionals []string
	for i := pushAt + 1; i < len(args); i++ {
		arg := lower[i]
		switch arg {
		case "-u", "--set-upstream", "-q", "--quiet", "-v", "--verbose", "--progress", "--no-progress":
			continue
		}
		if strings.HasPrefix(arg, "-") {
			// Behavior-changing and unknown push options are deliberately one-shot.
			// In particular, push-option/receive-pack/no-verify must not inherit a
			// grant issued for an ordinary push to the same ref.
			return taskGrantBoundary{}
		}
		positionals = append(positionals, strings.TrimSpace(args[i]))
	}
	// A reusable grant needs both an explicit remote and exactly one explicit
	// refspec. Bare `git push` depends on mutable branch/upstream configuration.
	if len(positionals) != 2 {
		return taskGrantBoundary{}
	}
	remote, refspec := positionals[0], positionals[1]
	if remote == "" || refspec == "" || strings.Contains(refspec, "*") {
		return taskGrantBoundary{}
	}
	target := refspec
	if before, after, ok := strings.Cut(refspec, ":"); ok {
		if strings.TrimSpace(before) == "" || strings.TrimSpace(after) == "" {
			return taskGrantBoundary{}
		}
		target = strings.TrimSpace(after)
	}
	if target == "HEAD" || target == "@" {
		return taskGrantBoundary{}
	}
	return taskGrantBoundary{
		key:     "bash:git.push:" + CallFingerprint("git.push", remote, target, nil),
		display: "git push " + remote + " → " + target,
	}
}

func ghTaskGrantBoundary(args []string) taskGrantBoundary {
	lower := lowerFields(args)
	group, rest := ghCommandGroup(lower)
	if len(rest) == 0 {
		return taskGrantBoundary{}
	}
	verb := rest[0]
	if (group != "pr" && group != "issue") || verb != "comment" {
		return taskGrantBoundary{}
	}
	if containsAny(lower, "--edit-last", "--delete-last") {
		return taskGrantBoundary{}
	}
	repo := "current"
	for i, arg := range lower {
		switch {
		case (arg == "--repo" || arg == "-r") && i+1 < len(args):
			repo = args[i+1]
		case strings.HasPrefix(arg, "--repo="):
			repo = strings.TrimSpace(args[i][len("--repo="):])
		case strings.HasPrefix(arg, "-r") && len(arg) > 2:
			repo = strings.TrimSpace(args[i][2:])
		}
	}
	target := "current"
	if len(rest) > 1 && !strings.HasPrefix(rest[1], "-") {
		target = rest[1]
	} else if len(rest) > 1 {
		// Options before a positional target are legal in gh. Avoid guessing
		// through their values; a form the host cannot scope exactly stays
		// one-shot rather than sharing an accidentally broad "current" grant.
		return taskGrantBoundary{}
	}
	// "current" can change after a checkout or branch switch. Require an
	// explicit PR/issue target before offering a reusable external-write grant.
	if target == "current" {
		return taskGrantBoundary{}
	}
	repo = strings.TrimSpace(repo)
	target = strings.TrimSpace(target)
	display := "gh " + group + " comment " + target
	if repo != "current" {
		display += " --repo " + repo
	}
	return taskGrantBoundary{
		key:     "bash:gh." + group + ".comment:" + CallFingerprint("gh."+group+".comment", repo, target, nil),
		display: display,
	}
}

func httpCommandHighRisk(args []string) bool {
	for _, arg := range args {
		upper := strings.ToUpper(arg)
		switch upper {
		case "POST", "PUT", "PATCH", "DELETE", "CONNECT", "PURGE", "LOCK", "UNLOCK":
			return true
		}
		lower := strings.ToLower(arg)
		if lower == "--raw" || lower == "--form" || strings.HasPrefix(lower, "--raw=") {
			return true
		}
		if strings.HasPrefix(arg, "-") || strings.Contains(arg, "://") {
			continue
		}
		if strings.Contains(arg, "==") && !strings.Contains(arg, ":=") && !strings.Contains(arg, "@") {
			continue // HTTPie query-string item; remains a GET by default.
		}
		// HTTPie-style request items with a value or file body implicitly switch
		// the default method from GET to a mutating request.
		if strings.Contains(arg, "=") || strings.Contains(arg, "@") {
			return true
		}
	}
	return false
}

func hasGlobalFlag(fields []string) bool {
	return containsAny(fields, "-g", "--global", "--system", "--user")
}

func unwrapEnvCommand(args []string) ([]string, bool) {
	for len(args) > 0 {
		arg := args[0]
		lower := strings.ToLower(arg)
		switch {
		case lower == "-i" || lower == "--ignore-environment" || lower == "-0" || lower == "--null":
			args = args[1:]
		case lower == "-u" || lower == "--unset" || lower == "-c" || lower == "--chdir":
			if len(args) < 2 {
				return nil, false
			}
			args = args[2:]
		case strings.HasPrefix(lower, "--unset=") || strings.HasPrefix(lower, "--chdir="):
			args = args[1:]
		case strings.HasPrefix(arg, "-"):
			// Split-string and unknown options can change the command shape.
			return nil, false
		case strings.Contains(arg, "="):
			args = args[1:]
		default:
			return args, true
		}
	}
	return nil, false
}

func unwrapCommandBuiltin(args []string) ([]string, bool) {
	for len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "-p":
			args = args[1:]
		case "-v":
			// Inspection-only command lookup; there is no wrapped execution.
			return nil, true
		default:
			if strings.HasPrefix(args[0], "-") {
				return nil, false
			}
			return args, true
		}
	}
	return nil, false
}

func trimLeadingOptions(args []string) []string {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		args = args[1:]
	}
	return args
}

func lowerFields(fields []string) []string {
	out := make([]string, len(fields))
	for i, field := range fields {
		out[i] = strings.ToLower(strings.TrimSpace(field))
	}
	return out
}

func containsAny(fields []string, values ...string) bool {
	wanted := make(map[string]struct{}, len(values))
	for _, value := range values {
		wanted[value] = struct{}{}
	}
	for _, field := range fields {
		if _, ok := wanted[field]; ok {
			return true
		}
	}
	return false
}

// WriteScopePaths extracts path-like targets from mutation args for scope compare.
func WriteScopePaths(tool string, args json.RawMessage) []string {
	tool = strings.TrimSpace(tool)
	paths := pathsFromArgs(args)
	for i := range paths {
		paths[i] = filepath.Clean(paths[i])
	}
	if tool == "multi_edit" || tool == "multi-edit" {
		var payload struct {
			Edits []struct {
				Path string `json:"path"`
			} `json:"edits"`
		}
		if err := json.Unmarshal(args, &payload); err == nil {
			for _, e := range payload.Edits {
				if strings.TrimSpace(e.Path) != "" {
					paths = append(paths, filepath.Clean(e.Path))
				}
			}
		}
	}
	if tool == "bash" {
		// Best-effort: do not invent paths from free-form shell.
		return paths
	}
	return uniqueStrings(paths)
}

// ScopeExpanded reports whether the proposal writes outside the failure's
// recorded path set (when both sides have path info).
func ScopeExpanded(failure *FailureEvent, proposal Proposal) bool {
	if proposal.ExpandedScope {
		return true
	}
	if failure == nil {
		return false
	}
	failedPaths := WriteScopePaths(failure.Tool, failure.Args)
	nextPaths := WriteScopePaths(proposal.Tool, proposal.Args)
	if len(failedPaths) == 0 || len(nextPaths) == 0 {
		return false
	}
	allowed := map[string]struct{}{}
	for _, p := range failedPaths {
		allowed[filepath.Clean(p)] = struct{}{}
		// Allow writes under the same directory as a failed file target.
		allowed[filepath.Clean(filepath.Dir(p))] = struct{}{}
	}
	for _, p := range nextPaths {
		p = filepath.Clean(p)
		if _, ok := allowed[p]; ok {
			continue
		}
		parent := filepath.Clean(filepath.Dir(p))
		if _, ok := allowed[parent]; ok {
			continue
		}
		// Outside all known failed paths.
		return true
	}
	return false
}

// StrategyChanged reports an explicit semantic method change. A tool-name
// transition is not enough: the normal recovery flow after a failing verifier
// is to inspect the evidence and edit the diagnosed code. Risk and scope have
// deterministic classifiers; ambiguous method changes are left to the reviewer.
func StrategyChanged(failure *FailureEvent, proposal Proposal) bool {
	_ = failure
	return proposal.StrategyChanged
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
