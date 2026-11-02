package openwith

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// fake is a machine whose disk is a set of paths.
type fake struct {
	m       machine
	files   map[string]bool
	dirs    map[string]bool
	started []call
}

func newFake(t *testing.T, goos string, env map[string]string) *fake {
	t.Helper()
	if goos != "windows" && runtime.GOOS == "windows" {
		t.Skip("fakes a POSIX disk; the code joins paths with the host separator")
	}
	f := &fake{files: map[string]bool{}, dirs: map[string]bool{}}
	f.m = machine{
		goos:   goos,
		getenv: func(k string) string { return env[k] },
		glob: func(pattern string) ([]string, error) {
			re := regexp.MustCompile("^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), `\*`, ".*") + "$")
			var out []string
			for p := range f.files {
				if re.MatchString(p) {
					out = append(out, p)
				}
			}
			sort.Strings(out)
			return out, nil
		},
		isFile: func(p string) bool { return f.files[p] },
		isDir:  func(p string) bool { return f.dirs[p] },
		eval:   func(p string) (string, error) { return p, nil },
		start:  func(exe string, args []string) error { f.started = append(f.started, call{exe, args}); return nil },
	}
	return f
}

func availability(f *fake) map[string]bool {
	out := map[string]bool{}
	for _, s := range f.m.list() {
		out[s.ID] = s.Available
	}
	return out
}

func TestAvailabilityIsWhetherADeclaredExecutableExists(t *testing.T) {
	f := newFake(t, "linux", map[string]string{"PATH": "/usr/bin"})
	f.files["/usr/bin/xdg-open"] = true
	f.files["/usr/bin/code"] = true
	f.files["/usr/bin/gnome-terminal"] = true
	got := availability(f)
	want := map[string]bool{"file-manager": true, "vscode": true, "cursor": false, "idea": false, "terminal": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("availability = %v, want %v", got, want)
	}
}

func TestAProgramNamedLikeATargetButNotDeclaredIsNotOffered(t *testing.T) {
	f := newFake(t, "linux", map[string]string{"PATH": "/usr/bin"})
	f.files["/usr/bin/mycoolide"] = true
	f.files["/usr/bin/Cursor-Installer"] = true
	for id, ok := range availability(f) {
		if ok {
			t.Fatalf("%s offered from a program nobody declared", id)
		}
	}
}

func TestTheFolderIsOneArgumentWhateverItContains(t *testing.T) {
	f := newFake(t, "linux", map[string]string{"PATH": "/usr/bin"})
	f.files["/usr/bin/code"] = true
	f.files["/usr/bin/gnome-terminal"] = true
	for _, dir := range []string{"/home/a b/proj", "/home/x'; rm -rf ~; '/p", "/home/$(whoami)/p", "/home/`id`/p", "/home/a\\\"b/p", "/tmp/--help"} {
		f.dirs[dir] = true
		f.started = nil
		if _, err := f.m.open("vscode", dir); err != nil {
			t.Fatal(err)
		}
		if _, err := f.m.open("terminal", dir); err != nil {
			t.Fatal(err)
		}
		want := []call{{"/usr/bin/code", []string{dir}}, {"/usr/bin/gnome-terminal", []string{"--working-directory=" + dir}}}
		if !reflect.DeepEqual(f.started, want) {
			t.Fatalf("started %v, want %v", f.started, want)
		}
	}
}

func TestOnlyAnAbsoluteFolderThatIsADirectoryIsAccepted(t *testing.T) {
	f := newFake(t, "linux", map[string]string{"PATH": "/usr/bin"})
	f.files["/usr/bin/code"] = true
	f.dirs["/work/p"] = true
	for _, dir := range []string{"", "p", "./p", "-n", "--new-window", "//server/share", "/work/p\x00", "/work/missing"} {
		if _, err := f.m.open("vscode", dir); !errors.Is(err, ErrBadFolder) {
			t.Fatalf("%q = %v, want ErrBadFolder", dir, err)
		}
	}
	if len(f.started) != 0 {
		t.Fatalf("started %v for refused folders", f.started)
	}
}

func TestUnknownAndMissingTargetsAreRefusedWithTheirOwnErrors(t *testing.T) {
	f := newFake(t, "linux", map[string]string{"PATH": "/usr/bin"})
	f.dirs["/work/p"] = true
	if _, err := f.m.open("rm", "/work/p"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("unknown = %v", err)
	}
	if _, err := f.m.open("cursor", "/work/p"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("not installed = %v", err)
	}
}

func TestAProgramInsideAProjectIsNeverStarted(t *testing.T) {
	f := newFake(t, "linux", map[string]string{"PATH": "/usr/bin"})
	f.m.getenv = func(k string) string { return map[string]string{"PATH": "/work/p/node_modules/.bin:/usr/bin"}[k] }
	f.m.forbidden = []string{"/work/p"}
	f.files["/work/p/node_modules/.bin/code"] = true
	f.dirs["/work/p"] = true
	if availability(f)["vscode"] {
		t.Fatal("a project's own `code` was offered")
	}
	if _, err := f.m.open("vscode", "/work/p"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("open = %v", err)
	}
	f.files["/usr/bin/code"] = true
	if _, err := f.m.open("vscode", "/work/p"); err != nil || f.started[0].exe != "/usr/bin/code" {
		t.Fatalf("the installed one was not preferred: %v %v", err, f.started)
	}
}

func TestWindowsStartsOnlyRealExecutablesNeverScriptsOrShares(t *testing.T) {
	env := map[string]string{"LOCALAPPDATA": `C:\Users\u\AppData\Local`, "ProgramFiles": `C:\Program Files`, "SystemRoot": `C:\Windows`}
	f := newFake(t, "windows", env)
	f.files[`C:\Users\u\AppData\Local\Programs\Microsoft VS Code\Code.exe`] = true
	f.files[`C:\Program Files\JetBrains\IntelliJ IDEA 2026.1\bin\idea64.exe`] = true
	f.files[`C:\Users\u\AppData\Local\Microsoft\WindowsApps\wt.exe`] = true
	f.dirs[`C:\work\my project`] = true
	got := availability(f)
	if !got["vscode"] || !got["idea"] || !got["terminal"] || got["cursor"] || got["file-manager"] {
		t.Fatalf("availability = %v", got)
	}
	if _, err := f.m.open("terminal", `C:\work\my project`); err != nil {
		t.Fatal(err)
	}
	want := call{`C:\Users\u\AppData\Local\Microsoft\WindowsApps\wt.exe`, []string{"-d", `C:\work\my project`}}
	if !reflect.DeepEqual(f.started[0], want) {
		t.Fatalf("started %v, want %v", f.started[0], want)
	}
	f.files[`C:\Windows\explorer.cmd`] = true
	for _, share := range []string{`\\server\share\p`, `\\?\C:\p`, `C:p`, ``} {
		if _, err := f.m.open("vscode", share); !errors.Is(err, ErrBadFolder) {
			t.Fatalf("%q = %v", share, err)
		}
	}
}

func TestWindowsRefusesAShimThatNeedsAShell(t *testing.T) {
	f := newFake(t, "windows", map[string]string{"ProgramFiles": `C:\Program Files`, "PATH": `C:\tools`})
	f.files[`C:\Program Files\Microsoft VS Code\Code.exe`] = true
	f.files[`C:\tools\code.cmd`] = true
	if c, ok := f.m.resolve(targets[1], `C:\p`); !ok || !strings.HasSuffix(c.exe, "Code.exe") {
		t.Fatalf("resolve = %v %v", c, ok)
	}
}

func TestMacApplicationsAreStartedThroughOpenWithTheBundle(t *testing.T) {
	f := newFake(t, "darwin", map[string]string{"HOME": "/Users/u"})
	f.files["/usr/bin/open"] = true
	f.dirs["/Applications/Visual Studio Code.app"] = true
	f.dirs["/Users/u/Applications/Terminal.app"] = true
	f.dirs["/work/p q"] = true
	if _, err := f.m.open("vscode", "/work/p q"); err != nil {
		t.Fatal(err)
	}
	want := call{"/usr/bin/open", []string{"-a", "/Applications/Visual Studio Code.app", "/work/p q"}}
	if !reflect.DeepEqual(f.started[0], want) {
		t.Fatalf("started %v, want %v", f.started[0], want)
	}
	if !availability(f)["terminal"] || availability(f)["cursor"] {
		t.Fatalf("availability = %v", availability(f))
	}
}

// A link on PATH that points into a project is that project's program.
func TestALinkIntoAProjectIsNeverStarted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating links needs a privilege Windows tests do not hold")
	}
	root := t.TempDir()
	project := filepath.Join(root, "project")
	bin := filepath.Join(root, "bin")
	for _, d := range []string{filepath.Join(project, "node_modules", ".bin"), bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	planted := filepath.Join(project, "node_modules", ".bin", "code")
	if err := os.WriteFile(planted, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(planted, filepath.Join(bin, "code")); err != nil {
		t.Fatal(err)
	}
	m := system(func(k string) string {
		if k == "PATH" {
			return bin
		}
		return ""
	})
	m.goos = "linux"
	m.forbidden = []string{project}
	if _, ok := m.resolve(targets[1], filepath.Join(root, "x")); ok {
		t.Fatal("a link pointing into the project was offered as VS Code")
	}

	// A project reached through a link is the same project.
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(project, alias); err != nil {
		t.Fatal(err)
	}
	m.forbidden = []string{alias}
	if _, ok := m.resolve(targets[1], filepath.Join(root, "x")); ok {
		t.Fatal("the project, named through a link, was not recognised")
	}

	// A link to somewhere outside every project is fine, and a dangling one is not.
	elsewhere := filepath.Join(root, "installed", "code")
	if err := os.MkdirAll(filepath.Dir(elsewhere), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(elsewhere, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(bin, "code")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(bin, "code")); err != nil {
		t.Fatal(err)
	}
	m.forbidden = []string{project}
	if c, ok := m.resolve(targets[1], filepath.Join(root, "x")); !ok || c.exe != filepath.Join(bin, "code") {
		t.Fatalf("a link to an installed program was refused: %v %v", c, ok)
	}
}

func TestAProgramInsideAProjectIsReportedWithWhereAndWhy(t *testing.T) {
	f := newFake(t, "linux", map[string]string{"PATH": "/home/u/.local/bin:/usr/bin"})
	f.files["/home/u/.local/bin/cursor"] = true
	f.m.forbidden = []string{"/home/u"}
	var cursor, idea Status
	for _, s := range f.m.list() {
		switch s.ID {
		case "cursor":
			cursor = s
		case "idea":
			idea = s
		}
	}
	if cursor.Available || cursor.Why != WhyInsideProject || cursor.Within != "/home/u" {
		t.Fatalf("cursor = %+v, want it unavailable because it sits inside the project folder, naming that folder", cursor)
	}
	if idea.Available || idea.Why != "" || idea.Within != "" {
		t.Fatalf("idea = %+v, want a program that is simply not installed to carry no reason", idea)
	}
}

func TestAProgramOutsideEveryProjectCarriesNoReason(t *testing.T) {
	f := newFake(t, "linux", map[string]string{"PATH": "/home/u/.local/bin:/usr/bin"})
	f.files["/home/u/.local/bin/cursor"] = true
	f.m.forbidden = []string{"/home/u/project"}
	for _, s := range f.m.list() {
		if s.ID == "cursor" && (!s.Available || s.Why != "" || s.Within != "") {
			t.Fatalf("cursor = %+v, want it listed and carrying no reason", s)
		}
	}
}
