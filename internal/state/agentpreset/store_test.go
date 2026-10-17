package agentpreset

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/agentcomposition"
)

func TestStoreScopesCASAndRetainedVersions(t *testing.T) {
	s := New(t.TempDir(), t.TempDir())
	d := agentcomposition.Builtins()[0]
	d.ID, d.Name = "user:review", "My review"
	one, err := s.Save(d, "")
	if err != nil {
		t.Fatal(err)
	}
	d.Name = "Updated review"
	two, err := s.Save(d, one.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Save(d, one.Revision); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}
	if err = s.Delete(d.ID, two.Revision); err != nil {
		t.Fatal(err)
	}
	old, err := s.Version(one.Revision)
	if err != nil || old.Definition.Name != "My review" {
		t.Fatalf("retained version: %+v %v", old, err)
	}
	d.ID = "project:review"
	if _, err = s.Save(d, ""); err != nil {
		t.Fatal(err)
	}
	other := New(s.Home, t.TempDir())
	if _, err = other.Get(d.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("project leaked: %v", err)
	}
}

func TestProjectCannotSelectDefaultAndBuiltinCannotBeWritten(t *testing.T) {
	s := New(t.TempDir(), t.TempDir())
	d := agentcomposition.Builtins()[0]
	if _, err := s.Save(d, ""); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("builtin write: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(s.Workspace, ".reasonix", "agent-presets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Workspace, ".reasonix", "agent-presets", "settings.json"), []byte(`{"default":"project:bad"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Default(); err != nil || got != "builtin:general" {
		t.Fatalf("default %s %v", got, err)
	}
}
