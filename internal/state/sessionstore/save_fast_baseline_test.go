package sessionstore

import (
	"fmt"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

func seededSession(t *testing.T, path string, n int) *Session {
	t.Helper()
	s := NewSession("sys")
	for i := range n / 2 {
		s.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("turn %d: %s", i, strings.Repeat("please keep going. ", 20))})
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("a plain english sentence about the work. ", 40)})
	}
	if err := s.Save(path); err != nil {
		t.Fatalf("seed save: %v", err)
	}
	return s
}

func allocatedBy(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// A runtime that wrote the last save and finds the files untouched has nothing
// to reload: one more message costs about one encoding pass, not a replay of the
// event log plus a decode and re-encode of what disk already holds.
func TestSavingOneMoreMessageDoesNotReplayTheLog(t *testing.T) {
	if raceDetector {
		t.Skip("allocation counts are not comparable under the race detector")
	}
	path := filepath.Join(testenv.TempDir(t), "s.jsonl")
	s := seededSession(t, path, 2000)
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	_, size, err := digestAndSizeSessionMessages(s.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	s.Add(provider.Message{Role: provider.RoleUser, Content: "next"})
	got := allocatedBy(func() {
		if err := s.Save(path); err != nil {
			t.Fatal(err)
		}
	})
	if limit := uint64(size) * 4; got > limit {
		t.Fatalf("one-message save allocated %d bytes for a %d byte transcript (limit %d)", got, size, limit)
	}
}

func TestSavingAfterAnotherRuntimeWroteStillSeesIt(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "s.jsonl")
	a := seededSession(t, path, 40)
	b, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	b.Add(provider.Message{Role: provider.RoleUser, Content: "written by the other runtime"})
	if err := b.Save(path); err != nil {
		t.Fatalf("other runtime save: %v", err)
	}
	a.Add(provider.Message{Role: provider.RoleUser, Content: "written by the stale runtime"})
	if err := a.Save(path); err == nil {
		after, lerr := LoadSession(path)
		if lerr != nil {
			t.Fatal(lerr)
		}
		var kept int
		for _, m := range after.Snapshot() {
			if strings.HasPrefix(m.Content, "written by the other runtime") {
				kept++
			}
		}
		if kept != 1 {
			t.Fatalf("the stale runtime's save dropped the other runtime's message (kept %d)", kept)
		}
	}
}

// Switching away from a session that was opened and never touched saves it
// once more; that snapshot has nothing to write and must not read the log back.
func TestSnapshotOfAnUntouchedLoadedSessionReadsNothing(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "s.jsonl")
	seededSession(t, path, 2000)
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	_, size, err := digestAndSizeSessionMessages(loaded.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	got := allocatedBy(func() {
		if err := loaded.SaveSnapshot(path); err != nil {
			t.Fatal(err)
		}
	})
	if limit := uint64(size) / 2; got > limit {
		t.Fatalf("snapshot of an unchanged loaded session allocated %d bytes for a %d byte transcript (limit %d)", got, size, limit)
	}
}

// A ledger that does not describe the transcript on disk is what an interrupted
// save leaves behind; the first save after a load is what heals it.
func TestSnapshotAfterLoadStillHealsALedgerThatDisagreesWithTheTranscript(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "s.jsonl")
	seededSession(t, path, 40)
	if err := UpdateBranchMeta(path, false, func(m *BranchMeta) error {
		m.ContentDigest = "stale"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	identity, ok, err := SessionContentIdentity(path)
	if err != nil || !ok {
		t.Fatalf("identity: ok=%v err=%v", ok, err)
	}
	want, _ := DigestSessionMessages(loaded.Snapshot())
	if identity.Digest != want {
		t.Fatalf("ledger digest %x was not healed to the transcript's %x", identity.Digest, want)
	}
}
