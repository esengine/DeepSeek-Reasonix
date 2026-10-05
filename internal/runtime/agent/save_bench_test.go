package agent

import (
	"fmt"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

// One more turn onto an already-saved session of n messages: the shape every
// real turn takes, and the one the autosave path pays on each of them.
func benchIncrementalSave(b *testing.B, n int) {
	dir := b.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	s := sessionstore.NewSession("sys")
	for i := range n / 2 {
		s.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("turn %d: %s", i, strings.Repeat("please keep going. ", 20))})
		s.Add(provider.Message{Role: provider.RoleAssistant, Content: strings.Repeat("a plain english sentence about the work. ", 40)})
	}
	if err := s.Save(path); err != nil {
		b.Fatalf("seed save: %v", err)
	}
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		s.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("next %d", i)})
		if err := s.Save(path); err != nil {
			b.Fatalf("save: %v", err)
		}
	}
}

func BenchmarkIncrementalSave(b *testing.B) {
	for _, n := range []int{100, 1000, 5000, 20000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) { benchIncrementalSave(b, n) })
	}
}
