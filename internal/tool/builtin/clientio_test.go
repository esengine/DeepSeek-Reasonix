package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/sandbox"
	"reasonix/internal/secrets"
)

// fakeOverlay serves a fixed path→content map and records writes.
type fakeOverlay struct {
	files  map[string]string
	writes map[string]string
	wErr   error
}

func (f *fakeOverlay) ReadTextFile(_ context.Context, path string) (string, bool) {
	content, ok := f.files[path]
	return content, ok
}

func (f *fakeOverlay) WriteTextFile(_ context.Context, path, content string) (bool, error) {
	if f.writes == nil {
		return false, nil
	}
	if f.wErr != nil {
		return true, f.wErr
	}
	f.writes[path] = content
	return true, nil
}

func TestReadFileOverlayServesBufferContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.go")
	if err := os.WriteFile(path, []byte("disk line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := &fakeOverlay{files: map[string]string{path: "buffer line one\nbuffer line two\n"}}
	rf := readFile{workDir: dir, overlay: overlay}

	out, err := rf.Execute(context.Background(), json.RawMessage(`{"path":"a.go"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "buffer line one") || strings.Contains(out, "disk line") {
		t.Fatalf("overlay content should win over disk; got:\n%s", out)
	}
	if !strings.Contains(out, "1→") && !strings.Contains(out, "1\t") {
		t.Fatalf("overlay content must keep the numbered-line rendering; got:\n%s", out)
	}
}

func TestReadFileOverlayFallsBackToDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.go")
	if err := os.WriteFile(path, []byte("disk only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rf := readFile{workDir: dir, overlay: &fakeOverlay{files: map[string]string{}}}
	out, err := rf.Execute(context.Background(), json.RawMessage(`{"path":"b.go"}`))
	if err != nil || !strings.Contains(out, "disk only") {
		t.Fatalf("overlay miss must fall back to disk; got %q, %v", out, err)
	}
}

func TestWriteFileOverlayAppliesWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.go")
	overlay := &fakeOverlay{writes: map[string]string{}}
	wf := writeFile{workDir: dir, roots: realRoots([]string{dir}), overlay: overlay}

	args, _ := json.Marshal(map[string]string{"path": "c.go", "content": "hello"})
	out, err := wf.Execute(context.Background(), json.RawMessage(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if overlay.writes[path] != "hello" {
		t.Fatalf("overlay writes = %v, want %s→hello", overlay.writes, path)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Fatal("overlay-handled write must not also write the local disk")
	}
	if !strings.Contains(out, "wrote 5 bytes") {
		t.Fatalf("output = %q", out)
	}

	// A client-side write failure surfaces instead of silently double-applying.
	overlay.wErr = fmt.Errorf("readonly buffer")
	if _, err := wf.Execute(context.Background(), json.RawMessage(args)); err == nil {
		t.Fatal("overlay write error must surface")
	}
}

func TestWriteFileOverlaySkipsNonUTF8(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "utf16.txt")
	// UTF-16LE BOM + "hi" — the overlay is text-only, so this file must stay on
	// the local encoding-preserving path.
	if err := os.WriteFile(path, []byte{0xFF, 0xFE, 'h', 0, 'i', 0}, 0o644); err != nil {
		t.Fatal(err)
	}
	overlay := &fakeOverlay{writes: map[string]string{}}
	wf := writeFile{workDir: dir, roots: realRoots([]string{dir}), overlay: overlay}
	args, _ := json.Marshal(map[string]string{"path": "utf16.txt", "content": "changed"})
	if _, err := wf.Execute(context.Background(), json.RawMessage(args)); err != nil {
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

// fakeTerminal records commands and returns a scripted result.
type fakeTerminal struct {
	out    string
	ok     bool
	err    error
	called []string
}

func (f *fakeTerminal) RunCommand(_ context.Context, command, _ string, _ time.Duration) (string, bool, error) {
	f.called = append(f.called, command)
	return f.out, f.ok, f.err
}

func TestBashRoutesToClientTerminal(t *testing.T) {
	term := &fakeTerminal{out: "client says hi", ok: true}
	b := bash{workDir: t.TempDir(), terminal: term}
	out, err := b.Execute(context.Background(), json.RawMessage(`{"command":"echo hi"}`))
	if err != nil || out != "client says hi" {
		t.Fatalf("Execute = %q, %v", out, err)
	}
	if len(term.called) != 1 || term.called[0] != "echo hi" {
		t.Fatalf("terminal calls = %v", term.called)
	}
}

func TestBashTerminalFallsBackWhenUnhandled(t *testing.T) {
	term := &fakeTerminal{ok: false}
	b := bash{workDir: t.TempDir(), terminal: term}
	out, err := b.Execute(context.Background(), json.RawMessage(`{"command":"printf local"}`))
	if err != nil || !strings.Contains(out, "local") {
		t.Fatalf("unhandled terminal must fall back to local execution; got %q, %v", out, err)
	}
}

func TestBashTerminalSkippedWhenSandboxEnforced(t *testing.T) {
	term := &fakeTerminal{out: "must not run", ok: true}
	b := bash{workDir: t.TempDir(), sb: sandbox.Spec{Mode: "enforce"}, terminal: term}
	// The command itself may fail (no sandbox binary in the test env); the
	// assertion is only that the client terminal was never consulted.
	_, _ = b.Execute(context.Background(), json.RawMessage(`{"command":"echo hi"}`))
	if len(term.called) != 0 {
		t.Fatalf("enforced sandbox must never route to the client terminal; calls = %v", term.called)
	}
}

func TestBashTerminalSkippedWhenEnvFilteringEnabled(t *testing.T) {
	secrets.SetFilterSubprocessEnv(true)
	t.Cleanup(func() { secrets.SetFilterSubprocessEnv(false) })
	term := &fakeTerminal{out: "must not run", ok: true}
	b := bash{workDir: t.TempDir(), terminal: term}
	// The client terminal spawns with its own unfiltered environment, so an
	// enabled [secrets].filter_subprocess_env must force local execution.
	out, err := b.Execute(context.Background(), json.RawMessage(`{"command":"printf local"}`))
	if err != nil || !strings.Contains(out, "local") {
		t.Fatalf("env filtering must fall back to local execution; got %q, %v", out, err)
	}
	if len(term.called) != 0 {
		t.Fatalf("env filtering must never route to the client terminal; calls = %v", term.called)
	}
}
