package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpsertDotEnv proves a new key is appended, an existing key is replaced in
// place, comments/other lines survive, and the process env is updated.
func TestUpsertDotEnv(t *testing.T) {
	// Point dotEnvPath at a temp directory so tests are hermetic.
	dir := t.TempDir()
	origPath := dotEnvPath
	dotEnvPath = filepath.Join(dir, ".env")
	defer func() { dotEnvPath = origPath }()

	if err := os.WriteFile(dotEnvPath, []byte("# comment\nFOO=old\nBAR=keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := upsertDotEnv("FOO", "new"); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if err := upsertDotEnv("BAZ", "added"); err != nil {
		t.Fatalf("append: %v", err)
	}

	b, _ := os.ReadFile(dotEnvPath)
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
