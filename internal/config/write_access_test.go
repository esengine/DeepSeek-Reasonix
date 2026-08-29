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
	fixture := "[sandbox]\nallow_write = " + renderStringArray([]string{parent}) + "\n"
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
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

func TestSetProjectWriteAccessReplacesListAndRemoves(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reasonix.toml")
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.MkdirAll(a, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b, 0o755); err != nil {
		t.Fatal(err)
	}
	fixture := "[sandbox]\nallow_write = " + renderStringArray([]string{a, b}) + "\n"
	if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	// Replace the whole list with a single entry => b is removed.
	if err := SetProjectWriteAccess(path, []string{a}, ""); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadForEditReadOnlyStrict(path)
	if err != nil {
		t.Fatal(err)
	}
	roots := reloaded.AllowWriteRoots()
	if len(roots) != 1 || filepath.Clean(roots[0]) != filepath.Clean(a) {
		t.Fatalf("allow_write after replace = %v, want [%s]", roots, a)
	}
	for _, r := range roots {
		if filepath.Clean(r) == filepath.Clean(b) {
			t.Fatalf("b should have been removed, got %v", roots)
		}
	}

	// Setting an empty list removes everything.
	if err := SetProjectWriteAccess(path, nil, ""); err != nil {
		t.Fatal(err)
	}
	reloaded, err = LoadForEditReadOnlyStrict(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.AllowWriteRoots()) != 0 {
		t.Fatalf("expected empty allow_write, got %v", reloaded.AllowWriteRoots())
	}
}
