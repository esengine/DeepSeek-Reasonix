package checkpoint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"reasonix/internal/diff"
	fileenc "reasonix/internal/fileutil/encoding"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}
func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func readBytes(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Two turns edit a.txt and create b.txt; rewinding restores each file to its
// state at the start of the chosen turn (b.txt being deleted when it post-dates it).
func TestRestoreToStartOfTurn(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "sub", "b.txt")
	write(t, a, "v0")
	s := New("", root)

	s.Begin(0, "first", 0)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "v0"})
	write(t, a, "v1") // the edit turn 0 made

	s.Begin(1, "second", 2)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "v1"})
	s.Snapshot(diff.Change{Path: b, Kind: diff.Create})
	write(t, a, "v2")
	write(t, b, "new")

	// Rewind to the start of turn 1: a back to v1, b gone.
	if _, _, err := s.RestoreCode(1); err != nil {
		t.Fatal(err)
	}
	if got := read(t, a); got != "v1" {
		t.Fatalf("a = %q, want v1", got)
	}
	if _, err := os.Stat(b); !os.IsNotExist(err) {
		t.Fatalf("b should have been deleted, stat err=%v", err)
	}
}

func TestRestoreToTurnZero(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	write(t, a, "v0")
	s := New("", root)
	s.Begin(0, "first", 0)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "v0"})
	write(t, a, "v1")
	s.Begin(1, "second", 2)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "v1"})
	write(t, a, "v2")

	if _, _, err := s.RestoreCode(0); err != nil {
		t.Fatal(err)
	}
	if got := read(t, a); got != "v0" {
		t.Fatalf("a = %q, want v0 (earliest snapshot)", got)
	}
}

func TestRestorePreservesGB18030Encoding(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "gbk.txt")
	original := "\u4f60\u597d\n\u65e7\u884c\n"
	edited := "\u4f60\u597d\n\u65b0\u884c\n"
	originalRaw := fileenc.Encode(original, fileenc.GB18030)
	if err := os.WriteFile(a, originalRaw, 0o644); err != nil {
		t.Fatal(err)
	}

	s := New("", root)
	s.Begin(0, "edit gbk", 0)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: original})
	if err := os.WriteFile(a, fileenc.Encode(edited, fileenc.GB18030), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.RestoreCode(0); err != nil {
		t.Fatal(err)
	}
	gotRaw := readBytes(t, a)
	if utf8.Valid(gotRaw) {
		t.Fatalf("restored GB18030 file became valid UTF-8 bytes: % x", gotRaw)
	}
	if !bytes.Equal(gotRaw, originalRaw) {
		t.Fatalf("restored bytes = % x, want original GB18030 bytes % x", gotRaw, originalRaw)
	}
}

func TestRestorePreservesGB18030EncodingAfterPersistence(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "sess.ckpt")
	a := filepath.Join(root, "gbk.txt")
	original := "\u4f60\u597d\n\u65e7\u884c\n"
	edited := "\u4f60\u597d\n\u65b0\u884c\n"
	originalRaw := fileenc.Encode(original, fileenc.GB18030)
	if err := os.WriteFile(a, originalRaw, 0o644); err != nil {
		t.Fatal(err)
	}

	s := New(dir, root)
	s.Begin(0, "edit gbk", 0)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: original})

	resumed := New(dir, root)
	if err := os.WriteFile(a, fileenc.Encode(edited, fileenc.GB18030), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := resumed.RestoreCode(0); err != nil {
		t.Fatal(err)
	}
	if gotRaw := readBytes(t, a); !bytes.Equal(gotRaw, originalRaw) {
		t.Fatalf("restored bytes after persistence = % x, want original GB18030 bytes % x", gotRaw, originalRaw)
	}
}

func TestRestoreLegacySnapshotRequiresExplicitSafePath(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "sess.ckpt")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(root, "gbk.txt")
	original := "\u4f60\u597d\n\u65e7\u884c\n"
	edited := "\u4f60\u597d\n\u65b0\u884c\n"
	if err := os.WriteFile(a, fileenc.Encode(edited, fileenc.GB18030), 0o644); err != nil {
		t.Fatal(err)
	}

	legacy := Checkpoint{
		Turn:     0,
		Time:     time.Now(),
		Prompt:   "legacy",
		MsgIndex: 0,
		Files: []FileSnap{{
			Path:    a,
			Content: &original,
		}},
	}
	b, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "turn-0.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	resumed := New(dir, root)
	if _, _, err := resumed.RestoreCode(0); err == nil {
		t.Fatal("legacy restore must not silently overwrite an unverifiable file")
	}
	if got := string(fileenc.Decode(readBytes(t, a), fileenc.GB18030)); got != edited {
		t.Fatalf("legacy refusal changed file to %q, want edited content preserved", got)
	}
}

func TestSnapshotDedupsFirstTouchWins(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	write(t, a, "orig")
	s := New("", root)
	s.Begin(0, "p", 0)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "orig"})
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "edited-once"}) // ignored
	write(t, a, "edited-twice")
	if _, _, err := s.RestoreCode(0); err != nil {
		t.Fatal(err)
	}
	if got := read(t, a); got != "orig" {
		t.Fatalf("a = %q, want orig (first snapshot wins)", got)
	}
}

func TestPersistV3KeepsCreatedFileSentinel(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "sess.ckpt")
	existing := filepath.Join(root, "existing.txt")
	created := filepath.Join(root, "created.txt")
	write(t, existing, "before")

	s := New(dir, root)
	s.Begin(0, "compat", 0)
	s.CaptureBefore(existing, CaptureBeforeOpts{Source: CaptureBeforeMutation})
	s.CaptureBefore(created, CaptureBeforeOpts{Source: CaptureBeforeMutation})

	type v3File struct {
		Path    string  `json:"path"`
		Content *string `json:"content"`
	}
	type v3Checkpoint struct {
		SchemaVersion int      `json:"schemaVersion"`
		Files         []v3File `json:"files"`
	}
	var meta v3Checkpoint
	b, err := os.ReadFile(filepath.Join(dir, "turns", "0", "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.SchemaVersion != SchemaV3 {
		t.Fatalf("schema = %d, want v3", meta.SchemaVersion)
	}
	var existingIdx = -1
	for i, file := range meta.Files {
		if file.Path == existing {
			existingIdx = i
			if file.Content != nil {
				t.Fatalf("v3 meta should not inline existing content: %#v", file.Content)
			}
		}
		if file.Path == created && file.Content != nil {
			t.Fatalf("created-file sentinel must stay nil: %#v", file.Content)
		}
	}
	if existingIdx < 0 {
		t.Fatal("existing file missing from v3 meta")
	}
	raw, err := os.ReadFile(filepath.Join(dir, "turns", "0", "files", fmt.Sprintf("%04d.before", existingIdx)))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "before" {
		t.Fatalf("before payload = %q", raw)
	}
}

func TestGCDoesNotDeleteSharedBlobStillReferencedByNewerCheckpoint(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "sess.ckpt")
	s := New(dir, root)
	content := "shared"
	ref, err := s.blobs.Put([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	s.done = []*Checkpoint{
		{SchemaVersion: SchemaV2, Turn: 0, Files: []FileSnap{{Path: "a.txt", Content: &content, SHA256: ref, BlobRef: ref}}},
		{SchemaVersion: SchemaV2, Turn: 1, Files: []FileSnap{{Path: "b.txt", Content: &content, SHA256: ref, BlobRef: ref}}},
	}

	s.mu.Lock()
	s.retainN = 1
	s.gcLocked()
	s.mu.Unlock()
	if ref == "" || !s.blobs.Has(ref) {
		t.Fatalf("shared blob %q was removed while the newer checkpoint still referenced it", ref)
	}
	if s.done[0].Files[0].BlobRef != "" || s.done[1].Files[0].BlobRef != ref {
		t.Fatalf("legacy GC refs = old %q new %q", s.done[0].Files[0].BlobRef, s.done[1].Files[0].BlobRef)
	}
}

func TestExpiredV2PayloadRemainsSafeForLegacyReader(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "sess.ckpt")
	content := "must not be interpreted as absent"
	checkpoint := &Checkpoint{
		SchemaVersion: SchemaV2,
		Turn:          0,
		Files: []FileSnap{{
			Path: "a.txt", Content: &content, SHA256: Digest([]byte(content)), BlobRef: Digest([]byte(content)),
		}},
	}
	store := New(dir, root)
	if err := store.persist(checkpoint); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	err := store.expirePayloadLocked(checkpoint)
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	// A previous release only scans turn-*.json in the checkpoint root. If the
	// expired checkpoint remains visible there, its content must never be nil:
	// old RestoreCode interprets nil as "delete this file".
	raw, err := os.ReadFile(filepath.Join(dir, "turn-0.json"))
	if err == nil {
		var legacy struct {
			Files []struct {
				Content *string `json:"content"`
			} `json:"files"`
		}
		if err := json.Unmarshal(raw, &legacy); err != nil {
			t.Fatal(err)
		}
		if len(legacy.Files) != 1 || legacy.Files[0].Content == nil {
			t.Fatal("expired v2 payload tells a legacy reader to delete an existing file")
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}

	reloaded := New(dir, root)
	metas := reloaded.List()
	if len(metas) != 1 || !metas[0].ExpiredFilePayload || metas[0].CanUndoFiles {
		t.Fatalf("expired metadata was not preserved for the new reader: %+v", metas)
	}
}

func TestBlobReadVerifiesContentAddress(t *testing.T) {
	store := NewBlobStore(t.TempDir())
	ref, err := store.Put([]byte("before"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.path(ref), []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Get(ref); err == nil {
		t.Fatalf("content-addressed read accepted bytes %q that do not match %s", got, ref)
	}
	if store.Has(ref) {
		t.Fatal("Has accepted a blob whose bytes do not match its content address")
	}
	if gotRef, err := store.Put([]byte("before")); err != nil || gotRef != ref {
		t.Fatalf("Put did not repair corrupt blob: ref=%q err=%v", gotRef, err)
	}
	if got, err := store.Get(ref); err != nil || string(got) != "before" {
		t.Fatalf("repaired blob = %q err=%v", got, err)
	}
}

func TestRestoreRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "evil.txt")
	write(t, outside, "keep")
	s := New("", root)
	s.Begin(0, "p", 0)
	s.Snapshot(diff.Change{Path: outside, Kind: diff.Modify, OldText: "hacked"})
	if _, _, err := s.RestoreCode(0); err == nil {
		t.Fatal("RestoreCode should reject a path outside the workspace")
	}
	if got := read(t, outside); got != "keep" {
		t.Fatalf("outside file was modified: %q", got)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "sess.ckpt")
	a := filepath.Join(root, "a.txt")

	s := New(dir, root)
	s.Begin(0, "hello", 1)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "v0"})
	s.Begin(1, "world", 5)

	// A fresh store over the same dir must see both turns and their boundaries.
	s2 := New(dir, root)
	metas := s2.List()
	if len(metas) != 2 {
		t.Fatalf("loaded %d checkpoints, want 2", len(metas))
	}
	if metas[0].Prompt != "hello" || metas[1].Prompt != "world" {
		t.Fatalf("prompts = %q, %q", metas[0].Prompt, metas[1].Prompt)
	}
	// Boundaries must survive the round-trip so a resumed session can rewind/fork.
	b := s2.Bounds()
	if b[0] != 1 || b[1] != 5 {
		t.Fatalf("bounds = %v, want {0:1, 1:5}", b)
	}
	if s2.NextTurn() != 2 {
		t.Fatalf("NextTurn = %d, want 2", s2.NextTurn())
	}
}

func TestListExposesCurrentTurnFiles(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	write(t, a, "v0")
	s := New("", root)
	s.Begin(0, "edit current", 0)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "v0"})

	metas := s.List()
	if len(metas) != 1 {
		t.Fatalf("metas = %d, want 1", len(metas))
	}
	if len(metas[0].Paths) != 1 || metas[0].Paths[0] != a {
		t.Fatalf("current turn paths = %#v, want [%q]", metas[0].Paths, a)
	}
}

func TestFileStateReturnsEarliestSnapshotAcrossPathForms(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "nested", "file.txt")
	s := New("", root)
	s.Begin(0, "first", 0)
	s.Snapshot(diff.Change{Path: path, Kind: diff.Modify, OldText: "original"})
	s.Begin(1, "second", 2)
	s.Snapshot(diff.Change{Path: filepath.Join("nested", "file.txt"), Kind: diff.Modify, OldText: "after first edit"})

	state, ok := s.FileState(filepath.Join("nested", "file.txt"))
	if !ok || state.Content == nil {
		t.Fatalf("FileState = %+v, %v; want earliest content", state, ok)
	}
	if got := *state.Content; got != "original" {
		t.Fatalf("FileState content = %q, want original", got)
	}
	if _, ok := s.FileState(filepath.Join("..", "outside.txt")); ok {
		t.Fatal("FileState accepted a path outside the workspace")
	}
}

func TestTruncateFromDropsFutureCheckpointsAndFiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "sess.ckpt")
	a := filepath.Join(root, "a.txt")
	write(t, a, "v0")
	s := New(dir, root)
	s.Begin(0, "first", 0)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "v0"})
	s.Begin(1, "second", 2)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: "v1"})
	s.Begin(2, "third", 4)

	if err := s.TruncateFrom(1); err != nil {
		t.Fatal(err)
	}

	metas := s.List()
	if len(metas) != 1 || metas[0].Turn != 0 {
		t.Fatalf("metas after truncate = %+v, want only turn 0", metas)
	}
	if s.NextTurn() != 1 {
		t.Fatalf("NextTurn after truncate = %d, want 1", s.NextTurn())
	}
	if _, err := os.Stat(filepath.Join(dir, "turns", "1")); !os.IsNotExist(err) {
		t.Fatalf("turn-1 checkpoint should be deleted, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "turns", "2")); !os.IsNotExist(err) {
		t.Fatalf("turn-2 checkpoint should be deleted, stat err=%v", err)
	}
	reloaded := New(dir, root)
	if got := reloaded.List(); len(got) != 1 || got[0].Turn != 0 {
		t.Fatalf("reloaded metas after truncate = %+v, want only turn 0", got)
	}
}

func TestTruncateFromReportsPersistentDeleteFailure(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "sess.ckpt")
	store := New(dir, root)
	store.Begin(0, "first", 0)
	store.Begin(1, "second", 2)
	blocked := filepath.Join(dir, "turn-1.json")
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := store.TruncateFrom(1); err == nil {
		t.Fatal("truncate reported success despite a persistent checkpoint delete failure")
	}
	metas := store.List()
	if len(metas) != 2 || metas[1].Turn != 1 {
		t.Fatalf("failed truncate mutated in-memory checkpoints: %+v", metas)
	}
}

func BenchmarkRestoreGB18030Encoding(b *testing.B) {
	root := b.TempDir()
	a := filepath.Join(root, "gbk.txt")
	original := strings.Repeat("\u4f60\u597d\u4e16\u754c\n\u65e7\u884c\n", 8192)
	edited := strings.Repeat("\u4f60\u597d\u4e16\u754c\n\u65b0\u884c\n", 8192)
	originalRaw := fileenc.Encode(original, fileenc.GB18030)
	editedRaw := fileenc.Encode(edited, fileenc.GB18030)
	if err := os.WriteFile(a, originalRaw, 0o644); err != nil {
		b.Fatal(err)
	}

	s := New("", root)
	s.Begin(0, "edit gbk", 0)
	s.Snapshot(diff.Change{Path: a, Kind: diff.Modify, OldText: original})

	b.SetBytes(int64(len(originalRaw)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := os.WriteFile(a, editedRaw, 0o644); err != nil {
			b.Fatal(err)
		}
		if _, _, err := s.RestoreCode(0); err != nil {
			b.Fatal(err)
		}
	}
}

func TestLazyDirectoryCreation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "lazy-sess.ckpt")

	s := New(dir, root)

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("directory should not exist yet: %v", err)
	}

	s.Begin(0, "lazy", 0)

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("directory should now exist: %v", err)
	}
	turnPath := filepath.Join(dir, "turns", "0", "meta.json")
	if _, err := os.Stat(turnPath); err != nil {
		t.Fatalf("turn file should now exist: %v", err)
	}
}
