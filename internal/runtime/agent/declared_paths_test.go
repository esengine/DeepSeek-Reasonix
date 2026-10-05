package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/sessionstore"
)

// setWriter writes two files named by nothing in its arguments: only its
// declaration tells the host which paths it touches.
type setWriter struct {
	paths    []string
	sched    *writeclaim.SubagentScheduler
	claimsAt int
}

func (w *setWriter) Name() string            { return "apply_set" }
func (w *setWriter) Description() string     { return "" }
func (w *setWriter) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (w *setWriter) ReadOnly() bool          { return false }
func (w *setWriter) DeclaredWritePaths(context.Context, json.RawMessage) ([]string, error) {
	return w.paths, nil
}

func (w *setWriter) Execute(context.Context, json.RawMessage) (string, error) {
	if w.sched != nil {
		w.claimsAt = len(w.sched.ActiveWriterClaims())
	}
	for _, p := range w.paths {
		if err := os.WriteFile(p, []byte("applied\n"), 0o644); err != nil {
			return "", err
		}
	}
	return "applied", nil
}

var _ tool.WritePathDeclarer = (*setWriter)(nil)

// A writer that declares its whole write set is observed like write_file: each
// path is reserved while it runs, named on the receipt, and restored by a code
// rewind — the modified file to what it held, the created one removed.
func TestDeclaredWritePathsAreReservedRecordedAndRewindable(t *testing.T) {
	root := testenv.TempDir(t)
	existing, created := filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")
	if err := os.WriteFile(existing, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sched := writeclaim.NewSubagentScheduler(4, 2)
	writer := &setWriter{paths: []string{existing, created}, sched: sched}
	reg := tool.NewRegistry()
	reg.Add(writer)
	prov := &scriptedProvider{name: "p", turns: [][]provider.Chunk{
		{toolCallChunk("c1", "apply_set", `{}`), {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession(""), Options{WriteWorkspaceRoot: root, WriteScheduler: sched}, nil)
	store := checkpoint.New(testenv.TempDir(t), root)
	store.Begin(1, "apply", 0)
	a.SetMutationObserver(checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store, OwnershipTurn: 1}))

	if err := a.Run(context.Background(), "apply"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if writer.claimsAt != 1 {
		t.Fatalf("write claims held during Execute = %d, want the declared set reserved", writer.claimsAt)
	}
	var paths []string
	for _, r := range a.task.ledger.Receipts() {
		if r.ToolName == "apply_set" {
			paths = r.Paths
		}
	}
	if !holdsPath(paths, existing) || !holdsPath(paths, created) {
		t.Fatalf("receipt paths = %v, want both declared paths", paths)
	}

	store.Begin(2, "next", 0)
	if _, _, err := store.RestoreCode(1); err != nil {
		t.Fatalf("RestoreCode: %v", err)
	}
	if b, _ := os.ReadFile(existing); string(b) != "original\n" {
		t.Fatalf("a.txt after rewind = %q, want the preimage", b)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("b.txt after rewind: %v, want it removed", err)
	}
}
