package gitcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// StageAll excludes nested repositories by literal name: a name that reads as
// pathspec magic or a glob must not drop a sibling or leak the repository in.
func TestStageAllExcludesNestedRepositoriesByLiteralName(t *testing.T) {
	f := newRepoFixture(t, "", map[string]string{"top.txt": "top\n"})
	names := []string{"a*", "[x]", " sp ace ", "-dash", "ünï", ":(glob)c", "a)b", "q?"}
	p := f.payload()
	for _, n := range names {
		dir := filepath.Join(f.dir, n)
		f.plainIn(f.dir, "init", "--quiet", dir)
		f.plainIn(dir, "config", "filter.pwn.clean", p)
		if err := os.WriteFile(filepath.Join(dir, ".gitattributes"), []byte("* filter=pwn\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "in.txt"), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// siblings a glob-read exclusion would wrongly drop
	for _, s := range []string{"ab.txt", "x", "q1", "a)bc.txt"} {
		f.write(s, s+"\n")
	}
	repo, err := Open(f.ctx, f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.StageAll(f.ctx); err != nil {
		t.Fatalf("StageAll: %v", err)
	}
	staged := f.plain("ls-files", "-s", "-z")
	for _, s := range []string{"ab.txt", "x", "q1", "a)bc.txt"} {
		if !strings.Contains(staged, "\t"+s+"\x00") {
			t.Errorf("sibling %q not staged: %q", s, staged)
		}
	}
	for _, n := range names {
		if strings.Contains(staged, "\t"+n+"\x00") || strings.Contains(staged, "\t"+n+"/") {
			t.Errorf("nested repo %q staged", n)
		}
	}
	f.assertNotExecuted()
}
