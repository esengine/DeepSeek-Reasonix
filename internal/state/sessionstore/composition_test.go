package sessionstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/provider"
)

func TestCompositionRoundTripAndProviderBoundary(t *testing.T) {
	d := agentcomposition.Builtins()[1]
	b := &agentcomposition.Binding{Version: agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)}, Resources: []agentcomposition.Resource{}}
	s := NewSession("stable")
	s.BindComposition(b)
	s.Add(provider.Message{Role: provider.RoleUser, Content: "review"})
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Composition(); got == nil || got.Version.Definition.ID != d.ID || !got.Started {
		t.Fatalf("binding lost: %+v", got)
	}
	for _, m := range provider.ModelMessages(loaded.Snapshot()) {
		if m.AgentComposition != nil {
			t.Fatal("composition leaked to provider")
		}
	}
	data, err := os.ReadFile(SessionEventLogPath(path))
	if err != nil || !strings.Contains(string(data), `"schema_version":4`) {
		t.Fatalf("missing version boundary: %s %v", data, err)
	}
	loaded.ReplaceLocalMetadata(loaded.Snapshot()[:1])
	if !loaded.Composition().Started {
		t.Fatal("rewind unlocked composition")
	}
}
