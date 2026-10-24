package boot

import (
	"path/filepath"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

func TestEffectRebuildRetainsExecutionPoolOwner(t *testing.T) {
	home, root := statedBootHome(t), t.TempDir()
	provider.Register("capacity-rebuild-effect", func(provider.Config) (provider.Provider, error) { return &effectRecordingProvider{}, nil })
	writeFile(t, root, "reasonix.toml", `default_model="test-model"
[agent]
collaboration="shared"
max_subagent_concurrency=1
max_parallel_writers=1
[codegraph]
enabled=false
[[providers]]
name="test-model"
kind="capacity-rebuild-effect"
model="x"
`)
	mirrorToUserConfigAt(t, home, root)
	approveWorkspaceAt(t, home, root)
	old, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	owner := old.DelegationScheduler()
	if owner == nil {
		t.Fatal("root did not retain admission owner")
	}
	initial := old.WriteLeaseSettings()
	if initial.Path != filepath.Join(home, "config.toml") || initial.Collaboration != "shared" || initial.Effective == nil || initial.Effective.Collaboration != "shared" || initial.Effective.MaxParallelWriters != 1 {
		t.Fatalf("build did not bind saved settings and effective policy to the stated home: %+v / %+v", initial, initial.Effective)
	}
	mode := "auto"
	if err := old.SaveWriteCoordinationSettings(control.WriteCoordinationEdit{Collaboration: &mode}); err != nil {
		t.Fatal(err)
	}
	edit, err := config.LoadForEditReadOnlyStrict(initial.Path)
	if err != nil {
		t.Fatal(err)
	}
	edit.Agent.MaxSubagentConcurrency, edit.Agent.MaxParallelWriters = 8, 4
	if err := edit.SaveToScope(initial.Path, config.RenderScopeUser); err != nil {
		t.Fatal(err)
	}
	if saved := old.WriteLeaseSettings(); saved.Collaboration != "auto" || saved.Effective.Collaboration != "shared" {
		t.Fatalf("saving changed a live runtime: %+v", saved)
	}
	release, err := owner.ReserveResident()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	endWriter := owner.TrackExecution(true)
	defer endWriter()
	res, err := Rebuild(t.Context(), old, Options{Home: home, WorkspaceRoot: root, Sink: event.Discard,
		Jobs: old.JobManager(), WorkspaceLease: old.WorkspaceLeaseOwner()})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Controller.CloseSharingJobs()
	if res.Controller.DelegationScheduler() != owner || res.Controller.DelegationScheduler().ActiveWriters() != 1 {
		t.Fatal("replacement created a separate execution allowance")
	}
	current := res.Controller.WriteLeaseSettings()
	if current.Effective == nil || current.Effective.Collaboration != "auto" || current.Effective.MaxSubagentConcurrency != 1 || current.Effective.MaxParallelWriters != 1 {
		t.Fatalf("rebuilt settings invented new capacity instead of retained owners: %+v", current.Effective)
	}
	endWriter()
	if res.Controller.DelegationScheduler().ActiveWriters() != 0 {
		t.Fatal("replacement lost old execution completion")
	}
}

func TestEffectLegacySettingsReportActualConcurrencyCaps(t *testing.T) {
	home, root := statedBootHome(t), t.TempDir()
	provider.Register("legacy-coordination-effect", func(provider.Config) (provider.Provider, error) { return &effectRecordingProvider{}, nil })
	writeFile(t, root, "reasonix.toml", `default_model="test-model"
[agent]
max_subagent_concurrency=2
max_parallel_writers=1
[codegraph]
enabled=false
[[providers]]
name="test-model"
kind="legacy-coordination-effect"
model="x"
`)
	mirrorToUserConfigAt(t, home, root)
	approveWorkspaceAt(t, home, root)
	c, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	got := c.WriteLeaseSettings()
	if got.Effective == nil || got.Effective.Collaboration != "legacy" || got.Effective.MaxSubagentConcurrency != 2 || got.Effective.MaxParallelWriters != 1 {
		t.Fatalf("legacy runtime's real caps were reported as absent or unlimited: %+v", got.Effective)
	}
	if c.DelegationScheduler() != nil {
		t.Fatal("reporting effective legacy limits changed the legacy retention policy")
	}
}
