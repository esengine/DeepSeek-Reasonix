package trust

import (
	"path/filepath"
	"testing"
)

func TestAuthorizeAndMissingRefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trust.json")
	s := NewStore(path)
	missing, err := s.MissingRefs("lab", "fp", []string{"deepseek/chat", "a/b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 2 {
		t.Fatalf("missing = %v", missing)
	}
	if err := s.AuthorizeAll("lab", "ssh-ed25519", "fp", []string{"deepseek/chat"}); err != nil {
		t.Fatal(err)
	}
	missing, err = s.MissingRefs("lab", "fp", []string{"deepseek/chat", "a/b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0] != "a/b" {
		t.Fatalf("missing after auth = %v", missing)
	}
	// Reload from disk.
	s2 := NewStore(path)
	rec, ok, err := s2.Get("lab", "fp")
	if err != nil || !ok {
		t.Fatalf("get = %v ok=%v", err, ok)
	}
	if len(rec.AllowedProviderRefs) != 1 {
		t.Fatalf("refs = %v", rec.AllowedProviderRefs)
	}
}

func TestIdentityDigestStable(t *testing.T) {
	a := IdentityDigest([]string{"ed25519:k1", "rsa:k2"})
	b := IdentityDigest([]string{"rsa:k2", "ed25519:k1"})
	if a != b || a == "" {
		t.Fatalf("digest a=%s b=%s", a, b)
	}
}
