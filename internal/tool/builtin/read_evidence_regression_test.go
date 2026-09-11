package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/tool"
)

func TestReadSnapshotDetectsSameSizeRestoredMtime(t *testing.T) {
	dir, path := writeEnvelopeFixture(t, "source.txt", 2)
	r := readFile{workDir: dir}
	before, _, _ := readEnvelope(t, r, `{"path":"source.txt"}`)
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = 'X'
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, _, _ := readEnvelope(t, r, `{"path":"source.txt"}`)
	if before.Source.Snapshot == after.Source.Snapshot {
		t.Fatal("same metadata concealed a raw content change")
	}
}

func TestLocalRangeDoesNotCaptureALargeWholeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("a bounded local line\n", 20000)), 0600); err != nil {
		t.Fatal(err)
	}
	r := readFile{workDir: dir}
	args := json.RawMessage(`{"path":"large.txt","offset":100,"limit":2}`)
	output, env, err := r.ExecuteRead(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if env.Source.Snapshot != "" || len(output) > 300 {
		t.Fatal("local range captured or delivered a whole large source")
	}
	_, full, err := r.ExecuteRead(tool.WithFullReadSnapshot(context.Background()), args)
	if err != nil || full.Source.Snapshot == "" {
		t.Fatalf("full continuation lost its version: %v", err)
	}
}

func TestReadPathIdentitySurvivesInvalidWindow(t *testing.T) {
	dir := t.TempDir()
	r := readFile{workDir: dir}
	path, err := r.ResolveReadPath(json.RawMessage(`{"path":"a.txt","intent":"full","offset":5}`))
	if err != nil || path != filepath.Join(dir, "a.txt") {
		t.Fatalf("lost failed continuation identity: %q %v", path, err)
	}
}

type changingReadOverlay struct{ calls int }

func (o *changingReadOverlay) ReadTextFile(context.Context, string) (string, bool) {
	o.calls++
	if o.calls == 1 {
		return "first\n", true
	}
	return "second\n", true
}
func (*changingReadOverlay) WriteTextFile(context.Context, string, string) (bool, error) {
	return false, nil
}

func TestReadSnapshotUsesExactlyTheServingOverlayBuffer(t *testing.T) {
	o := &changingReadOverlay{}
	r := readFile{workDir: t.TempDir(), overlay: o}
	output, env, err := r.ExecuteRead(context.Background(), json.RawMessage(`{"path":"buffer.txt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if o.calls != 1 || !strings.Contains(output, "first") || env.Source.Identity != "overlay:"+digestText("first\n") {
		t.Fatalf("output/version came from different buffers: %q %+v calls=%d", output, env.Source, o.calls)
	}
}

func TestWriterEvidenceUsesAllOriginalChangedRanges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("alpha\nmiddle\nomega\n"), 0600); err != nil {
		t.Fatal(err)
	}
	m := multiEdit{workDir: dir}
	info, err := m.DeclareEvidenceTarget(context.Background(), json.RawMessage(`{"path":"a.txt","edits":[{"old_string":"alpha","new_string":"ALPHA"},{"old_string":"ALPHA","new_string":"NEW"},{"old_string":"omega","new_string":"LAST"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Ranges) != 2 || len(info.Hashes) != 2 || info.Hashes[0] != digestText("alpha") || info.Hashes[1] != digestText("omega") {
		t.Fatalf("wrong original evidence: %+v", info)
	}
}

func TestWriterChecksPreflightSourceAtExecution(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("alpha\n"), 0600); err != nil {
		t.Fatal(err)
	}
	w := writeFile{workDir: dir}
	args := json.RawMessage(`{"path":"a.txt","content":"replacement"}`)
	info, err := w.DeclareEvidenceTarget(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("new user content\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = w.Execute(tool.WithExpectedWriteSource(context.Background(), info), args)
	if !errors.Is(err, ErrFileChanged) {
		t.Fatalf("want stale preflight rejection: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new user content\n" {
		t.Fatal("overwrote new user content")
	}
}

func TestWriteEvidenceIncludesUnsavedNewBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unsaved.txt")
	w := writeFile{workDir: dir, overlay: &fakeOverlay{files: map[string]string{path: "unsaved\n"}}}
	info, err := w.DeclareEvidenceTarget(context.Background(), json.RawMessage(`{"path":"unsaved.txt","content":"replace"}`))
	if err != nil || !info.WholeFile || info.Snapshot == "" {
		t.Fatalf("unsaved buffer treated as a new empty file: %+v %v", info, err)
	}
}
