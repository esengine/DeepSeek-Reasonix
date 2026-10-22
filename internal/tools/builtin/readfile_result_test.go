package builtin

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
)

func TestReadFileResultReferenceUsesSessionReaderAndKeepsProvenance(t *testing.T) {
	ref := tool.ResultReferencePrefix + strings.Repeat("a", 64)
	args, _ := json.Marshal(map[string]any{"path": ref, "offset": 1, "limit": 1})
	called := false
	ctx := tool.WithResultReader(t.Context(), func(got string) (io.ReadCloser, error) {
		called = true
		if got != ref {
			t.Errorf("reference changed: %s", got)
		}
		return io.NopCloser(strings.NewReader("first\nsecond\nthird\n")), nil
	})
	r := readFile{workDir: t.TempDir(), readRoots: []string{t.TempDir()}}
	out, err := r.Execute(ctx, args)
	if err != nil || !called || !strings.Contains(out, "second") || strings.Contains(out, "first") {
		t.Fatalf("read result = %s, %v", out, err)
	}
	if p := tool.ProvenanceOf(r, args); p.Kind != tool.ProvenanceMCP {
		t.Fatalf("lost external origin: %+v", p)
	}
	if target := r.ReadTarget(args); target != "" {
		t.Fatalf("artifact treated as workspace file: %s", target)
	}
	if _, err := r.Execute(context.Background(), args); err == nil {
		t.Fatal("artifact read without session grant")
	}
}
