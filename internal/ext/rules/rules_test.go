package rules

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func rels(rs []Rule) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Label)
	}
	return out
}

func TestLoadSplitsUnconditionalFromPathScopedAndOrdersBroadToSpecific(t *testing.T) {
	root, user := testenv.TempDir(t), testenv.TempDir(t)
	write(t, filepath.Join(user, "rules", "style.md"), "User style.\n")
	write(t, filepath.Join(root, ".claude", "rules", "claude.md"), "Claude-dir rule.\n")
	write(t, filepath.Join(root, ".reasonix", "rules", "b.md"), "B rule.\n")
	write(t, filepath.Join(root, ".reasonix", "rules", "a.md"), "A rule.\n")
	write(t, filepath.Join(root, ".reasonix", "rules", "web", "ts.md"), "---\npaths:\n  - \"src/**/*.ts\"\n  - web/*.vue\n---\nTypeScript rule.\n")
	write(t, filepath.Join(root, ".reasonix", "rules", "notes.txt"), "not a rule\n")

	got := Load(Options{WorkspaceRoot: root, UserDir: user})
	if want := []string{"user/rules/style.md", "workspace/.claude/rules/claude.md", "workspace/.reasonix/rules/a.md", "workspace/.reasonix/rules/b.md"}; !slices.Equal(rels(got.Always), want) {
		t.Fatalf("always = %v, want %v", rels(got.Always), want)
	}
	if len(got.Scoped) != 1 || got.Scoped[0].Label != "workspace/.reasonix/rules/web/ts.md" {
		t.Fatalf("scoped = %v", rels(got.Scoped))
	}
	ts := got.Scoped[0]
	if !slices.Equal(ts.Globs, []string{"src/**/*.ts", "web/*.vue"}) || strings.Contains(ts.Body, "paths") || strings.TrimSpace(ts.Body) != "TypeScript rule." {
		t.Fatalf("scoped rule = %+v, want its globs and a body without the frontmatter", ts)
	}
}

func TestAGatedRuleWithNoUsableGlobStaysGatedAndIsReported(t *testing.T) {
	root := testenv.TempDir(t)
	write(t, filepath.Join(root, ".reasonix", "rules", "typo.md"), "---\npaths: \"!src/**\"\n---\nNever applies.\n")
	got := Load(Options{WorkspaceRoot: root})
	if len(got.Always) != 0 || len(got.Scoped) != 1 || len(got.Scoped[0].Globs) != 0 {
		t.Fatalf("always=%v scoped=%+v, want one scoped rule with no usable glob", rels(got.Always), got.Scoped)
	}
	if len(got.Diagnostics) != 1 || got.Diagnostics[0].Code != CodeInvalidGlob {
		t.Fatalf("diagnostics = %+v, want one %s", got.Diagnostics, CodeInvalidGlob)
	}
}

func TestEmptyBodiesAndLargeFilesAreSkippedAndSaid(t *testing.T) {
	root := testenv.TempDir(t)
	write(t, filepath.Join(root, ".reasonix", "rules", "empty.md"), "---\npaths: x\n---\n\n")
	write(t, filepath.Join(root, ".reasonix", "rules", "big.md"), strings.Repeat("x", MaxFileBytes+1))
	write(t, filepath.Join(root, ".reasonix", "rules", "ok.md"), "fine\n")
	got := Load(Options{WorkspaceRoot: root})
	if !slices.Equal(rels(got.Always), []string{"workspace/.reasonix/rules/ok.md"}) || len(got.Scoped) != 0 {
		t.Fatalf("always=%v scoped=%v", rels(got.Always), rels(got.Scoped))
	}
	codes := map[string]bool{}
	for _, d := range got.Diagnostics {
		codes[d.Code] = true
	}
	if !codes[CodeTooLarge] {
		t.Fatalf("diagnostics = %+v, want %s", got.Diagnostics, CodeTooLarge)
	}
}

func TestEachLayerStopsAtItsByteCapAndNamesWhatItDropped(t *testing.T) {
	root := testenv.TempDir(t)
	chunk := strings.Repeat("y", MaxFileBytes-10)
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		write(t, filepath.Join(root, ".reasonix", "rules", name), chunk)
	}
	got := Load(Options{WorkspaceRoot: root})
	if len(got.Always) != 2 {
		t.Fatalf("always = %v, want the first two under the %d byte cap", rels(got.Always), MaxLayerBytes)
	}
	dropped := ""
	for _, d := range got.Diagnostics {
		if d.Code == CodeLayerCap {
			dropped = d.Path
		}
	}
	if !strings.HasSuffix(filepath.ToSlash(dropped), "rules/c.md") {
		t.Fatalf("diagnostics = %+v, want %s naming c.md", got.Diagnostics, CodeLayerCap)
	}
}

func TestASymlinkedRuleIsNotFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root, outside := testenv.TempDir(t), testenv.TempDir(t)
	write(t, filepath.Join(outside, "secret.md"), "outside the project\n")
	if err := os.MkdirAll(filepath.Join(root, ".reasonix", "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(root, ".reasonix", "rules", "link.md")); err != nil {
		t.Fatal(err)
	}
	got := Load(Options{WorkspaceRoot: root})
	if len(got.Always) != 0 {
		t.Fatalf("a symlinked rule was loaded: %v", rels(got.Always))
	}
}

func TestBlockRendersEachRuleUnderItsOwnHeadingAndNothingForNone(t *testing.T) {
	if Block(nil) != "" {
		t.Fatal("no rules rendered a block")
	}
	root := testenv.TempDir(t)
	write(t, filepath.Join(root, ".reasonix", "rules", "a.md"), "Use tabs.\n")
	write(t, filepath.Join(root, ".reasonix", "rules", "ts.md"), "---\npaths: src/**/*.ts\n---\nUse strict mode.\n")
	got := Load(Options{WorkspaceRoot: root})
	always, scoped := Block(got.Always), ScopedBlock(got.Scoped)
	for _, want := range []string{"# Rules", "## workspace/.reasonix/rules/a.md", "Use tabs."} {
		if !strings.Contains(always, want) {
			t.Fatalf("always block lacks %q:\n%s", want, always)
		}
	}
	if !strings.HasPrefix(scoped, "# Path-scoped rules") || !strings.Contains(scoped, "applies to src/**/*.ts") || !strings.Contains(scoped, "Use strict mode.") {
		t.Fatalf("scoped block = %q", scoped)
	}
	if again := Block(Load(Options{WorkspaceRoot: root}).Always); again != always {
		t.Fatal("the same state rendered different bytes")
	}
}
