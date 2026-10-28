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

func TestScanGitConfigExcludesReadsValueLikeGit(t *testing.T) {
	dir := testenv.TempDir(t)
	path := filepath.Join(dir, ".gitconfig")
	cases := []struct {
		name, body, want string
	}{
		{"last value wins", "[core]\n\texcludesFile = /first\n\texcludesFile = /second\n", "/second"},
		{"semicolon comment", "[core]\n\texcludesFile = /ignore ; global ignore\n", "/ignore"},
		{"hash comment", "[core]\n\texcludesfile = /ignore# global ignore\n", "/ignore"},
		{"quoted with spaces and a hash", "[core]\n\texcludesFile = \"/my ignore#1\" ; c\n", "/my ignore#1"},
		{"escaped backslashes", "[core]\n\texcludesFile = C:\\\\Users\\\\me\\\\.gitignore\n", `C:\Users\me\.gitignore`},
		{"raw windows path", "[core]\n\texcludesFile = C:\\Users\\me\\.gitignore\n", `C:\Users\me\.gitignore`},
		{"other section", "[core]\n\texcludesFile = /core\n[user]\n\texcludesFile = /user\n", "/core"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := scanGitConfigExcludes(path, readUTF8Lines); got != tc.want {
				t.Fatalf("scanGitConfigExcludes = %q, want %q", got, tc.want)
			}
		})
	}
}

// Git reads $XDG_CONFIG_HOME/git/config before ~/.gitconfig and the later
// value wins, so core.excludesFile in ~/.gitconfig is the one git uses.
func TestGlobalExcludesFileHomeConfigOverridesXDG(t *testing.T) {
	home := testenv.TempDir(t)
	xdg := filepath.Join(home, "xdg")
	if err := os.MkdirAll(filepath.Join(xdg, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	fromXDG := filepath.Join(home, "xdg-ignore")
	fromHome := filepath.Join(home, "home-ignore")
	for _, p := range []string{fromXDG, fromHome} {
		if err := os.WriteFile(p, []byte("*.log\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig := func(path, excludes string) {
		body := "[core]\n\texcludesFile = " + strings.ReplaceAll(excludes, `\`, `\\`) + "\n"
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig(filepath.Join(xdg, "git", "config"), fromXDG)
	writeConfig(filepath.Join(home, ".gitconfig"), fromHome)
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if got := globalExcludesFile(readUTF8Lines); got != fromHome {
		t.Fatalf("globalExcludesFile = %q, want %q (from ~/.gitconfig)", got, fromHome)
	}
}

// "\!name" and "\#name" ignore a file literally named "!name" or "#name". In a
// nested .gitignore that already worked; at the root the escape was dropped,
// turning the first into a negation and the second into a comment.
func TestRootPatternWithEscapedBangOrHashIgnoresLiteralName(t *testing.T) {
	dir := testenv.TempDir(t)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n!keep.log\n\\!important.txt\n\\#notes.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", ".gitignore"), []byte("\\!sub.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rules := At(dir, Options{})
	sub := rules.Descend(filepath.Join(dir, "sub"))
	cases := []struct {
		rules *Rules
		path  string
		want  bool
	}{
		{rules, "!important.txt", true},
		{rules, "#notes.txt", true},
		{rules, "important.txt", false},
		{rules, "notes.txt", false},
		{rules, "debug.log", true},
		{rules, "keep.log", false},
		{sub, "sub/!sub.txt", true},
		{sub, "sub/sub.txt", false},
	}
	for _, tc := range cases {
		if got := tc.rules.Ignored(filepath.Join(dir, filepath.FromSlash(tc.path)), false); got != tc.want {
			t.Errorf("Ignored(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A linked worktree's .git is a file, and its info/exclude lives in the common
// directory; a submodule's lives in the gitdir the file names. Git reads the
// repository's own exclude file from there in both.
func TestInfoExcludeIsReadWhenDotGitIsAFile(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	base := testenv.TempDir(t)
	common := filepath.Join(base, "main", ".git")
	writeTestFile(t, filepath.Join(common, "info", "exclude"), "secret.txt\n")
	writeTestFile(t, filepath.Join(common, "worktrees", "wt", "commondir"), "../..\n")
	modules := filepath.Join(base, "main", ".git", "modules", "sub")
	writeTestFile(t, filepath.Join(modules, "info", "exclude"), "private.txt\n")
	for _, gitDir := range []string{common, filepath.Join(common, "worktrees", "wt"), modules} {
		writeTestFile(t, filepath.Join(gitDir, "HEAD"), "ref: refs/heads/main\n")
	}
	// Directories that hold an info/exclude but no HEAD are not git directories,
	// so a pointer to one is not followed.
	writeTestFile(t, filepath.Join(base, "fake", "info", "exclude"), "other.txt\n")
	writeTestFile(t, filepath.Join(base, "fake-wt", "commondir"), "../fake\n")
	writeTestFile(t, filepath.Join(base, "fake-wt", "HEAD"), "ref: refs/heads/main\n")

	cases := map[string]struct {
		pointer string
		ignored string
	}{
		"plain repository":       {pointer: "", ignored: "plain.txt"},
		"linked worktree":        {pointer: "gitdir: " + filepath.Join(common, "worktrees", "wt") + "\n", ignored: "secret.txt"},
		"relative gitdir":        {pointer: "gitdir: ../main/.git/worktrees/wt\n", ignored: "secret.txt"},
		"submodule":              {pointer: "gitdir: ../main/.git/modules/sub\n", ignored: "private.txt"},
		"unreadable gitdir":      {pointer: "gitdir: ../nowhere\n", ignored: ""},
		"not a gitdir pointer":   {pointer: "garbage\n", ignored: ""},
		"gitdir without HEAD":    {pointer: "gitdir: ../fake\n", ignored: ""},
		"commondir without HEAD": {pointer: "gitdir: ../fake-wt\n", ignored: ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			wt := filepath.Join(base, "wt-"+strings.ReplaceAll(name, " ", "-"))
			if c.pointer == "" {
				writeTestFile(t, filepath.Join(wt, ".git", "info", "exclude"), "plain.txt\n")
			} else {
				writeTestFile(t, filepath.Join(wt, ".git"), c.pointer)
			}
			rules := At(wt, Options{})
			for _, f := range []string{"plain.txt", "secret.txt", "private.txt", "other.txt"} {
				got := rules.Ignored(filepath.Join(wt, f), false)
				if want := f == c.ignored; got != want {
					t.Errorf("Ignored(%s) = %v, want %v", f, got, want)
				}
			}
		})
	}
}
