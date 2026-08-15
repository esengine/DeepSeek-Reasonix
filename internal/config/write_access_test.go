package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistProjectWriteAccessWritesBothSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reasonix.toml")
	if err := os.WriteFile(path, []byte("# keep\n[permissions]\nallow = [\"Bash(go test:*)\"]\n\n[sandbox]\nbash = \"enforce\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(dir, ".local")
	if err := PersistProjectWriteAccess(path, []string{extra}, "Edit"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if !strings.Contains(body, "# keep") {
		t.Fatal("comments must be preserved")
	}
	if !strings.Contains(body, "Bash(go test:*)") || !strings.Contains(body, "Edit") {
		t.Fatalf("permission rules missing: %s", body)
	}
	if !strings.Contains(body, "allow_write") || !strings.Contains(body, extra) && !strings.Contains(body, ".local") {
		t.Fatalf("allow_write missing: %s", body)
	}
}

func TestPersistProjectWriteAccessDoesNotDuplicateAncestor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reasonix.toml")
	parent := filepath.Join(dir, "home")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[sandbox]\nallow_write = [\""+parent+"\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PersistProjectWriteAccess(path, []string{filepath.Join(parent, "bin")}, ""); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), "allow_write") != 1 {
		t.Fatalf("unexpected allow_write rewrite: %s", raw)
	}
	if strings.Contains(string(raw), filepath.Join(parent, "bin")) {
		t.Fatalf("child should not be persisted when ancestor exists: %s", raw)
	}
}
