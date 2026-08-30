package main

import (
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/control"
)

func TestSaveTabSessionMetaPersistsDeliveryFloor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	meta, err := agent.EnsureBranchMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	meta.AgentPreset = boot.AgentPresetStandard
	meta.TokenMode = boot.TokenModeFull
	if err := agent.SaveBranchMetaPreserveUpdated(path, meta); err != nil {
		t.Fatal(err)
	}

	if err := saveTabSessionMetaSnapshot(tabSessionMetaSnapshot{path: path, qualityFloor: control.QualityFloorDelivery}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta = %+v, %v, %v", got, ok, err)
	}
	if got.QualityFloor != control.QualityFloorDelivery {
		t.Fatalf("persisted floor = %q, want delivery", got.QualityFloor)
	}
	if got.AgentPreset != boot.AgentPresetDelivery {
		t.Fatalf("dual-write preset = %q, want delivery", got.AgentPreset)
	}
	if got.TokenMode != boot.TokenModeDelivery {
		t.Fatalf("dual-write tokenMode = %q, want delivery", got.TokenMode)
	}
}

func TestSaveTabSessionMetaStandardWritesNoFloor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "standard.jsonl")
	if _, err := agent.EnsureBranchMeta(path); err != nil {
		t.Fatal(err)
	}
	if err := saveTabSessionMetaSnapshot(tabSessionMetaSnapshot{path: path, qualityFloor: control.QualityFloorStandard}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta = %+v, %v, %v", got, ok, err)
	}
	if got.QualityFloor != "" {
		t.Fatalf("standard floor must not persist a value, got %q", got.QualityFloor)
	}
}

func TestTabSessionProfileFromMetaDecodesLegacyDelivery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.jsonl")
	meta, err := agent.EnsureBranchMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	meta.AgentPreset = boot.AgentPresetDelivery
	meta.TokenMode = boot.TokenModeDelivery
	profile := tabSessionProfileFromMeta(path, meta)
	if profile.qualityFloor != control.QualityFloorDelivery {
		t.Fatalf("legacy decode floor = %q, want delivery", profile.qualityFloor)
	}
	tab := &WorkspaceTab{}
	applyTabSessionProfile(tab, profile)
	if got := tab.qualityFloor; got != control.QualityFloorDelivery {
		t.Fatalf("legacy delivery must reach the tab floor: got %q", got)
	}
	if got := currentTabTokenMode(tab); got != boot.TokenModeDelivery {
		t.Fatalf("derived tokenMode = %q, want delivery", got)
	}
}

func TestTabSessionProfileFromMetaFoldsLegacyLight(t *testing.T) {
	path := filepath.Join(t.TempDir(), "light.jsonl")
	meta, err := agent.EnsureBranchMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	meta.AgentPreset = "light"
	meta.TokenMode = "economy"
	profile := tabSessionProfileFromMeta(path, meta)
	if profile.qualityFloor != control.QualityFloorStandard {
		t.Fatalf("legacy light must fold to standard, got %q", profile.qualityFloor)
	}
}
