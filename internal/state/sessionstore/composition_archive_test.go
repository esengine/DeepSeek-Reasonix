package sessionstore

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/provider"
)

func TestNativeArchivePreservesBindingResourcesAndVersionBoundary(t *testing.T) {
	d := agentcomposition.Builtins()[1]
	b := &agentcomposition.Binding{Resolved: d, Version: agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)}, Resources: []agentcomposition.Resource{{Kind: "command", ID: "retained", Data: []byte(`{"body":"immutable source"}`)}}}
	b.Seal()
	s := NewSession("original prompt")
	s.BindComposition(b)
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first turn"})
	path := filepath.Join(t.TempDir(), "original.jsonl")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := ExportNativeArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := DecodeNativeArchive(data)
	if err != nil {
		t.Fatal(err)
	}
	if copy.Composition().Digest != b.Digest || !copy.Composition().Started || !bytes.Contains(copy.Composition().Resources[0].Data, []byte("immutable source")) {
		t.Fatal("archive lost binding")
	}
	dest := filepath.Join(t.TempDir(), "imported.jsonl")
	if err := copy.SaveIfAbsent(dest); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(SessionEventLogPath(dest))
	if err != nil || !bytes.Contains(log, []byte(`"schema_version":4`)) {
		t.Fatalf("import omitted version boundary: %v", err)
	}
	if _, err := DecodeNativeArchive(bytes.Replace(data, []byte(`"version":2`), []byte(`"version":999`), 1)); err == nil {
		t.Fatal("future archive accepted")
	}
	if err := copy.SaveIfAbsent(dest); !os.IsExist(err) {
		t.Fatalf("import overwrote destination: %v", err)
	}
	version, err := SaveSupersededVersion(dest, copy.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession(version)
	if err != nil || loaded.Composition().Digest != b.Digest || !loaded.Composition().Started {
		t.Fatalf("version lost binding: %v", err)
	}
}
