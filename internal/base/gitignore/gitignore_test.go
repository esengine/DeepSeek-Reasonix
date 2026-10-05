package gitignore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/base/fileutil/encoding/encodingtest"
	"reasonix/internal/base/testenv"
)

// gb18030Lines is the kind of reader a caller supplies when the ignore files on
// this machine are not UTF-8. The package's own default is not that reader, so
// this is also what keeps the injection point from quietly going unused.
func gb18030Lines(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	enc, _ := fileencoding.Detect(b)
	return strings.Split(string(fileencoding.Decode(b, enc)), "\n")
}

func TestScanGitConfigExcludesDecodesGB18030Path(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, ".gitconfig")
	want := filepath.Join(dir, "中文忽略规则.txt")
	body := "[core]\n\texcludesFile = " + want + "\n"
	if err := os.WriteFile(path, encodingtest.MustEncode(body, fileencoding.GB18030), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := scanGitConfigExcludes(path, gb18030Lines); got != want {
		t.Fatalf("scanGitConfigExcludes = %q, want %q", got, want)
	}
}

func TestFindRepoRoot(t *testing.T) {
	dir := testenv.TempDir(t)
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := filepath.EvalSymlinks(RepoRoot(sub))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	if got != want {
		t.Fatalf("RepoRoot(%q) = %q, want %q", sub, got, want)
	}
	if rr := RepoRoot(testenv.TempDir(t)); rr != "" {
		t.Fatalf("a dir with no .git ancestor should return \"\", got %q", rr)
	}
}
