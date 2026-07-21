package mcpauth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tempStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	return NewStore(filepath.Join(dir, "creds.json"))
}

func TestStorePutGetDelete(t *testing.T) {
	s := tempStore(t)
	origin := "https://mcp.example.com"
	cred := &StoredCredential{
		Token: Token{AccessToken: "abc", RefreshToken: "xyz", Expiry: time.Now().Add(time.Hour)},
	}

	if _, ok := s.Get(origin); ok {
		t.Fatal("expected no credential before Put")
	}
	if err := s.Put(origin, cred); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := s.Get(origin)
	if !ok || got.Token.AccessToken != "abc" {
		t.Fatalf("Get after Put = %+v ok=%v", got, ok)
	}
	if got.IssuedAt.IsZero() {
		t.Fatal("Put should stamp IssuedAt")
	}

	if err := s.Delete(origin); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := s.Get(origin); ok {
		t.Fatal("Get should miss after Delete")
	}
}

func TestStorePersistsAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.json")
	s1 := NewStore(path)
	origin := "https://mcp.example.com"
	if err := s1.Put(origin, &StoredCredential{Token: Token{AccessToken: "persisted"}}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// A fresh Store over the same path must read the persisted credential.
	s2 := NewStore(path)
	got, ok := s2.Get(origin)
	if !ok || got.Token.AccessToken != "persisted" {
		t.Fatalf("re-opened store lost the credential: %+v ok=%v", got, ok)
	}
}

func TestStoreFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.json")
	s := NewStore(path)
	if err := s.Put("https://mcp.example.com", &StoredCredential{Token: Token{AccessToken: "a"}}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("file mode = %o, want 0600", mode)
	}
}

func TestStoreRecoversFromCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "creds.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(path)
	// A corrupt store reads as empty rather than erroring forever.
	if _, ok := s.Get("https://mcp.example.com"); ok {
		t.Fatal("corrupt store should read as empty")
	}
	// And a subsequent Put overwrites the corrupt file cleanly.
	if err := s.Put("https://mcp.example.com", &StoredCredential{Token: Token{AccessToken: "fixed"}}); err != nil {
		t.Fatalf("Put after corrupt: %v", err)
	}
	data, _ := os.ReadFile(path)
	var cf credentialsFile
	if err := json.Unmarshal(data, &cf); err != nil {
		t.Fatalf("file not valid JSON after Put: %v", err)
	}
	if cf.Credentials["https://mcp.example.com"] == nil {
		t.Fatal("credential missing after Put")
	}
}

func TestStoreMissingFileNotError(t *testing.T) {
	// A path whose parent exists but file does not should not error on Get.
	s := NewStore(filepath.Join(t.TempDir(), "absent.json"))
	if _, ok := s.Get("https://mcp.example.com"); ok {
		t.Fatal("missing file should read as empty")
	}
}
