package config

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func withPackageSource(t *testing.T, src func(home string) []InstalledPackage) {
	t.Helper()
	prev := SetInstalledPackages(src)
	t.Cleanup(func() { SetInstalledPackages(prev) })
}

func TestEnabledPackagesConcurrentCallersShareParses(t *testing.T) {
	var calls atomic.Int32
	withPackageSource(t, func(string) []InstalledPackage {
		calls.Add(1)
		time.Sleep(40 * time.Millisecond)
		return []InstalledPackage{{Name: "ecc"}}
	})
	r := RootsForHome(t.TempDir())

	const callers = 16
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range callers {
		wg.Go(func() {
			<-start
			if got := r.enabledPackages(); len(got) != 1 || got[0].Name != "ecc" {
				t.Errorf("enabledPackages = %+v", got)
			}
		})
	}
	close(start)
	wg.Wait()

	if n := calls.Load(); n > 2 {
		t.Fatalf("%d concurrent callers ran the package parse %d times, want at most 2", callers, n)
	}
}

func TestEnabledPackagesCallerNeverReadsStateOlderThanItsArrival(t *testing.T) {
	var generation atomic.Int32
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	withPackageSource(t, func(string) []InstalledPackage {
		seen := generation.Load()
		entered <- struct{}{}
		<-release
		return []InstalledPackage{{Name: "gen", Version: string(rune('0' + seen))}}
	})
	r := RootsForHome(t.TempDir())

	first := make(chan []InstalledPackage, 1)
	go func() { first <- r.enabledPackages() }()
	<-entered

	generation.Store(1)
	late := make(chan []InstalledPackage, 1)
	go func() { late <- r.enabledPackages() }()
	time.Sleep(20 * time.Millisecond)
	release <- struct{}{}
	<-entered
	release <- struct{}{}

	if got := <-first; got[0].Version != "0" {
		t.Fatalf("in-flight caller = %q", got[0].Version)
	}
	if got := <-late; got[0].Version != "1" {
		t.Fatalf("a caller that arrived after the state changed read generation %q, want 1", got[0].Version)
	}
}

func TestEnabledPackagesCallersDoNotShareMutableResults(t *testing.T) {
	withPackageSource(t, func(string) []InstalledPackage {
		return []InstalledPackage{{
			Name:       "ecc",
			SkillRoots: []string{"skills"},
			MCPServers: map[string]PackageMCPServer{"a": {Args: []string{"x"}}},
		}}
	})
	r := RootsForHome(t.TempDir())

	one := r.enabledPackages()
	one[0].SkillRoots[0] = "changed"
	one[0].MCPServers["a"].Args[0] = "changed"
	delete(one[0].MCPServers, "a")

	two := r.enabledPackages()
	if two[0].SkillRoots[0] != "skills" || two[0].MCPServers["a"].Args[0] != "x" {
		t.Fatalf("a caller's edits reached another caller: %+v", two[0])
	}
}
