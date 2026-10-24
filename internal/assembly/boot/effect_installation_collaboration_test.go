package boot

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

func installationEffectOptions(t *testing.T, home, root string, p provider.Provider) Options {
	t.Helper()
	ref := "fixture/installation"
	resolver := &provider.StaticResolver{Descriptors: []provider.Descriptor{{Ref: ref, Model: "installation", Tools: true, ContextWindow: 128000}}, Providers: map[string]provider.Provider{ref: p}}
	return Options{Home: home, WorkspaceRoot: root, Model: ref, ProviderResolver: resolver, Sink: event.Discard}
}

func TestEffectNewInstallationUsesAutomaticRootSelectionWithoutChangingOldConfig(t *testing.T) {
	t.Setenv("REASONIX_STATE_HOME", "")
	home, root := filepath.Join(t.TempDir(), "new-profile"), t.TempDir()
	writeFile(t, root, "base.txt", "base\n")
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "fixture")
	p := &presenceWaitingProvider{entered: make(chan struct{}), leave: make(chan struct{})}
	opts := installationEffectOptions(t, home, root, p)
	first, err := Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if policy := first.WriteLeaseSettings().Effective; policy == nil || policy.Collaboration != "auto" {
		t.Fatalf("fresh runtime did not use automatic strategy: %+v", policy)
	}
	bindPresenceSession(t, first)
	done := make(chan error, 1)
	finished := make(chan struct{})
	var once sync.Once
	defer func() { once.Do(func() { close(p.leave) }); <-finished }()
	go func() { defer close(finished); done <- first.Run(t.Context(), "hold source writer") }()
	select {
	case <-p.entered:
	case err := <-done:
		t.Fatalf("source writer did not start: %v", err)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	second, err := Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	bindPresenceSession(t, second)
	release, err := second.BeginRuntimeReplacement()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	prepared, err := PrepareRootExecution(t.Context(), second, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	cfg, err := config.RootsForHome(home).LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if automaticIsolationAvailability(opts, cfg) == nil {
		if prepared.Result == nil || prepared.Controller.WorkspaceRoot() == root {
			t.Fatal("fresh parallel chat did not choose an independent environment")
		}
	} else if prepared.Result != nil || prepared.Controller != second {
		t.Fatal("unavailable automatic environment did not retain shared transactions")
	}
	if _, err := os.Stat(filepath.Join(home, "config.toml")); !os.IsNotExist(err) {
		t.Fatal("first execution manufactured user configuration", err)
	}
	if body, err := os.ReadFile(filepath.Join(root, "base.txt")); err != nil || string(body) != "base\n" {
		t.Fatal("selection changed source", err)
	}
}

func TestEffectExistingInstallationRetainsCompatibilityStrategyAndOriginalChoices(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	body := []byte("# preserved user choices\n[agent]\nwrite_lease='optimistic'\nwrite_lease_max_wait_minutes=7\n")
	if err := os.WriteFile(filepath.Join(home, "config.toml"), body, 0600); err != nil {
		t.Fatal(err)
	}
	p := &presenceWaitingProvider{entered: make(chan struct{}), leave: make(chan struct{})}
	ctrl, err := Build(t.Context(), installationEffectOptions(t, home, root, p))
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	if policy := ctrl.WriteLeaseSettings().Effective; policy == nil || policy.Collaboration != "legacy" || !policy.Optimistic || policy.MaxWaitMinutes != 7 {
		t.Fatalf("old choices changed: %+v", policy)
	}
	after, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil || string(after) != string(body) {
		t.Fatal("runtime rewrote old config", err)
	}
}
