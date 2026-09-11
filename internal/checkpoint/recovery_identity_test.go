package checkpoint

import (
	"encoding/json"
	"os"
	"testing"

	"reasonix/internal/provider"
)

func TestRecoveryIdentityPersistsOnlyFirstWriter(t *testing.T) {
	dir := t.TempDir()
	s := New(dir, dir)
	s.Begin(1, "write", 1)
	first := RecoveryIdentity{Action: provider.ActionIdentity{TurnID: "turn", CallID: "first", AttemptID: "attempt", ArgumentDigest: "args"}, TranscriptDigest: "transcript"}
	if err := s.BindRecoveryIdentity(first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Action.CallID = "second"
	if err := s.BindRecoveryIdentity(second); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.v3MetaPath(1))
	if err != nil {
		t.Fatal(err)
	}
	var persisted Checkpoint
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Recovery == nil || *persisted.Recovery != first {
		t.Fatalf("checkpoint identity=%+v", persisted.Recovery)
	}
}
