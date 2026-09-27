package gitcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// externalDiffEnvKey overrides the diff.external config key for every git that
// inherits it. An empty value is not a neutralizer — git execs "" and dies — so
// the variable has to be removed, not blanked.
const externalDiffEnvKey = "GIT_EXTERNAL_DIFF"

// StripExternalDiff returns env with GIT_EXTERNAL_DIFF removed. A git the agent
// runs itself carries no --no-ext-diff, so an inherited value would silently
// override diff.external and can break git outright.
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

// GlobalConfigWithoutExternalDiff returns the path to a cached copy of the
// user's global git config with every diff.external assignment removed, or ""
// when none sets one. Pointing GIT_CONFIG_GLOBAL at it keeps a `git diff` the
// agent types on git's internal diff when the user selected difftastic or delta
// (--no-ext-diff is per-invocation and cannot cover those).
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

	rootsKey := strings.Join(roots, "\n")
	if globalCfgMemo != nil && globalCfgMemo.roots == rootsKey &&
		globalCfgMemo.destPath == dest && sourcesFresh(globalCfgMemo.files) {
		return globalCfgMemo.result
	}
	if files, ok := configCacheFresh(dest, sidecar, roots); ok {
		globalCfgMemo = &configMemo{roots: rootsKey, destPath: dest, files: files, result: dest}
		return dest
	}

	var merged strings.Builder
	var files []configSource
	flattenConfig(globalConfigSeed(roots), "", map[string]bool{}, &merged, &files)
	filtered, removed := stripDiffExternal(merged.String())
	if !removed {
		_ = os.Remove(dest) // a copy from an earlier run is now stale
		_ = os.Remove(sidecar)
		globalCfgMemo = &configMemo{roots: rootsKey, destPath: dest, files: files}
		return ""
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return ""
	}
	if err := os.WriteFile(dest, []byte(filtered), 0o600); err != nil {
		return ""
	}
	_ = os.WriteFile(sidecar, []byte(encodeConfigSources(roots, files)), 0o600)
	globalCfgMemo = &configMemo{roots: rootsKey, destPath: dest, files: files, result: dest}
	return dest
}

var (
	globalCfgMu   sync.Mutex
	globalCfgMemo *configMemo
)

// configMemo is the last flattening's conclusion, kept in memory so a repeated
// call whose source files are unchanged skips re-reading them. result is "" when
// the global config selected no external diff.
type configMemo struct {
	roots    string
	destPath string
	files    []configSource
	result   string
}

// configSource records one file read while flattening, for staleness checks.
type configSource struct {
	path  string
	mtime time.Time
}

// globalConfigRoots returns the global-scope config files git reads, in git's
// order: GIT_CONFIG_GLOBAL when set, else the XDG file and ~/.gitconfig.
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
// resolving each path against dir and recursing into included files. [includeIf]
// sections are kept — their condition still applies — with their path made
// absolute. Every file read is recorded in files.
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

// configCacheFresh reports whether the cached copy on disk still matches the
// current root list and every recorded source file's mtime, returning those
// sources so the caller can seed its in-memory memo.
func configCacheFresh(dest, sidecar string, roots []string) ([]configSource, bool) {
	if _, err := os.Stat(dest); err != nil {
		return nil, false
	}
	recordedRoots, files, err := decodeConfigSources(sidecar)
	if err != nil || recordedRoots != strings.Join(roots, "\n") || !sourcesFresh(files) {
		return nil, false
	}
	return files, true
}

// sourcesFresh reports whether every recorded source file still exists with the
// recorded mtime. An empty set is never fresh: nothing was read to anchor it.
func sourcesFresh(files []configSource) bool {
	if len(files) == 0 {
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

// stripDiffExternal returns src with every diff.external assignment removed from
// a [diff] section, and whether any was removed.
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

// envConfig is git config every git the agent starts must see, layered over the
// user's own through git's environment-config mechanism (GIT_CONFIG_COUNT and
// GIT_CONFIG_KEY_n/VALUE_n, equivalent to -c, git >= 2.31). It reaches the
// commands the agent types, which the Args -c baseline does not.
var envConfig = [][2]string{
	// An interactive-rebase todo must carry the long command words ("pick", not
	// "p"); the agent matches them as text, so a user's abbreviateCommands=true
	// would break the match.
	{"rebase.abbreviateCommands", "false"},
}

// WithConfigEnv returns env with git's environment-config entries appended so a
// git subprocess inherits envConfig on top of the user's own configuration. An
// existing GIT_CONFIG_COUNT is extended rather than overwritten. Git older than
// 2.31 ignores these variables, leaving the user's config in effect.
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

// AgentEnv returns env for the commands the agent runs through the shell,
// hardened so their output is deterministic and their git ignores the user's
// external diff: a fixed locale, no colour, no pager, Reasonix's config layered
// in, and GIT_EXTERNAL_DIFF removed. The user's identity and credentials stay
// intact.
func AgentEnv(env []string, stateDir string) []string {
	env = setEnv(env,
		"LC_MESSAGES=C", "NO_COLOR=1", "TERM=dumb", "GIT_PAGER=cat", "PAGER=cat")
	env = WithConfigEnv(StripExternalDiff(env))
	if cfg := GlobalConfigWithoutExternalDiff(stateDir); cfg != "" {
		env = setEnv(env, "GIT_CONFIG_GLOBAL="+cfg)
	}
	return env
}

// setEnv returns a copy of env with each "KEY=VALUE" pair set, replacing an
// existing entry for the same key rather than appending a duplicate.
func setEnv(env []string, pairs ...string) []string {
	out := slices.Clone(env)
	for _, pair := range pairs {
		key, _, _ := strings.Cut(pair, "=")
		replaced := false
		for i, kv := range out {
			if k, _, ok := strings.Cut(kv, "="); ok && envKeyEqual(k, key) {
				out[i], replaced = pair, true
				break
			}
		}
		if !replaced {
			out = append(out, pair)
		}
	}
	return out
}
