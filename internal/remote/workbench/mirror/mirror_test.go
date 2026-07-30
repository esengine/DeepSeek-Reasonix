package mirror

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestApplyAndReadCheckpoint(t *testing.T) {
	base := t.TempDir()
	store := Store{Base: base}
	body := []byte("{\"role\":\"user\"}\n")
	sum := sha256.Sum256(body)
	man := Manifest{
		SessionID: "rs1",
		Revision:  1,
		Digest:    DigestArtifacts(map[string][]byte{"session.jsonl": body}),
		ArtifactSHA: map[string]string{
			"session.jsonl": hex.EncodeToString(sum[:]),
		},
	}
	if err := store.ApplyCheckpoint("fp", "/w", man, map[string][]byte{"session.jsonl": body}); err != nil {
		t.Fatal(err)
	}
	got, man2, err := store.ReadSessionJSONL("fp", "/w", "rs1")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) || man2.SessionID != "rs1" {
		t.Fatalf("got %q man=%+v", got, man2)
	}
}

func TestApplyRejectsDigestMismatch(t *testing.T) {
	store := Store{Base: t.TempDir()}
	err := store.ApplyCheckpoint("fp", "/w", Manifest{
		SessionID: "s", Revision: 1, Digest: "deadbeef",
	}, map[string][]byte{"session.jsonl": []byte("x")})
	if err == nil {
		t.Fatal("expected digest mismatch")
	}
}

func TestApplyRejectsMissingSessionJSONL(t *testing.T) {
	store := Store{Base: t.TempDir()}
	err := store.ApplyCheckpoint("fp", "/w", Manifest{
		SessionID: "s", Revision: 1, Digest: DigestArtifacts(map[string][]byte{}),
	}, map[string][]byte{"other.txt": []byte("x")})
	if err == nil {
		t.Fatal("expected missing session.jsonl")
	}
}
