package control

import (
	"os"
	"reflect"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/workspacelease"
)

func TestWriteCoordinationKeepsSavedAndEffectiveValuesIndependent(t *testing.T) {
	path := seedShellConfig(t)
	runtime := &WriteCoordinationRuntime{Collaboration: "shared", MaxSubagentConcurrency: 2, MaxParallelWriters: 1, TransactionWaitSeconds: 30, AutoIntegrate: true}
	c := New(Options{WriteCoordination: runtime})
	defer c.Close()
	runtime.Collaboration = "isolated"
	mode, optimistic, minutes := "auto", true, 17
	autoIntegrate := false
	if err := c.SaveWriteCoordinationSettings(WriteCoordinationEdit{Collaboration: &mode, Optimistic: &optimistic, MaxWaitMinutes: &minutes, AutoIntegrate: &autoIntegrate}); err != nil {
		t.Fatal(err)
	}
	got := c.WriteLeaseSettings()
	if got.AutoIntegrate || !got.Effective.AutoIntegrate {
		t.Fatal("saved integration setting rewrote effective policy")
	}
	if got.Collaboration != "auto" || !got.Optimistic || got.MaxWaitMinutes != 17 || got.Effective == nil || got.Effective.Collaboration != "shared" || got.Effective.MaxParallelWriters != 1 {
		t.Fatalf("saved choices changed effective runtime: %+v / %+v", got, got.Effective)
	}
	got.Effective.Collaboration = "isolated"
	if c.WriteLeaseSettings().Effective.Collaboration != "shared" {
		t.Fatal("settings response mutated runtime authority")
	}
	mode = "legacy"
	if err := c.SaveWriteCoordinationSettings(WriteCoordinationEdit{Collaboration: &mode}); err != nil {
		t.Fatal(err)
	}
	saved := config.LoadForEdit(path)
	savedMode, err := saved.CollaborationMode()
	if err != nil || savedMode != "legacy" || !saved.WriteLeaseOptimistic() || saved.Agent.WriteLeaseMaxWaitMinutes != 17 {
		t.Fatal("returning to compatibility mode rewrote legacy choices")
	}
	if _, ok := saved.Provider("deepseek-flash"); !ok {
		t.Fatal("coordination edit removed provider configuration")
	}
}

func TestWriteCoordinationRejectsInvalidEditWithoutWriting(t *testing.T) {
	path := seedShellConfig(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	c := &Controller{}
	bad, negative, optimistic := "future-protocol", -1, true
	for _, edit := range []WriteCoordinationEdit{
		{Collaboration: &bad, Optimistic: &optimistic},
		{MaxWaitMinutes: &negative, Optimistic: &optimistic},
	} {
		if err := c.SaveWriteCoordinationSettings(edit); err == nil {
			t.Fatal("invalid coordination edit accepted")
		}
		after, err := os.ReadFile(path)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatal("refused edit changed the user file")
		}
	}
}

func TestWriteCoordinationReportsRetainedOwners(t *testing.T) {
	cfg := config.Default()
	cfg.Agent.Collaboration = "shared"
	cfg.Agent.MaxSubagentConcurrency, cfg.Agent.MaxParallelWriters = 8, 4
	owner, err := workspacelease.New(t.TempDir(), t.TempDir(), nil, workspacelease.WithoutWholeWorkspaceHold(), workspacelease.WithMaxWait(7*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	scheduler := writeclaim.NewSubagentScheduler(2, 1)
	got := WriteCoordinationFromConfig(cfg, owner, scheduler)
	if got == nil || !got.Optimistic || got.MaxWaitMinutes != 7 || got.MaxSubagentConcurrency != 2 || got.MaxParallelWriters != 1 || got.TransactionWaitSeconds != 30 {
		t.Fatalf("reported saved limits instead of retained owners: %+v", got)
	}
}
