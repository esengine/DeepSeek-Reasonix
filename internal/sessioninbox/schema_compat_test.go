package sessioninbox

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/store"
)

func TestPreviousInboxReaderPausesSchemaThree(t *testing.T) {
	const previousSchema = 2
	if SchemaVersion <= previousSchema {
		t.Fatalf("inbox schema %d is not newer than previous reader %d", SchemaVersion, previousSchema)
	}
	session := filepath.Join(t.TempDir(), "s.jsonl")
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Snapshot().SchemaVersion != SchemaVersion {
		t.Fatalf("schema = %d, want %d", s.Snapshot().SchemaVersion, SchemaVersion)
	}
	if !(s.Snapshot().SchemaVersion > previousSchema) {
		t.Fatal("previous inbox reader would not pause schema 3")
	}
}

func TestInboxNewerSchemaIsReadonlyPaused(t *testing.T) {
	session := filepath.Join(t.TempDir(), "s.jsonl")
	inboxDir := store.SessionInboxDir(session)
	if err := os.MkdirAll(inboxDir, 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(manifest{SchemaVersion: SchemaVersion + 1, Items: []InboxItemMeta{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inboxDir, manifestName), body, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	snap := s.Snapshot()
	if !snap.Paused || !snap.Readonly || snap.SchemaVersion != SchemaVersion+1 {
		t.Fatalf("newer schema snapshot = %+v, want readonly pause", snap)
	}
	if _, err := s.Enqueue(EnqueueRequest{Envelope: PromptEnvelope{SubmitText: "nope"}}); !errors.Is(err, ErrSchemaReadonly) {
		t.Fatalf("enqueue newer schema = %v, want %v", err, ErrSchemaReadonly)
	}
}
