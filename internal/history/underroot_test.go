package history

import (
	"path/filepath"
	"testing"
)

func TestUnderRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	parent := filepath.Dir(root)

	tests := []struct {
		name string
		path string
		root string
		want bool
	}{
		{"empty path", "", root, false},
		{"empty root", filepath.Join(root, "a.jsonl"), "", false},
		{"direct child", filepath.Join(root, "a.jsonl"), root, true},
		{"nested descendant", filepath.Join(root, "sub", "deep", "a.jsonl"), root, true},
		{"equal to root", root, root, true},
		{"root with trailing separator", filepath.Join(root, "a.jsonl"), root + string(filepath.Separator), true},
		{"sibling", filepath.Join(parent, "sessions2", "a.jsonl"), root, false},
		{"parent directory", parent, root, false},
		{"leading .. escape", filepath.Join(root, "..", "a.jsonl"), root, false},
		{"trailing .. resolves to root", filepath.Join(root, "sub", ".."), root, true},
		{"middle .. stays under root", filepath.Join(root, "a", "..", "b.jsonl"), root, true},
		{"middle .. escapes", filepath.Join(root, "..", "x", "a.jsonl"), root, false},
		{"relative path under relative root", filepath.Join("rel-root", "a.jsonl"), "rel-root", true},
		{"relative path escaping relative root", filepath.Join("rel-root", "..", "a.jsonl"), "rel-root", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := underRoot(tt.path, tt.root); got != tt.want {
				t.Errorf("underRoot(%q, %q) = %v, want %v", tt.path, tt.root, got, tt.want)
			}
		})
	}
}
