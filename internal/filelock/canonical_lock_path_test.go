package filelock

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCanonicalLockPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{name: "empty", path: "", wantErr: true},
		{name: "whitespace only", path: "   \t\n", wantErr: true},
		{name: "relative path", path: "state.lock"},
		{name: "absolute path", path: filepath.Join(string(filepath.Separator), "tmp", "state.lock")},
		{name: "dot segments", path: "a/./b/../b.lock"},
		{name: "trailing separator", path: "a/b.lock/"},
		{name: "repeated separators", path: "a//b///c.lock"},
		{name: "surrounding whitespace", path: "  state.lock  "},
		{name: "parent traversal", path: "../state.lock"},
		{name: "current directory", path: "."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := canonicalLockPath(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("canonicalLockPath(%q) error = nil, want error", tt.path)
				}
				return
			}
			if err != nil {
				t.Fatalf("canonicalLockPath(%q) error = %v", tt.path, err)
			}

			// The canonical form must mirror trim -> Abs -> Clean, plus the
			// Windows lowercasing/slash normalization.
			want := filepath.Clean(filepath.Abs(strings.TrimSpace(tt.path)))
			if runtime.GOOS == "windows" {
				want = strings.ToLower(filepath.ToSlash(want))
			}
			if got != want {
				t.Errorf("canonicalLockPath(%q) = %q, want %q", tt.path, got, want)
			}

			// Platform invariants independent of the mirror above.
			if !filepath.IsAbs(got) {
				t.Errorf("canonicalLockPath(%q) = %q, want absolute path", tt.path, got)
			}
			if strings.Contains(got, "..") || strings.HasPrefix(got, "./") {
				t.Errorf("canonicalLockPath(%q) = %q, want cleaned path", tt.path, got)
			}
			if runtime.GOOS == "windows" {
				if got != strings.ToLower(got) {
					t.Errorf("canonicalLockPath(%q) = %q, want lowercase on windows", tt.path, got)
				}
				if strings.Contains(got, "\\") {
					t.Errorf("canonicalLockPath(%q) = %q, want slash separators on windows", tt.path, got)
				}
			}
		})
	}
}

func TestCanonicalLockPathEquivalentForms(t *testing.T) {
	// All of these must canonicalize to the same lock key as "state.lock".
	forms := []string{
		"state.lock",
		"./state.lock",
		"sub/../state.lock",
		"state.lock/",
		"  state.lock  ",
	}
	var want string
	for i, form := range forms {
		got, err := canonicalLockPath(form)
		if err != nil {
			t.Fatalf("canonicalLockPath(%q) error = %v", form, err)
		}
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Errorf("canonicalLockPath(%q) = %q, want %q", form, got, want)
		}
	}
}

func TestCanonicalLockPathEmptyError(t *testing.T) {
	if _, err := canonicalLockPath(""); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("canonicalLockPath(\"\") error = %v, want empty-path error", err)
	}
}
