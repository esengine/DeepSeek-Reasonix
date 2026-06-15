package builtin

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestGrepGlobalExcludesFile(t *testing.T) {
	dir := mkRepo(t)
	writeFileT(t, filepath.Join(dir, "keep.txt"), "NEEDLE\n")
	writeFileT(t, filepath.Join(dir, "notes.tmp"), "NEEDLE\n")

	cfgDir := t.TempDir()
	excludes := filepath.Join(cfgDir, "global_ignore")
	writeFileT(t, excludes, "*.tmp\n")
	gitconfig := filepath.Join(cfgDir, "gitconfig")
	writeFileT(t, gitconfig, "[core]\n\texcludesFile = "+filepath.ToSlash(excludes)+"\n")
	t.Setenv("GIT_CONFIG_GLOBAL", gitconfig)

	out := runTool(t, grepTool{}, map[string]any{"pattern": "NEEDLE", "path": dir})
	if !strings.Contains(out, "keep.txt") {
		t.Fatalf("kept file must be found: %q", out)
	}
	if strings.Contains(out, "notes.tmp") {
		t.Fatalf("a file matched by core.excludesFile must be skipped: %q", out)
	}
}

// TestGrepWorkspaceE2E drives grep through the real per-workspace tool assembly
// (builtin.Workspace, what the desktop and ACP frontends use) against a repo that
// exercises every ignore rule at once: root + nested + /anchored .gitignore,
// nested negation, hidden dirs, and vendor dirs.
func TestGrepWorkspaceE2E(t *testing.T) {
	// Neutralize any machine-global git ignore so the fixture is hermetic.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "none"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	repo := mkRepo(t)
	writeFileT(t, filepath.Join(repo, ".gitignore"), "*.log\nbuild/\n/dist/\n")
	writeFileT(t, filepath.Join(repo, "src", ".gitignore"), "generated.go\n")
	writeFileT(t, filepath.Join(repo, "pkg", ".gitignore"), "*.log\n!keep.log\n")

	keep := map[string]string{
		"src/app.go":   "NEEDLE app",
		"pkg/keep.log": "NEEDLE re-included",
		"README.md":    "NEEDLE readme",
	}
	drop := map[string]string{
		"src/generated.go":        "NEEDLE nested-ignored",
		"app.log":                 "NEEDLE root-log",
		"build/out.txt":           "NEEDLE ignored-dir",
		"dist/bundle.js":          "NEEDLE anchored-dir",
		"pkg/drop.log":            "NEEDLE nested-log",
		".github/ci.yml":          "NEEDLE hidden",
		"node_modules/x/index.js": "NEEDLE vendor",
	}
	for rel, body := range keep {
		writeFileT(t, filepath.Join(repo, filepath.FromSlash(rel)), body+"\n")
	}
	for rel, body := range drop {
		writeFileT(t, filepath.Join(repo, filepath.FromSlash(rel)), body+"\n")
	}

	grep := byName(Workspace{Dir: repo}.Tools())["grep"]
	out, err := grep.Execute(context.Background(), argsJSON(t, map[string]any{"pattern": "NEEDLE", "path": "."}))
	if err != nil {
		t.Fatal(err)
	}

	found := matchedRelFiles(repo, out)
	for rel := range keep {
		if !found[rel] {
			t.Errorf("expected %s in results, got %v", rel, sortedKeys(found))
		}
	}
	for rel := range drop {
		if found[rel] {
			t.Errorf("did not expect %s in results (got %v)", rel, sortedKeys(found))
		}
	}
}

// matchedRelFiles parses grep's "path:line:text" output into the set of matched
// files, relative to repo with forward slashes.
func matchedRelFiles(repo, out string) map[string]bool {
	found := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		i := strings.LastIndex(line, ":")
		if i < 0 {
			continue
		}
		j := strings.LastIndex(line[:i], ":")
		if j < 0 {
			continue
		}
		if rel, err := filepath.Rel(repo, line[:j]); err == nil {
			found[filepath.ToSlash(rel)] = true
		}
	}
	return found
}

func sortedKeys(m map[string]bool) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
