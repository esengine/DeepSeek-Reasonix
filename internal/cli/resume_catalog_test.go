package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/provider"
	"reasonix/internal/session"
)

func createCanonicalTestSession(t *testing.T, v4root, id, prompt string) {
	t.Helper()
	persistence := session.NewFilesystemPersistence(v4root)
	sess, err := persistence.Create(session.CreateOptions{SessionID: id})
	if err != nil {
		t.Fatal(err)
	}
	if prompt != "" {
		messagePayload, err := json.Marshal(map[string]any{
			"message": provider.Message{ID: id + "-m1", Role: provider.RoleUser, Content: prompt},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sess.Append(context.Background(), session.Batch{OperationID: "seed", TurnID: id + "-turn", Events: []session.Event{
			{Kind: "message/complete", Payload: messagePayload},
			{Kind: "turn/start"},
			{Kind: "turn/end", Payload: json.RawMessage(`{"status":"completed"}`)},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sess.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := sess.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func waitForCatalogMetadata(t *testing.T, sessionDir string, ids ...string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	service := cliSessionService(sessionDir)
	if service == nil {
		t.Fatal("no session service for test workspace")
	}
	for time.Now().Before(deadline) {
		page, err := service.Query().List(context.Background(), "", 100)
		if err != nil {
			t.Fatal(err)
		}
		ready := map[string]bool{}
		for _, info := range page.Sessions {
			ready[info.SessionID] = info.MetadataStatus == session.MetadataReady
		}
		all := true
		for _, id := range ids {
			if !ready[id] {
				all = false
				break
			}
		}
		if all {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("catalog metadata for %v did not become ready", ids)
}

func writeTestMigrationMap(t *testing.T, v4root, sourcePath, targetID string) {
	t.Helper()
	mapping := session.MigrationMapping{SchemaVersion: session.SchemaVersion, Entries: []session.MigrationEntry{{
		SourcePath: sourcePath, TargetCodec: session.Codec, TargetID: targetID,
	}}}
	data, err := json.Marshal(mapping)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v4root, "migration-map.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestMergedResumeEntriesUnifiesStores proves the picker offers the same
// conversations the desktop tree shows: final-format catalog rows appear,
// never-chatted canonical placeholders stay hidden, and a legacy source with
// exactly one canonical successor is folded into that successor.
func TestMergedResumeEntriesUnifiesStores(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "sessions")
	v4root := filepath.Join(dir, "sessions-v4")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}

	migrated := saveQueryTestSession(t, sessionDir, "migrated-source.jsonl", "old conversation")
	fresh := saveQueryTestSession(t, sessionDir, "fresh-legacy.jsonl", "fresh legacy conversation")
	createCanonicalTestSession(t, v4root, "canchat1", "canonical hello")
	createCanonicalTestSession(t, v4root, "canempty1", "")
	writeTestMigrationMap(t, v4root, migrated, "canchat1")
	waitForCatalogMetadata(t, sessionDir, "canchat1", "canempty1")

	entries := mergedResumeEntries(sessionDir, resumeListCap)
	var sawFresh, sawMigrated, sawChatted, sawEmpty bool
	for _, entry := range entries {
		switch {
		case entry.session.Path == fresh:
			sawFresh = true
			if entry.target.canonical() || entry.target.path != fresh {
				t.Fatalf("legacy row carries wrong target: %+v", entry.target)
			}
		case entry.session.Path == migrated:
			sawMigrated = true
		case entry.target.ref.SessionID == "canchat1":
			sawChatted = true
			if entry.session.Turns != 1 || entry.session.Preview != "canonical hello" {
				t.Fatalf("canonical row = %+v", entry.session)
			}
		case entry.target.ref.SessionID == "canempty1":
			sawEmpty = true
		}
	}
	if !sawFresh {
		t.Fatal("fresh legacy session missing from merged picker list")
	}
	if !sawChatted {
		t.Fatal("chatted canonical session missing from merged picker list")
	}
	if sawMigrated {
		t.Fatal("migrated legacy source still listed beside its canonical successor")
	}
	if sawEmpty {
		t.Fatal("never-chatted canonical session listed")
	}
}

func TestCanonicalResumeHidden(t *testing.T) {
	if !canonicalResumeHidden(session.SessionInfo{MetadataStatus: session.MetadataReady}) {
		t.Fatal("ready empty session should be hidden")
	}
	if canonicalResumeHidden(session.SessionInfo{MetadataStatus: session.MetadataReady, Turns: 2}) {
		t.Fatal("session with turns should stay listed")
	}
	if canonicalResumeHidden(session.SessionInfo{MetadataStatus: session.MetadataReady, Preview: "hi"}) {
		t.Fatal("session with preview should stay listed")
	}
	if canonicalResumeHidden(session.SessionInfo{MetadataStatus: session.MetadataPending}) {
		t.Fatal("pending metadata must stay listed while the rebuild runs")
	}
}


// TestMergedResumeEntriesCapsAcrossStores keeps the picker cap honest when
// both stores contribute rows: the cap applies to the merged stream, not per
// store.
func TestMergedResumeEntriesCapsAcrossStores(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "sessions")
	v4root := filepath.Join(dir, "sessions-v4")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		saveQueryTestSession(t, sessionDir, "legacy-"+string(rune('a'+i))+"-session.jsonl", "legacy prompt "+string(rune('a'+i)))
	}
	var ids []string
	for i := 0; i < 4; i++ {
		id := "bbbb" + string(rune('0'+i)) + "cccc"
		createCanonicalTestSession(t, v4root, id, "canonical prompt "+string(rune('0'+i)))
		ids = append(ids, id)
	}
	waitForCatalogMetadata(t, sessionDir, ids...)

	entries := mergedResumeEntries(sessionDir, 5)
	if len(entries) > 5+1 {
		t.Fatalf("merged list kept %d entries above the cap", len(entries))
	}
	if len(entries) == 0 {
		t.Fatal("merged list is empty")
	}
	newest := time.Time{}
	for _, entry := range entries {
		if entry.session.ModTime.After(newest) {
			newest = entry.session.ModTime
		}
	}
	if newest.IsZero() {
		t.Fatal("merged rows carry no recency stamps")
	}
}
