package agent

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func TestRepairSessionListingProjectionHealsRecoveryLedgerOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-recovery.jsonl")
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "question"},
		{Role: provider.RoleAssistant, Content: "answer"},
	}
	writeSessionFile(t, path, msgs)
	wrong := strings.Repeat("0", 64)
	if err := SaveBranchMeta(path, BranchMeta{
		ID: BranchID(path), Recovered: true, Revision: 7,
		ContentDigest: wrong, RecoveryDigest: wrong, SchemaVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}

	result, err := RepairSessionListingProjection(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != SessionListingRepairApplied || !result.LedgerRepaired || result.Preview != "question" || result.Turns != 1 {
		t.Fatalf("repair result = %+v", result)
	}
	digest, err := ContentDigestForMessages(msgs)
	if err != nil {
		t.Fatal(err)
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	if meta.Revision != 8 || meta.ContentDigest != digest || meta.RecoveryDigest != digest ||
		meta.ListingRevision != 8 || meta.ListingContentDigest != digest || meta.Preview != "question" || meta.Turns != 1 {
		t.Fatalf("healed meta = %+v", meta)
	}
	idx, err := LoadSessionDisplayIndex(store.SessionDisplayIndex(path))
	if err != nil || idx.Revision != 8 || idx.ContentDigest != digest || !idx.ListingPreviewKnown || idx.ListingPreview != "question" {
		t.Fatalf("display index = %+v err=%v", idx, err)
	}

	result, err = RepairSessionListingProjection(context.Background(), path)
	if err != nil || result.Status != SessionListingRepairAlreadyCurrent || result.LedgerRepaired {
		t.Fatalf("second repair = %+v err=%v", result, err)
	}
}

func TestRepairSessionListingProjectionYieldsToForegroundSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.jsonl")
	writeSessionFile(t, path, []provider.Message{{Role: provider.RoleUser, Content: "question"}})
	unlock := lockSessionSavePath(path)
	defer unlock()
	started := time.Now()
	_, err := RepairSessionListingProjection(context.Background(), path)
	if !errors.Is(err, ErrSessionListingRepairBusy) {
		t.Fatalf("repair err = %v, want busy", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("busy repair waited %v", elapsed)
	}
}

func TestRepairSessionListingProjectionHonorsCancellationDuringLargeDecode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.jsonl")
	writeSessionFile(t, path, []provider.Message{{
		Role: provider.RoleUser, Content: "large", Images: []string{strings.Repeat("a", 49<<20)},
	}})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(time.Millisecond, cancel)
	started := time.Now()
	_, err := RepairSessionListingProjection(ctx, path)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("repair err = %v, want context cancellation", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("cancellation took %v", elapsed)
	}
}
