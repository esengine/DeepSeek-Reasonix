package builtin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// grepGlobTree is one nested tree both engines search, so their answers can be
// compared rather than asserted separately.
func grepGlobTree(t *testing.T) string {
	t.Helper()
	dir := testenv.TempDir(t)
	files := map[string]string{
		"top.go": "needle at the top\n",
		"top.md": "needle in markdown\n",
		filepath.Join("internal", "deep", "nested.go"):      "needle nested deep\n",
		filepath.Join("internal", "deep", "nested_test.go"): "needle in a test\n",
		filepath.Join("internal", "deep", "notes.md"):       "needle in notes\n",
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// A glob without a slash matches the file name at any depth — ripgrep's rule,
// and the one a caller writing "*.go" means.
func TestGrepGlobMatchesFileNameAtAnyDepth(t *testing.T) {
	dir := grepGlobTree(t)
	for _, g := range grepEnginesUnderTest(t) {
		out := runTool(t, g.tool, map[string]any{"pattern": "needle", "path": dir, "glob": "*.go"})
		if !strings.Contains(out, "top.go") || !strings.Contains(out, "nested.go") {
			t.Fatalf("%s: out = %q, want both the top-level and the nested .go file", g.name, out)
		}
		if strings.Contains(out, ".md") {
			t.Fatalf("%s: out = %q, want markdown filtered out", g.name, out)
		}
	}
}

// A glob with a slash is matched against the path below the search root.
func TestGrepGlobWithSlashMatchesPath(t *testing.T) {
	dir := grepGlobTree(t)
	for _, g := range grepEnginesUnderTest(t) {
		out := runTool(t, g.tool, map[string]any{"pattern": "needle", "path": dir, "glob": "internal/**/*_test.go"})
		if !strings.Contains(out, "nested_test.go") {
			t.Fatalf("%s: out = %q, want the nested test file", g.name, out)
		}
		if strings.Contains(out, "top.go") || strings.Contains(out, "notes.md") {
			t.Fatalf("%s: out = %q, want everything outside the pattern filtered out", g.name, out)
		}
	}
}

func TestGrepSlashGlobThroughSymlinkedParent(t *testing.T) {
	root := grepGlobTree(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(filepath.Dir(root), alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	searchRoot := filepath.Join(alias, filepath.Base(root))
	for _, engine := range grepEnginesUnderTest(t) {
		t.Run(engine.name, func(t *testing.T) {
			for _, wide := range []bool{false, true} {
				rp := ResolvedPath{Path: searchRoot, Root: searchRoot, DisplayRoot: "external/source", External: true}
				if engine.tool.rg != "" {
					out, _, _, err := engine.tool.ripgrepPass(t.Context(), "needle", searchRoot, "internal/**/*_test.go", true, rp, wide)
					want := "external/source/internal/deep/nested_test.go:1:needle in a test"
					if err != nil || len(out) != 1 || out[0] != want {
						t.Fatalf("wide=%v: matches=%q, err=%v; want %q", wide, out, err, want)
					}
				}
			}
			out := runTool(t, engine.tool, map[string]any{"pattern": "needle", "path": searchRoot, "glob": "internal/**/*_test.go"})
			want := filepath.Join(searchRoot, "internal", "deep", "nested_test.go") + ":1:needle in a test"
			if out != want {
				t.Fatalf("matches=%q, want original path %q", out, want)
			}
		})
	}
}

func TestGrepRelativeSearchRootKeepsItsOutputPath(t *testing.T) {
	dir := grepGlobTree(t)
	t.Chdir(filepath.Dir(dir))
	root := filepath.Base(dir)
	for _, engine := range grepEnginesUnderTest(t) {
		out := runTool(t, engine.tool, map[string]any{"pattern": "needle", "path": root, "glob": "internal/**/*_test.go"})
		want := filepath.Join(root, "internal", "deep", "nested_test.go") + ":1:needle in a test"
		if out != want {
			t.Fatalf("%s: result=%q, want %q", engine.name, out, want)
		}
	}
}

// An omitted glob keeps the unfiltered behavior every existing caller relies on.
func TestGrepWithoutGlobSearchesEverything(t *testing.T) {
	dir := grepGlobTree(t)
	for _, g := range grepEnginesUnderTest(t) {
		out := runTool(t, g.tool, map[string]any{"pattern": "needle", "path": dir})
		for _, want := range []string{"top.go", "top.md", "nested.go", "notes.md"} {
			if !strings.Contains(out, want) {
				t.Fatalf("%s: out = %q, want %s included", g.name, out, want)
			}
		}
	}
}

// A negated glob would re-admit what the sensitive-file denylist excludes, so
// it is refused before either engine sees it.
func TestGrepRejectsNegatedGlob(t *testing.T) {
	dir := grepGlobTree(t)
	_, err := grepTool{}.Execute(context.Background(), argsJSON(t, map[string]any{
		"pattern": "needle", "path": dir, "glob": "!*.go",
	}))
	if err == nil {
		t.Fatal("a negated glob was accepted; it can cancel the sensitive-file excludes")
	}
	if !strings.Contains(err.Error(), "select files") {
		t.Fatalf("err = %v, want the reason stated", err)
	}
}

type grepEngine struct {
	name string
	tool grepTool
}

// grepEnginesUnderTest returns the native scanner plus ripgrep when installed,
// so a rule is asserted against every engine that can serve it.
func grepEnginesUnderTest(t *testing.T) []grepEngine {
	t.Helper()
	engines := []grepEngine{{name: "native", tool: grepTool{}}}
	if rg, err := exec.LookPath("rg"); err == nil {
		engines = append(engines, grepEngine{name: "ripgrep", tool: grepTool{rg: rg}})
	}
	return engines
}
