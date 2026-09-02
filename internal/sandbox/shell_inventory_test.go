package sandbox

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func fakeLookPath(found map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if p, ok := found[name]; ok {
			return p, nil
		}
		return "", exec.ErrNotFound
	}
}

// TestBashCandidatesFromGitBinary pins the git.exe → bash.exe derivation: the
// install root's bin\bash.exe and usr\bin\bash.exe, walking up from the binary
// directory so cmd\, mingw64\bin, and root-level layouts all resolve.
// filepath.Join uses the host separator, so compare with separators
// normalized — the derivation itself only runs on Windows in production.
func TestBashCandidatesFromGitBinary(t *testing.T) {
	got := bashCandidatesFromGitBinary(`C:\Program Files\Git\cmd\git.exe`)
	want := []string{
		`C:\Program Files\Git\cmd\bin\bash.exe`,
		`C:\Program Files\Git\cmd\usr\bin\bash.exe`,
		`C:\Program Files\Git\bin\bash.exe`,
		`C:\Program Files\Git\usr\bin\bash.exe`,
		`C:\Program Files\bin\bash.exe`,
		`C:\Program Files\usr\bin\bash.exe`,
	}
	norm := func(p string) string { return strings.ReplaceAll(filepath.Clean(p), "/", `\`) }
	if len(got) != len(want) {
		t.Fatalf("derived %d candidates, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if norm(got[i]) != want[i] {
			t.Errorf("candidate[%d] = %q, want %q", i, norm(got[i]), want[i])
		}
	}
}

// TestWindowsBashCandidateOrder pins discovery priority: the configured path
// first (with git-bash.exe rewritten to bin\bash.exe), then candidates derived
// from a PATH-visible git.exe, deduplicated case-insensitively. PATH bash.exe
// itself is resolved by resolveShell ahead of every candidate, so it must not
// appear in the list.
func TestWindowsBashCandidateOrder(t *testing.T) {
	lookPath := fakeLookPath(map[string]string{
		"bash":    `C:\Windows\System32\bash.exe`,
		"git.exe": `D:\Tools\Git\mingw64\bin\git.exe`,
	})
	// The configured git-bash.exe rewrites to a sibling bin\bash.exe that
	// "exists", so the config candidate survives sanitization.
	exists := func(p string) bool { return strings.EqualFold(p, `E:\Portable\Git\bin\bash.exe`) }

	got, sources := windowsBashCandidateSources("auto", `E:\Portable\Git\git-bash.exe`, lookPath, exists)
	if len(got) == 0 {
		t.Fatal("expected candidates")
	}
	if got[0] != `E:\Portable\Git\bin\bash.exe` {
		t.Errorf("first candidate = %q, want the rewritten config path", got[0])
	}
	if sources[strings.ToLower(got[0])] != ShellSourceConfig {
		t.Errorf("first candidate source = %q, want %q", sources[strings.ToLower(got[0])], ShellSourceConfig)
	}
	gitRoot := filepath.Clean(`D:\Tools\Git`)
	wantGitDerived := filepath.Join(gitRoot, "bin", "bash.exe")
	found := false
	for _, p := range got {
		if p == wantGitDerived && sources[strings.ToLower(p)] == ShellSourceGitDerived {
			found = true
		}
	}
	if !found {
		t.Errorf("derived candidate %q with source %q missing from %v", wantGitDerived, ShellSourceGitDerived, got)
	}
	for _, p := range got {
		if strings.EqualFold(p, `C:\Windows\System32\bash.exe`) {
			t.Errorf("PATH bash must not be listed as a candidate: %v", got)
		}
	}
}

func TestWindowsShellCapabilitiesPreferConfiguredBashOverPath(t *testing.T) {
	const (
		configured = `E:\Portable\Git\bin\bash.exe`
		onPath     = `C:\Program Files\Git\bin\bash.exe`
	)
	snap := &shellSnapshot{
		lookPath:   fakeLookPath(map[string]string{"bash": onPath}),
		exists:     func(path string) bool { return path == configured || path == onPath },
		isWSL:      func(string) bool { return false },
		bashCands:  []string{configured},
		sources:    map[string]string{strings.ToLower(configured): ShellSourceConfig},
		probeFunc:  func(string) bool { return true },
		probeCache: map[string]bool{},
	}
	caps := windowsShellCapabilities(snap)
	if len(caps) == 0 || !caps[0].Available || caps[0].Path != configured || caps[0].Source != ShellSourceConfig {
		t.Fatalf("Git Bash capability = %+v, want configured path before PATH", caps)
	}
}

func TestConfiguredWindowsBashPathMatchesPreference(t *testing.T) {
	exists := func(path string) bool {
		return strings.EqualFold(path, `E:\Portable\Git\bin\bash.exe`)
	}
	tests := []struct {
		name   string
		prefer string
		path   string
		want   string
	}{
		{"auto accepts bash", "auto", `E:\Portable\Git\bin\bash.exe`, `E:\Portable\Git\bin\bash.exe`},
		{"auto rewrites git bash", "auto", `E:\Portable\Git\git-bash.exe`, `E:\Portable\Git\bin\bash.exe`},
		{"auto rejects stale pwsh", "auto", `C:\Program Files\PowerShell\7\pwsh.exe`, ""},
		{"powershell rejects stale bash", "powershell", `E:\Portable\Git\bin\bash.exe`, ""},
		{"forced bash accepts custom wrapper", "bash", `E:\Custom\shell-wrapper.exe`, `E:\Custom\shell-wrapper.exe`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := configuredWindowsBashPath(test.prefer, test.path, exists); got != test.want {
				t.Fatalf("configuredWindowsBashPath(%q, %q) = %q, want %q", test.prefer, test.path, got, test.want)
			}
		})
	}
}

// TestWindowsBashCandidatesDedupeAndWSLExclusion exercises dedupe (the same
// path reachable through two buckets) and the %SystemRoot% WSL launcher
// exclusion, both of which need a Windows host to observe.
func TestWindowsBashCandidatesDedupeAndWSLExclusion(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("WSL launcher detection reads %SystemRoot% and only fires on Windows")
	}
	t.Setenv("SystemRoot", `C:\Windows`)
	lookPath := fakeLookPath(map[string]string{
		"git.exe": `C:\Program Files\Git\cmd\git.exe`,
	})
	got, _ := windowsBashCandidateSources("auto", "", lookPath, fileExists)
	seen := map[string]bool{}
	for _, p := range got {
		key := strings.ToLower(p)
		if seen[key] {
			t.Errorf("duplicate candidate %q", p)
		}
		seen[key] = true
		if isWindowsWSLBash(p) {
			t.Errorf("WSL launcher %q must be excluded", p)
		}
	}
}

// TestShellInventoryCache covers the singleflight cache contract: concurrent
// lookups share one build, the entry survives within the TTL, a different
// config path rebuilds, and InvalidateShellInventory forces a rebuild while a
// stale in-flight build cannot republish itself.
func TestShellInventoryCache(t *testing.T) {
	inv := newShellInventory()
	var builds sync.Mutex
	buildCount := 0
	release := make(chan struct{})
	inv.build = func(goos, prefer, configPath string) *shellSnapshot {
		builds.Lock()
		buildCount++
		builds.Unlock()
		if configPath == "slow" {
			<-release
		}
		return &shellSnapshot{key: shellInventoryKey(goos, prefer, configPath), builtAt: time.Now(), probeCache: map[string]bool{}}
	}

	// Singleflight: concurrent snapshot() calls for one key build once.
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			_ = inv.snapshot("windows", "auto", "")
		})
	}
	// The first build ("") does not block, so wait for the burst to finish.
	wg.Wait()
	builds.Lock()
	if buildCount != 1 {
		builds.Unlock()
		t.Fatalf("concurrent lookups built %d times, want 1", buildCount)
	}
	builds.Unlock()

	// Within the TTL the cached entry is served without a rebuild.
	_ = inv.snapshot("windows", "auto", "")
	builds.Lock()
	count := buildCount
	builds.Unlock()
	if count != 1 {
		t.Fatalf("TTL hit rebuilt: %d", count)
	}

	// A different preference is a different cache key even when the configured
	// path stays the same, because only Bash preferences may consume that path.
	_ = inv.snapshot("windows", "bash", "")
	builds.Lock()
	count = buildCount
	builds.Unlock()
	if count != 2 {
		t.Fatalf("different preference should rebuild: %d", count)
	}

	// A different config path is also a different cache key.
	_ = inv.snapshot("windows", "auto", "other")
	builds.Lock()
	count = buildCount
	builds.Unlock()
	if count != 3 {
		t.Fatalf("different path should rebuild: %d", count)
	}

	// Invalidation forces a rebuild and drops a stale in-flight result.
	var inflight sync.WaitGroup
	inflight.Go(func() {
		_ = inv.snapshot("windows", "auto", "slow")
	})
	// Wait until the slow build has started, then invalidate under it.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		inv.mu.Lock()
		refreshing := inv.refreshing != nil
		inv.mu.Unlock()
		if refreshing {
			break
		}
		time.Sleep(time.Millisecond)
	}
	inv.invalidate()
	close(release)
	inflight.Wait()

	inv.mu.Lock()
	current := inv.current
	inv.mu.Unlock()
	if current != nil {
		t.Fatalf("stale in-flight build was cached under key %q after invalidation", current.key)
	}

	// And the next lookup builds fresh.
	_ = inv.snapshot("windows", "auto", "")
	builds.Lock()
	count = buildCount
	builds.Unlock()
	if count != 5 {
		t.Fatalf("post-invalidation lookup should rebuild: %d builds", count)
	}
}

// TestSnapshotProbeCachesResults proves repeated resolutions inside one
// snapshot do not re-probe the same path.
func TestSnapshotProbeCachesResults(t *testing.T) {
	snap := &shellSnapshot{probeFunc: func(string) bool { return true }, probeCache: map[string]bool{}}
	if !snap.probe(`C:\Git\bin\bash.exe`) {
		t.Fatal("probe on a non-Windows host always succeeds")
	}
	snap.probeMu.Lock()
	cached, ok := snap.probeCache[`C:\Git\bin\bash.exe`]
	snap.probeMu.Unlock()
	if !ok || !cached {
		t.Fatalf("probe result not cached: %v", snap.probeCache)
	}
}

func TestUnixShellCapabilitiesReportsBashZshAndSh(t *testing.T) {
	snap := &shellSnapshot{
		lookPath: fakeLookPath(map[string]string{"zsh": "/opt/homebrew/bin/zsh"}),
		exists: func(path string) bool {
			return path == "/bin/sh"
		},
	}
	caps := unixShellCapabilities(snap)
	if len(caps) != 3 {
		t.Fatalf("capabilities = %+v, want bash, zsh, and sh", caps)
	}
	byID := map[string]ShellCapability{}
	for _, capability := range caps {
		byID[capability.ID] = capability
	}
	if bash := byID[ShellCapabilityBash]; bash.Available || bash.Reason != "not-found" {
		t.Fatalf("bash = %+v, want unavailable with a reason", bash)
	}
	if zsh := byID[ShellCapabilityZsh]; !zsh.Available || zsh.Path != "/opt/homebrew/bin/zsh" || zsh.Source != ShellSourcePath {
		t.Fatalf("zsh = %+v, want PATH capability", zsh)
	}
	if sh := byID[ShellCapabilitySh]; !sh.Available || sh.Path != "/bin/sh" || sh.Source != ShellSourceStandard {
		t.Fatalf("sh = %+v, want standard-path capability", sh)
	}
}

func TestDiscoverGitCapabilityIsIndependentFromShells(t *testing.T) {
	snap := &shellSnapshot{
		goos:     "darwin",
		lookPath: fakeLookPath(map[string]string{}),
		exists: func(path string) bool {
			return path == "/opt/homebrew/bin/git"
		},
	}
	got := discoverGitCapability(snap)
	if !got.Available || got.ID != HostCapabilityGit || got.Path != "/opt/homebrew/bin/git" || got.Source != ShellSourceStandard {
		t.Fatalf("Git capability = %+v, want independent Homebrew standard path", got)
	}
}

func TestDiscoverGitCapabilityRejectsUnusableShim(t *testing.T) {
	snap := &shellSnapshot{
		goos:     "darwin",
		lookPath: fakeLookPath(map[string]string{"git": "/usr/bin/git"}),
		exists:   func(path string) bool { return path == "/usr/bin/git" },
		gitProbe: func(string) bool { return false },
	}
	got := discoverGitCapability(snap)
	if got.Available || got.Reason != "not-usable" {
		t.Fatalf("Git capability = %+v, want unusable shim rejected", got)
	}
}

func TestDiscoverGitCapabilityPreflightRejectsAppleShimWithoutRunningIt(t *testing.T) {
	preflightCalls := 0
	probeCalls := 0
	snap := &shellSnapshot{
		goos:     "darwin",
		lookPath: fakeLookPath(map[string]string{"git": "/usr/bin/git"}),
		exists:   func(path string) bool { return path == "/usr/bin/git" },
		gitPreflight: func(path string) bool {
			preflightCalls++
			return false
		},
		gitProbe: func(string) bool {
			probeCalls++
			return true
		},
	}
	got := discoverGitCapability(snap)
	if got.Available || got.Reason != "not-usable" {
		t.Fatalf("Git capability = %+v, want inactive Apple shim rejected", got)
	}
	if preflightCalls != 1 {
		t.Fatalf("Apple shim preflight calls = %d, want one cached decision", preflightCalls)
	}
	if probeCalls != 0 {
		t.Fatalf("git --version probe ran %d times after preflight rejection", probeCalls)
	}
}

func TestGitCandidatesFromWindowsBashFindInstallRoot(t *testing.T) {
	got := gitCandidatesFromWindowsBash(`C:\Program Files\Git\bin\bash.exe`)
	norm := func(path string) string {
		return strings.ToLower(strings.ReplaceAll(filepath.Clean(path), `\`, "/"))
	}
	want := norm(`C:\Program Files\Git\cmd\git.exe`)
	found := false
	for _, candidate := range got {
		if norm(candidate) == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("candidates = %v, missing %q", got, want)
	}
}

// TestShellCapabilitiesShape ensures the exported capability report matches
// the platform: git-bash plus both PowerShells on Windows, and bash/zsh/sh on
// Unix — with unavailable entries carrying a reason, never an error.
func TestShellCapabilitiesShape(t *testing.T) {
	caps := ShellCapabilities()
	if len(caps) == 0 {
		t.Fatal("ShellCapabilities returned no entries")
	}
	ids := map[string]bool{}
	for _, cap := range caps {
		ids[cap.ID] = true
		if cap.Available && cap.Path == "" {
			t.Errorf("capability %q is available without a path", cap.ID)
		}
		if !cap.Available && cap.Reason == "" {
			t.Errorf("unavailable capability %q must carry a reason", cap.ID)
		}
	}
	if runtime.GOOS == "windows" {
		for _, id := range []string{ShellCapabilityGitBash, ShellCapabilityPowerShell, ShellCapabilityPwsh} {
			if !ids[id] {
				t.Errorf("Windows report missing %q: %v", id, caps)
			}
		}
		if ids[ShellCapabilityBash] {
			t.Errorf("Windows report must not advertise a generic bash id: %v", caps)
		}
		for _, id := range []string{ShellCapabilityZsh, ShellCapabilitySh} {
			if ids[id] {
				t.Errorf("Windows report must not advertise %q: %v", id, caps)
			}
		}
	} else {
		for _, id := range []string{ShellCapabilityBash, ShellCapabilityZsh, ShellCapabilitySh} {
			if !ids[id] {
				t.Errorf("non-Windows report missing %q: %v", id, caps)
			}
		}
		for _, id := range []string{ShellCapabilityGitBash, ShellCapabilityPowerShell, ShellCapabilityPwsh} {
			if ids[id] {
				t.Errorf("non-Windows report must not advertise %q: %v", id, caps)
			}
		}
	}
}
