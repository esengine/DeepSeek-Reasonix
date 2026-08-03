package serve

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTitleCacheStickyAcrossMtimeChanges(t *testing.T) {
	dir := t.TempDir()
	c := newTitleCache(dir)

	if _, ok := c.get("a.jsonl"); ok {
		t.Fatal("empty cache should miss")
	}

	c.put("a.jsonl", "First Title", 100)
	if got, ok := c.get("a.jsonl"); !ok || got != "First Title" {
		t.Fatalf("hit after put = %q,%v, want First Title,true", got, ok)
	}
	// Mtime churn on an active session must not invalidate: the title
	// summarizes the first message, which never changes. Re-validating by
	// mtime cost one LLM call on every sidebar poll while chatting.
	c.put("a.jsonl", "First Title", 200)
	if got, ok := c.get("a.jsonl"); !ok || got != "First Title" {
		t.Fatalf("mtime change must not invalidate sticky title = %q,%v", got, ok)
	}
}

func TestTitleCachePersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	newTitleCache(dir).put("a.jsonl", "Persisted", 7)

	if _, err := os.Stat(filepath.Join(dir, ".session-titles.json")); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}
	if got, ok := newTitleCache(dir).get("a.jsonl"); !ok || got != "Persisted" {
		t.Fatalf("fresh instance get = %q,%v, want Persisted,true", got, ok)
	}
}
