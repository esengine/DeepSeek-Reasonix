package shellsafe

import (
	"path/filepath"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"

	"reasonix/internal/shellparse"
)

// Certainty reports whether the host could statically prove a command's
// effects. Unknown commands fail closed at mutation and permission boundaries.
type Certainty uint8

const (
	EffectUnknown Certainty = iota
	EffectKnown
)

// WriteDomain identifies durable state a command can change.
type WriteDomain uint8

const (
	WriteWorkspaceContent WriteDomain = 1 << iota
	WriteRepositoryMetadata
	WriteHostState
	WriteExternalState
)

// CommandEffect is the shared, host-derived effect classification for one Bash
// invocation. CommandFamily and Reason are deliberately argument-free so they
// are safe to surface in policy diagnostics.
type CommandEffect struct {
	Certainty      Certainty
	Writes         WriteDomain
	PermissionSafe bool
	ExecutesCode   bool
	UsesNetwork    bool
	CommandFamily  string
	Reason         string
}

func (e CommandEffect) AnyMutation() bool { return e.Certainty != EffectKnown || e.Writes != 0 }
func (e CommandEffect) WorkspaceMutation() bool {
	return e.Certainty != EffectKnown || e.Writes&(WriteWorkspaceContent|WriteRepositoryMetadata) != 0
}
func (e CommandEffect) ContentMutation() bool {
	return e.Certainty != EffectKnown || e.Writes&WriteWorkspaceContent != 0
}
func (e CommandEffect) RepositoryMutation() bool { return e.Writes&WriteRepositoryMetadata != 0 }
func (e CommandEffect) IsPermissionReader() bool {
	return e.Certainty == EffectKnown && e.Writes == 0 && e.PermissionSafe
}

// ClassifyBash statically classifies every top-level command segment and
// combines their effects. It never executes expansions or helper programs.
func ClassifyBash(command string) CommandEffect {
	command = strings.TrimSpace(command)
	if command == "" {
		return unknownEffect("shell", "empty command")
	}
	if bashHasUnsafeLifecycle(command) {
		return unknownEffect(commandFamilyFromSource(command), "background or process lifecycle syntax")
	}
	segments, _, ok := shellparse.SplitTopLevel(command)
	if !ok || len(segments) == 0 {
		return unknownEffect(commandFamilyFromSource(command), "unsupported shell syntax")
	}

	var aggregate CommandEffect
	aggregate.Certainty = EffectKnown
	aggregate.PermissionSafe = true
	for _, segment := range segments {
		effect := classifyBashSegment(segment)
		aggregate.Writes |= effect.Writes
		aggregate.ExecutesCode = aggregate.ExecutesCode || effect.ExecutesCode
		aggregate.UsesNetwork = aggregate.UsesNetwork || effect.UsesNetwork
		aggregate.PermissionSafe = aggregate.PermissionSafe && effect.IsPermissionReader()
		if aggregate.CommandFamily == "" || effect.AnyMutation() || !effect.PermissionSafe {
			aggregate.CommandFamily = effect.CommandFamily
		}
		if aggregate.Reason == "" && effect.Reason != "" {
			aggregate.Reason = effect.Reason
		}
		if effect.Certainty != EffectKnown {
			aggregate.Certainty = EffectUnknown
		}
	}
	if aggregate.CommandFamily == "" {
		aggregate.CommandFamily = "shell"
	}
	return aggregate
}

func classifyBashSegment(segment string) CommandEffect {
	if normalized, ok := NormalizeBashSafeRedirectsForMatch(segment); ok {
		if fields, envPrefixed, ok := staticArgv(normalized); ok {
			return applyEnvPrefix(classifyStaticFields(fields), envPrefixed)
		}
		// Preserve the existing narrow, recursively proven command-substitution
		// reader path. Its returned fields contain only opaque placeholders.
		if base, sub, fields, ok := ClassifyReadOnlyCommand(normalized); ok {
			effect := classifyStaticFields(fields)
			if effect.Certainty == EffectKnown && effect.Writes == 0 {
				effect.CommandFamily = joinFamily(base, sub)
				return effect
			}
		}
		return unknownEffect(commandFamilyFromSource(normalized), "dynamic command shape")
	}

	fields, writesFile, ok := staticFieldsWithOutputRedirect(segment)
	if !ok {
		return unknownEffect(commandFamilyFromSource(segment), "unsafe or unsupported redirection")
	}
	effect := classifyStaticFields(fields)
	if writesFile {
		effect.Writes |= WriteWorkspaceContent
		effect.PermissionSafe = false
		if effect.Reason == "" {
			effect.Reason = "writes command output"
		}
	}
	return effect
}

// staticArgv reduces one statically proven command to argv after stripping a
// leading env wrapper. Env flags and expansions fail closed.
func staticArgv(command string) ([]string, bool, bool) {
	cmd, err := shellparse.ParseStaticCommand(command, shellparse.StaticCommandPolicy{
		AllowEnvAssignments: true,
		AllowStderrToStdout: true,
	})
	if err != nil || len(cmd.Argv) == 0 {
		return nil, false, false
	}
	fields, envPrefixed, ok := unwrapEnvArgv(cmd.Argv)
	if !ok {
		return nil, envPrefixed, false
	}
	return fields, envPrefixed || len(cmd.Env) > 0, true
}

func unwrapEnvArgv(argv []string) ([]string, bool, bool) {
	if len(argv) == 0 {
		return nil, false, false
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(argv[0]), ".exe"))
	if base != "env" {
		return argv, false, true
	}
	rest := argv[1:]
	for len(rest) > 0 {
		tok := rest[0]
		if strings.HasPrefix(tok, "-") {
			return nil, true, false
		}
		if !strings.Contains(tok, "=") {
			break
		}
		rest = rest[1:]
	}
	if len(rest) == 0 {
		return nil, true, false
	}
	return rest, true, true
}

func applyEnvPrefix(effect CommandEffect, envPrefixed bool) CommandEffect {
	if !envPrefixed {
		return effect
	}
	effect.PermissionSafe = false
	if effect.Reason == "" {
		effect.Reason = "environment prefix requires permission"
	}
	return effect
}

// CommandArgv returns statically proven argv after stripping a leading env
// prefix. ok is false for expansions, env flags, or unsupported syntax.
func CommandArgv(command string) (argv []string, envPrefixed bool, ok bool) {
	return staticArgv(strings.TrimSpace(command))
}

func classifyStaticFields(fields []string) CommandEffect {
	if len(fields) == 0 {
		return unknownEffect("shell", "empty command")
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(fields[0]), ".exe"))
	args := fields[1:]
	switch base {
	case "git":
		return classifyGit(args)
	case "date":
		return classifyDate(args)
	case "go":
		return classifyGo(args)
	case "npm":
		return classifyNPM(args)
	case "cargo":
		return classifyCargo(args)
	case "test-netconnection":
		return CommandEffect{Certainty: EffectKnown, UsesNetwork: true, CommandFamily: base, Reason: "network probe requires permission"}
	}

	if readOnlyCommands[base] {
		effect := knownReader(base)
		switch base {
		case "find":
			if hasEffectArg(args, "-exec", "-execdir", "-ok", "-okdir") {
				return unknownCodeEffect(base, "executes a nested command")
			}
			if hasEffectArg(args, "-delete", "-fls", "-fprint", "-fprint0", "-fprintf") {
				return knownWriter(base, WriteWorkspaceContent, "writes filesystem state")
			}
		case "sort":
			if hasOutputArg(args) {
				return knownWriter(base, WriteWorkspaceContent, "writes command output")
			}
		}
		return effect
	}
	if len(args) > 0 && readOnlyPrefixes[base][strings.ToLower(args[0])] {
		effect := knownReader(joinFamily(base, strings.ToLower(args[0])))
		if base == "docker" || base == "kubectl" {
			effect.UsesNetwork = true
		}
		return effect
	}
	return unknownEffect(base, "command effects are not statically known")
}

func classifyGit(args []string) CommandEffect {
	args = skipGitGlobalArgs(args)
	if len(args) == 0 {
		return unknownEffect("git", "missing git subcommand")
	}
	sub := strings.ToLower(args[0])
	rest := args[1:]
	switch sub {
	case "branch":
		return classifyGitBranch(rest)
	case "tag":
		return classifyGitTag(rest)
	case "remote":
		return classifyGitRemote(rest)
	case "config":
		return classifyGitConfig(rest)
	case "worktree":
		return classifyGitWorktree(rest)
	case "stash":
		return classifyGitStash(rest)
	case "clean":
		return classifyGitClean(rest)
	case "submodule":
		return classifyGitSubmodule(rest)
	case "commit":
		return classifyGitCommit(rest)
	case "push":
		effect := knownWriter("git push", WriteExternalState, "updates remote refs")
		effect.UsesNetwork = true
		return effect
	case "fetch":
		effect := knownWriter("git fetch", WriteRepositoryMetadata, "updates remote-tracking refs")
		effect.UsesNetwork = true
		return effect
	case "pull":
		effect := knownWriter("git pull", WriteWorkspaceContent|WriteRepositoryMetadata, "updates workspace content and refs")
		effect.UsesNetwork = true
		return effect
	case "reflog":
		if len(rest) > 0 && slices.Contains([]string{"expire", "delete", "drop"}, strings.ToLower(rest[0])) {
			return knownWriter("git reflog", WriteRepositoryMetadata, "updates reflog metadata")
		}
		return knownReader("git reflog")
	case "diff", "show", "log":
		if hasOutputArg(rest) {
			return knownWriter("git "+sub, WriteWorkspaceContent, "writes command output")
		}
		if hasEffectArg(rest, "--ext-diff", "--textconv") {
			return unknownCodeEffect("git "+sub, "may execute an external diff helper")
		}
		return knownReader("git " + sub)
	case "grep":
		if hasArgPrefix(rest, "--open-files-in-pager") {
			return unknownCodeEffect("git grep", "may execute an external pager")
		}
		return knownReader("git grep")
	case "cat-file":
		if hasEffectArg(rest, "--filters", "--textconv") {
			return unknownCodeEffect("git cat-file", "may execute an external content filter")
		}
		return knownReader("git cat-file")
	}
	if readOnlyPrefixes["git"][sub] {
		return knownReader("git " + sub)
	}
	return unknownEffect("git "+sub, "git subcommand effects are not statically known")
}

func skipGitGlobalArgs(args []string) []string {
	for len(args) >= 2 && (args[0] == "-C" || args[0] == "--git-dir" || args[0] == "--work-tree") {
		args = args[2:]
	}
	return args
}

func classifyGitBranch(args []string) CommandEffect {
	family := "git branch"
	if slices.ContainsFunc(args, branchMutationArg) {
		return knownWriter(family, WriteRepositoryMetadata, "updates branch refs or configuration")
	}
	listing := len(args) == 0
	operands := 0
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-l" || arg == "--list" || arg == "--all" || arg == "-a" || arg == "--remotes" || arg == "-r" || arg == "--show-current":
			listing = true
		case arg == "-v" || arg == "-vv" || arg == "--verbose" || arg == "-q" || arg == "--quiet" || arg == "-i" || arg == "--ignore-case" || arg == "--omit-empty" || arg == "--no-omit-empty":
			listing = true
		case branchValueOption(arg):
			listing = true
			if !strings.Contains(arg, "=") {
				i++
				if i >= len(args) {
					return unknownEffect(family, "missing branch filter value")
				}
			}
		case strings.HasPrefix(arg, "-"):
			return unknownEffect(family, "unknown branch option")
		default:
			operands++
		}
	}
	if operands > 0 && !hasEffectArg(args, "-l", "--list") {
		return knownWriter(family, WriteRepositoryMetadata, "creates or resets a branch ref")
	}
	if listing {
		return knownReader(family)
	}
	return unknownEffect(family, "ambiguous branch operation")
}

func classifyGitTag(args []string) CommandEffect {
	family := "git tag"
	listing := len(args) == 0
	verify := false
	operands := 0
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if tagMutationArg(arg) {
			return knownWriter(family, WriteRepositoryMetadata, "updates tag refs")
		}
		switch {
		case arg == "-l" || arg == "--list":
			listing = true
		case arg == "-v" || arg == "--verify":
			verify = true
		case arg == "-i" || arg == "--ignore-case" || strings.HasPrefix(arg, "-n") || tagFlagOption(arg):
			listing = true
			if tagOptionConsumesNext(arg) {
				i++
				if i >= len(args) {
					return unknownEffect(family, "missing tag filter value")
				}
			}
		case strings.HasPrefix(arg, "-"):
			return unknownEffect(family, "unknown tag option")
		default:
			operands++
		}
	}
	if verify {
		if operands == 0 {
			return unknownEffect(family, "missing tag verification operand")
		}
		effect := CommandEffect{Certainty: EffectKnown, ExecutesCode: true, CommandFamily: family, Reason: "tag verification may execute a signature helper"}
		return effect
	}
	if operands > 0 && !hasEffectArg(args, "-l", "--list") {
		return knownWriter(family, WriteRepositoryMetadata, "creates a tag ref")
	}
	if listing {
		return knownReader(family)
	}
	return unknownEffect(family, "ambiguous tag operation")
}

func classifyGitRemote(args []string) CommandEffect {
	family := "git remote"
	if len(args) == 0 || allEffectArgs(args, "-v", "--verbose") {
		return knownReader(family)
	}
	action := strings.ToLower(args[0])
	if action == "get-url" {
		return knownReader(family)
	}
	if slices.Contains([]string{"add", "remove", "rm", "rename", "set-url", "prune", "update"}, action) {
		return knownWriter(family, WriteRepositoryMetadata, "updates remote configuration or refs")
	}
	return unknownEffect(family, "remote operation is not a proven local reader")
}

func classifyGitConfig(args []string) CommandEffect {
	family := "git config"
	hostScope := hasEffectArg(args, "--global", "--system")
	for _, arg := range args {
		name := strings.ToLower(strings.SplitN(arg, "=", 2)[0])
		if slices.Contains([]string{"--edit", "-e"}, name) {
			domain := WriteRepositoryMetadata
			if hostScope {
				domain = WriteHostState
			}
			return knownWriter(family, domain, "opens an editor that can write git configuration")
		}
		if slices.Contains([]string{"set", "unset", "unset-all", "add", "replace-all", "rename-section", "remove-section", "--unset", "--unset-all", "--add", "--replace-all", "--rename-section", "--remove-section"}, name) {
			domain := WriteRepositoryMetadata
			if hostScope {
				domain = WriteHostState
			}
			return knownWriter(family, domain, "updates git configuration")
		}
	}
	positional := 0
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if configOptionConsumesNext(arg) {
			i++
			continue
		}
		if strings.HasPrefix(arg, "-") || configReadAction(arg) {
			continue
		}
		positional++
	}
	if positional <= 1 || containsConfigReadAction(args) {
		return knownReader(family)
	}
	domain := WriteRepositoryMetadata
	if hostScope {
		domain = WriteHostState
	}
	return knownWriter(family, domain, "updates git configuration")
}

func classifyGitWorktree(args []string) CommandEffect {
	family := "git worktree"
	if len(args) > 0 && strings.EqualFold(args[0], "list") {
		return knownReader(family)
	}
	if len(args) > 0 && slices.Contains([]string{"add", "remove", "move", "prune"}, strings.ToLower(args[0])) {
		return knownWriter(family, WriteWorkspaceContent|WriteRepositoryMetadata, "updates worktrees")
	}
	if len(args) > 0 && slices.Contains([]string{"lock", "unlock", "repair"}, strings.ToLower(args[0])) {
		return knownWriter(family, WriteRepositoryMetadata, "updates worktree metadata")
	}
	return unknownEffect(family, "worktree operation is not a proven reader")
}

func classifyGitStash(args []string) CommandEffect {
	family := "git stash"
	if len(args) > 0 && slices.Contains([]string{"list", "show"}, strings.ToLower(args[0])) {
		return knownReader(family)
	}
	if len(args) > 0 && slices.Contains([]string{"push", "save", "pop", "apply", "drop", "clear", "branch", "store", "create"}, strings.ToLower(args[0])) {
		return knownWriter(family, WriteWorkspaceContent|WriteRepositoryMetadata, "updates stash or working tree state")
	}
	return unknownEffect(family, "stash operation is not a proven reader")
}

func classifyGitClean(args []string) CommandEffect {
	family := "git clean"
	dryRun := false
	force := false
	for _, arg := range args {
		dryRun = dryRun || arg == "--dry-run" || shortOptionContains(arg, 'n')
		force = force || arg == "--force" || shortOptionContains(arg, 'f')
	}
	if dryRun && !force {
		return knownReader(family)
	}
	return knownWriter(family, WriteWorkspaceContent, "removes untracked workspace content")
}

func classifyGitSubmodule(args []string) CommandEffect {
	family := "git submodule"
	if len(args) > 0 && slices.Contains([]string{"status", "summary"}, strings.ToLower(args[0])) {
		return knownReader(family)
	}
	if len(args) > 0 && slices.Contains([]string{"add", "init", "update", "deinit", "sync", "set-url", "set-branch", "foreach", "absorbgitdirs"}, strings.ToLower(args[0])) {
		return knownWriter(family, WriteWorkspaceContent|WriteRepositoryMetadata, "updates submodule state")
	}
	return unknownEffect(family, "submodule operation is not a proven reader")
}

func classifyGitCommit(args []string) CommandEffect {
	family := "git commit"
	pure := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-q" || arg == "--quiet":
		case arg == "-m" || arg == "--message":
			if i+1 >= len(args) {
				return knownWriter(family, WriteWorkspaceContent|WriteRepositoryMetadata, "ambiguous commit form")
			}
			pure = true
			i++
		case strings.HasPrefix(arg, "--message="):
			pure = true
		default:
			return knownWriter(family, WriteWorkspaceContent|WriteRepositoryMetadata, "commit form may include workspace content")
		}
	}
	if pure {
		return knownWriter(family, WriteRepositoryMetadata, "creates a commit and moves refs")
	}
	return knownWriter(family, WriteWorkspaceContent|WriteRepositoryMetadata, "ambiguous commit form")
}

func classifyDate(args []string) CommandEffect {
	family := "date"
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-s" || arg == "--set" || strings.HasPrefix(arg, "--set="):
			return knownWriter(family, WriteHostState, "sets the host clock")
		case strings.HasPrefix(arg, "+"):
		case slices.Contains([]string{"-u", "--utc", "--universal", "-R", "--rfc-email", "--resolution", "--debug", "-j"}, arg):
		case strings.HasPrefix(arg, "--iso-8601") || strings.HasPrefix(arg, "--rfc-3339"):
		case slices.Contains([]string{"-d", "--date", "-r", "--reference", "-f", "--file"}, arg):
			i++
			if i >= len(args) {
				return unknownEffect(family, "missing date reader value")
			}
		case strings.HasPrefix(arg, "--date=") || strings.HasPrefix(arg, "--reference=") || strings.HasPrefix(arg, "--file="):
		case strings.HasPrefix(arg, "-"):
			return unknownEffect(family, "unknown date option")
		default:
			return knownWriter(family, WriteHostState, "may set the host clock")
		}
	}
	return knownReader(family)
}

func classifyGo(args []string) CommandEffect {
	if len(args) == 0 {
		return unknownEffect("go", "missing go subcommand")
	}
	sub := strings.ToLower(args[0])
	rest := args[1:]
	family := "go " + sub
	if sub == "env" && hasEffectArg(rest, "-w", "-u") {
		return knownWriter(family, WriteHostState, "updates Go environment configuration")
	}
	if sub == "list" {
		for i, arg := range rest {
			if arg == "-modfile" || strings.HasPrefix(arg, "-modfile=") || arg == "-mod=mod" || (arg == "-mod" && i+1 < len(rest) && rest[i+1] == "mod") {
				return knownWriter(family, WriteWorkspaceContent, "may update module files")
			}
		}
	}
	if readOnlyPrefixes["go"][sub] {
		return knownReader(family)
	}
	return unknownEffect(family, "go subcommand effects are not statically known")
}

func classifyNPM(args []string) CommandEffect {
	if len(args) == 0 {
		return unknownEffect("npm", "missing npm subcommand")
	}
	sub := strings.ToLower(args[0])
	family := "npm " + sub
	if sub == "audit" {
		effect := knownReader(family)
		effect.UsesNetwork = true
		if hasEffectArg(args[1:], "fix", "--fix") {
			effect.Writes = WriteWorkspaceContent
			effect.PermissionSafe = false
			effect.Reason = "updates dependency manifests or lockfiles"
		}
		return effect
	}
	if readOnlyPrefixes["npm"][sub] {
		effect := knownReader(family)
		effect.UsesNetwork = sub == "view" || sub == "info" || sub == "outdated"
		return effect
	}
	return unknownEffect(family, "npm subcommand effects are not statically known")
}

func classifyCargo(args []string) CommandEffect {
	if len(args) == 0 {
		return unknownEffect("cargo", "missing cargo subcommand")
	}
	sub := strings.ToLower(args[0])
	family := "cargo " + sub
	if sub == "check" || sub == "doc" {
		effect := knownWriter(family, WriteWorkspaceContent, "writes build artifacts and may run build scripts")
		effect.ExecutesCode = true
		return effect
	}
	if sub == "search" {
		effect := knownReader(family)
		effect.UsesNetwork = true
		return effect
	}
	return unknownEffect(family, "cargo subcommand effects are not statically known")
}

func knownReader(family string) CommandEffect {
	return CommandEffect{Certainty: EffectKnown, PermissionSafe: true, CommandFamily: family}
}
func knownWriter(family string, writes WriteDomain, reason string) CommandEffect {
	return CommandEffect{Certainty: EffectKnown, Writes: writes, CommandFamily: family, Reason: reason}
}
func unknownEffect(family, reason string) CommandEffect {
	return CommandEffect{Certainty: EffectUnknown, CommandFamily: strings.TrimSpace(family), Reason: reason}
}

func unknownCodeEffect(family, reason string) CommandEffect {
	effect := unknownEffect(family, reason)
	effect.ExecutesCode = true
	return effect
}

func joinFamily(base, sub string) string {
	if sub == "" {
		return base
	}
	return base + " " + sub
}

func commandFamilyFromSource(command string) string {
	features, ok := shellparse.AnalyzeApprovalFeatures(command)
	if !ok || len(features.CommandPrefix) == 0 {
		return "shell"
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(features.CommandPrefix[0]), ".exe"))
	if len(features.CommandPrefix) > 1 && slices.Contains([]string{"git", "go", "npm", "cargo", "docker", "kubectl"}, base) {
		return joinFamily(base, strings.ToLower(features.CommandPrefix[1]))
	}
	return base
}

func bashHasUnsafeLifecycle(command string) bool {
	file, err := shellparse.ParseBash(command)
	if err != nil {
		return false
	}
	unsafe := false
	syntax.Walk(file, func(node syntax.Node) bool {
		stmt, ok := node.(*syntax.Stmt)
		if ok && (stmt.Background || stmt.Coprocess || stmt.Disown) {
			unsafe = true
			return false
		}
		return !unsafe
	})
	return unsafe
}

func staticFieldsWithOutputRedirect(command string) ([]string, bool, bool) {
	file, err := shellparse.ParseBash(command)
	if err != nil || shellparse.HasHereDoc(file) || len(file.Stmts) != 1 {
		return nil, false, false
	}
	stmt := file.Stmts[0]
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) > 0 || stmt.Background || stmt.Negated || stmt.Coprocess || stmt.Disown {
		return nil, false, false
	}
	fields := make([]string, 0, len(call.Args))
	for _, word := range call.Args {
		field, static := shellparse.StaticWord(word)
		if !static {
			return nil, false, false
		}
		fields = append(fields, field)
	}
	writes := false
	for _, redirect := range stmt.Redirs {
		switch redirect.Op {
		case syntax.RdrOut, syntax.AppOut, syntax.RdrClob, syntax.AppClob, syntax.RdrAll, syntax.AppAll, syntax.RdrAllClob, syntax.AppAllClob:
			writes = true
		default:
			return nil, false, false
		}
	}
	return fields, writes, len(fields) > 0
}

func branchMutationArg(arg string) bool {
	return slices.Contains([]string{"-d", "-D", "--delete", "--no-delete", "-m", "-M", "--move", "--no-move", "-c", "-C", "--copy", "--no-copy", "-f", "--force", "--no-force", "-t", "--track", "--no-track", "-u", "--set-upstream-to", "--unset-upstream", "--edit-description", "--create-reflog", "--recurse-submodules"}, strings.SplitN(arg, "=", 2)[0])
}
func branchValueOption(arg string) bool {
	return slices.Contains([]string{"--contains", "--no-contains", "--merged", "--no-merged", "--points-at", "--sort", "--format", "--color", "--column", "--abbrev"}, strings.SplitN(arg, "=", 2)[0])
}
func tagMutationArg(arg string) bool {
	return slices.Contains([]string{"-d", "--delete", "-a", "--annotate", "-s", "--sign", "-u", "--local-user", "-f", "--force", "-m", "--message", "-F", "--file", "-e", "--edit", "--trailer", "--cleanup", "--create-reflog"}, strings.SplitN(arg, "=", 2)[0])
}
func tagFlagOption(arg string) bool {
	return slices.Contains([]string{"--contains", "--no-contains", "--merged", "--no-merged", "--points-at", "--sort", "--format", "--column", "--no-column", "--color", "--no-color", "--omit-empty", "--no-omit-empty"}, strings.SplitN(arg, "=", 2)[0])
}

func tagOptionConsumesNext(arg string) bool {
	return !strings.Contains(arg, "=") && slices.Contains([]string{"--contains", "--no-contains", "--merged", "--no-merged", "--points-at", "--sort", "--format"}, arg)
}

func configReadAction(arg string) bool {
	return slices.Contains([]string{"get", "get-all", "get-regexp", "get-urlmatch", "list", "--get", "--get-all", "--get-regexp", "--get-urlmatch", "--list", "--get-color", "--get-colorbool"}, strings.ToLower(arg))
}

func containsConfigReadAction(args []string) bool { return slices.ContainsFunc(args, configReadAction) }
func configOptionConsumesNext(arg string) bool {
	return slices.Contains([]string{"--file", "-f", "--blob", "--type", "--default", "--fixed-value"}, strings.ToLower(arg))
}

func hasEffectArg(args []string, candidates ...string) bool {
	return slices.ContainsFunc(args, func(arg string) bool {
		return slices.Contains(candidates, strings.SplitN(arg, "=", 2)[0])
	})
}

func hasArgPrefix(args []string, prefix string) bool {
	return slices.ContainsFunc(args, func(arg string) bool { return strings.HasPrefix(arg, prefix) })
}
func allEffectArgs(args []string, allowed ...string) bool {
	return len(args) > 0 && !slices.ContainsFunc(args, func(arg string) bool { return !slices.Contains(allowed, arg) })
}
func hasOutputArg(args []string) bool {
	return hasEffectArg(args, "-o", "--output") || hasArgPrefix(args, "-o")
}
func shortOptionContains(arg string, flag byte) bool {
	return len(arg) > 1 && arg[0] == '-' && arg[1] != '-' && strings.ContainsRune(arg[1:], rune(flag))
}
