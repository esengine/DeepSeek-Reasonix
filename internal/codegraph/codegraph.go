// Package codegraph integrates the CodeGraph code-intelligence engine
// (https://github.com/colbymchenry/codegraph) as a built-in MCP server. CodeGraph
// indexes a project into a local symbol and call graph (tree-sitter + SQLite,
// FTS5) and serves it over stdio MCP, giving the agent symbol search, caller /
// callee, and change-impact tools without the per-language setup an LSP fleet
// would need.
//
// CodeGraph is fetched on first use into a per-version cache (see Install) rather
// than shipped in the reasonix binary, which keeps installs small. Resolve finds
// the cached launcher; an explicit config path, a system-installed `codegraph` on
// PATH, and a bundle placed beside the executable are also honored. boot injects
// the resolved launcher as one more stdio plugin, pinned to the project root via
// plugin.Spec.Dir (CodeGraph detects the project from its working directory).
package codegraph

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"reasonix/internal/proc"
)

const initTimeout = 30 * time.Second

// SteerText is injected into the system prompt when CodeGraph tools are
// available, so the model knows to prefer them for symbol-level questions.
const SteerText = `## Code Intelligence (codegraph)
You have codegraph tools for symbol-level code intelligence. For architecture questions, "how does X work", call graphs, symbol search, and impact analysis, prefer codegraph tools over grep/read_file:
- codegraph_context — entry points + related symbols + key code in one call (USE THIS FIRST for "how does X work")
- codegraph_search — find symbols by name (functions, types, interfaces)
- codegraph_callers / codegraph_callees — trace call chains
- codegraph_impact — what breaks if I change X
- codegraph_trace — full call path between two symbols
- codegraph_files — project file tree with symbol counts
Use grep/read_file for content search (comments, strings, config values) and when codegraph is not available.`

// BundleDirName is the directory, beside the reasonix executable, that the release
// archive unpacks the CodeGraph bundle into. Its launcher lives at
// <BundleDirName>/bin/codegraph, with the bundled node runtime and lib/ beside it;
// the launcher resolves those relative to itself, so the bundle is relocatable.
const BundleDirName = "codegraph"

// Resolve returns the absolute path to the CodeGraph launcher. Search order:
//  1. override — an explicit [codegraph].path from config (~ and ${VAR} expanded);
//  2. the per-version cache populated by Install (the normal case);
//  3. a system-installed `codegraph` on PATH;
//  4. a bundle placed beside the reasonix executable (fallback for manual setups).
//
// ok is false when none resolves — the caller then triggers Install (or skips the
// feature), so the codegraph_* tools come online once the cache is populated.
func Resolve(override string) (string, bool) {
	if override != "" {
		if p := expand(override); isExec(p) {
			return p, true
		}
	}
	if p, ok := cached(); ok {
		return p, true
	}
	if p, err := exec.LookPath("codegraph"); err == nil {
		return p, true
	}
	if p, ok := bundled(); ok {
		return p, true
	}
	return "", false
}

// bundled looks for the CodeGraph launcher unpacked beside the reasonix binary.
// The executable path is symlink-resolved first so a launcher installed via a
// symlink (e.g. a package manager's bin shim) still points at the real bundle.
func bundled() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	base := filepath.Join(filepath.Dir(exe), BundleDirName)
	for _, rel := range launcherNames() {
		if p := filepath.Join(base, rel); isExec(p) {
			return p, true
		}
	}
	return "", false
}

// launcherNames are the bundle-relative launcher paths to try, per OS. The unix
// bundle ships a POSIX-sh launcher at bin/codegraph; the Windows zip ships a
// batch / exe shim, so try the common names there.
func launcherNames() []string {
	if runtime.GOOS == "windows" {
		return []string{
			filepath.Join("bin", "codegraph.cmd"),
			filepath.Join("bin", "codegraph.exe"),
			filepath.Join("bin", "codegraph.bat"),
			"codegraph.cmd",
			"codegraph.exe",
		}
	}
	return []string{filepath.Join("bin", "codegraph")}
}

// EnsureInit initialises CodeGraph for root when it has not been already, by
// running a bare `codegraph init` (no -i). That only creates the .codegraph/
// structure — fast and independent of repo size (~100ms) — because the actual
// indexing is done by `serve --mcp`'s daemon in the background once connected: the
// MCP handshake returns in a few hundred ms and symbols fill in shortly after,
// with CodeGraph flagging partial results as stale meanwhile. So startup never
// blocks on indexing, even for a huge monorepo.
//
// An existing .codegraph/ is left untouched — serve re-syncs it on connect and the
// file-watcher keeps it fresh thereafter. The init step is required because serve
// does NOT auto-create .codegraph/: without it, it runs in a degraded, no-index
// mode rather than building one.
func EnsureInit(ctx context.Context, bin, root string) error {
	if root == "" {
		return nil
	}
	if Initialized(root) {
		return nil // already initialised — serve re-syncs and the watcher keeps it fresh
	}
	ctx, cancel := context.WithTimeout(ctx, initTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "init", root)
	cmd.Cancel = func() error { proc.KillTree(cmd); return nil }
	cmd.WaitDelay = 3 * time.Second
	proc.HideWindow(cmd)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("codegraph init: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Initialized reports whether root already has CodeGraph's project state. Boot
// uses this to keep warm projects eager while moving first-time project setup to
// background startup, avoiding a cold MCP handshake on the app's critical path.
func Initialized(root string) bool {
	if root == "" {
		return false
	}
	fi, err := os.Stat(filepath.Join(root, ".codegraph"))
	return err == nil && fi.IsDir()
}

func expand(p string) string {
	p = os.ExpandEnv(p)
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[2:])
		}
	}
	return p
}

// isExec reports whether p is an existing regular file that looks runnable. On
// Unix it must carry an execute bit; on Windows existence is enough, since there
// runnability is decided by extension, not a mode bit.
func isExec(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return fi.Mode()&0o111 != 0
}
