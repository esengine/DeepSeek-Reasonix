package sessionstore

import (
	"fmt"
	"path/filepath"
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
	path := filepath.Join(t.TempDir(), "s.jsonl")
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
	path := filepath.Join(t.TempDir(), "s.jsonl")
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
