package provider

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileScopeIsolatesKeysAndProtocols(t *testing.T) {
	chat := FileScope("https://api.deepseek.com///", "key", "openai")
	if chat != FileScope("https://api.deepseek.com", "key", "openai") {
		t.Fatal("trailing slashes must not change Chat scope")
	}
	if FileScope("https://api.deepseek.com", "a", "openai") == FileScope("https://api.deepseek.com", "b", "openai") {
		t.Fatal("API keys must not share a scope")
	}
	if FileScope("https://api.deepseek.com/anthropic", "key", "anthropic") == chat {
		t.Fatal("Messages and Chat Completions must not share file ids")
	}
}

func TestFileIndexReusesUntilRefreshMargin(t *testing.T) {
	idx := NewFileIndex(filepath.Join(t.TempDir(), "files-v1.json"))
	rec := FileIndexRecord{
		Scope: "aa", Variant: "bb", FileID: "file-api-one",
		Bytes: 3, CreatedAt: 1000, ExpiresAt: 10_000,
	}
	got, accepted := idx.Commit(rec, 1000, 1000)
	if !accepted || got.FileID != rec.FileID {
		t.Fatalf("commit = %+v accepted=%v", got, accepted)
	}
	found, ok := idx.Lookup("aa", "bb", 1000, 1000)
	if !ok || found.FileID != rec.FileID || found.Bytes != 3 {
		t.Fatalf("lookup = %+v ok=%v", found, ok)
	}
	if _, ok := idx.Lookup("aa", "bb", 9000, 1000); ok {
		t.Fatal("records inside the refresh margin must not be reused")
	}
	dup := rec
	dup.FileID = "file-api-two"
	winner, accepted := idx.Commit(dup, 1000, 1000)
	if accepted || winner.FileID != rec.FileID {
		t.Fatalf("duplicate commit = %+v accepted=%v", winner, accepted)
	}
}

func TestFileIndexCorruptFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "files-v1.json")
	if err := os.WriteFile(path, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	idx := NewFileIndex(path)
	if _, ok := idx.Lookup("aa", "bb", 1, 1); ok {
		t.Fatal("corrupt index must look empty")
	}
	rec := FileIndexRecord{Scope: "aa", Variant: "bb", FileID: "file-api-repaired", Bytes: 2, CreatedAt: 1, ExpiresAt: 10_000}
	if _, accepted := idx.Commit(rec, 1, 1); !accepted {
		t.Fatal("commit must repair a corrupt index")
	}
	if n, ok := idx.BytesForFileID("file-api-repaired"); !ok || n != 2 {
		t.Fatalf("bytes = %d ok=%v", n, ok)
	}
}
