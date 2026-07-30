package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/sessionruntime"
)

func TestSessionResourceConfigKeyTracksOwnedConfiguration(t *testing.T) {
	t.Parallel()

	base := config.Default()
	baseKey := SessionResourceConfigKey(base)
	if baseKey == "" || baseKey == "invalid:nil-config" || baseKey == "invalid:resource-config" {
		t.Fatalf("base resource config key = %q", baseKey)
	}

	unrelated := config.Default()
	unrelated.DefaultModel = "another/model"
	if got := SessionResourceConfigKey(unrelated); got != baseKey {
		t.Fatalf("model-only change altered resource key: got %q want %q", got, baseKey)
	}

	stalled := config.Default()
	stalledSeconds := stalled.BackgroundJobStalledWarningSeconds() + 1
	stalled.Tools.BackgroundJobs.StalledWarningSeconds = &stalledSeconds
	if got := SessionResourceConfigKey(stalled); got == baseKey {
		t.Fatal("background job configuration change did not alter resource key")
	}

	concurrency := config.Default()
	concurrency.Agent.MaxSubagentConcurrency = 1
	if got := SessionResourceConfigKey(concurrency); got == baseKey {
		t.Fatal("subagent scheduler configuration change did not alter resource key")
	}

	lspConfig := config.Default()
	lspConfig.LSP.Servers = map[string]config.LSPServer{
		"go": {Command: "custom-gopls"},
	}
	if got := SessionResourceConfigKey(lspConfig); got == baseKey {
		t.Fatal("LSP configuration change did not alter resource key")
	}
}

func TestSessionResourceConfigKeyIgnoresDisabledLSPSpecs(t *testing.T) {
	t.Parallel()

	first := config.Default()
	first.LSP.Enabled = false
	first.LSP.Servers = map[string]config.LSPServer{
		"go": {Command: "first-gopls"},
	}
	second := config.Default()
	second.LSP.Enabled = false
	second.LSP.Servers = map[string]config.LSPServer{
		"go": {Command: "second-gopls"},
	}
	if got, want := SessionResourceConfigKey(first), SessionResourceConfigKey(second); got != want {
		t.Fatalf("disabled LSP specs changed resource key: got %q want %q", got, want)
	}
}

func TestBuildRejectsReusedResourcesWithoutEnabledLSP(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.LSP.Enabled {
		t.Fatal("default config must enable LSP for this regression")
	}

	maxSubagents, maxWriters := agent.NormalizeConcurrencyLimits(
		cfg.Agent.MaxSubagentConcurrency,
		cfg.Agent.MaxParallelWriters,
	)
	resources := sessionruntime.New(sessionruntime.Config{
		Jobs:       jobs.NewManager(event.Discard),
		Scheduler:  agent.NewSubagentScheduler(maxSubagents, maxWriters),
		RuntimeKey: TokenModeFull,
		ConfigKey:  SessionResourceConfigKey(cfg),
		// LSP is deliberately absent: Build must not create an unowned manager
		// beside a reused session resource bag.
	})
	t.Cleanup(func() {
		resources.Release()
		<-resources.Done()
	})

	ctrl, err := Build(context.Background(), Options{
		WorkspaceRoot:    root,
		SessionDir:       t.TempDir(),
		Sink:             event.Discard,
		TokenMode:        TokenModeFull,
		SessionResources: resources,
	})
	if ctrl != nil {
		ctrl.Close()
		t.Fatal("Build returned a controller for an incomplete resource bag")
	}
	if err == nil || !strings.Contains(err.Error(), "missing enabled LSP manager") {
		t.Fatalf("Build error = %v, want missing enabled LSP manager", err)
	}
}
