package browser

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

var networkSpellings = []string{
	`\\evil\share\x.html`,
	`//evil/share/x.html`,
	`/\evil\x`,
	`\/evil/x`,
	`\\127.0.0.1\c$\windows\win.ini`,
	`\\?\UNC\evil\share\x`,
	`//?/UNC/evil/share/x`,
	`\\.\UNC\evil\share\x`,
	`\\.\pipe\x`,
	`\??\UNC\evil\share\x`,
	`///evil/share`,
}

// onWindowsPaths makes the Windows reading of a path hold for the test and
// counts every filesystem call fileWithin and its callers make.
func onWindowsPaths(t *testing.T) *int {
	t.Helper()
	calls := new(int)
	oldWin, oldResolve, oldStat := windowsPaths, resolveLinks, statFile
	windowsPaths = true
	resolveLinks = func(p string) (string, error) { *calls++; return filepath.EvalSymlinks(p) }
	statFile = func(p string) (os.FileInfo, error) { *calls++; return os.Stat(p) }
	t.Cleanup(func() { windowsPaths, resolveLinks, statFile = oldWin, oldResolve, oldStat })
	return calls
}

func TestFileWithinRefusesANetworkPathBeforeTouchingTheFilesystem(t *testing.T) {
	calls := onWindowsPaths(t)
	roots := []string{testenv.TempDir(t)}
	for _, p := range networkSpellings {
		if fileWithin(p, roots) {
			t.Errorf("fileWithin(%q) = true", p)
		}
		if got := judgeFile(p, roots); got != fileNetwork {
			t.Errorf("judgeFile(%q) = %v, want fileNetwork", p, got)
		}
	}
	if *calls != 0 {
		t.Errorf("a network path reached the filesystem %d time(s)", *calls)
	}
}

func TestFileWithinStillResolvesALocalPath(t *testing.T) {
	root := testenv.TempDir(t)
	page := filepath.Join(root, "a.html")
	if err := os.WriteFile(page, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	calls := onWindowsPaths(t)
	if got := judgeFile(page, []string{root}); got != fileInside {
		t.Errorf("judgeFile(%q) = %v, want fileInside", page, got)
	}
	if *calls == 0 {
		t.Error("a local path was judged without resolving it")
	}
}

func TestCheckURLNamesANetworkPath(t *testing.T) {
	calls := onWindowsPaths(t)
	roots := []string{testenv.TempDir(t)}
	inputs := []string{
		"file:////evil/x",
		"file:///\\\\evil\\x",
		"file://evil/share/x",
		"file:///%5C%5Cevil/x",
		`\\evil\share\x.html`,
		`\\127.0.0.1\c$\windows\win.ini`,
		`\\?\UNC\evil\share\x`,
		`\\.\UNC\evil\share\x`,
		`\\.\pipe\x`,
	}
	for _, in := range inputs {
		_, err := checkURL(in, roots)
		if CodeOf(err) != CodeNetworkPath {
			t.Errorf("checkURL(%q) = %v, want %s", in, err, CodeNetworkPath)
		}
	}
	if *calls != 0 {
		t.Errorf("a network address reached the filesystem %d time(s)", *calls)
	}
}

func TestUploadPathsRefusesANetworkPathBeforeTouchingTheFilesystem(t *testing.T) {
	calls := onWindowsPaths(t)
	roots := []string{testenv.TempDir(t)}
	for _, p := range networkSpellings {
		_, err := uploadPaths([]string{p}, roots)
		if CodeOf(err) != CodeNetworkPath {
			t.Errorf("uploadPaths(%q) = %v, want %s", p, err, CodeNetworkPath)
		}
	}
	if *calls != 0 {
		t.Errorf("a network path reached the filesystem %d time(s)", *calls)
	}
}

func TestUploadPathsKeepsItsOtherRefusals(t *testing.T) {
	root := testenv.TempDir(t)
	inside := filepath.Join(root, "f.txt")
	if err := os.WriteFile(inside, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(testenv.TempDir(t), "o.txt")
	if err := os.WriteFile(outside, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := uploadPaths([]string{inside}, []string{root}); err != nil || len(got) != 1 {
		t.Errorf("a file inside the workspace = %v, %v", got, err)
	}
	if _, err := uploadPaths([]string{outside}, []string{root}); CodeOf(err) != CodeURLRefused {
		t.Errorf("a file outside = %v, want %s", err, CodeURLRefused)
	}
	statFile = func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	t.Cleanup(func() { statFile = os.Stat })
	if _, err := uploadPaths([]string{inside}, []string{root}); CodeOf(err) != CodeBadStep {
		t.Errorf("a missing file = %v, want %s", err, CodeBadStep)
	}
}

func TestANetworkPathRefusalNamesItsCause(t *testing.T) {
	_, err := checkURL(`\\evil\share\x.html`, nil)
	f, ok := errors.AsType[*Failure](err)
	if !ok || f.Code != CodeNetworkPath || f.Detail == "" {
		t.Fatalf("got %#v", err)
	}
}

func TestNetworkRootKeepsItsOwnFilesWithoutALookup(t *testing.T) {
	calls := onWindowsPaths(t)
	roots := []string{`\\nas\team\proj`}
	for _, inside := range []string{`\\nas\team\proj\a.html`, `//NAS/team/proj/sub/a.html`, `\\?\UNC\nas\team\proj\a.html`} {
		if got := judgeFile(inside, roots); got != fileInside {
			t.Errorf("judgeFile(%q) = %v, want fileInside under a network root", inside, got)
		}
	}
	for _, outside := range []string{`\\nas\team\other\a.html`, `\\evil\team\proj\a.html`, `\\nas\team\proj\..\x`, `\\localhost\team\proj\a.html`} {
		if got := judgeFile(outside, roots); got != fileNetwork {
			t.Errorf("judgeFile(%q) = %v, want fileNetwork", outside, got)
		}
	}
	if *calls != 0 {
		t.Errorf("network spellings reached the filesystem %d time(s)", *calls)
	}
}
