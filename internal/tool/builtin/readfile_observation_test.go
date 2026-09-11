package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFileModelTextObservationHashesReturnedWindow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	if err := os.WriteFile(path, []byte("zero\none\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := readFile{workDir: dir}
	args := argsJSON(t, map[string]any{"path": "sample.txt", "offset": 1, "limit": 2})
	output, err := r.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	observation, ok := r.ObserveModelText(args, output)
	if !ok {
		t.Fatalf("ObserveModelText returned no observation for %q", output)
	}
	if observation.Path != path || observation.StartLine != 2 || len(observation.LineHashes) != 2 {
		t.Fatalf("observation = %+v, want path %q, lines 2-3", observation, path)
	}
	for i, line := range []string{"one", "two"} {
		sum := sha256.Sum256([]byte(line))
		if got := observation.LineHashes[i]; got != hex.EncodeToString(sum[:]) {
			t.Fatalf("line %d hash = %q, want %q", i, got, hex.EncodeToString(sum[:]))
		}
	}
	if _, ok := r.ObserveModelText(args, "   2→one\n   4→three\n"); ok {
		t.Fatal("non-contiguous output must not be recorded as one observation")
	}
}
