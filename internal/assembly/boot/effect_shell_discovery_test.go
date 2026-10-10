package boot

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/safety/sandbox"
)

// slowShellBuild assembles through the real Build with a bash on PATH whose
// launch probe is the injected one, and returns what the start-up trace saw.
func slowShellBuild(t *testing.T, kind string, tune func(*sandbox.ShellDiscovery)) (phases []string, took time.Duration) {
	t.Helper()
	dir := robustTempDir(t)
	t.Chdir(dir)
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return testutil.NewMock("m"), nil })
	writeFile(t, dir, "reasonix.toml", "default_model = \"t\"\n[[providers]]\nname = \"t\"\nkind = \""+kind+"\"\nmodel = \"x\"\n")
	approveWorkspace(t, dir)
	begin := time.Now()
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, tuneShell: tune, OnPhase: func(p Phase) { phases = append(phases, p.Name) }})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { ctrl.Close() })
	return phases, time.Since(begin)
}

func fakeBashOnPath(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	name := "bash"
	if runtime.GOOS == "windows" {
		name = "bash.exe"
	}
	if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestEffectHungShellProbeDoesNotHoldTheAssemblyPastItsBudget(t *testing.T) {
	isolateConfigHome(t)
	fakeBashOnPath(t)
	release := make(chan struct{})
	defer close(release)
	phases, took := slowShellBuild(t, "hung-shell", func(d *sandbox.ShellDiscovery) {
		d.Probe = func(string) bool { <-release; return true }
		d.Budget, d.Hedge = 400*time.Millisecond, 50*time.Millisecond
	})
	if took > 10*time.Second {
		t.Fatalf("Build took %v behind a hung bash probe, want the discovery budget", took)
	}
	want := []string{"provider", "shell", "prompt", "environment", "memory", "skills"}
	at := -1
	for _, name := range want {
		i := slices.Index(phases, name)
		if i <= at {
			t.Fatalf("trace %v lacks %q in order after index %d", phases, name, at)
		}
		at = i
	}
}

func TestEffectProvenShellIsNotLaunchedAgainByTheNextStart(t *testing.T) {
	isolateConfigHome(t)
	fakeBashOnPath(t)
	var launches atomic.Int32
	tune := func(d *sandbox.ShellDiscovery) {
		d.Probe = func(string) bool {
			launches.Add(1)
			time.Sleep(300 * time.Millisecond)
			return true
		}
		d.Budget, d.Hedge = 5*time.Second, time.Second
	}

	_, first := slowShellBuild(t, "proof-shell-a", tune)
	if launches.Load() != 1 {
		t.Fatalf("first start launched %d probes, want 1", launches.Load())
	}
	_, second := slowShellBuild(t, "proof-shell-b", tune)
	if launches.Load() != 1 {
		t.Fatalf("second start launched the probe again (%d launches)", launches.Load())
	}
	if second >= first {
		t.Fatalf("second start %v not faster than first %v", second, first)
	}
}
