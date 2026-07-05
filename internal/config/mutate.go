package config

import "sync"

// userEditMu serializes in-process read-modify-write cycles on the user config
// file. LoadForEdit+SaveTo is not atomic: two concurrent editors each load,
// mutate their own copy, and save — the second save silently drops the first
// writer's fields (e.g. bot auto-session mappings vs. a settings-page save).
// Cross-process writers still race. Every runtime in-process editor takes this
// lock around its load→mutate→save cycle: bot mapping/pairing persistence,
// desktop settings and MCP writers, serve effort switches, controller skill
// toggles, and the CLI TUI / `reasonix config` write paths. One writer stays
// unlocked on purpose: the single-threaded `reasonix setup` wizard (an
// interactive whole-file write with no concurrent editors in that process).
// Desktop's read-only config loads (tray/view/bot-runtime paths) never write:
// they apply legacy migrations in memory only, and the migrated form reaches
// disk through the first locked write path (loadDesktopUserConfigForEdit,
// called with this lock held).
var userEditMu sync.Mutex

// LockUserConfigEdits acquires the process-wide user-config edit lock and
// returns the unlock. Hold it across the full LoadForEdit→mutate→SaveTo
// cycle; do not hold it across controller rebuilds or other slow non-config
// work, and never call another LockUserConfigEdits taker while holding it.
func LockUserConfigEdits() func() {
	userEditMu.Lock()
	return userEditMu.Unlock
}
