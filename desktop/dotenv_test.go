package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpsertEnvFile proves a new key is appended, an existing key is replaced in
// place, comments/other lines survive, and the process env is updated.
func TestUpsertEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	if err := os.WriteFile(path, []byte("# comment\nFOO=old\nBAR=keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := upsertEnvFile(path, "FOO", "new"); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if err := upsertEnvFile(path, "BAZ", "added"); err != nil {
		t.Fatalf("append: %v", err)
	}

	b, _ := os.ReadFile(path)
	got := string(b)
	for _, want := range []string{"# comment", "FOO=new", "BAR=keep", "BAZ=added"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "FOO=old") {
		t.Errorf("old value should be replaced:\n%s", got)
	}
	if os.Getenv("FOO") != "new" || os.Getenv("BAZ") != "added" {
		t.Errorf("process env not updated: FOO=%q BAZ=%q", os.Getenv("FOO"), os.Getenv("BAZ"))
	}
}
