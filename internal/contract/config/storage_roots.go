// storage_roots.go — the closed set of directory roots the runtime writes into.
package config

import (
	"path/filepath"
	"slices"
	"sync"
)

// RootID names one storage root. The set is closed: every directory the runtime
// writes belongs to exactly one, so usage accounting, the storage panel, and
// relocation can enumerate roots instead of each keeping its own list of places
// to look.
type RootID string

const (
	// RootHome holds config.toml and the credentials .env — kilobytes that must
	// stay findable, so it is the one root a user cannot move: nothing can name
	// the location of the file that names locations.
	RootHome RootID = "home"
	// RootState holds transcripts, archives, stats, and per-project memory.
	RootState RootID = "state"
	// RootCache holds the listing catalogs and every other derived artefact.
	RootCache RootID = "cache"
	// RootWorktrees holds the isolated git worktrees Delivery checks out.
	RootWorktrees RootID = "worktrees"
	// RootLocks holds the cross-process locks two isolated runtimes must both
	// find, which is why it is pinned to the OS user rather than to a profile.
	RootLocks RootID = "locks"
)

// storageRoot declares one root. What separates roots — the variable that pins
// it, whether a user may move it, where it lands when nothing says — is data
// here, so resolveRoot below is the process's only precedence chain and a new
// root costs one entry rather than another resolver carrying its own fallbacks.
type storageRoot struct {
	id RootID
	// env pins this root from the environment. "" when no variable names it.
	env string
	// relocatable reports whether a user may move this root. False is a
	// contract, not an omission: locks must converge across instances, and home
	// cannot be named by the configuration it holds.
	relocatable bool
	// fallback answers when nothing overrides, against the binding being
	// resolved, so a stated home carries its derived roots along as
	// REASONIX_HOME does. Acyclic: home resolves without reading a root.
	fallback func(Roots) string
	// owns names what this root may claim inside a directory it shares — state
	// falls back to home, so on Windows they are one folder and a move of one
	// must not carry the other's credentials away. Empty means sole occupancy.
	owns []string
}

// storageRootTable is the declaration. Order is stable so listings and
// diagnostics read the same way every time. It is built inside a function
// because entries resolve each other — a root that derives from another names
// it here instead of repeating its chain, and a package-level initializer
// cannot hold a table whose values reach back into it.
var (
	rootsOnce  sync.Once
	rootsTable []storageRoot
)

func storageRootTable() []storageRoot {
	rootsOnce.Do(func() {
		rootsTable = []storageRoot{
			{id: RootHome, env: "REASONIX_HOME", relocatable: false, fallback: Roots.defaultHomeDir},
			{id: RootState, env: "REASONIX_STATE_HOME", relocatable: true,
				owns:     StateRootEntries,
				fallback: Roots.defaultStateDir},
			{id: RootCache, env: "REASONIX_CACHE_HOME", relocatable: true, fallback: Roots.defaultCacheDir},
			{id: RootWorktrees, env: "", relocatable: true, fallback: Roots.defaultWorktreeDir},
			{id: RootLocks, env: "", relocatable: false, fallback: Roots.defaultLocksDir,
				owns: []string{"workspace-leases", "repair-mutation-locks"}},
		}
	})
	return rootsTable
}

// Roots is the storage binding one call resolves against; the zero value is
// what the environment says, which every package-level path function still
// resolves. A stated home outranks REASONIX_HOME: the environment is how a
// host works out which home to use, and once it has, it says so — the same
// process may serve a second session that says something else.
type Roots struct {
	home string
}

// RootsForHome states the Reasonix home this binding resolves against. Empty
// follows the environment, so a caller with nothing to say passes on what it
// was given rather than working out a default of its own.
func RootsForHome(home string) Roots { return Roots{home: expandDirValue(home)} }

// Dir is the whole precedence chain: what holds this root in place, then what
// the configuration chose, then the root's own default. Every root runs it, so
// relocation, isolation, and the OS defaults compose the same way for all of
// them instead of each resolver spelling out its own order.
func (r Roots) Dir(id RootID) string {
	root, ok := lookupRoot(id)
	if !ok {
		return ""
	}
	if dir := r.pinnedRootDir(root); dir != "" {
		return dir
	}
	if dir := r.configuredRootDir(id); dir != "" {
		return dir
	}
	return root.fallback(r)
}

// pinnedRootDir is what holds a root in place before the configuration is
// read: for home, what the caller stated, otherwise the root's variable.
func (r Roots) pinnedRootDir(root storageRoot) string {
	if root.id == RootHome && r.home != "" {
		return r.home
	}
	if root.env == "" {
		return ""
	}
	return cleanEnvDir(root.env)
}

// pinnedHomeDir is the home only insofar as something deliberate holds it
// there — stated, or named by REASONIX_HOME. A caller that must tell an
// isolated runtime from a default one reads this, never the resolved home.
func (r Roots) pinnedHomeDir() string {
	if r.home != "" {
		return r.home
	}
	return cleanEnvDir("REASONIX_HOME")
}

// processRoots is the binding every package-level path function resolves
// against: whatever the environment says, with nothing stated.
func processRoots() Roots { return Roots{} }

func storageRootDir(id RootID) string { return processRoots().Dir(id) }

// Roots is the binding this configuration was loaded against. A caller holding
// a Config derives paths from it rather than from the process environment,
// which cannot tell which of two loaded homes this one came from.
func (c *Config) Roots() Roots {
	if c == nil {
		return Roots{}
	}
	return c.roots
}

func lookupRoot(id RootID) (storageRoot, bool) {
	table := storageRootTable()
	if i := slices.IndexFunc(table, func(r storageRoot) bool { return r.id == id }); i >= 0 {
		return table[i], true
	}
	return storageRoot{}, false
}

// RootIDs lists every declared root in table order. Usage accounting, the
// storage panel, and diagnostics enumerate this rather than each carrying a
// list, so declaring a root is the only edit a new one needs.
func RootIDs() []RootID {
	table := storageRootTable()
	out := make([]RootID, 0, len(table))
	for _, root := range table {
		out = append(out, root.id)
	}
	return out
}

// StateRootEntries is everything written at the top of the state root. An entry
// missing here is stranded by a relocation while the code reads only the new
// root, so the project list, instructions and memory looked deleted. The
// state-root guard test fails on a joined path this list does not name.
var StateRootEntries = []string{
	"sessions", "archive", "stats", "projects", "appearance", "themes", "repair",
	"serve-workspaces.json", "memory", "schedules", "trusted", "machine-id.key", "mcp-global-migration-v1", "commands",
	"REASONIX.md", "AGENTS.md", "CLAUDE.md", "REASONIX.local.md", "AGENTS.local.md", "CLAUDE.local.md",
}

// stateRootEntriesFirstMove is what relocation owned when it shipped.
var stateRootEntriesFirstMove = []string{"sessions", "archive", "stats", "projects"}

// StateRootEntriesEarlyMovesLeft is what a move made by an earlier version left
// in the previous root: every entry the first relocation did not own. The
// recovery and the panel notice both read it.
var StateRootEntriesEarlyMovesLeft = slices.DeleteFunc(slices.Clone(StateRootEntries), func(name string) bool {
	return slices.Contains(stateRootEntriesFirstMove, name)
})

// RootOwns names the entries a root may claim inside its directory, empty when
// it has the directory to itself. A move and a size report read this rather
// than assuming a root owns everything beneath it, because state shares a
// folder with home on a default Windows install.
func RootOwns(id RootID) []string {
	root, ok := lookupRoot(id)
	if !ok || len(root.owns) == 0 {
		return nil
	}
	return append([]string(nil), root.owns...)
}

// RootRelocatable reports whether a user may move this root. Callers that offer
// relocation read it rather than listing the immovable roots themselves.
func RootRelocatable(id RootID) bool {
	root, ok := lookupRoot(id)
	return ok && root.relocatable
}

// RootPinnedBy names the environment variable holding this root in place, or ""
// when none does. A surface that offers to move a root shows this instead of
// accepting an edit the environment would silently override.
func RootPinnedBy(id RootID) string {
	root, ok := lookupRoot(id)
	if !ok || root.env == "" {
		return ""
	}
	if cleanEnvDir(root.env) == "" {
		return ""
	}
	return root.env
}

// RootDir resolves one root's absolute directory, "" when it cannot be resolved
// (a machine with no usable home or cache location) — callers degrade rather
// than write to a relative path.
func RootDir(id RootID) string { return storageRootDir(id) }

// RootConfiguredDir reports the location the configuration chose for a root, ""
// when it chose none. A caller that has to tell a deliberate relocation from a
// default reads this rather than comparing RootDir against a default it works
// out for itself — and the two differ, because the environment outranks both.
func RootConfiguredDir(id RootID) string { return processRoots().configuredRootDir(id) }

// joinRoot appends sub to the first base that resolves, and answers "" when
// none does. The guard matters: filepath.Join("", "x") is the relative path
// "x", which would land runtime state in the working directory.
func joinRoot(base, sub string, more ...func() string) string {
	if base == "" {
		for _, next := range more {
			if base = next(); base != "" {
				break
			}
		}
	}
	if base == "" {
		return ""
	}
	return filepath.Join(base, sub)
}

// defaultHomeDir is where configuration lands when REASONIX_HOME is unset:
// the roaming profile on Windows, a dotfile directory elsewhere.
func (Roots) defaultHomeDir() string {
	if runtimeGOOS == "windows" {
		if dir := osUserConfigDir(); dir != "" {
			return filepath.Join(dir, "reasonix")
		}
		if home, err := osUserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, "AppData", "Roaming", "reasonix")
		}
		return ""
	}
	if home, err := osUserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".reasonix")
	}
	if dir := osUserConfigDir(); dir != "" {
		return filepath.Join(dir, "reasonix")
	}
	return ""
}

// defaultCacheDir is where derived data lands when no variable names it. A
// pinned home takes its cache along as a subdirectory; otherwise the cache is
// the OS location itself, which on Windows is the profile that does not roam.
func (r Roots) defaultCacheDir() string {
	if home := r.pinnedHomeDir(); home != "" {
		return filepath.Join(home, "cache")
	}
	return osCacheBase()
}

// defaultStateDir is the home root: state that nothing relocated lives beside
// the configuration that would have named its new location.
func (r Roots) defaultStateDir() string { return r.Dir(RootHome) }

// defaultLocksDir ignores the binding on purpose — two isolated runtimes must
// find the same lock, so it converges on the OS user's cache. The price is that
// a lock file outlives its use: deleting one breaks exclusion for a holder that
// still has it open, so every distinct home ever run leaves a few empty files
// here — a constant for an install, one set per run for a harness making homes.
func (Roots) defaultLocksDir() string { return osCacheBase() }

// osCacheBase is the OS user's cache location for this app. It reads no
// Reasonix variable on purpose: the locks root has to converge there even when
// two instances run under different profiles.
func osCacheBase() string {
	if dir := osUserCacheDir(); dir != "" {
		return filepath.Join(dir, "reasonix")
	}
	if runtimeGOOS == "windows" {
		if home, err := osUserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, "AppData", "Local", "reasonix")
		}
	}
	return ""
}

// defaultWorktreeDir places checkouts that are as large as the repositories
// they mirror. A state root someone pinned deliberately takes them along;
// otherwise Windows keeps them out of the roaming profile, where they would be
// copied between machines.
func (r Roots) defaultWorktreeDir() string {
	if dir := r.pinnedStateDir(); dir != "" {
		return filepath.Join(dir, "worktrees")
	}
	if runtimeGOOS == "windows" {
		return joinRoot(osCacheBase(), "worktrees")
	}
	return joinRoot(r.Dir(RootState), "worktrees")
}

// pinnedStateDir is the state root only insofar as a variable holds it there:
// its own, or the home root it otherwise follows. Roots that inherit a
// deliberate relocation consult this rather than the resolved value, which
// cannot tell a chosen location from a default one.
func (r Roots) pinnedStateDir() string {
	if dir := cleanEnvDir("REASONIX_STATE_HOME"); dir != "" {
		return dir
	}
	return r.pinnedHomeDir()
}
