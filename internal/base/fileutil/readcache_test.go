package fileutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadCacheDetectsAppendReplaceAndRestoredMtime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "record")
	if err := os.WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	var cache ReadCache[string]
	reads := 0
	read := func(path string) (string, error) { reads++; data, err := os.ReadFile(path); return string(data), err }
	load := func(want string) {
		t.Helper()
		got, err := cache.Load(path, read)
		if err != nil || got != want {
			t.Fatalf("load = %q, %v; want %q", got, err, want)
		}
	}
	load("one")
	entry, ok := cache.entries[path]
	if !ok {
		t.Skip("platform deliberately rereads without file change-time evidence")
	}
	entry.read = time.Now().Add(3 * time.Second)
	cache.entries[path] = entry
	load("one")
	if reads != 1 {
		t.Fatalf("unchanged file reread %d times", reads)
	}
	if err := os.WriteFile(path, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, entry.info.ModTime(), entry.info.ModTime()); err != nil {
		t.Fatal(err)
	}
	load("two")
	if err := AtomicWriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	load("new")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("er"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	load("newer")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.Load(path, read); !os.IsNotExist(err) {
		t.Fatalf("deleted file = %v", err)
	}
	if err := os.WriteFile(path, []byte("back"), 0o600); err != nil {
		t.Fatal(err)
	}
	load("back")
	var restarted ReadCache[string]
	if got, err := restarted.Load(path, read); err != nil || got != "back" {
		t.Fatalf("restart = %q, %v", got, err)
	}
}
