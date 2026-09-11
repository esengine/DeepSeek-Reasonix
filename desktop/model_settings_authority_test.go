package main

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
)

func TestModelSettingsFinalAuthorityFailurePreservesRuntimeAndRecovers(t *testing.T) {
	isolateDesktopUserDirs(t)
	oldRef, nextRef := configureSwitchableDefaultModels(t)
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	tab := modelSettingsBootTab(t, app, "authority", t.TempDir(), oldRef)
	app.activeTabID = tab.ID
	old := tab.Ctrl.(*control.Controller)
	if err := tab.ensureSessionLease(old.SessionPath()); err != nil {
		t.Fatal(err)
	}
	if err := bindTabWriteAuthority(tab, old); err != nil {
		t.Fatal(err)
	}
	if err := old.Snapshot(); err != nil {
		t.Fatal(err)
	}
	history := old.History()
	if err := app.SetPlannerModel(nextRef); err != nil {
		t.Fatal(err)
	}
	lost := false
	app.rebindCandidateHook = func(stage string) error {
		if stage == "settings_before_authority" && !lost {
			tab.sessionLeaseMu.Lock()
			lease := tab.sessionLease
			tab.sessionLeaseMu.Unlock()
			if lease == nil {
				t.Fatal("candidate has no lease")
			}
			lease.Release() // exercise real authority issuance failure, not a mock error
			lost = true
		}
		return nil
	}
	result := app.RetryModelSettingsApplication(tab.ID)
	if !lost || result.Application != "failed" || tab.Ctrl != old || tab.model != oldRef || !reflect.DeepEqual(old.History(), history) {
		t.Fatalf("failed final bind changed the runtime: lost=%v result=%+v", lost, result)
	}
	if err := old.Run(context.Background(), "must be refused before a provider call"); !errors.Is(err, agent.ErrSessionWriteAuthorityStale) {
		t.Fatalf("lost lease did not fail closed: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(old.SessionDir(), "*.jsonl"))
	var transcripts []string
	for _, path := range files {
		if !strings.HasSuffix(path, ".events.jsonl") && !strings.HasSuffix(path, ".turns.jsonl") {
			transcripts = append(transcripts, path)
		}
	}
	if err != nil || len(transcripts) != 1 || transcripts[0] != old.SessionPath() {
		t.Fatalf("failure created a recovery transcript: %v %v", files, err)
	}
	result = app.RetryModelSettingsApplication(tab.ID)
	if result.Application != "applied" || tab.Ctrl == old {
		t.Fatalf("retry did not restore ownership: %+v", result)
	}
	if err := tab.Ctrl.Snapshot(); err != nil {
		t.Fatal("replacement cannot persist after retry", err)
	}
}
