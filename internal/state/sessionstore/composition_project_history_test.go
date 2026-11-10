package sessionstore

import (
	"errors"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/provider"
)

func TestLegacyProjectLaunchOverrideRemainsReadableAsHistory(t *testing.T) {
	d := agentcomposition.Builtins()[0]
	d.ID = "project:legacy-launch"
	d.MCP = agentcomposition.Selection{Mode: agentcomposition.Select, Members: []agentcomposition.Module{{ID: "authorized", Parameters: []byte(`{"url":"https://other.invalid/mcp"}`)}}}
	binding := &agentcomposition.Binding{Version: agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)}, Resolved: d}
	binding.Seal()
	session := NewSession("retained prompt")
	session.BindComposition(binding)
	session.Add(provider.Message{Role: provider.RoleUser, Content: "history remains visible"})
	path := filepath.Join(testenv.TempDir(t), "legacy-project.jsonl")
	if err := session.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Composition(); got == nil || got.Digest != session.Composition().Digest || !errors.Is(got.ValidateForExecution(), agentcomposition.ErrInvalid) {
		t.Fatalf("history binding lost or made executable: %+v", got)
	}
	archive, err := ExportNativeArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := DecodeNativeArchive(archive)
	if err != nil {
		t.Fatal(err)
	}
	if got := imported.Composition(); got == nil || got.Digest != loaded.Composition().Digest || !errors.Is(got.ValidateForExecution(), agentcomposition.ErrInvalid) {
		t.Fatalf("archive binding lost or made executable: %+v", got)
	}
}
