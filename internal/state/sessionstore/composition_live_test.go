package sessionstore

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reasonix/internal/base/testenv"
	"testing"

	"reasonix/internal/contract/agentcomposition"
)

func TestLiveCompositionStorageAndArchiveBoundary(t *testing.T) {
	d := agentcomposition.Builtins()[0]
	b := &agentcomposition.Binding{FormatVersion: 2, ResourceMode: "live", Version: agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)}, Resolved: d, Resources: []agentcomposition.Resource{}}
	b.Seal()
	s := NewSession("fixed prompt")
	s.BindComposition(b)
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(SessionEventLogPath(path))
	if err != nil || !bytes.Contains(raw, []byte(`"schema_version":5`)) {
		t.Fatalf("missing downgrade fence: %s %v", raw, err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if summary := loaded.CompositionSummary(); summary == nil || summary.ResourceMode != "live" || summary.Digest != b.Digest {
		t.Fatalf("binding summary changed: %+v", summary)
	}
	archive, err := ExportNativeArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope NativeArchive
	if err := json.Unmarshal(archive, &envelope); err != nil || envelope.Version != 3 {
		t.Fatalf("archive version: %d %v", envelope.Version, err)
	}
	imported, err := DecodeNativeArchive(archive)
	if err != nil || imported.CompositionSummary().Digest != b.Digest {
		t.Fatalf("archive round trip: %v", err)
	}
	envelope.Version = 2
	downgraded, _ := json.Marshal(envelope)
	if _, err := DecodeNativeArchive(downgraded); err == nil {
		t.Fatal("live binding accepted inside a legacy archive")
	}
}

func TestLiveBindingRejectsInlineResources(t *testing.T) {
	d := agentcomposition.Builtins()[0]
	b := &agentcomposition.Binding{FormatVersion: 2, ResourceMode: "live", Version: agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)}, Resolved: d,
		Resources: []agentcomposition.Resource{{Kind: "skill", ID: "x", Data: json.RawMessage(`{}`)}}}
	b.Seal()
	if b.Validate() == nil {
		t.Fatal("live binding retained resource bytes")
	}
}
