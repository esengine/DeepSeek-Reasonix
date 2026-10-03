package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestTitleCacheKeepsTitleAcrossMtimeChanges(t *testing.T) {
	dir := testenv.TempDir(t)
	c := newTitleCache(dir)

	if _, ok := c.get("a.jsonl", "first prompt", 100); ok {
		t.Fatal("empty cache should miss")
	}

	c.put("a.jsonl", "First Title", "first prompt", 100)
	if got, ok := c.get("a.jsonl", "first prompt", 200); !ok || got != "First Title" {
		t.Fatalf("hit after mtime change = %q,%v, want First Title,true", got, ok)
	}
}

func TestTitleCacheInvalidatesWhenFirstMessageChanges(t *testing.T) {
	dir := testenv.TempDir(t)
	c := newTitleCache(dir)
	c.put("a.jsonl", "First Title", "first prompt", 100)

	if _, ok := c.get("a.jsonl", "replacement prompt", 100); ok {
		t.Fatal("a replaced first message must invalidate the cached title")
	}
}

func TestTitleCachePersistsAcrossInstances(t *testing.T) {
	dir := testenv.TempDir(t)
	newTitleCache(dir).put("a.jsonl", "Persisted", "first prompt", 7)

	if _, err := os.Stat(filepath.Join(dir, ".session-titles.json")); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}
	if got, ok := newTitleCache(dir).get("a.jsonl", "first prompt", 8); !ok || got != "Persisted" {
		t.Fatalf("fresh instance get = %q,%v, want Persisted,true", got, ok)
	}
}

func TestTitleCacheReadsLegacyMtimeEntries(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, ".session-titles.json")
	if err := os.WriteFile(path, []byte(`{"a.jsonl":{"title":"Legacy","mod":7}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if got, ok := newTitleCache(dir).get("a.jsonl", "first prompt", 7); !ok || got != "Legacy" {
		t.Fatalf("matching legacy entry = %q,%v, want Legacy,true", got, ok)
	}
	if _, ok := newTitleCache(dir).get("a.jsonl", "first prompt", 8); ok {
		t.Fatal("legacy entry with changed mtime must regenerate once before becoming sticky")
	}
}

func TestTitleCacheWritesRemainReadableByOlderVersions(t *testing.T) {
	dir := testenv.TempDir(t)
	newTitleCache(dir).put("a.jsonl", "Compatible", "first prompt", 7)
	data, err := os.ReadFile(filepath.Join(dir, ".session-titles.json"))
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]struct {
		Title string `json:"title"`
		Mod   int64  `json:"mod"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatalf("legacy unmarshal: %v", err)
	}
	if got := legacy["a.jsonl"]; got.Title != "Compatible" || got.Mod != 7 {
		t.Fatalf("legacy entry = %+v, want title and mod preserved", got)
	}
}

func TestTitleSnapshotReusesOneReadAndRefreshesNextList(t *testing.T) {
	dir := testenv.TempDir(t)
	c := newTitleCache(dir)
	c.put("a.jsonl", "First Title", "first prompt", 1)
	snapshot := c.snapshot()
	if err := os.WriteFile(filepath.Join(dir, ".session-titles.json"), []byte(`{"a.jsonl":{"title":"Next Title","mod":2}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 50 {
		if got := snapshot.display("a.jsonl", "first prompt", 2, ""); got != "First Title" {
			t.Fatalf("list reread the title file: %q", got)
		}
	}
	if got := c.snapshot().display("a.jsonl", "first prompt", 2, ""); got != "Next Title" {
		t.Fatalf("next list did not refresh: %q", got)
	}
}

func TestTitleCacheReadErrorIsNotTreatedAsCorruption(t *testing.T) {
	dir := testenv.TempDir(t)
	if err := os.Mkdir(filepath.Join(dir, ".session-titles.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	c := newTitleCache(dir)
	if _, err := c.load(); err == nil {
		t.Fatal("a file read error must not become an empty cache")
	}
	if err := c.putExplicit("a.jsonl", "new title"); err == nil {
		t.Fatal("a file read error must prevent saving the title")
	}
}
