package agentpreset

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/agentcomposition"
)

func TestProjectDeclarationCannotFollowSymlinkOutsideWorkspace(t *testing.T) {
	s := New(t.TempDir(), t.TempDir())
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(s.Workspace, ".reasonix")); err != nil {
		t.Fatal(err)
	}
	d := agentcomposition.Builtins()[0]
	d.ID = "project:outside"
	if _, err := s.Save(d, ""); !errors.Is(err, agentcomposition.ErrInvalid) {
		t.Fatalf("symlink save: %v", err)
	}
	if _, err := s.Get(d.ID); !errors.Is(err, agentcomposition.ErrInvalid) {
		t.Fatalf("symlink read: %v", err)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("outside changed: %v %v", entries, err)
	}
}

func TestDeclarationSizeBoundAndExplicitDefaultSurviveMigration(t *testing.T) {
	s := New(t.TempDir(), t.TempDir())
	path, err := s.declarationPath("user:large")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxDefinitionBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	if _, err := s.Get("user:large"); !errors.Is(err, agentcomposition.ErrInvalid) {
		t.Fatalf("large declaration: %v", err)
	}
	if err := s.SetDefault("builtin:minimal", false); err != nil {
		t.Fatal(err)
	}
	if err := s.MigratePersona(agentcomposition.Persona{Prefix: "old persona", IncludeRuntimeContext: true}); err != nil {
		t.Fatal(err)
	}
	if id, err := s.Default(); err != nil || id != "builtin:minimal" {
		t.Fatalf("explicit default overwritten: %s %v", id, err)
	}
}
