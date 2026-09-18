// Package gitcmd builds the git invocations Reasonix runs on its own behalf:
// the status readout, workspace change probes, worktree management, and plugin
// source checkouts.
//
// Every one of those may point at a repository Reasonix did not create, and a
// repository's own .git/config is data authored by whoever produced the
// repository — not configuration the user chose. Several config keys name a
// command that git then executes during ordinary read-only work: an index
// refresh runs core.fsmonitor, a diff runs diff.external or a textconv driver,
// auto-maintenance spawns a background daemon. Command-line -c overrides win
// over repository config, and the corresponding --no-* flags win over both, so
// every invocation carries the same baseline.
//
// Centralizing that baseline is the point of this package. The same overrides
// used to be spelled out at each call site, which is exactly how three of the
// five sites ended up carrying them and two did not.
//
// Content filters (filter.<driver>.clean/process) are selected per driver name
// through .gitattributes, so diffs neutralize every driver defined in the
// repository's local .git/config instead (see filterNeutralizingConfig).
// include.path chains and core.sshCommand remain the user's own to vet.
package gitcmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/proc"
	"reasonix/internal/secrets"
)

// baseConfig is the -c override set every invocation carries.
var baseConfig = []string{
	// An index refresh (status, diff, rev-parse --show-toplevel in a dirty
	// tree) executes this as a command when the repository sets it.
	"core.fsmonitor=false",
	// Keeps a probe from starting git's background maintenance daemon.
	"maintenance.auto=false",
}

// Args returns the full argument list for a git invocation: the hardening
// overrides, an optional -C directory, then the caller's arguments. extraConfig
// entries are "key=value" pairs appended after the baseline, so a call site can
// add its own preferences but cannot drop the baseline.
func Args(dir string, extraConfig []string, args ...string) []string {
	return argsFor(runtime.GOOS, dir, extraConfig, args...)
}

func argsFor(goos, dir string, extraConfig []string, args ...string) []string {
	// No capacity hint: these lists are a handful of entries, and computing one
	// from the input lengths buys nothing measurable.
	var out []string
	for _, cfg := range baseConfig {
		out = append(out, "-c", cfg)
	}
	if goos == "windows" {
		out = append(out, "-c", "core.longpaths=true")
	}
	for _, cfg := range extraConfig {
		if cfg == "" {
			continue
		}
		out = append(out, "-c", cfg)
	}
	for _, cfg := range filterNeutralizingConfig(dir, args) {
		out = append(out, "-c", cfg)
	}
	if dir != "" {
		out = append(out, "-C", dir)
	}
	return append(out, hardenSubcommand(args)...)
}

// filterNeutralizingConfig returns -c overrides that blank every filter driver
// command the repository's local .git/config defines, but only when args runs a
// diff — the one gitcmd subcommand that invokes clean filters on working-tree
// content. A diff compares the file's raw bytes, so an emptied filter is the
// correct rendering, not a degraded one. Git prefers a long-running process
// filter over clean when one is configured, so both command forms are emptied;
// required is forced off so a disabled required filter does not fail the diff.
func filterNeutralizingConfig(dir string, args []string) []string {
	sub, cDir := gitSubcommand(args)
	if sub != "diff" {
		return nil
	}
	if cDir != "" {
		dir = cDir
	}
	drivers := localFilterDrivers(dir)
	if len(drivers) == 0 {
		return nil
	}
	out := make([]string, 0, 3*len(drivers))
	for _, name := range drivers {
		out = append(out,
			"filter."+name+".clean=",
			"filter."+name+".process=",
			"filter."+name+".required=false",
		)
	}
	return out
}

// gitSubcommand finds the first non-global argument — the subcommand — and the
// directory named by a leading -C, if the caller put it inside args instead of
// the dir parameter. Global options before the subcommand are limited to the
// forms gitcmd's call sites use (-c v, -C d, --opt=v, --opt d); anything else
// still terminates the scan at the first bare word.
func gitSubcommand(args []string) (sub, cDir string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-C":
			if i+1 < len(args) {
				cDir = args[i+1]
				i++
			}
		case strings.HasPrefix(a, "-C"):
			cDir = strings.TrimPrefix(a, "-C")
		case a == "-c":
			i++ // skip the key=value that follows
		case strings.HasPrefix(a, "-"):
			// Any other global flag; --flag=value and bare flags alike carry
			// no value we track. The next bare word ends global options.
		default:
			return a, cDir
		}
	}
	return "", cDir
}

// localFilterDrivers lists the filter driver names defined by sections of the
// repository-local git config under dir. Only [filter "<name>"] sections are
// collected: the driver's command lives in the config, and the config is the
// part of a distributed repository its author controls. A missing or unreadable
// config yields no drivers (nothing to neutralize). User and system config are
// deliberately not read — those are the user's own choices.
func localFilterDrivers(dir string) []string {
	var drivers []string
	for _, cfgPath := range localGitConfigPaths(dir) {
		f, err := os.Open(cfgPath)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if len(line) < 2 || line[0] != '[' || line[len(line)-1] != ']' {
				continue
			}
			section := strings.TrimSpace(line[1 : len(line)-1])
			i := strings.IndexAny(section, " \t")
			if i < 0 || !strings.EqualFold(section[:i], "filter") {
				continue
			}
			name := strings.Trim(strings.TrimSpace(section[i:]), `"`)
			if name == "" || slices.Contains(drivers, name) {
				continue
			}
			drivers = append(drivers, name)
		}
		_ = f.Close()
	}
	return drivers
}

// localGitConfigPaths resolves the repository-local configs for a working tree.
// Linked worktrees inherit <commondir>/config and may add
// <gitdir>/config.worktree when extensions.worktreeConfig is enabled.
func localGitConfigPaths(dir string) []string {
	if dir == "" {
		dir = "."
	}
	dotGit := filepath.Join(dir, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return nil
	}
	gitdir := dotGit
	if !info.IsDir() {
		data, readErr := os.ReadFile(dotGit)
		if readErr != nil {
			return nil
		}
		line := strings.TrimSpace(string(data))
		rest, ok := strings.CutPrefix(line, "gitdir:")
		if !ok {
			return nil
		}
		gitdir = strings.TrimSpace(rest)
		if !filepath.IsAbs(gitdir) {
			gitdir = filepath.Join(dir, gitdir)
		}
	}
	gitdir = filepath.Clean(gitdir)
	commonDir := gitdir
	if data, readErr := os.ReadFile(filepath.Join(gitdir, "commondir")); readErr == nil {
		commonDir = strings.TrimSpace(string(data))
		if !filepath.IsAbs(commonDir) {
			commonDir = filepath.Join(gitdir, commonDir)
		}
		commonDir = filepath.Clean(commonDir)
	}
	paths := []string{filepath.Join(commonDir, "config")}
	worktreeConfig := filepath.Join(gitdir, "config.worktree")
	if worktreeConfig != paths[0] {
		paths = append(paths, worktreeConfig)
	}
	return paths
}

// hardenSubcommand adds the flags that disable repository-configured programs
// for the subcommands that can invoke them. The flags go after the subcommand
// name, where git accepts them, and are only added when absent so an explicit
// caller flag is never duplicated.
func hardenSubcommand(args []string) []string {
	if len(args) == 0 || args[0] != "diff" {
		return args
	}
	out := []string{args[0]}
	for _, flag := range []string{"--no-ext-diff", "--no-textconv"} {
		if !slices.Contains(args, flag) {
			out = append(out, flag)
		}
	}
	return append(out, args[1:]...)
}

// Command builds a hardened git command rooted at dir (empty runs in the
// process working directory). The environment drops credential variables so a
// git subprocess — and anything git itself starts — never inherits provider
// keys, and disables interactive prompts so a probe cannot block on one.
func Command(ctx context.Context, dir string, args ...string) *exec.Cmd {
	return CommandWithConfig(ctx, dir, nil, args...)
}

// CommandWithConfig is Command with additional "key=value" config overrides
// layered on top of the baseline.
func CommandWithConfig(ctx context.Context, dir string, extraConfig []string, args ...string) *exec.Cmd {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := proc.CommandContext(ctx, "git", Args(dir, extraConfig, args...)...)
	cmd.Env = Env()
	proc.HideWindow(cmd)
	return cmd
}

// Env is the environment a git subprocess runs with. GIT_EXTERNAL_DIFF is
// covered by --no-ext-diff on diff invocations, which outranks both the config
// key and the environment variable. GIT_SSH_COMMAND is deliberately left
// alone: it governs the ssh network transport (fetch/ls-remote/push), not diff
// rendering, and clearing it would break legitimate ssh remotes; repository
// config that sets core.sshCommand is a plugin-trust concern, not one this
// diff-oriented baseline can address (see the package residual note).
func Env() []string {
	return WithConfigEnv(StripExternalDiff(append(secrets.ProcessEnv(),
		// Read-only probes must not take the index lock.
		"GIT_OPTIONAL_LOCKS=0",
		// Fail fast instead of blocking on a credential prompt for a terminal
		// the TUI owns and the desktop app does not have.
		"GIT_TERMINAL_PROMPT=0",
	)))
}

// externalDiffEnvKey overrides the diff.external config key for every git that
// inherits it. An empty value is NOT a neutralizer — git tries to run "" and
// dies ("cannot run : No such file or directory") — so the only safe handling
// is to remove the variable from the environment entirely.
const externalDiffEnvKey = "GIT_EXTERNAL_DIFF"

// StripExternalDiff returns env with GIT_EXTERNAL_DIFF removed. Reasonix's
// subprocess environment must never carry it: it silently overrides the
// diff.external config key for every git the agent runs, and an empty value
// breaks git outright, so leaving it set is a footgun with no upside.
func StripExternalDiff(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if k, _, ok := strings.Cut(kv, "="); ok && envKeyEqual(k, externalDiffEnvKey) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// GlobalConfigWithoutExternalDiff returns the path to a flattened copy of the
// user's global git config — every source file, and every file they include,
// inlined in git's read order with any diff.external assignment removed — or ""
// when nothing sets diff.external. The copy lives at a stable path under
// stateDir and is rewritten only when a source file changes, so callers pay a
// stat per source and a write only on change.
//
// The flatten starts from a synthetic config whose [include] directives name the
// real roots, so the recursion is uniform: an included file is processed exactly
// like a root, and relative include.path values resolve against the file that
// contains them. Pointing GIT_CONFIG_GLOBAL at the copy is enough because a
// global diff.external is the common case (difftastic, delta), and no
// environment can override a repository-local one anyway.
//
// This copy-and-filter is needed only because git 2.47.3 — the current git —
// cannot override a diff.external back to its default internal-git diff. An
// empty diff.external (or GIT_EXTERNAL_DIFF) is not a neutralizer: git execs
// "" and dies. --no-ext-diff does restore the internal diff, but it is a
// per-invocation flag and cannot cover the arbitrary git commands the agent
// runs. Once git gains a config knob for this, the copy can be dropped and
// GIT_CONFIG_GLOBAL left alone.
func GlobalConfigWithoutExternalDiff(stateDir string) string {
	if strings.TrimSpace(stateDir) == "" {
		return ""
	}
	roots := globalConfigRoots()
	if len(roots) == 0 {
		return ""
	}
	dest := filepath.Join(stateDir, "git", "gitconfig")
	sidecar := dest + ".sources"

	globalCfgMu.Lock()
	defer globalCfgMu.Unlock()

	if configCacheFresh(dest, sidecar, roots) {
		return dest
	}

	var merged strings.Builder
	var files []configSource
	flattenConfig(globalConfigSeed(roots), "", map[string]bool{}, &merged, &files)
	filtered, removed := stripDiffExternal(merged.String())
	if !removed {
		_ = os.Remove(dest) // a copy from an earlier run is now stale
		_ = os.Remove(sidecar)
		return ""
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return ""
	}
	if err := os.WriteFile(dest, []byte(filtered), 0o600); err != nil {
		return ""
	}
	_ = os.WriteFile(sidecar, []byte(encodeConfigSources(roots, files)), 0o600)
	return dest
}

var globalCfgMu sync.Mutex

// configSource records one file read while flattening, for staleness checks.
type configSource struct {
	path  string
	mtime time.Time
}

// globalConfigRoots returns the global-scope config files git reads, in git's
// order: GIT_CONFIG_GLOBAL when set, else the XDG file and ~/.gitconfig (git
// reads both) when they exist.
func globalConfigRoots() []string {
	if p := strings.TrimSpace(os.Getenv("GIT_CONFIG_GLOBAL")); p != "" {
		return []string{p}
	}
	var out []string
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		if p := filepath.Join(xdg, "git", "config"); fileExists(p) {
			out = append(out, p)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if p := filepath.Join(home, ".gitconfig"); fileExists(p) {
			out = append(out, p)
		}
	}
	return out
}

// globalConfigSeed returns a synthetic config whose [include] directives name
// roots, so flattening starts uniformly from a config text.
func globalConfigSeed(roots []string) string {
	var b strings.Builder
	for _, p := range roots {
		fmt.Fprintf(&b, "[include]\n\tpath = %s\n", quoteConfigValue(p))
	}
	return b.String()
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// flattenConfig inlines every unconditional [include] in src into out,
// resolving each path against dir and recursing into the included files.
// [includeIf] sections are kept — their condition still applies — with their
// path made absolute. Every file read is recorded in files.
func flattenConfig(src, dir string, seen map[string]bool, out *strings.Builder, files *[]configSource) {
	inInclude, inIncludeIf := false, false
	for _, line := range strings.SplitAfter(src, "\n") {
		if name, _, ok := sectionHeader(line); ok {
			inInclude = name == "include"
			inIncludeIf = name == "includeif"
			out.WriteString(line)
			continue
		}
		if inInclude {
			if target, ok := includePathTarget(line); ok {
				flattenFile(resolveConfigPath(target, dir), seen, out, files)
				continue
			}
		}
		if inIncludeIf {
			if rewritten, ok := rewriteIncludePathLine(line, dir); ok {
				out.WriteString(rewritten)
				continue
			}
		}
		out.WriteString(line)
	}
}

// flattenFile reads one config file and appends its flattened content, guarding
// against include cycles.
func flattenFile(path string, seen map[string]bool, out *strings.Builder, files *[]configSource) {
	abs := absConfigPath(path)
	if seen[abs] {
		return
	}
	seen[abs] = true
	raw, err := os.ReadFile(abs)
	if err != nil {
		return
	}
	if info, err := os.Stat(abs); err == nil {
		*files = append(*files, configSource{path: abs, mtime: info.ModTime()})
	}
	flattenConfig(string(raw), filepath.Dir(abs), seen, out, files)
}

// resolveConfigPath resolves an include.path value against the including file's
// directory, expanding a leading "~/".
func resolveConfigPath(target, dir string) string {
	if rest, ok := strings.CutPrefix(target, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	} else if target == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(dir, target)
}

func absConfigPath(p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return filepath.Clean(p)
}

// configCacheFresh reports whether the cached copy still matches the current
// root list and every recorded source file's mtime.
func configCacheFresh(dest, sidecar string, roots []string) bool {
	if _, err := os.Stat(dest); err != nil {
		return false
	}
	recordedRoots, files, err := decodeConfigSources(sidecar)
	if err != nil || recordedRoots != strings.Join(roots, "\n") {
		return false
	}
	for _, f := range files {
		info, err := os.Stat(f.path)
		if err != nil || !info.ModTime().Equal(f.mtime) {
			return false
		}
	}
	return true
}

// encodeConfigSources serialises the root path list (one per line, then a blank
// line) followed by every source file's mtime and path, so a later call can
// tell whether the copy is stale.
func encodeConfigSources(roots []string, files []configSource) string {
	var b strings.Builder
	for _, r := range roots {
		b.WriteString(r)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	for _, f := range files {
		fmt.Fprintf(&b, "%d\t%s\n", f.mtime.UnixNano(), f.path)
	}
	return b.String()
}

// decodeConfigSources returns the recorded root block and the source files.
func decodeConfigSources(sidecar string) (string, []configSource, error) {
	raw, err := os.ReadFile(sidecar)
	if err != nil {
		return "", nil, err
	}
	roots, fileBlock, ok := strings.Cut(string(raw), "\n\n")
	if !ok {
		return "", nil, fmt.Errorf("malformed source list")
	}
	var files []configSource
	for _, line := range strings.Split(fileBlock, "\n") {
		ts, path, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			continue
		}
		files = append(files, configSource{path: path, mtime: time.Unix(0, n)})
	}
	return roots, files, nil
}

// stripDiffExternal returns src with every diff.external assignment removed
// from a [diff] section, and whether any was removed.
func stripDiffExternal(src string) (string, bool) {
	var out strings.Builder
	inDiff, removed := false, false
	for _, line := range strings.SplitAfter(src, "\n") {
		if name, sub, ok := sectionHeader(line); ok {
			inDiff = name == "diff" && sub == ""
			out.WriteString(line)
			continue
		}
		if inDiff && isExternalAssignment(line) {
			removed = true
			continue
		}
		out.WriteString(line)
	}
	return out.String(), removed
}

// sectionHeader parses a "[name]" or "[name \"sub\"]" line.
func sectionHeader(line string) (name, sub string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[") {
		return "", "", false
	}
	end := strings.IndexByte(trimmed, ']')
	if end < 0 {
		return "", "", false
	}
	name, rest, _ := strings.Cut(trimmed[1:end], " ")
	return strings.ToLower(strings.TrimSpace(name)), strings.Trim(strings.TrimSpace(rest), `"`), true
}

// isExternalAssignment reports whether a config line sets the "external" key.
func isExternalAssignment(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
		return false
	}
	key, _, _ := strings.Cut(trimmed, "=")
	return strings.EqualFold(strings.TrimSpace(key), "external")
}

// includePathTarget returns the value of a "path" assignment line, unquoted and
// without a trailing comment. ok is false when the line is not one.
func includePathTarget(line string) (string, bool) {
	body, _ := strings.CutSuffix(line, "\n")
	trimmed := strings.TrimSpace(body)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
		return "", false
	}
	key, rest, ok := strings.Cut(body, "=")
	if !ok || !strings.EqualFold(strings.TrimSpace(key), "path") {
		return "", false
	}
	value := strings.Trim(strings.TrimSpace(stripConfigComment(rest)), `"`)
	if value == "" {
		return "", false
	}
	return value, true
}

// rewriteIncludePathLine rewrites one relative "path = ..." line to an absolute
// path resolved against dir, preserving the trailing newline. ok is false when
// the line is not a relative include path.
func rewriteIncludePathLine(line, dir string) (string, bool) {
	target, ok := includePathTarget(line)
	if !ok || filepath.IsAbs(target) || strings.HasPrefix(target, "~") {
		return "", false
	}
	body, hadNL := strings.CutSuffix(line, "\n")
	nl := ""
	if hadNL {
		nl = "\n"
	}
	key, _, _ := strings.Cut(body, "=")
	return strings.TrimRight(key, " \t") + " = " + quoteConfigValue(resolveConfigPath(target, dir)) + nl, true
}

// stripConfigComment drops an unquoted "#" or ";" comment tail.
func stripConfigComment(v string) string {
	inQuote := false
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '"':
			inQuote = !inQuote
		case '#', ';':
			if !inQuote {
				return v[:i]
			}
		}
	}
	return v
}

// quoteConfigValue quotes a git config value when it contains a character that
// would otherwise end the value.
func quoteConfigValue(v string) string {
	if v == "" {
		return `""`
	}
	if !strings.ContainsAny(v, " \t#;\"\\") {
		return v
	}
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return `"` + v + `"`
}

// envConfig is git config every git subprocess Reasonix starts must see,
// layered over the user's own ~/.gitconfig through git's environment-config
// mechanism (GIT_CONFIG_COUNT/GIT_CONFIG_KEY_n/GIT_CONFIG_VALUE_n, equivalent
// to -c on the command line, git >= 2.31). It applies to the agent's own git
// invocations too, which the Args -c baseline does not reach.
//
// rebase.abbreviateCommands=false pins the interactive-rebase todo to the long
// command words. Reasonix's agent rewrites that todo by matching the leading
// command name, so a user whose git config sets abbreviateCommands=true would
// otherwise get a "p" the rewrite does not recognize. Forcing the long form
// makes the todo match the assumption Reasonix already makes, for every user,
// without editing the user's config file.
var envConfig = [][2]string{
	{"rebase.abbreviateCommands", "false"},
}

// WithConfigEnv returns env with git's environment-config entries appended so a
// git subprocess inherits envConfig on top of the user's own configuration. An
// existing GIT_CONFIG_COUNT is extended rather than overwritten, so any
// GIT_CONFIG_KEY_n/GIT_CONFIG_VALUE_n pairs already in env survive. Git older
// than 2.31 ignores these variables, leaving the user's config in effect.
func WithConfigEnv(env []string) []string {
	if len(envConfig) == 0 {
		return env
	}
	count := 0
	if v, ok := envValue(env, "GIT_CONFIG_COUNT"); ok {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			count = n
		}
	}
	out := make([]string, 0, len(env)+2*len(envConfig)+1)
	replaced := false
	for _, kv := range env {
		k, _, ok := strings.Cut(kv, "=")
		if ok && envKeyEqual(k, "GIT_CONFIG_COUNT") {
			if !replaced {
				out = append(out, fmt.Sprintf("GIT_CONFIG_COUNT=%d", count+len(envConfig)))
				replaced = true
			}
			continue
		}
		out = append(out, kv)
	}
	if !replaced {
		out = append(out, fmt.Sprintf("GIT_CONFIG_COUNT=%d", count+len(envConfig)))
	}
	for i, kv := range envConfig {
		out = append(out, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", count+i, kv[0]))
		out = append(out, fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", count+i, kv[1]))
	}
	return out
}

// envValue returns the last value for key in env. Keys compare the way the OS
// does: case-insensitively on Windows, exactly elsewhere.
func envValue(env []string, key string) (string, bool) {
	for _, entry := range slices.Backward(env) {
		k, v, ok := strings.Cut(entry, "=")
		if ok && envKeyEqual(k, key) {
			return v, true
		}
	}
	return "", false
}

func envKeyEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
