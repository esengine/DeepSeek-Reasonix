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
// available, so the model knows to prefer them for symbol-level questions. The
// tool names are the model-visible registered names (mcp__codegraph__<tool>);
// TestSteerTextNamesMatchRegisteredTools fails if they drift from the daemon's
// actual tools so the model is never told to call a name it can't use.
const SteerText = `## Code Intelligence (codegraph)
You have codegraph tools for symbol-level code intelligence. For architecture questions, "how does X work", call graphs, symbol search, and impact analysis, prefer codegraph tools over grep/read_file:
- mcp__codegraph__context — entry points + related symbols + key code in one call (USE THIS FIRST for "how does X work")
- mcp__codegraph__search — find symbols by name (functions, types, interfaces)
- mcp__codegraph__callers / mcp__codegraph__callees — trace call chains
- mcp__codegraph__impact — what breaks if I change X
- mcp__codegraph__trace — full call path between two symbols
- mcp__codegraph__explore — walk the graph around a symbol (neighbours and relationships)
- mcp__codegraph__node — full detail for one symbol (definition, location, signature)
- mcp__codegraph__files — project file tree with symbol counts
- mcp__codegraph__status — code-intelligence index build/health status
Use grep/read_file for content search (comments, strings, config values) and when codegraph is not available.`

// BundleDirName is the optional directory, beside the reasonix executable, where
// an operator can place an unpacked CodeGraph bundle for offline use. Its
// launcher lives at <BundleDirName>/bin/codegraph, with the bundled node runtime
// and lib/ beside it; the launcher resolves those relative to itself, so the
// bundle is relocatable.
const BundleDirName = "codegraph"

// Resolve returns the absolute path to the CodeGraph launcher. Search order:
//  1. override — an explicit [codegraph].path from config (~ and ${VAR} expanded);
//  2. the per-version cache populated by Install;
//  3. a system-installed `codegraph` on PATH;
//  4. a bundle placed beside the executable (manual/offline fallback).
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
	base, ok := bundledBaseDir()
	if !ok {
		return "", false
	}
	for _, rel := range launcherNames() {
		if p := filepath.Join(base, rel); isExec(p) {
			return p, true
		}
	}
	return "", false
}

func bundledBaseDir() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Join(filepath.Dir(exe), BundleDirName), true
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
	proc.SetProcessGroupKill(cmd) // own group so Cancel→KillTree reaps the tree off Windows (no-op on Windows)
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

// IndexableRoot reports whether root is a real project directory CodeGraph can
// safely be pinned to. A filesystem root (a Windows drive root like C:\, a UNC
// share root, or the unix /) is rejected: serve --mcp walks its working
// directory, so a root cwd makes it index the whole volume — C:\Windows,
// C:\Program Files, everything — pinning gigabytes of RAM (#3747). An empty
// root is rejected too: there is nothing to pin a cwd-aware server to.
func IndexableRoot(root string) bool {
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	return filepath.Dir(abs) != abs // a filesystem root is its own parent
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
