package sessionstore

import (
	"fmt"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
)

func BenchmarkSetSessionLineageArchived(b *testing.B) {
	dir := testenv.TempDir(b)
	const n = 200
	paths := make([]string, n)
	for i := range paths {
		p := filepath.Join(dir, fmt.Sprintf("session-%03d.jsonl", i))
		s := NewSession("sys")
		s.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
		if err := s.SaveSnapshot(p); err != nil {
			b.Fatal(err)
		}
		paths[i] = p
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		if err := SetSessionLineageArchived(paths[i%n], i%2 == 0); err != nil {
			b.Fatal(err)
		}
	}
}
