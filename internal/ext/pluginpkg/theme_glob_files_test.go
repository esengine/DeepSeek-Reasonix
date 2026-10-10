package pluginpkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestThemeGlobsSkipFileSiblingsBeforeRemainingSegments(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pattern string
		theme   string
		note    string
		link    bool
		dirLink bool
	}{
		{name: "one wildcard", pattern: "themes/*/theme.json", theme: "themes/dawn/theme.json", note: "themes/README.md"},
		{name: "nested wildcards", pattern: "themes/*/palettes/*/theme.json", theme: "themes/collection/palettes/dawn/theme.json", note: "themes/collection/palettes/README.md"},
		{name: "file alias", pattern: "themes/*/theme.json", theme: "themes/dawn/theme.json", note: "themes/README.md", link: true},
		{name: "directory alias", pattern: "themes/*/theme.json", theme: "themes/dawn/theme.json", note: "themes/README.md", dirLink: true},
		{name: "only files", pattern: "themes/*/theme.json", note: "themes/README.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := testenv.TempDir(t)
			writeV2Plugin(t, root, `{"apiVersion":"reasonix.io/plugin/v2","name":"glob-files","contributes":{"themes":["`+tc.pattern+`"]}}`)
			writeTestFile(t, filepath.Join(root, tc.note), "Theme author notes.")
			if tc.theme != "" {
				writeTestFile(t, filepath.Join(root, tc.theme), `{"schemaVersion":1}`)
			}
			if tc.link {
				if err := os.Symlink(filepath.Join(root, tc.note), filepath.Join(root, "themes", "notes-link")); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			if tc.dirLink {
				if err := os.Symlink(filepath.Dir(filepath.Join(root, tc.theme)), filepath.Join(root, "themes", "dawn-alias")); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			pkg, warnings, err := ParseDir(root)
			if err != nil {
				t.Fatalf("ParseDir with file siblings: %v", err)
			}
			files := pkg.ThemeFiles()
			if tc.theme == "" {
				if len(files) != 0 || len(warnings) != 1 || !strings.Contains(warnings[0], "matched no files") {
					t.Fatalf("files=%v warnings=%v, want the unmatched-glob warning", files, warnings)
				}
				return
			}
			if tc.dirLink {
				if len(files) != 2 || pkg.ThemeCount() != 2 || len(warnings) != 0 {
					t.Fatalf("files=%v warnings=%v, want both in-root directory paths", files, warnings)
				}
				return
			}
			if len(files) != 1 || files[0].Path != filepath.Join(root, tc.theme) || len(warnings) != 0 || pkg.ThemeCount() != 1 {
				t.Fatalf("files=%v warnings=%v, want only the declared theme", files, warnings)
			}
		})
	}
}

func TestThemeGlobStillValidatesFinalCandidates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pattern string
		link    bool
	}{
		{name: "final directory", pattern: "themes/*"},
		{name: "unresolved directory alias", pattern: "themes/*/theme.json", link: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := testenv.TempDir(t)
			writeV2Plugin(t, root, `{"apiVersion":"reasonix.io/plugin/v2","name":"glob-invalid","contributes":{"themes":["`+tc.pattern+`"]}}`)
			if err := os.MkdirAll(filepath.Join(root, "themes"), 0o755); err != nil {
				t.Fatal(err)
			}
			if tc.link {
				if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "themes", "unresolved")); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			} else if err := os.Mkdir(filepath.Join(root, "themes", "directory"), 0o755); err != nil {
				t.Fatal(err)
			}
			if _, _, err := ParseDir(root); err == nil {
				t.Fatal("invalid theme candidate was accepted")
			}
		})
	}
}
