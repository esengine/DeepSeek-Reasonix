package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Shell capability identifiers as surfaced to hosts (doctor, desktop settings).
const (
	HostCapabilityGit         = "git"
	ShellCapabilityBash       = "bash"
	ShellCapabilityGitBash    = "git-bash"
	ShellCapabilityPowerShell = "powershell"
	ShellCapabilityPwsh       = "pwsh"
	ShellCapabilityZsh        = "zsh"
	ShellCapabilitySh         = "sh"
)

// How a discovered shell was found. Discovery walks these in the fixed
// priority order documented on buildShellSnapshot; the winner's source is
// reported so settings surfaces can explain the resolution.
const (
	ShellSourceConfig     = "config"        // user-configured absolute path
	ShellSourcePath       = "path"          // executable found on PATH
	ShellSourceGitDerived = "git-derived"   // sibling of git.exe / git-bash.exe
	ShellSourceRegistry   = "registry"      // Git for Windows InstallPath
	ShellSourceStandard   = "standard-path" // Program Files and friends
)

// ExecutableCapability is one discovered host executable: whether it is
// usable, where it lives, how discovery found it, and why not when unavailable.
type ExecutableCapability struct {
	ID        string `json:"id"`
	Variant   string `json:"variant,omitempty"`
	Available bool   `json:"available"`
	Path      string `json:"path,omitempty"`
	Source    string `json:"source,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// ShellCapability keeps the interpreter inventory API source-compatible while
// Git is modeled separately through GitCapabilityForConfig.
type ShellCapability = ExecutableCapability

// shellInventoryTTL bounds how long a discovery snapshot stays trusted. A
// manual repair changes the filesystem without changing config, so the
// desktop's explicit re-detect action invalidates the inventory immediately
// instead of waiting for this expiry.
const shellInventoryTTL = 30 * time.Second

// shellSnapshot is one discovery pass: the environment inputs ResolveShell
// consumes (candidate lists, probe results) plus the capability report. It is
// immutable once built, so concurrent resolutions share the same probe cache
// instead of each spawning `bash -c true` health checks.
type shellSnapshot struct {
	key          string
	builtAt      time.Time
	goos         string
	lookPath     func(string) (string, error)
	exists       func(string) bool
	isWSL        func(string) bool
	bashCands    []string
	psCands      []string
	sources      map[string]string
	caps         []ShellCapability
	gitOnce      sync.Once
	git          ExecutableCapability
	gitPreflight func(string) bool
	gitProbe     func(string) bool
	probeFunc    func(string) bool
	probeMu      sync.Mutex
	probeCache   map[string]bool
}

func (s *shellSnapshot) probe(path string) bool {
	s.probeMu.Lock()
	defer s.probeMu.Unlock()
	if v, ok := s.probeCache[path]; ok {
		return v
	}
	probe := s.probeFunc
	if probe == nil {
		probe = probeBash
	}
	v := probe(path)
	s.probeCache[path] = v
	return v
}

// shellInventory is the process-wide single-entry, singleflight discovery
// cache. The entry is keyed by (GOOS, preference, configured shell path): a
// different preference or path misses and rebuilds rather than serving
// candidates attributed to the wrong shell kind. build is the discovery pass
// (injectable so the cache contract itself is testable).
type shellInventory struct {
	mu         sync.Mutex
	current    *shellSnapshot
	refreshing chan struct{}
	generation uint64
	build      func(goos, prefer, configPath string) *shellSnapshot
}

func newShellInventory() *shellInventory {
	return &shellInventory{build: buildShellSnapshot}
}

var defaultShellInventory = newShellInventory()

// InvalidateShellInventory drops the cached discovery snapshot. Call after a
// helper install finishes (or the user asks to re-detect) so the next
// ResolveShell re-probes instead of trusting the pre-install result.
func InvalidateShellInventory() {
	defaultShellInventory.invalidate()
}

func (inv *shellInventory) invalidate() {
	inv.mu.Lock()
	defer inv.mu.Unlock()
	inv.current = nil
	// Bump the generation so a refresh that started before the invalidation
	// cannot republish its already-stale result as the new snapshot.
	inv.generation++
}

// snapshot returns a discovery result for (goos, prefer, configPath), building
// one when the cached entry is missing, keyed differently, or older than the TTL.
// Concurrent callers coalesce onto the in-flight build (singleflight): the
// first caller builds, the rest wait and then re-check the cache.
func (inv *shellInventory) snapshot(goos, prefer, configPath string) *shellSnapshot {
	key := shellInventoryKey(goos, prefer, configPath)
	for {
		inv.mu.Lock()
		if inv.current != nil && inv.current.key == key && time.Since(inv.current.builtAt) < shellInventoryTTL {
			snap := inv.current
			inv.mu.Unlock()
			return snap
		}
		if inv.refreshing != nil {
			wait := inv.refreshing
			inv.mu.Unlock()
			<-wait
			continue
		}
		inv.refreshing = make(chan struct{})
		generation := inv.generation
		inv.mu.Unlock()

		snap := inv.build(goos, prefer, configPath)

		inv.mu.Lock()
		done := inv.refreshing
		inv.refreshing = nil
		if inv.generation == generation {
			inv.current = snap
		}
		close(done)
		inv.mu.Unlock()
		return snap
	}
}

func shellInventoryKey(goos, prefer, configPath string) string {
	return goos + "\x00" + strings.ToLower(strings.TrimSpace(prefer)) + "\x00" + strings.ToLower(filepath.Clean(strings.TrimSpace(configPath)))
}

// buildShellSnapshot performs one discovery pass. Windows candidate priority:
// a compatible configured Bash path first, then bash.exe on PATH (checked by
// resolveShell before any candidate), then bash.exe derived from the installed
// git.exe / git-bash.exe, then the Git for Windows registry InstallPath, then
// the standard install roots; pwsh and powershell remain the auto fallback.
func buildShellSnapshot(goos, prefer, configPath string) *shellSnapshot {
	snap := &shellSnapshot{
		key:          shellInventoryKey(goos, prefer, configPath),
		builtAt:      time.Now(),
		goos:         goos,
		lookPath:     exec.LookPath,
		exists:       fileExists,
		isWSL:        isWindowsWSLBash,
		gitPreflight: gitCandidatePreflight,
		gitProbe:     probeGit,
		probeFunc:    probeBash,
		sources:      map[string]string{},
		probeCache:   map[string]bool{},
	}
	if goos == "windows" {
		snap.bashCands, snap.sources = windowsBashCandidateSources(prefer, configPath, exec.LookPath, fileExists)
		snap.psCands = windowsPowerShellCandidates()
		snap.caps = windowsShellCapabilities(snap)
	} else {
		snap.caps = unixShellCapabilities(snap)
	}
	return snap
}

// ShellCapabilitiesForConfig reports the discovered interpreter inventory for
// a complete [tools.shell] selection. The preference is part of the cache key
// because a retained PowerShell path must never become an auto-detected Bash.
func ShellCapabilitiesForConfig(prefer, configPath string) []ShellCapability {
	snap := defaultShellInventory.snapshot(runtime.GOOS, prefer, configPath)
	out := make([]ShellCapability, len(snap.caps))
	copy(out, snap.caps)
	return out
}

// ShellCapabilitiesForPath reports the discovered interpreter inventory for
// this host while honoring an explicit [tools.shell] path. On Windows this is
// important for portable Git installations that are intentionally outside
// PATH, the registry, and standard install roots.
func ShellCapabilitiesForPath(configPath string) []ShellCapability {
	return ShellCapabilitiesForConfig("bash", configPath)
}

// ShellCapabilities is the config-free inventory used by callers such as
// doctor that do not own a loaded desktop configuration.
func ShellCapabilities() []ShellCapability {
	return ShellCapabilitiesForConfig("", "")
}

// GitCapabilityForConfig reports Git independently from the shell inventory.
// The full shell selection prevents a retained PowerShell path from being used
// as the root for Git-for-Windows discovery.
func GitCapabilityForConfig(prefer, configPath string) ExecutableCapability {
	snap := defaultShellInventory.snapshot(runtime.GOOS, prefer, configPath)
	snap.gitOnce.Do(func() { snap.git = discoverGitCapability(snap) })
	return snap.git
}

// GitCapabilityForPath reports Git independently from the shell inventory.
// configPath only helps portable Git for Windows discovery; Git never changes
// the configured or resolved shell.
func GitCapabilityForPath(configPath string) ExecutableCapability {
	return GitCapabilityForConfig("bash", configPath)
}

// shellCandidate pairs an ordered discovery path with the source bucket it
// came from, so the winning capability can explain itself.
type shellCandidate struct {
	path   string
	source string
}

// windowsBashCandidateSources lists Git-for-Windows bash.exe candidates in
// discovery priority order with their sources, deduplicated case-insensitively
// and with the WSL launcher excluded: the only bash.exe under %SystemRoot% is
// the WSL bootstrapper, and a native Windows workspace must never be routed
// into the Linux VM's /mnt/* view of itself. lookPath and exists are injected
// so the ordering is testable on any host.
func windowsBashCandidateSources(prefer, configPath string, lookPath func(string) (string, error), exists func(string) bool) ([]string, map[string]string) {
	var ordered []shellCandidate
	seen := map[string]bool{}
	push := func(path, source string) {
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if isWindowsWSLBash(path) {
			return
		}
		key := strings.ToLower(path)
		if seen[key] {
			return
		}
		seen[key] = true
		ordered = append(ordered, shellCandidate{path: path, source: source})
	}

	// 1. The user-configured absolute path, with git-bash.exe rewritten to the
	// real console binary bin\bash.exe (never MinTTY).
	if path := configuredWindowsBashPath(prefer, configPath, exists); path != "" {
		push(path, ShellSourceConfig)
	}
	// 2. bash.exe on PATH is resolved by resolveShell ahead of every candidate,
	// so it needs no entry here — only the capability attribution below.
	// 3. Derive the install root from a git.exe / git-bash.exe that IS on PATH.
	for _, name := range []string{"git-bash.exe", "git.exe", "git"} {
		if bin, err := lookPath(name); err == nil {
			for _, derived := range bashCandidatesFromGitBinary(bin) {
				push(derived, ShellSourceGitDerived)
			}
		}
	}
	// 4. Git for Windows registry InstallPath (HKLM and HKCU, native + WOW64).
	for _, root := range windowsRegistryGitRoots() {
		push(filepath.Join(root, "bin", "bash.exe"), ShellSourceRegistry)
		push(filepath.Join(root, "usr", "bin", "bash.exe"), ShellSourceRegistry)
	}
	// 5. Standard install roots: Program Files, per-user Programs, and the
	// fixed layouts Scoop and Chocolatey use.
	for _, root := range windowsStandardGitRoots() {
		push(filepath.Join(root, "bin", "bash.exe"), ShellSourceStandard)
		push(filepath.Join(root, "usr", "bin", "bash.exe"), ShellSourceStandard)
	}

	paths := make([]string, len(ordered))
	sources := map[string]string{}
	for i, c := range ordered {
		paths[i] = c.path
		sources[strings.ToLower(c.path)] = c.source
	}
	return paths, sources
}

// configuredWindowsBashPath admits an arbitrary explicit path only when the
// user forced Bash. Auto-detection accepts well-known Bash executable names but
// rejects a retained PowerShell path from an earlier preference.
func configuredWindowsBashPath(prefer, path string, exists func(string) bool) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(prefer)) {
	case "bash":
		return configuredShellPath("windows", ShellBash, path, exists, isWindowsWSLBash)
	case "powershell", "pwsh":
		return ""
	}
	base := strings.ToLower(strings.TrimSuffix(pathBase(path), ".exe"))
	if base != "bash" && base != "git-bash" {
		return ""
	}
	return configuredShellPath("windows", ShellBash, path, exists, isWindowsWSLBash)
}

// bashCandidatesFromGitBinary maps a git.exe or git-bash.exe location to the
// bash.exe files shipped beside it. Git for Windows keeps bash.exe in
// <root>\bin and <root>\usr\bin while git.exe lives in <root>\cmd,
// <root>\bin, <root>\mingw64\bin, or the root itself (portable layouts), so
// probe the binary's directory and its first two ancestors.
func bashCandidatesFromGitBinary(bin string) []string {
	var out []string
	dir := pathDir(bin)
	for range 3 {
		if dir == "" || dir == "." {
			break
		}
		out = append(out,
			filepath.Join(dir, "bin", "bash.exe"),
			filepath.Join(dir, "usr", "bin", "bash.exe"),
		)
		next := pathDir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	return out
}

// windowsStandardGitRoots lists directories that contain a "Git" subdirectory
// (winget/msi defaults and per-user installs) plus roots that already sit at a
// Git install tree (Scoop, Chocolatey).
func windowsStandardGitRoots() []string {
	var withGitSubdir []string
	for _, env := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		if v := os.Getenv(env); v != "" {
			withGitSubdir = append(withGitSubdir, filepath.Join(v, "Git"))
		}
	}
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		withGitSubdir = append(withGitSubdir, filepath.Join(v, "Programs", "Git"))
	}
	var atGitRoot []string
	if v := os.Getenv("ProgramData"); v != "" {
		atGitRoot = append(atGitRoot, filepath.Join(v, "chocolatey", "lib", "git", "tools"))
	}
	if v := os.Getenv("USERPROFILE"); v != "" {
		atGitRoot = append(atGitRoot, filepath.Join(v, "scoop", "apps", "git", "current"))
	}
	return append(withGitSubdir, atGitRoot...)
}

// windowsShellCapabilities reports git-bash plus both PowerShells, walking the
// same order resolveShell's auto path uses so the reported winner matches what
// a fresh session would actually bind.
func windowsShellCapabilities(snap *shellSnapshot) []ShellCapability {
	caps := make([]ShellCapability, 0, 3)

	gitBash := ShellCapability{ID: ShellCapabilityGitBash, Variant: "git-for-windows"}
	acceptCandidate := func(path, source string) bool {
		if path == "" || !snap.exists(path) || snap.isWSL(path) || !snap.probe(path) {
			return false
		}
		gitBash.Available = true
		gitBash.Path = path
		gitBash.Source = source
		return true
	}
	// Match ResolveShell's Windows priority: explicit compatible config, PATH,
	// then Git-derived, registry, and standard locations.
	for _, path := range snap.bashCands {
		if snap.sources[strings.ToLower(path)] == ShellSourceConfig && acceptCandidate(path, ShellSourceConfig) {
			break
		}
	}
	if !gitBash.Available {
		if path, err := snap.lookPath("bash"); err == nil {
			acceptCandidate(path, ShellSourcePath)
		}
	}
	if !gitBash.Available {
		for _, path := range snap.bashCands {
			source := snap.sources[strings.ToLower(path)]
			if source != ShellSourceConfig && acceptCandidate(path, source) {
				break
			}
		}
	}
	if !gitBash.Available {
		gitBash.Reason = "not-installed"
	}
	caps = append(caps, gitBash)

	caps = append(caps, windowsPowerShellCapability(snap, ShellCapabilityPwsh, []string{"pwsh", "pwsh.exe"}, "pwsh"))
	caps = append(caps, windowsPowerShellCapability(snap, ShellCapabilityPowerShell, []string{"powershell", "powershell.exe"}, "powershell"))
	return caps
}

func windowsPowerShellCapability(snap *shellSnapshot, id string, names []string, base string) ShellCapability {
	cap := ShellCapability{ID: id}
	for _, p := range snap.psCands {
		fileBase := strings.ToLower(pathBase(p))
		fileBase = strings.TrimSuffix(fileBase, ".exe")
		if fileBase != base {
			continue
		}
		if snap.exists(p) {
			cap.Available = true
			cap.Path = p
			cap.Source = ShellSourceStandard
			return cap
		}
	}
	for _, name := range names {
		if p, err := snap.lookPath(name); err == nil {
			cap.Available = true
			cap.Path = p
			cap.Source = ShellSourcePath
			return cap
		}
	}
	cap.Reason = "not-installed"
	return cap
}
