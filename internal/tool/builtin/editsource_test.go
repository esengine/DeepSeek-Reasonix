package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The disk copy is the last saved version; the overlay holds the user's unsaved
// buffer. An edit must read and write the buffer, never round-trip through the
// stale disk copy and overwrite what the user has not saved yet.
func TestEditFileOverlayEditsUnsavedBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("saved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := &fakeOverlay{
		files:  map[string]string{path: "unsaved edit\n"},
		writes: map[string]string{},
	}
	ef := editFile{workDir: dir, roots: realRoots([]string{dir}), overlay: overlay}

	args, _ := json.Marshal(map[string]string{
		"path": "a.go", "old_string": "unsaved", "new_string": "agent",
	})
	if _, err := ef.Execute(context.Background(), json.RawMessage(args)); err != nil {
		t.Fatalf("Execute: %v — old_string only exists in the buffer, so a disk read fails here", err)
	}
	if got := overlay.writes[path]; got != "agent edit\n" {
		t.Fatalf("overlay writes[a.go] = %q, want %q", got, "agent edit\n")
	}
	if b, _ := os.ReadFile(path); string(b) != "saved\n" {
		t.Fatalf("disk = %q, want it untouched — the host owns persisting the buffer", b)
	}
}

// Preview must resolve the file exactly as Execute will, or the diff a user
// approves is not the change that runs.
func TestEditFilePreviewMatchesOverlayExecute(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("saved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := &fakeOverlay{
		files:  map[string]string{path: "unsaved edit\n"},
		writes: map[string]string{},
	}
	ef := editFile{workDir: dir, roots: realRoots([]string{dir}), overlay: overlay}
	args, _ := json.Marshal(map[string]string{
		"path": "a.go", "old_string": "unsaved", "new_string": "agent",
	})

	change, err := ef.Preview(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if change.OldText != "unsaved edit\n" {
		t.Fatalf("preview OldText = %q, want the buffer content", change.OldText)
	}
	if _, err := ef.Execute(context.Background(), json.RawMessage(args)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := overlay.writes[path]; got != change.NewText {
		t.Fatalf("executed %q but previewed %q", got, change.NewText)
	}
}

func TestMultiEditOverlayEditsUnsavedBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.go")
	if err := os.WriteFile(path, []byte("saved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := &fakeOverlay{
		files:  map[string]string{path: "alpha\nbeta\n"},
		writes: map[string]string{},
	}
	me := multiEdit{workDir: dir, roots: realRoots([]string{dir}), overlay: overlay}

	args, _ := json.Marshal(map[string]any{
		"path": "m.go",
		"edits": []map[string]any{
			{"old_string": "alpha", "new_string": "ALPHA"},
			{"old_string": "beta", "new_string": "BETA"},
		},
	})
	if _, err := me.Execute(context.Background(), json.RawMessage(args)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := overlay.writes[path]; got != "ALPHA\nBETA\n" {
		t.Fatalf("overlay writes = %q, want %q", got, "ALPHA\nBETA\n")
	}
	if b, _ := os.ReadFile(path); string(b) != "saved\n" {
		t.Fatalf("disk = %q, want untouched", b)
	}
}

// A non-UTF-8 file must stay entirely on the disk route. Routing it through the
// text-only overlay would rewrite it as UTF-8 and destroy the original encoding.
func TestEditFileOverlaySkipsNonUTF8(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "utf16.txt")
	if err := os.WriteFile(path, []byte{0xFF, 0xFE, 'h', 0, 'i', 0}, 0o644); err != nil {
		t.Fatal(err)
	}
	// Content that would make the edit fail if the overlay were consulted.
	overlay := &fakeOverlay{
		files:  map[string]string{path: "OVERLAY"},
		writes: map[string]string{},
	}
	ef := editFile{workDir: dir, roots: realRoots([]string{dir}), overlay: overlay}

	args, _ := json.Marshal(map[string]string{
		"path": "utf16.txt", "old_string": "hi", "new_string": "ok",
	})
	if _, err := ef.Execute(context.Background(), json.RawMessage(args)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(overlay.writes) != 0 {
		t.Fatalf("non-UTF-8 target must bypass the overlay; writes = %v", overlay.writes)
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) < 2 || b[0] != 0xFF || b[1] != 0xFE {
		t.Fatalf("local write must preserve the UTF-16 BOM; got % x, %v", b, err)
	}
}

func TestEditFileOverlayMissFallsBackToDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.go")
	if err := os.WriteFile(path, []byte("disk only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := &fakeOverlay{files: map[string]string{}, writes: map[string]string{}}
	ef := editFile{workDir: dir, roots: realRoots([]string{dir}), overlay: overlay}

	args, _ := json.Marshal(map[string]string{
		"path": "d.go", "old_string": "disk", "new_string": "local",
	})
	if _, err := ef.Execute(context.Background(), json.RawMessage(args)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(overlay.writes) != 0 {
		t.Fatalf("a read that missed the overlay must not write to it; writes = %v", overlay.writes)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "local only") {
		t.Fatalf("disk = %q, want the edit applied locally", b)
	}
}

func TestDeleteRangeOverlayEditsUnsavedBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.txt")
	if err := os.WriteFile(path, []byte("saved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := &fakeOverlay{
		files:  map[string]string{path: "keep\ncut one\ncut two\nkeep tail\n"},
		writes: map[string]string{},
	}
	dr := deleteRange{workDir: dir, roots: realRoots([]string{dir}), overlay: overlay}

	args, _ := json.Marshal(map[string]string{
		"path": "r.txt", "start_anchor": "cut one", "end_anchor": "cut two",
	})
	if _, err := dr.Execute(context.Background(), json.RawMessage(args)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := overlay.writes[path]; got != "keep\nkeep tail\n" {
		t.Fatalf("overlay writes = %q, want %q", got, "keep\nkeep tail\n")
	}
	if b, _ := os.ReadFile(path); string(b) != "saved\n" {
		t.Fatalf("disk = %q, want untouched", b)
	}
}
