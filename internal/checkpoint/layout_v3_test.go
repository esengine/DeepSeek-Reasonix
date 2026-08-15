package checkpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestV3PersistAndReload(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "ckpt")
	s := New(dir, root)
	s.Begin(1, "edit", 0)
	target := filepath.Join(root, "a.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.CaptureBefore(target, CaptureBeforeOpts{})

	meta := filepath.Join(dir, "turns", "1", "meta.json")
	if _, err := os.Stat(meta); err != nil {
		t.Fatalf("v3 meta missing: %v", err)
	}
	markerPath := filepath.Join(dir, "turn-1.json")
	markerBytes, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("compatibility marker missing: %v", err)
	}
	var marker Checkpoint
	if err := json.Unmarshal(markerBytes, &marker); err != nil {
		t.Fatalf("decode compatibility marker: %v", err)
	}
	if marker.SchemaVersion != SchemaV2 || !marker.ExpiredFilePayload || len(marker.Files) != 0 {
		t.Fatalf("compatibility marker = %+v, want payload-free expired v2", marker)
	}
	before := filepath.Join(dir, "turns", "1", "files", "0000.before")
	raw, err := os.ReadFile(before)
	if err != nil {
		t.Fatalf("before payload: %v", err)
	}
	if string(raw) != "hello" {
		t.Fatalf("before payload = %q", raw)
	}
	if size, err := s.blobs.Size(); err != nil || size != 0 {
		t.Fatalf("v3 capture should not duplicate payloads in blobs: size=%d err=%v", size, err)
	}

	reloaded := New(dir, root)
	if len(reloaded.done) != 1 || reloaded.done[0].Turn != 1 || reloaded.done[0].SchemaVersion != SchemaV3 {
		t.Fatalf("reloaded = %+v", reloaded.done)
	}
	got := reloaded.done[0].Files
	if len(got) != 1 || got[0].Content == nil || *got[0].Content != "hello" {
		t.Fatalf("reloaded files = %+v", got)
	}
}

func TestV3PersistsMalformedEncodedPreimageExactly(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "ckpt")
	target := filepath.Join(root, "odd-utf16.txt")
	want := []byte{0xff, 0xfe, 0x00}
	if err := os.WriteFile(target, want, 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir, root)
	s.Begin(0, "edit", 0)
	s.CaptureBefore(target, CaptureBeforeOpts{})

	got, err := os.ReadFile(filepath.Join(dir, "turns", "0", "files", "0000.before"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("raw preimage = %x, want %x", got, want)
	}
}

func TestV3LoadRejectsCorruptPayload(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "ckpt")
	target := filepath.Join(root, "a.txt")
	if err := os.WriteFile(target, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir, root)
	s.Begin(0, "edit", 0)
	s.CaptureBefore(target, CaptureBeforeOpts{})
	if err := os.WriteFile(filepath.Join(dir, "turns", "0", "files", "0000.before"), []byte("corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}

	reloaded := New(dir, root)
	if len(reloaded.done) != 1 || len(reloaded.done[0].Files) != 1 {
		t.Fatalf("reloaded = %+v", reloaded.done)
	}
	if reloaded.done[0].Files[0].Content != nil {
		t.Fatal("corrupt payload must not become restore content")
	}
	conflicts := reloaded.precheckFiles(0)
	if len(conflicts) != 1 || conflicts[0].Reason != ConflictMissingPayload {
		t.Fatalf("conflicts = %+v, want missing payload", conflicts)
	}
}

func TestV3RetentionRemovesWholeOldTurnDirectories(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "ckpt")
	target := filepath.Join(root, "a.txt")
	s := New(dir, root)
	s.retainN = 2
	for turn := range 4 {
		if err := os.WriteFile(target, []byte{byte('0' + turn)}, 0o644); err != nil {
			t.Fatal(err)
		}
		s.Begin(turn, "edit", turn)
		s.CaptureBefore(target, CaptureBeforeOpts{})
	}
	for _, turn := range []string{"0", "1"} {
		if _, err := os.Stat(filepath.Join(dir, "turns", turn)); !os.IsNotExist(err) {
			t.Fatalf("old turn %s was not removed: %v", turn, err)
		}
	}
	for _, turn := range []string{"2", "3"} {
		if _, err := os.Stat(filepath.Join(dir, "turns", turn, "meta.json")); err != nil {
			t.Fatalf("retained turn %s missing: %v", turn, err)
		}
	}
	metas := s.List()
	if len(metas) != 2 || metas[0].Turn != 2 || metas[1].Turn != 3 {
		t.Fatalf("retained turns = %+v", metas)
	}
}

func TestV3LoadKeepsLegacyTurnJSON(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ckpt")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"schemaVersion":2,"turn":0,"prompt":"old","files":[{"path":"a.txt","content":"v2"}]}`)
	if err := os.WriteFile(filepath.Join(dir, "turn-0.json"), legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir, t.TempDir())
	s.Begin(1, "new", 1)
	s.Begin(2, "flush", 2)
	reloaded := New(dir, t.TempDir())
	if len(reloaded.done) < 1 {
		t.Fatal("expected reloaded checkpoints")
	}
	var sawLegacy, sawV3 bool
	for _, c := range reloaded.done {
		if c.Turn == 0 && c.SchemaVersion == SchemaV2 {
			sawLegacy = true
		}
		if c.Turn == 1 && c.SchemaVersion == SchemaV3 {
			sawV3 = true
		}
	}
	if !sawLegacy || !sawV3 {
		t.Fatalf("legacy=%v v3=%v done=%+v", sawLegacy, sawV3, reloaded.done)
	}
}

func TestV3CompatibilityMarkerKeepsPreviousReaderMonotonic(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "ckpt")
	target := filepath.Join(root, "a.txt")
	if err := os.WriteFile(target, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(dir, root)
	s.Begin(0, "v3", 0)
	s.CaptureBefore(target, CaptureBeforeOpts{})

	previousNext := 0
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		var turn int
		if n, scanErr := fmt.Sscanf(entry.Name(), "turn-%d.json", &turn); scanErr != nil || n != 1 {
			continue
		}
		if turn >= previousNext {
			previousNext = turn + 1
		}
	}
	if previousNext != 1 {
		t.Fatalf("previous reader NextTurn = %d, want 1", previousNext)
	}

	legacy := Checkpoint{
		SchemaVersion: SchemaV2,
		Turn:          previousNext,
		Time:          time.Now().Add(time.Second),
		Prompt:        "downgrade-new",
		Files:         []FileSnap{},
	}
	b, err := json.Marshal(&legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "turn-1.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	reloaded := New(dir, root)
	metas := reloaded.List()
	if len(metas) != 2 || metas[0].Turn != 0 || metas[1].Turn != 1 || metas[1].Prompt != "downgrade-new" {
		t.Fatalf("reloaded checkpoints = %+v", metas)
	}
}

func TestV3MarkerDeletionFromPreviousReaderTombstonesTurn(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "ckpt")
	target := filepath.Join(root, "a.txt")
	s := New(dir, root)
	for turn := range 3 {
		if err := os.WriteFile(target, []byte{byte('0' + turn)}, 0o644); err != nil {
			t.Fatal(err)
		}
		s.Begin(turn, "edit", turn)
		s.CaptureBefore(target, CaptureBeforeOpts{})
	}

	// Supported previous readers truncate only their visible turn-N.json files;
	// they do not know about turns/<n>. Simulate a downgrade rewind at turn 1.
	for _, turn := range []int{1, 2} {
		if err := os.Remove(filepath.Join(dir, fmt.Sprintf("turn-%d.json", turn))); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "turns", fmt.Sprint(turn), "meta.json")); err != nil {
			t.Fatalf("v3 directory %d unexpectedly missing: %v", turn, err)
		}
	}

	reloaded := New(dir, root)
	metas := reloaded.List()
	if len(metas) != 1 || metas[0].Turn != 0 {
		t.Fatalf("markerless future turns resurrected: %+v", metas)
	}
	if got := reloaded.NextTurn(); got != 1 {
		t.Fatalf("NextTurn after downgrade truncate = %d, want 1", got)
	}
}

func TestV3LoadPrefersNewerLegacyCheckpointOnHistoricalTurnCollision(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "ckpt")
	s := New(dir, root)
	s.Begin(0, "v3-old", 0)

	legacy := Checkpoint{
		SchemaVersion: SchemaV2,
		Turn:          0,
		Time:          s.cur.Time.Add(time.Minute),
		Prompt:        "downgrade-new",
		Files:         []FileSnap{},
	}
	b, err := json.Marshal(&legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "turn-0.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	reloaded := New(dir, root)
	if len(reloaded.done) != 1 || reloaded.done[0].SchemaVersion != SchemaV2 || reloaded.done[0].Prompt != "downgrade-new" {
		t.Fatalf("collision selected %+v, want newer legacy checkpoint", reloaded.done)
	}
}

func TestV3PayloadQuotaPrunesOldestWholeTurn(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(t.TempDir(), "ckpt")
	target := filepath.Join(root, "a.txt")
	s := New(dir, root)
	s.retainN = 100
	s.blobQuota = 8
	for turn, body := range []string{"123456", "abcdef"} {
		if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		s.Begin(turn, "edit", turn)
		s.CaptureBefore(target, CaptureBeforeOpts{})
	}
	if _, err := os.Stat(filepath.Join(dir, "turns", "0")); !os.IsNotExist(err) {
		t.Fatalf("old v3 turn survived payload quota: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "turn-0.json")); !os.IsNotExist(err) {
		t.Fatalf("old compatibility marker survived payload quota: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "turns", "1", "meta.json")); err != nil {
		t.Fatalf("current v3 turn was pruned: %v", err)
	}
}
