package builtin

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/safety/sandbox"
)

// makeNUL writes a regular file named NUL. On Windows only the extended-length
// form reaches the name; every other path resolves it to the device.
func makeNUL(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "NUL")
	if runtime.GOOS == "windows" {
		path = `\\?\` + path
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestNULWatchReportsOnlyAFileTheCommandCreated(t *testing.T) {
	gitBash := sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}

	dir := testenv.TempDir(t)
	w := armNULWatch("windows", gitBash, dir)
	if w.note() != "" {
		t.Fatal("reported a NUL file before any was created")
	}
	makeNUL(t, dir)
	if note := w.note(); !strings.Contains(note, "/dev/null") || !strings.Contains(note, dir) {
		t.Fatalf("note = %q, want the directory and the /dev/null remedy", note)
	}

	if armNULWatch("windows", gitBash, dir).armed {
		t.Fatal("armed over a NUL file that was already there")
	}
	for name, w := range map[string]nulWatch{
		"linux":      armNULWatch("linux", gitBash, testenv.TempDir(t)),
		"powershell": armNULWatch("windows", sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: "powershell"}, testenv.TempDir(t)),
	} {
		if w.armed {
			t.Errorf("%s: armed where MSYS does not run", name)
		}
	}
}

// Through the real tool: Git Bash turns a NUL path into a file, and the result
// has to say so rather than leave the model believing it discarded output.
func TestBashNamesTheNULFileGitBashCreated(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("NUL is an ordinary name outside Windows")
	}
	b := bash{sb: sandbox.Spec{Mode: "off"}, workDir: testenv.TempDir(t)}
	b.shell = b.resolved()
	if b.shell.Kind != sandbox.ShellBash {
		t.Skip("Git Bash is not installed")
	}
	out, err := b.Execute(context.Background(), argsJSON(t, map[string]any{"command": "touch NUL"}))
	if err != nil {
		t.Fatalf("touch: %v", err)
	}
	if !nulFileIn(b.workDir) {
		t.Fatal("Git Bash did not create the file; this test proves nothing")
	}
	if !strings.Contains(out, "created a file named NUL") {
		t.Fatalf("output = %q, want the NUL file named", out)
	}
}
