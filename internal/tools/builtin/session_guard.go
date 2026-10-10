package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/tool"
)

// SessionDataGuard rejects agent writes into Reasonix's own session stores:
// <state root>/sessions and <state root>/projects/<slug>/sessions. The runtime
// is the only writer of those files (CAS ledger + autosave); an agent editing
// them from inside a chat races the app's own saves, which surfaces to the user
// as endless "conflict copy" forks — the agent sees its write "not take",
// retries, and loops. The zero value is unconfined, matching the confine
// helpers, so tools registered at init keep their historical behavior.
//
// allowRoots are the explicitly configured [sandbox] allow_write entries: a
// user who deliberately lists a session directory there keeps raw access, so
// the guard only blocks the accidental self-write path (a workspace root that
// happens to cover the state root, e.g. a home-directory workspace).
type SessionDataGuard struct {
	stateRoot   string
	allowRoots  []string
	hintNeedles []string
}

// NewSessionDataGuard builds a guard for the given Reasonix state root
// (config.MemoryUserDir()) and the explicit allow_write entries. Both are
// resolved to absolute, symlink-free paths once here, mirroring realRoots.
// An empty stateRoot yields an unconfined guard.
func NewSessionDataGuard(stateRoot string, allowRoots []string) SessionDataGuard {
	g := SessionDataGuard{}
	if strings.TrimSpace(stateRoot) == "" {
		return g
	}
	real, err := realPath(stateRoot)
	if err != nil {
		return g
	}
	g.stateRoot = real
	g.allowRoots = realRoots(allowRoots)
	g.hintNeedles = sessionHintNeedles(stateRoot, real, g.allowRoots)
	return g
}

// Check returns an error when target resolves into a guarded session store and
// is not covered by an explicit allow_write root. The error text is written for
// the model: it names why the write is refused and the durable ways forward.
func (g SessionDataGuard) Check(target string) error {
	if g.stateRoot == "" {
		return nil
	}
	abs, err := realPath(target)
	if err != nil {
		return nil // can't resolve -> let the caller's normal error path handle it
	}
	if g.deniesSecurity(abs) {
		return fmt.Errorf("path %q is a Reasonix security boundary file (%s holds the global hooks; hooks execute arbitrary shell commands on every future session). Agents may not modify it. "+
			"Ask the user to edit it themselves, or to add the directory to [sandbox] allow_write in reasonix.toml if raw access is truly intended",
			target, g.stateRoot)
	}
	if g.deniesTrusted(abs) {
		return tool.Refusal{Code: CodeTrustedStateWrite, Message: fmt.Sprintf(
			"write refused: `%s` is inside Reasonix's trusted host state (%s). The host alone records evidence and contracts there, so agents may not modify it; report the underlying problem instead",
			target, filepath.Join(g.stateRoot, TrustedStateDir))}
	}
	if g.deniesSchedule(abs) {
		return tool.Refusal{Code: CodeScheduleStateWrite, Message: fmt.Sprintf(
			"write refused: `%s` is inside the scheduled-runs store (%s). Schedules, budgets and run records are changed by the person through the app, never by an agent, so an unattended run's limits cannot be edited from a chat; report the underlying problem instead",
			target, filepath.Join(g.stateRoot, ScheduleStateDir))}
	}
	if !g.denies(abs) {
		return nil
	}
	return fmt.Errorf("path %q is inside Reasonix's own session/state data (%s); the app is the only writer of these files, and edits from a chat race its saves — that surfaces as repeated save-conflict copies. "+
		"Do not modify session or runtime-state files directly; report the underlying problem instead. If raw access is truly intended, add the directory to [sandbox] allow_write in reasonix.toml",
		target, g.stateRoot)
}

// securityStateFile reports whether name (a state-root-direct file name,
// already case-folded when the platform folds) is a security boundary rather
// than a mere runtime ledger. settings.json defines the global hooks:
// arbitrary shell commands executed on harness events in every project.
func securityStateFile(name string) bool {
	switch name {
	case "settings.json":
		return true
	}
	return false
}

// deniesSecurity reports whether abs (absolute, symlink-free) is a state-root-
// direct security boundary file (see securityStateFile) not covered by an
// explicit allow_write root. Deny-side, so comparisons fold case on
// case-insensitive platforms, mirroring denies.
func (g SessionDataGuard) deniesSecurity(abs string) bool {
	root := g.stateRoot
	allow := g.allowRoots
	if foldPaths {
		abs = strings.ToLower(abs)
		root = strings.ToLower(root)
		folded := make([]string, len(allow))
		for i, a := range allow {
			folded[i] = strings.ToLower(a)
		}
		allow = folded
	}
	for _, a := range allow {
		if within(a, abs) {
			return false
		}
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || strings.Contains(rel, string(filepath.Separator)) {
		return false
	}
	return securityStateFile(rel)
}

// TrustedStateDir is the state-root subdirectory holding Trusted Host State.
const TrustedStateDir = "trusted"

// CodeTrustedStateWrite identifies a file-tool write refused for landing in
// Trusted Host State.
const CodeTrustedStateWrite = "workspace.trusted_state_write"

// ScheduleStateDir is the state-root subdirectory holding the scheduled-runs
// store.
const ScheduleStateDir = "schedules"

// CodeScheduleStateWrite identifies a file-tool write refused for landing in the
// scheduled-runs store.
const CodeScheduleStateWrite = "workspace.schedule_state_write"

// deniesSchedule reports whether abs lies in the scheduled-runs store and no
// explicit allow_write root covers it.
func (g SessionDataGuard) deniesSchedule(abs string) bool {
	for _, a := range g.allowRoots {
		if withinFold(a, abs) {
			return false
		}
	}
	return withinFold(filepath.Join(g.stateRoot, ScheduleStateDir), abs)
}

// deniesTrusted reports whether abs lies in Trusted Host State and no explicit
// allow_write root covers it. An allow_write entry there also reaches the bash
// sandbox's write roots, which is what drops the host's integrity level.
func (g SessionDataGuard) deniesTrusted(abs string) bool {
	root := filepath.Join(g.stateRoot, TrustedStateDir)
	for _, a := range g.allowRoots {
		if withinFold(a, abs) {
			return false
		}
	}
	return withinFold(root, abs)
}

// runtimeStateFile reports whether name (a state-root-direct file name, already
// case-folded when the platform folds) is a desktop runtime ledger the app
// rewrites wholesale while running — quit snapshots, topic-index rebuilds,
// periodic flushes — so an agent edit vanishes the same way a session-file edit
// does. config.toml / credentials / skills stay writable: editing those on the
// user's request is a legitimate flow with no autonomous rewriter racing it —
// but settings.json is a security boundary, not a ledger, and is denied
// separately by securityStateFile.
// heartbeat-tasks.json stays writable too — it is documented as human- and
// AI-editable (desktop/heartbeat.go, and the heartbeat panel tip says "AI
// agents can also edit heartbeat-tasks.json"), so the product explicitly
// accepts agent edits racing the engine there.
func runtimeStateFile(name string) bool {
	if strings.HasPrefix(name, "desktop-") {
		return true // desktop-tabs.json(+.tmp), desktop-projects.json, desktop-window.json, desktop-workspace…
	}
	switch name {
	case "metrics-pending.json", "crash-pending.json":
		return true
	}
	return false
}

// denies reports whether abs (absolute, symlink-free) is inside a guarded
// session store or a runtime ledger file, and not explicitly allowed. All
// comparisons are deny-side, so they fold case on case-insensitive platforms:
// EvalSymlinks keeps the caller's spelling, and on default macOS/Windows
// volumes ~/.reasonix/SESSIONS reaches the very same files (the same shape as
// the Windows lease-key case split fixed in #6023).
func (g SessionDataGuard) denies(abs string) bool {
	root := g.stateRoot
	allow := g.allowRoots
	if foldPaths {
		abs = strings.ToLower(abs)
		root = strings.ToLower(root)
		folded := make([]string, len(allow))
		for i, a := range allow {
			folded[i] = strings.ToLower(a)
		}
		allow = folded
	}
	for _, a := range allow {
		if within(a, abs) {
			return false
		}
	}
	if within(filepath.Join(root, "sessions"), abs) {
		return true
	}
	// State-root-direct runtime ledgers (desktop-tabs.json & friends).
	if rel, err := filepath.Rel(root, abs); err == nil && rel != "." && !strings.Contains(rel, string(filepath.Separator)) {
		if runtimeStateFile(rel) {
			return true
		}
	}
	// <state root>/projects/<slug>/sessions/** — every per-project store, so
	// the slug segment is matched positionally rather than enumerated.
	projects := filepath.Join(root, "projects")
	if !within(projects, abs) {
		return false
	}
	rel, err := filepath.Rel(projects, abs)
	if err != nil {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	return len(parts) >= 2 && parts[1] == "sessions"
}

// CommandHint returns a warning to append to bash output when the command
// references the guarded state trees, and "" otherwise. bash cannot know what a
// command actually wrote (off mode runs raw, and write roots may legitimately
// cover the state root), so this is a lexical check on the command text —
// enough to break the agent's "write → app overwrites it → looks like my write
// failed → retry" loop, which is how session-data self-writes burn tokens in
// the wild. It never blocks: reading session files for diagnostics is
// legitimate. workDir is the directory the command runs in: when it sits
// inside the state root (the desktop Global workspace lives at
// <state root>/global-workspace), relative references like ../sessions reach
// the stores without ever spelling an absolute path, so relative forms are
// matched too — and a workDir already inside a guarded store warns on every
// command.
func (g SessionDataGuard) CommandHint(workDir, command string) string {
	if g.stateRoot == "" || command == "" {
		return ""
	}
	warn := fmt.Sprintf("WARNING: this command referenced Reasonix's own session/state data under %s. "+
		"This is a notice, not a refusal: the command was not blocked, and an empty result above means it matched nothing. "+
		"To read a spilled tool output (<session>.outputs/), use read_file (offset/limit) or grep rather than shell text tools. "+
		"The app is actively saving those files; external modifications conflict with its saves and are preserved as conflict copies, so an edit can look like it \"did not take\". "+
		"Do not modify session files from a chat — stop retrying a write and report the underlying problem instead.", g.stateRoot)
	haystack := strings.ToLower(filepath.ToSlash(command))
	for _, needle := range g.hintNeedles {
		if strings.Contains(haystack, needle) {
			return warn
		}
	}
	if workDir != "" {
		if absWork, err := realPath(workDir); err == nil {
			if g.denies(absWork) {
				return warn // cwd is already inside a guarded store: every command operates on it
			}
			if withinFold(g.stateRoot, absWork) {
				for _, sub := range []string{"sessions", "projects"} {
					rel, err := filepath.Rel(absWork, filepath.Join(g.stateRoot, sub))
					if err != nil {
						continue
					}
					if needle := strings.ToLower(filepath.ToSlash(rel)); strings.Contains(haystack, needle) {
						return warn
					}
				}
			}
		}
	}
	return ""
}

// sessionHintNeedles precomputes the lowercase, slash-normalized textual forms
// of the guarded trees as they may appear in a command: under the state root as
// given, its symlink-resolved form, and abbreviated variants ("~/", "$HOME/",
// "${HOME}/", and on the config-dir side "%APPDATA%"/"$env:APPDATA") when a
// form sits under the respective base. A tree wholly covered by an allow_write
// root is skipped — the user sanctioned raw access there, so warnings would
// only nag.
func sessionHintNeedles(rawRoot, realRoot string, allowRoots []string) []string {
	prefixes := map[string]bool{}
	addPrefix := func(p string) {
		if p == "" {
			return
		}
		if abs, err := filepath.Abs(p); err == nil {
			prefixes[filepath.Clean(abs)] = true
		}
	}
	addPrefix(rawRoot)
	addPrefix(realRoot)
	home, _ := os.UserHomeDir()
	cfgDir, _ := os.UserConfigDir()

	var needles []string
	abbreviate := func(base, tree string, forms ...string) {
		if base == "" {
			return
		}
		rel, err := filepath.Rel(base, tree)
		if err != nil || rel == "." || !filepath.IsLocal(rel) {
			return
		}
		slashRel := strings.ToLower(filepath.ToSlash(rel))
		for _, form := range forms {
			needles = append(needles, form+"/"+slashRel)
		}
	}
	for prefix := range prefixes {
		for _, sub := range []string{"sessions", "projects", "desktop-", "metrics-pending.json", "crash-pending.json"} {
			tree := filepath.Join(prefix, sub)
			if covered := func() bool {
				for _, a := range allowRoots {
					if withinFold(a, filepath.Join(realRoot, sub)) {
						return true
					}
				}
				return false
			}(); covered {
				continue
			}
			needles = append(needles, strings.ToLower(filepath.ToSlash(tree)))
			abbreviate(home, tree, "~", "$home", "${home}")
			abbreviate(cfgDir, tree, "%appdata%", "$env:appdata")
		}
	}
	return needles
}
