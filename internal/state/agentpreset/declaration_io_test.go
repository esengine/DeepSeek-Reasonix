package agentpreset

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/agentcomposition"
)

func TestPersonalDeclarationReadsStayWithinTheStoreRoot(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(fmt.Sprint(external), func(t *testing.T) {
			s := New(testenv.TempDir(t), testenv.TempDir(t))
			if err := os.MkdirAll(filepath.Join(s.UserDirectory(), "definitions"), 0o700); err != nil {
				t.Fatal(err)
			}
			d := agentcomposition.Builtins()[0]
			d.ID = "user:linked"
			data, err := json.Marshal(d)
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(s.UserDirectory(), "payload.json")
			link := "../payload.json"
			if external {
				target = filepath.Join(testenv.TempDir(t), "payload.json")
				link = target
			}
			if err := os.WriteFile(target, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(link, filepath.Join(s.UserDirectory(), "definitions", "linked.json")); err != nil {
				t.Fatal(err)
			}
			got, err := s.Get(d.ID)
			if external && err == nil || !external && (err != nil || got.Definition.ID != d.ID) {
				t.Fatalf("external=%v: declaration=%+v err=%v", external, got, err)
			}
		})
	}
}

func TestProjectDeclarationCannotFollowSymlinkOutsideWorkspace(t *testing.T) {
	s := New(testenv.TempDir(t), testenv.TempDir(t))
	outside := testenv.TempDir(t)
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

func TestDeclarationSizeBoundAndExplicitDefault(t *testing.T) {
	s := New(testenv.TempDir(t), testenv.TempDir(t))
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
	if id, err := s.Default(); err != nil || id != "builtin:minimal" {
		t.Fatalf("explicit default overwritten: %s %v", id, err)
	}
}
