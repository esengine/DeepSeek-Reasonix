package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// --- loadSessionTitles ---

func TestLoadSessionTitlesMissing(t *testing.T) {
	dir := t.TempDir()
	m := loadSessionTitles(dir)
	if len(m) != 0 {
		t.Errorf("missing file should return empty map, got %v", m)
	}
}

func TestLoadSessionTitlesCorrupt(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(sessionTitlesPath(dir), []byte(`{not json`), 0o644)
	m := loadSessionTitles(dir)
	if len(m) != 0 {
		t.Errorf("corrupt file should return empty map, got %v", m)
	}
}

func TestLoadSessionTitlesValid(t *testing.T) {
	dir := t.TempDir()
	data := map[string]string{"session-1.jsonl": "My Session", "session-2.jsonl": "Another"}
	b, _ := json.Marshal(data)
	os.WriteFile(sessionTitlesPath(dir), b, 0o644)
	m := loadSessionTitles(dir)
	if m["session-1.jsonl"] != "My Session" || m["session-2.jsonl"] != "Another" {
		t.Errorf("loaded = %v", m)
	}
}

// --- saveSessionTitles ---

func TestSaveSessionTitlesCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "sessions")
	m := map[string]string{"a.jsonl": "title A"}
	if err := saveSessionTitles(dir, m); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Verify file exists and is valid JSON.
	b, err := os.ReadFile(sessionTitlesPath(dir))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["a.jsonl"] != "title A" {
		t.Errorf("decoded = %v", decoded)
	}
}

func TestSaveSessionTitlesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	original := map[string]string{"s1.jsonl": "First", "s2.jsonl": "Second"}
	if err := saveSessionTitles(dir, original); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded := loadSessionTitles(dir)
	if loaded["s1.jsonl"] != "First" || loaded["s2.jsonl"] != "Second" {
		t.Errorf("round-trip = %v", loaded)
	}
}

// --- setSessionTitle ---

func TestSetSessionTitle(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "my-session.jsonl")

	// Set a title.
	if err := setSessionTitle(dir, sessionPath, "Custom Title"); err != nil {
		t.Fatalf("set: %v", err)
	}
	m := loadSessionTitles(dir)
	if m["my-session.jsonl"] != "Custom Title" {
		t.Errorf("title = %q", m["my-session.jsonl"])
	}

	// Clear the title (empty string).
	if err := setSessionTitle(dir, sessionPath, ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	m = loadSessionTitles(dir)
	if _, ok := m["my-session.jsonl"]; ok {
		t.Error("cleared title should be removed from map")
	}
}

func TestSetSessionTitleTrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "s.jsonl")
	if err := setSessionTitle(dir, sessionPath, "  trimmed  "); err != nil {
		t.Fatalf("set: %v", err)
	}
	m := loadSessionTitles(dir)
	if m["s.jsonl"] != "trimmed" {
		t.Errorf("title = %q, want trimmed", m["s.jsonl"])
	}
}

// --- deleteSessionFile ---

func TestDeleteSessionFile(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	os.WriteFile(sessionPath, []byte("data"), 0o644)

	// Set a title first.
	setSessionTitle(dir, sessionPath, "My Title")
	if err := recordSessionDisplay(dir, sessionPath, "expanded prompt", "[Pasted text #1 · 5 lines]"); err != nil {
		t.Fatalf("record display: %v", err)
	}

	if err := deleteSessionFile(dir, sessionPath); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// File should be gone.
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Error("session file should be deleted")
	}
	// Title should be gone.
	m := loadSessionTitles(dir)
	if _, ok := m["session.jsonl"]; ok {
		t.Error("title should be removed after delete")
	}
	if got := resolveSessionDisplay(dir, sessionPath, "expanded prompt"); got != "expanded prompt" {
		t.Errorf("display sidecar should be removed after delete, got %q", got)
	}
}

func TestDeleteSessionFileNoTitle(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "no-title.jsonl")
	os.WriteFile(sessionPath, []byte("data"), 0o644)

	if err := deleteSessionFile(dir, sessionPath); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Error("session file should be deleted")
	}
}

func TestDeleteSessionFileMissing(t *testing.T) {
	dir := t.TempDir()
	// Deleting a non-existent file should not error.
	if err := deleteSessionFile(dir, filepath.Join(dir, "missing.jsonl")); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
}

// --- sessionTitlesPath ---

func TestSessionTitlesPath(t *testing.T) {
	got := sessionTitlesPath("/sessions")
	want := filepath.Join("/sessions", ".titles.json")
	if got != want {
		t.Errorf("sessionTitlesPath = %q, want %q", got, want)
	}
}

// --- errActiveSession ---

func TestErrActiveSession(t *testing.T) {
	if errActiveSession.Error() == "" {
		t.Error("errActiveSession should have a message")
	}
}

func TestSessionDisplayRoundTrip(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "s.jsonl")
	content := "prefix\n--- Begin [Pasted text #1 · 5 lines] ---\nfull text\n--- End [Pasted text #1 · 5 lines] ---"
	display := "[Pasted text #1 · 5 lines]"
	if err := recordSessionDisplay(dir, sessionPath, content, display); err != nil {
		t.Fatalf("record display: %v", err)
	}
	if got := resolveSessionDisplay(dir, sessionPath, content); got != display {
		t.Fatalf("display = %q, want %q", got, display)
	}
	if got := resolveSessionDisplay(dir, sessionPath, "other"); got != "other" {
		t.Fatalf("unknown content should pass through, got %q", got)
	}
}

func TestRecordSessionDisplaySkipsNoop(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "s.jsonl")
	if err := recordSessionDisplay(dir, sessionPath, "same", "same"); err != nil {
		t.Fatalf("record display: %v", err)
	}
	if _, err := os.Stat(sessionDisplayPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("noop display should not create sidecar, stat err = %v", err)
	}
}
