package sessioncatalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkReconcileDirectory10K measures the full incremental reconcile, not
// the unchanged-signature fast path. File creation and the initial snapshot are
// excluded so the result can be compared directly with the pre-atomic code.
func BenchmarkReconcileDirectory10K(b *testing.B) {
	ctx := context.Background()
	dir := b.TempDir()
	paths := make([]string, 10_000)
	for i := range paths {
		paths[i] = filepath.Join(dir, fmt.Sprintf("session-%05d.jsonl", i))
		if err := os.WriteFile(paths[i], []byte("{}\n"), 0o600); err != nil {
			b.Fatal(err)
		}
	}
	catalog, err := Open(ctx, Options{Path: filepath.Join(b.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = catalog.Close(context.Background()) })
	target := DirectoryTarget{Path: dir, Scope: "project", WorkspaceRoot: "/workspace"}
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := range b.N {
		b.StopTimer()
		stamp := time.Unix(1_800_000_000+int64(i), 0)
		if err := os.Chtimes(paths[i%len(paths)], stamp, stamp); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if err := catalog.ReconcileDirectory(ctx, target); err != nil {
			b.Fatal(err)
		}
	}
}
