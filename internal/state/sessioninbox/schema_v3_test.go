package sessioninbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/store"
)

func openFresh(t *testing.T) (*Store, string) {
	t.Helper()
	session := filepath.Join(testenv.TempDir(t), "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	return s, session
}

func TestSchemaThreeQueueFromOneXOpensWritable(t *testing.T) {
	session := filepath.Join(testenv.TempDir(t), "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := store.SessionInboxDir(session)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(manifest{SchemaVersion: 3, Items: []InboxItemMeta{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestName), body, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(session, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if snap := s.Snapshot(); snap.Readonly {
		t.Fatalf("a schema-3 queue written by 1.x opened read-only: %+v", snap)
	}
	if _, err := s.Enqueue(EnqueueRequest{Intent: IntentFollowup, Source: "test", Envelope: PromptEnvelope{SubmitText: "hello"}}); err != nil {
		t.Fatalf("enqueue onto a schema-3 queue: %v", err)
	}
	if s.Snapshot().SchemaVersion != 3 {
		t.Fatalf("schema after a write = %d, want 3", s.Snapshot().SchemaVersion)
	}
}

func TestOneXImageFieldsSurviveAStateChange(t *testing.T) {
	s, _ := openFresh(t)
	defer s.Close()
	inputs := json.RawMessage(`[{"kind":"attachment","attachment":{"content":{"sha256":"ab"},"name":"shot.png"}}]`)
	env := PromptEnvelope{
		DisplayText: "look", SubmitText: "look",
		FingerprintVersion: 1, RequestFingerprint: strings.Repeat("a", 64),
		AttachmentIdentities: []string{"att-1"},
		ImageInputs:          inputs,
		ImageSourceRefs:      map[string]string{"att-1": "shot.png"},
	}
	rec, err := s.Enqueue(EnqueueRequest{Intent: IntentFollowup, Source: "test", Envelope: env})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetState(rec.ItemID, StateBlocked, "held"); err != nil {
		t.Fatal(err)
	}
	_, got, err := s.ReadItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.ImageInputs) != string(inputs) || got.ImageSourceRefs["att-1"] != "shot.png" ||
		got.FingerprintVersion != 1 || got.RequestFingerprint != env.RequestFingerprint ||
		len(got.AttachmentIdentities) != 1 {
		t.Fatalf("1.x image fields were not kept: %+v", got)
	}
	for _, it := range s.Snapshot().Items {
		if it.ID == rec.ItemID && !it.Images {
			t.Fatal("an item carrying 1.x image inputs does not read as carrying an image")
		}
	}
}

func TestRequestHashFollowsTheOneXFingerprint(t *testing.T) {
	fp := strings.Repeat("b", 64)
	got, err := idempotencyRequestHash(PromptEnvelope{SubmitText: "x", FingerprintVersion: 1, RequestFingerprint: fp})
	if err != nil || got != fp {
		t.Fatalf("hash = %q, %v; want the stored fingerprint", got, err)
	}
	if _, err := idempotencyRequestHash(PromptEnvelope{SubmitText: "x", FingerprintVersion: 2, RequestFingerprint: fp}); err == nil {
		t.Fatal("an unknown fingerprint version was accepted")
	}
	plain, err := idempotencyRequestHash(PromptEnvelope{SubmitText: "x"})
	if err != nil {
		t.Fatal(err)
	}
	withIDs, err := idempotencyRequestHash(PromptEnvelope{SubmitText: "x", AttachmentIdentities: []string{"att-1"}})
	if err != nil || withIDs == plain {
		t.Fatalf("attachment identities do not take part in the hash: %q %q %v", plain, withIDs, err)
	}
}
