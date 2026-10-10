package serve

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/store"
)

// oneXLogHeader is the first record 1.x writes: a schema 2 log this build reads
// and never writes.
const oneXLogHeader = `{"schema_version":2,"type":"log","at":"2026-10-05T00:00:00Z","generation":1}`

// nativeLogHeader is this build's own first record.
const nativeLogHeader = `{"schema_version":3,"type":"replace","revision":1,"messages":[]}`

// legacySession lays down a session with the given event log, or none at all.
func legacySession(t *testing.T, dir, name, log string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if log != "" {
		if err := os.WriteFile(store.SessionEventLog(path), []byte(log+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestLegacyCacheReadsTheEventLog(t *testing.T) {
	dir := testenv.TempDir(t)
	oneX := legacySession(t, dir, "onex.jsonl", oneXLogHeader)
	mine := legacySession(t, dir, "native.jsonl", nativeLogHeader)

	c := newLegacyCache(dir)
	if !c.legacyOf(oneX) {
		t.Fatal("a schema 2 log is 1.x's and must read as legacy")
	}
	if c.legacyOf(mine) {
		t.Fatal("this build's own log must not read as legacy")
	}
}

func TestLegacyCacheForgetsWhenTheLogAppears(t *testing.T) {
	dir := testenv.TempDir(t)
	path := legacySession(t, dir, "late.jsonl", "")

	c := newLegacyCache(dir)
	if c.legacyOf(path) {
		t.Fatal("a session with no event log is this build's own")
	}
	// 1.x later takes the conversation over, so the answer must not stick.
	if err := os.WriteFile(store.SessionEventLog(path), []byte(oneXLogHeader+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !c.legacyOf(path) {
		t.Fatal("the cached answer outlived the log it was decided from")
	}
}

func TestLegacyCachePersistsAcrossInstances(t *testing.T) {
	dir := testenv.TempDir(t)
	oneX := legacySession(t, dir, "onex.jsonl", oneXLogHeader)
	c := newLegacyCache(dir)
	c.legacyOf(oneX)
	c.flush()

	if _, err := os.Stat(filepath.Join(dir, ".session-legacy.json")); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}
	if !newLegacyCache(dir).legacyOf(oneX) {
		t.Fatal("a fresh instance must read the cached answer")
	}
}

func TestLegacyCacheTrustsOnlyAStampIdenticalEntry(t *testing.T) {
	dir := testenv.TempDir(t)
	path := legacySession(t, dir, "native.jsonl", nativeLogHeader)
	cacheFile := filepath.Join(dir, ".session-legacy.json")
	size, mod := sessionEventLogStamp(path)

	// An entry matching the log's stamp is the answer: the log itself says
	// otherwise, so a re-read would have flipped it.
	entry := fmt.Appendf(nil, `{"native.jsonl":{"legacy":true,"size":%d,"mod":%d}}`, size, mod)
	if err := os.WriteFile(cacheFile, entry, 0o600); err != nil {
		t.Fatal(err)
	}
	if !newLegacyCache(dir).legacyOf(path) {
		t.Fatal("a stamp-identical entry must be trusted rather than re-read")
	}
	// A different stamp is stale, so the log decides again.
	if err := os.WriteFile(cacheFile, []byte(`{"native.jsonl":{"legacy":true,"size":1,"mod":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if newLegacyCache(dir).legacyOf(path) {
		t.Fatal("a stale entry must be re-decided from the log")
	}
}

func TestLegacyCacheWritesOncePerListing(t *testing.T) {
	dir := testenv.TempDir(t)
	c := newLegacyCache(dir)
	a := legacySession(t, dir, "a.jsonl", oneXLogHeader)
	b := legacySession(t, dir, "b.jsonl", nativeLogHeader)

	c.legacyOf(a)
	c.legacyOf(b)
	if _, err := os.Stat(filepath.Join(dir, ".session-legacy.json")); err == nil {
		t.Fatal("records stay in memory until the listing flushes")
	}

	c.flush()
	if _, err := os.Stat(filepath.Join(dir, ".session-legacy.json")); err != nil {
		t.Fatalf("flush writes the file once: %v", err)
	}
}

// The mark has to reach the tree the sidebar draws: the cache decides, but
// the row is where the person sees it.
func TestWorkspaceSessionsCarriesTheLegacyMark(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	sessions := SessionDirFor(root)
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	legacySession(t, sessions, "onex.jsonl", oneXLogHeader)
	legacySession(t, sessions, "mine.jsonl", nativeLogHeader)

	h := NewHub(HubOptions{})
	got := h.workspaceSessions(root, map[string]string{})
	if len(got) != 2 {
		t.Fatalf("tree has %d rows, want 2", len(got))
	}
	for _, row := range got {
		switch filepath.Base(row.Path) {
		case "onex.jsonl":
			if !row.Legacy {
				t.Fatal("the 1.x session must carry legacy: true in the tree")
			}
		case "mine.jsonl":
			if row.Legacy {
				t.Fatal("this build's own session must not carry the mark")
			}
		}
	}
}
