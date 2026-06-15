package serve

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTitleCacheReusesUntilMtimeChanges(t *testing.T) {
	dir := t.TempDir()
	c := newTitleCache(dir)

	if _, ok := c.get("a.jsonl", 100); ok {
		t.Fatal("empty cache should miss")
	}

	c.put("a.jsonl", "First Title", 100)
	if got, ok := c.get("a.jsonl", 100); !ok || got != "First Title" {
		t.Fatalf("hit on matching mtime = %q,%v, want First Title,true", got, ok)
	}
	if _, ok := c.get("a.jsonl", 200); ok {
		t.Fatal("changed mtime should invalidate the cached title")
	}
}

func TestTitleCachePersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	newTitleCache(dir).put("a.jsonl", "Persisted", 7)

	if _, err := os.Stat(filepath.Join(dir, ".session-titles.json")); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}
	if got, ok := newTitleCache(dir).get("a.jsonl", 7); !ok || got != "Persisted" {
		t.Fatalf("fresh instance get = %q,%v, want Persisted,true", got, ok)
	}
}
