package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLegacyRescueArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    string
		wantSrc string
		wantExp bool
		wantErr string // substring; empty means no error
	}{
		{"empty", "", "", false, ""},
		{"bare flag", "--from", "", false, "--from requires a legacy directory path"},
		{"empty equals form", "--from=", "", false, "--from requires a legacy directory path"},
		{"empty quoted value", `--from ""`, "", false, "--from requires a legacy directory path"},
		{"equals form", "--from=/data/legacy", "/data/legacy", true, ""},
		{"space form quoted", `--from "/data legacy"`, "/data legacy", true, ""},
		{"equals form quoted", `--from="/data legacy"`, "/data legacy", true, ""},
		{"tab form", "--from\t/data/legacy", "/data/legacy", true, ""},
		{"unknown option", "bogus-option", "", false, `unknown /migrate option "bogus-option"`},
		{"unknown option with trailing words", "bogus extra", "", false, `unknown /migrate option "bogus"`},
		{"unbalanced quote preserved", `--from "/data/legacy`, `"/data/legacy`, true, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, explicit, err := parseLegacyRescueArgs(tt.args)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("parseLegacyRescueArgs(%q) unexpected error: %v", tt.args, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("parseLegacyRescueArgs(%q) error = %v, want substring %q", tt.args, err, tt.wantErr)
			}
			if src != tt.wantSrc || explicit != tt.wantExp {
				t.Fatalf("parseLegacyRescueArgs(%q) = (%q, %v), want (%q, %v)", tt.args, src, explicit, tt.wantSrc, tt.wantExp)
			}
		})
	}
}

func TestTrimMatchingQuotes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"double quotes", `"foo"`, "foo"},
		{"single quotes", `'foo'`, "foo"},
		{"leading quote only", `"foo`, `"foo`},
		{"trailing quote only", `foo"`, `foo"`},
		{"empty pair", `""`, ""},
		{"short string", `"`, `"`},
		{"whitespace padded", `  "a b"  `, "a b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trimMatchingQuotes(tt.in); got != tt.want {
				t.Fatalf("trimMatchingQuotes(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSamePath(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"both empty", "", "", false},
		{"one empty", dir, "", false},
		{"identical", dir, dir, true},
		{"trailing separator", dir, dir + string(filepath.Separator), true},
		{"relative and absolute", filepath.Join(cwd, "sub", "dir"), filepath.Join(cwd, "sub", "dir"), true},
		{"different paths", filepath.Join(dir, "a"), filepath.Join(dir, "b"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := samePath(tt.a, tt.b); got != tt.want {
				t.Fatalf("samePath(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCleanAbs(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"blank", "   ", ""},
		{"dot segments normalized", "a/b/../c", filepath.Join(cwd, "a", "c")},
		{"trailing separator removed", filepath.Join(cwd, "sub") + string(filepath.Separator), filepath.Join(cwd, "sub")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanAbs(tt.in); got != tt.want {
				t.Fatalf("cleanAbs(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestLegacySessionArtifactName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"events jsonl", "chat.events.jsonl", true},
		{"plain jsonl", "chat.jsonl", true},
		{"jsonl backup", "chat.jsonl.bak", true},
		{"json only", "chat.json", false},
		{"no leading dot", "jsonl", false},
		{"hidden jsonl", ".jsonl", true},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := legacySessionArtifactName(tt.in); got != tt.want {
				t.Fatalf("legacySessionArtifactName(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestDirLooksLikeLegacySessionDir(t *testing.T) {
	mkdir := func(t *testing.T, path string) string {
		t.Helper()
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	write := func(t *testing.T, path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("missing directory", func(t *testing.T) {
		if dirLooksLikeLegacySessionDir(filepath.Join(t.TempDir(), "nope")) {
			t.Fatal("missing directory detected as legacy session dir")
		}
	})
	t.Run("empty directory", func(t *testing.T) {
		if dirLooksLikeLegacySessionDir(t.TempDir()) {
			t.Fatal("empty directory detected as legacy session dir")
		}
	})
	t.Run("unrelated files only", func(t *testing.T) {
		dir := mkdir(t, filepath.Join(t.TempDir(), "sessions"))
		write(t, filepath.Join(dir, "readme.md"))
		if dirLooksLikeLegacySessionDir(dir) {
			t.Fatal("directory with unrelated files detected as legacy session dir")
		}
	})
	t.Run("jsonl file at top level", func(t *testing.T) {
		dir := mkdir(t, filepath.Join(t.TempDir(), "sessions"))
		write(t, filepath.Join(dir, "chat.jsonl"))
		if !dirLooksLikeLegacySessionDir(dir) {
			t.Fatal("directory with jsonl file not detected")
		}
	})
	t.Run("subagents subtree ignored", func(t *testing.T) {
		dir := mkdir(t, filepath.Join(t.TempDir(), "sessions"))
		sub := mkdir(t, filepath.Join(dir, "subagents"))
		write(t, filepath.Join(sub, "worker.jsonl"))
		if dirLooksLikeLegacySessionDir(dir) {
			t.Fatal("subagents-only jsonl detected as legacy session dir")
		}
	})
	t.Run("jsonl in project subdirectory", func(t *testing.T) {
		dir := mkdir(t, filepath.Join(t.TempDir(), "sessions"))
		proj := mkdir(t, filepath.Join(dir, "proj"))
		write(t, filepath.Join(proj, "chat.jsonl"))
		if !dirLooksLikeLegacySessionDir(dir) {
			t.Fatal("jsonl in project subdirectory not detected")
		}
	})
}
