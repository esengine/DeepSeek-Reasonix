package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateSupportDataCarriesGlobalHooks(t *testing.T) {
	legacy := t.TempDir()
	fresh := t.TempDir()
	hooks := `{"hooks":{"Stop":[{"command":"echo done"}]}}`
	if err := os.WriteFile(filepath.Join(legacy, "settings.json"), []byte(hooks), 0o600); err != nil {
		t.Fatal(err)
	}

	migrateSupportData(legacy, fresh)

	got, err := os.ReadFile(filepath.Join(fresh, "settings.json"))
	if err != nil {
		t.Fatalf("global hooks were not migrated: %v", err)
	}
	if string(got) != hooks {
		t.Fatalf("migrated hooks = %q, want %q", got, hooks)
	}
}

func TestMigrateSupportDataKeepsExistingFiles(t *testing.T) {
	legacy := t.TempDir()
	fresh := t.TempDir()
	if err := os.WriteFile(filepath.Join(legacy, "settings.json"), []byte(`{"hooks":{"Stop":[]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	current := `{"hooks":{"Stop":[{"command":"current"}]}}`
	if err := os.WriteFile(filepath.Join(fresh, "settings.json"), []byte(current), 0o600); err != nil {
		t.Fatal(err)
	}

	warnings := migrateSupportData(legacy, fresh)

	got, err := os.ReadFile(filepath.Join(fresh, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != current {
		t.Fatalf("migration overwrote live settings: got %q, want %q", got, current)
	}
	var reported bool
	for _, w := range warnings {
		if strings.Contains(w, "kept existing file settings.json") {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("skipping an existing file should be reported, got %v", warnings)
	}
}
