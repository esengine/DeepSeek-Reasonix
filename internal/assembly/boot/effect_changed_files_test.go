package boot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

// changedFileProvider reads a file, lets something else edit it when edit is
// set, then overwrites the whole file the way a model writing from memory does.
type changedFileProvider struct {
	mu   sync.Mutex
	path string
	edit string
	turn int
	reqs []provider.Request
}

func (p *changedFileProvider) Name() string { return "boot-changed-file" }

func (p *changedFileProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.turn++
	turn := p.turn
	p.mu.Unlock()

	ch := make(chan provider.Chunk, 3)
	switch turn {
	case 1:
		emitReadFile(ch, "call-read", p.path)
	case 2:
		if p.edit != "" {
			_ = os.WriteFile(p.path, []byte(p.edit), 0o644)
		}
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "call-write", Name: "write_file",
			Arguments: `{"path":` + jsonString(p.path) + `,"content":"agent version\n"}`,
		}}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// The kind registers once per process; each run points it at its own script.
var (
	changedFileRegister sync.Once
	changedFileMu       sync.Mutex
	changedFileCurrent  *changedFileProvider
)

func jsonString(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

func runChangedFile(t *testing.T, edit, extraConfig string) (content, writeResult string) {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	target := filepath.Join(dir, "notes.md")
	writeFile(t, dir, "notes.md", "original\n")
	rec := &changedFileProvider{path: target, edit: edit}
	changedFileRegister.Do(func() {
		provider.Register("boot-changed-file", func(provider.Config) (provider.Provider, error) {
			changedFileMu.Lock()
			defer changedFileMu.Unlock()
			return changedFileCurrent, nil
		})
	})
	changedFileMu.Lock()
	changedFileCurrent = rec
	changedFileMu.Unlock()
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"
`+extraConfig+`
[codegraph]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-changed-file"
model = "x"
`)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	// The subject is the file and what the model was told; a turn that writes
	// and never verifies is refused at its end by an unrelated gate.
	_ = ctrl.Run(context.Background(), "rewrite notes.md")
	rec.mu.Lock()
	last := rec.reqs[len(rec.reqs)-1]
	rec.mu.Unlock()
	results := effectToolResults(last)
	if len(results) < 2 {
		t.Fatalf("want the read and the write at the boundary, got %d result(s)", len(results))
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	return string(got), results[len(results)-1]
}

// A whole-file write that would discard an edit made after the agent read the
// file is refused, and the model is told why and what to do.
func TestEffectChangedFileEditAfterTheReadIsKept(t *testing.T) {
	content, result := runChangedFile(t, "original\nthe user's own line\n", "")
	if content != "original\nthe user's own line\n" {
		t.Fatalf("the user's edit was overwritten: file now %q", content)
	}
	if !strings.Contains(result, "modified after you last read or wrote it") || !strings.Contains(result, "read it again") {
		t.Fatalf("the model was not told the file changed underneath it: %q", result)
	}
}

func TestEffectChangedFileUnchangedIsWritten(t *testing.T) {
	content, result := runChangedFile(t, "", "")
	if content != "agent version\n" {
		t.Fatalf("an unchanged file was not written: file %q, result %q", content, result)
	}
}

func TestEffectChangedFileProtectionCanBeTurnedOff(t *testing.T) {
	content, _ := runChangedFile(t, "original\nthe user's own line\n", "\n[tools]\nprotect_changed_files = false\n")
	if content != "agent version\n" {
		t.Fatalf("with protection off the write should go through as before: file %q", content)
	}
}
