package main

import (
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/control"
)

func TestSaveTabSessionMetaFoldsDeliveryFloorToStandard(t *testing.T) {
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

	if err := saveTabSessionMetaSnapshot(tabSessionMetaSnapshot{path: legacySessionPath(path), qualityFloor: control.QualityFloorDelivery}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta = %+v, %v, %v", got, ok, err)
	}
	if got.QualityFloor != control.QualityFloorStandard {
		t.Fatalf("persisted floor = %q, want standard", got.QualityFloor)
	}
	if got.AgentPreset != boot.AgentPresetStandard {
		t.Fatalf("dual-write preset = %q, want standard", got.AgentPreset)
	}
	if got.TokenMode != boot.TokenModeFull {
		t.Fatalf("dual-write tokenMode = %q, want full", got.TokenMode)
	}
}

func TestSaveTabSessionMetaStandardWritesCompatibilityValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "standard.jsonl")
	if _, err := agent.EnsureBranchMeta(path); err != nil {
		t.Fatal(err)
	}
	if err := saveTabSessionMetaSnapshot(tabSessionMetaSnapshot{path: legacySessionPath(path), qualityFloor: control.QualityFloorStandard}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta = %+v, %v, %v", got, ok, err)
	}
	if got.QualityFloor != control.QualityFloorStandard {
		t.Fatalf("standard compatibility floor = %q", got.QualityFloor)
	}
}

func TestTabSessionProfileFromMetaFoldsLegacyDelivery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.jsonl")
	meta, err := agent.EnsureBranchMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	meta.AgentPreset = boot.AgentPresetDelivery
	meta.TokenMode = boot.TokenModeDelivery
	profile := tabSessionProfileFromMeta(path, meta)
	if profile.qualityFloor != control.QualityFloorStandard {
		t.Fatalf("legacy decode floor = %q, want standard", profile.qualityFloor)
	}
	tab := &WorkspaceTab{}
	applyTabSessionProfile(tab, profile)
	if got := tab.qualityFloor; got != control.QualityFloorStandard {
		t.Fatalf("legacy delivery must fold at the tab boundary: got %q", got)
	}
	if got := currentTabTokenMode(tab); got != boot.TokenModeFull {
		t.Fatalf("derived tokenMode = %q, want full", got)
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
