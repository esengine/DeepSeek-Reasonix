package main

import (
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
)

func TestSaveTabSessionMetaReplacesStaleAgentPreset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	meta, err := agent.EnsureBranchMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	meta.AgentPreset = boot.AgentPresetBalanced
	meta.TokenMode = boot.TokenModeFull
	if err := agent.SaveBranchMetaPreserveUpdated(path, meta); err != nil {
		t.Fatal(err)
	}

	if err := saveTabSessionMetaSnapshot(tabSessionMetaSnapshot{path: path, tokenMode: boot.TokenModeDelivery}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta = %+v, %v, %v", got, ok, err)
	}
	if got.AgentPreset != boot.AgentPresetBalanced || got.TokenMode != boot.TokenModeFull {
		t.Fatalf("persisted role = preset:%q tokenMode:%q, want pinned balanced/full", got.AgentPreset, got.TokenMode)
	}
	if restored := tabSessionProfileFromMeta(path, got).tokenMode; restored != boot.TokenModeFull {
		t.Fatalf("restored tokenMode = %q, want pinned full", restored)
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
	if restored := tabSessionProfileFromMeta(path, meta).tokenMode; restored != boot.TokenModeDelivery {
		t.Fatalf("legacy decode tokenMode = %q, want delivery", restored)
	}
	tab := &WorkspaceTab{}
	applyTabSessionProfile(tab, tabSessionProfileFromMeta(path, meta))
	if got := currentTabTokenMode(tab); got != boot.TokenModeFull {
		t.Fatalf("legacy delivery must not change runtime tokenMode: got %q", got)
	}
}
