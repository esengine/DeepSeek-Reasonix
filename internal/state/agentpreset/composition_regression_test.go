package agentpreset

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
)

func TestMCPRetainsParametersButOnlyReferencesCredentialValues(t *testing.T) {
	live := config.PluginEntry{Name: "docs", Command: "server", Args: []string{"--token", "very-private-token", "--mode=read"}, Env: map[string]string{"ACCESS_TOKEN": "very-private-env"}, Headers: map[string]string{"Authorization": "very-private-header"}, URL: "https://example.test/mcp?token=very-private-url"}
	snapshot := CaptureMCP(live)
	bytes, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bytes), "very-private") {
		t.Fatal("credential value retained in binding")
	}
	live.Env["ACCESS_TOKEN"] = "rotated"
	live.Env["NEW_PARAMETER"] = "must-not-follow"
	live.Args[2] = "--mode=write"
	restored, err := snapshot.Resolve(live)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Env["ACCESS_TOKEN"] != "rotated" || restored.Args[2] != "--mode=read" || restored.Env["NEW_PARAMETER"] != "" {
		t.Fatalf("resolution drift: %+v", restored)
	}
	live.Command = "replacement"
	if _, err := snapshot.Resolve(live); !errors.Is(err, agentcomposition.ErrUnavailable) {
		t.Fatalf("entry replacement accepted: %v", err)
	}
}

func TestPersonaMigrationRetriesAndPreservesCollidingDeclarations(t *testing.T) {
	s := New(t.TempDir(), t.TempDir())
	d := agentcomposition.Builtins()[0]
	d.ID, d.Name = "user:migrated-persona", "Existing unrelated preset"
	if _, err := s.Save(d, ""); err != nil {
		t.Fatal(err)
	}
	p := agentcomposition.Persona{Prefix: "old identity", Suffix: "old tail", Complete: true, IncludeRuntimeContext: false}
	if err := s.MigratePersona(p); err != nil {
		t.Fatal(err)
	}
	id, err := s.Default()
	if err != nil {
		t.Fatal(err)
	}
	if id == d.ID {
		t.Fatal("migration replaced a colliding user declaration")
	}
	v, err := s.Get(id)
	if err != nil || v.Definition.Persona != p {
		t.Fatalf("migration lost persona: %+v %v", v, err)
	}
	if err := s.MigratePersona(agentcomposition.Persona{Prefix: "changed config"}); err != nil {
		t.Fatal(err)
	}
	again, _ := s.Get(id)
	if again.Revision != v.Revision {
		t.Fatal("migration was not idempotent")
	}
	old, _ := s.Get(d.ID)
	if old.Definition.Name != d.Name {
		t.Fatal("collision was overwritten")
	}
}

func TestRetainedResourceTamperingAndUntrustedBindingAreRefused(t *testing.T) {
	s := New(t.TempDir(), t.TempDir())
	asset := DirectorySnapshot{Files: []AssetFile{{Path: "script", Data: []byte("original"), Mode: 0o700}}}
	root, err := s.Materialize(asset)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "script"), []byte("tampered"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Materialize(asset); !errors.Is(err, agentcomposition.ErrUnavailable) {
		t.Fatalf("tampering accepted: %v", err)
	}
	d := agentcomposition.Builtins()[1]
	b := &agentcomposition.Binding{Version: agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)}, Resolved: d}
	b.Seal()
	if err := s.VerifyBinding(b); !errors.Is(err, agentcomposition.ErrUnavailable) {
		t.Fatalf("self-sealed imported binding became authority: %v", err)
	}
	if err := s.RetainBinding(b); err != nil {
		t.Fatal(err)
	}
	if err := s.VerifyBinding(b); err != nil {
		t.Fatal(err)
	}
}
