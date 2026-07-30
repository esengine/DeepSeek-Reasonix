package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	fileencoding "reasonix/internal/fileutil/encoding"
)

func writeFileT(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGrepNestedGitignore(t *testing.T) {
	dir := mkRepo(t)
	writeFileT(t, filepath.Join(dir, ".gitignore"), "*.log\n")
	writeFileT(t, filepath.Join(dir, "pkg", ".gitignore"), "secret.txt\n")
	writeFileT(t, filepath.Join(dir, "pkg", "keep.go"), "NEEDLE\n")
	writeFileT(t, filepath.Join(dir, "pkg", "secret.txt"), "NEEDLE\n") // ignored by nested
	writeFileT(t, filepath.Join(dir, "pkg", "app.log"), "NEEDLE\n")    // ignored by root

	out := runTool(t, grepTool{}, map[string]any{"pattern": "NEEDLE", "path": dir})
	if !strings.Contains(out, "keep.go") {
		t.Fatalf("kept file must be found: %q", out)
	}
	if strings.Contains(out, "secret.txt") {
		t.Fatalf("a nested .gitignore must apply: %q", out)
	}
	if strings.Contains(out, "app.log") {
		t.Fatalf("an ancestor .gitignore must apply in subdirs: %q", out)
	}
}

func TestGrepNestedNegationReincludes(t *testing.T) {
	dir := mkRepo(t)
	writeFileT(t, filepath.Join(dir, ".gitignore"), "*.log\n")
	writeFileT(t, filepath.Join(dir, "pkg", ".gitignore"), "!keep.log\n")
	writeFileT(t, filepath.Join(dir, "pkg", "keep.log"), "NEEDLE\n") // re-included by nested negation
	writeFileT(t, filepath.Join(dir, "pkg", "drop.log"), "NEEDLE\n") // still ignored by root

	out := runTool(t, grepTool{}, map[string]any{"pattern": "NEEDLE", "path": dir})
	if !strings.Contains(out, "keep.log") {
		t.Fatalf("a nested !negation must re-include: %q", out)
	}
	if strings.Contains(out, "drop.log") {
		t.Fatalf("a non-negated .log must stay ignored: %q", out)
	}
}

func TestGrepSkipsHidden(t *testing.T) {
	dir := mkRepo(t)
	writeFileT(t, filepath.Join(dir, "visible.txt"), "NEEDLE\n")
	writeFileT(t, filepath.Join(dir, ".env"), "NEEDLE\n")
	writeFileT(t, filepath.Join(dir, ".github", "ci.yml"), "NEEDLE\n")

	out := runTool(t, grepTool{}, map[string]any{"pattern": "NEEDLE", "path": dir})
	if !strings.Contains(out, "visible.txt") {
		t.Fatalf("a visible file must be found: %q", out)
	}
	if strings.Contains(out, ".env") || strings.Contains(out, "ci.yml") {
		t.Fatalf("hidden files/dirs must be skipped by default: %q", out)
	}
}

func TestGrepExplicitHiddenRootSearched(t *testing.T) {
	dir := mkRepo(t)
	writeFileT(t, filepath.Join(dir, ".github", "ci.yml"), "NEEDLE\n")
	// Pointing grep straight at a hidden dir searches it in full.
	out := runTool(t, grepTool{}, map[string]any{"pattern": "NEEDLE", "path": filepath.Join(dir, ".github")})
	if !strings.Contains(out, "ci.yml") {
		t.Fatalf("an explicitly targeted hidden dir should be searched: %q", out)
	}
}

// repo scaffolds a fake git repo: a .git marker, a .gitignore, and files both
// kept and ignored — enough for the native grep walk to exercise .gitignore.
func gitignoreRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFileT(t, filepath.Join(dir, ".gitignore"), "ignored.txt\nbuild/\n")
	writeFileT(t, filepath.Join(dir, "keep.txt"), "NEEDLE in kept file\n")
	writeFileT(t, filepath.Join(dir, "ignored.txt"), "NEEDLE in ignored file\n")
	writeFileT(t, filepath.Join(dir, "build", "out.txt"), "NEEDLE in ignored dir\n")
	writeFileT(t, filepath.Join(dir, "src", "deep.txt"), "NEEDLE deep in tree\n")
	return dir
}

func TestGrepSkipsGitignored(t *testing.T) {
	dir := gitignoreRepo(t)
	out := runTool(t, grepTool{}, map[string]any{"pattern": "NEEDLE", "path": dir})

	if !strings.Contains(out, "keep.txt") || !strings.Contains(out, "deep.txt") {
		t.Fatalf("kept files must be searched: %q", out)
	}
	if strings.Contains(out, "ignored.txt") {
		t.Fatalf("a .gitignore'd file must be skipped: %q", out)
	}
	if strings.Contains(out, "out.txt") {
		t.Fatalf("files under a .gitignore'd dir must be skipped: %q", out)
	}
}

func TestGrepDecodesGB18030Gitignore(t *testing.T) {
	dir := mkRepo(t)
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), fileencoding.Encode("秘密.txt\n", fileencoding.GB18030), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFileT(t, filepath.Join(dir, "秘密.txt"), "NEEDLE ignored\n")
	writeFileT(t, filepath.Join(dir, "公开.txt"), "NEEDLE kept\n")

	out := runTool(t, grepTool{}, map[string]any{"pattern": "NEEDLE", "path": dir})
	if !strings.Contains(out, "公开.txt") {
		t.Fatalf("kept Chinese file must be searched: %q", out)
	}
	if strings.Contains(out, "秘密.txt") {
		t.Fatalf("GB18030 .gitignore pattern should skip Chinese file: %q", out)
	}
}

func TestScanGitConfigExcludesDecodesGB18030Path(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitconfig")
	want := filepath.Join(dir, "中文忽略规则.txt")
	body := "[core]\n\texcludesFile = " + want + "\n"
	if err := os.WriteFile(path, fileencoding.Encode(body, fileencoding.GB18030), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := scanGitConfigExcludes(path); got != want {
		t.Fatalf("scanGitConfigExcludes = %q, want %q", got, want)
	}
}

func TestGrepExplicitIgnoredRootStillSearched(t *testing.T) {
	dir := gitignoreRepo(t)
	// Pointing grep straight at a gitignored directory still searches it — the
	// walk root is never pruned.
	out := runTool(t, grepTool{}, map[string]any{"pattern": "NEEDLE", "path": filepath.Join(dir, "build")})
	if !strings.Contains(out, "out.txt") {
		t.Fatalf("an explicitly targeted ignored dir should still be searched: %q", out)
	}
}

func TestGrepNoRepoIgnoresNothing(t *testing.T) {
	// Same layout but without a .git marker: there is no repository, so
	// .gitignore is not consulted and every file is searched.
	dir := t.TempDir()
	writeFileT(t, filepath.Join(dir, ".gitignore"), "ignored.txt\n")
	writeFileT(t, filepath.Join(dir, "ignored.txt"), "NEEDLE here\n")

	out := runTool(t, grepTool{}, map[string]any{"pattern": "NEEDLE", "path": dir})
	if !strings.Contains(out, "ignored.txt") {
		t.Fatalf("outside a git repo, .gitignore must not be applied: %q", out)
	}
}

func TestFindRepoRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := filepath.EvalSymlinks(findRepoRoot(sub))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	if got != want {
		t.Fatalf("findRepoRoot(%q) = %q, want %q", sub, got, want)
	}
	if rr := findRepoRoot(t.TempDir()); rr != "" {
		t.Fatalf("a dir with no .git ancestor should return \"\", got %q", rr)
	}
}
