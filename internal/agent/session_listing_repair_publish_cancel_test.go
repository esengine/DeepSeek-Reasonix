package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/store"
)

func TestRepairSessionListingProjectionCancelsDuringPublish(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large-publish.jsonl")
	writeSessionFile(t, path, []provider.Message{{
		Role: provider.RoleUser, Content: "large", Images: []string{strings.Repeat("a", 49<<20)},
	}})
	wrong := strings.Repeat("0", 64)
	if err := SaveBranchMeta(path, BranchMeta{
		ID: BranchID(path), Revision: 7, ContentDigest: wrong, SchemaVersion: 1,
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	publishStarted := make(chan struct{})
	ctx = withSessionPublishStartHook(ctx, func() {
		close(publishStarted)
		<-ctx.Done()
	})
	done := make(chan error, 1)
	go func() {
		_, err := RepairSessionListingProjection(ctx, path)
		done <- err
	}()
	select {
	case <-publishStarted:
	case <-time.After(30 * time.Second):
		cancel()
		t.Fatal("repair never reached transcript publication")
	}
	if !sessionPublishTempExists(t, dir) {
		cancel()
		t.Fatal("publication boundary did not create its temp file")
	}
	cancelStarted := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("repair err = %v, want context cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("publish cancellation did not release the session locks promptly")
	}
	if elapsed := time.Since(cancelStarted); elapsed > 2*time.Second {
		t.Fatalf("publish cancellation took %v", elapsed)
	}
	if sessionPublishTempExists(t, dir) {
		t.Fatal("canceled repair left a transcript temp file")
	}
	meta, ok, err := LoadBranchMeta(path)
	if err != nil || !ok || meta.Revision != 7 || meta.ContentDigest != wrong {
		t.Fatalf("canceled repair published meta: ok=%v err=%v meta=%+v", ok, err, meta)
	}
	for _, sidecar := range []string{store.SessionEventIndex(path), store.SessionDisplayIndex(path)} {
		if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
			t.Fatalf("canceled repair published sidecar %q: %v", sidecar, err)
		}
	}
}

func sessionPublishTempExists(t *testing.T, dir string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".session.") && strings.HasSuffix(entry.Name(), ".tmp") {
			return true
		}
	}
	return false
}
