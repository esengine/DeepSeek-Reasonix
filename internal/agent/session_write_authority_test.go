package agent

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestWriteAuthorityStaleAfterRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := lease.IssueWriteAuthority(NextSessionWriteGeneration())
	if err != nil {
		t.Fatal(err)
	}
	if !auth.Valid() || !auth.Covers(path) {
		t.Fatal("fresh authority should be valid")
	}
	lease.Release()
	if auth.Valid() {
		t.Fatal("authority must be stale after lease release")
	}
	if _, err := auth.BeginSave(path); !errors.Is(err, ErrSessionWriteAuthorityStale) {
		t.Fatalf("BeginSave = %v, want stale", err)
	}
}

func TestWriteAuthorityReleaseWaitsForInFlightSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := lease.IssueWriteAuthority(NextSessionWriteGeneration())
	if err != nil {
		t.Fatal(err)
	}
	releaseSave, err := auth.BeginSave(path)
	if err != nil {
		t.Fatal(err)
	}
	releaseWaiting := make(chan struct{})
	var waitOnce sync.Once
	lease.beforeReleaseWait = func() { waitOnce.Do(func() { close(releaseWaiting) }) }
	done := make(chan struct{})
	go func() {
		lease.Release()
		close(done)
	}()
	<-releaseWaiting
	select {
	case <-done:
		t.Fatal("Release returned while save still in flight")
	default:
	}
	releaseSave()
	<-done
}

func TestWriteAuthorityGenerationInvalidatesPriorToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	oldAuth, err := lease.IssueWriteAuthority(1)
	if err != nil {
		t.Fatal(err)
	}
	newAuth, err := lease.IssueWriteAuthority(2)
	if err != nil {
		t.Fatal(err)
	}
	if oldAuth.Valid() {
		t.Fatal("old generation remained valid after replacement authority")
	}
	if _, err := oldAuth.BeginSave(path); !errors.Is(err, ErrSessionWriteAuthorityStale) {
		t.Fatalf("old BeginSave = %v, want stale", err)
	}
	if !newAuth.Valid() {
		t.Fatal("replacement authority should be valid")
	}
}
