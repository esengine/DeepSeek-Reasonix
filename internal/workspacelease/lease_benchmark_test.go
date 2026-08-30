package workspacelease

import (
	"context"
	"path/filepath"
	"testing"
)

func BenchmarkUncontendedPathHold(b *testing.B) {
	root := b.TempDir()
	owner, err := New(root, b.TempDir(), nil)
	if err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(root, "src", "internal", "package", "file.go")
	b.ResetTimer()
	for b.Loop() {
		release, err := owner.HoldWriteForPath(context.Background(), path)
		if err != nil {
			b.Fatal(err)
		}
		release()
	}
}
