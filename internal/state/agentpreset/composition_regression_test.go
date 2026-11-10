package agentpreset

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
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

func TestRetainedResourceTamperingAndUntrustedBindingAreRefused(t *testing.T) {
	s := New(testenv.TempDir(t), testenv.TempDir(t))
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
