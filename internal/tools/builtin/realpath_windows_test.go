//go:build windows

package builtin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// Every spelling Windows has for one place has to reach one comparison.
func TestRealPathSettlesEveryWindowsSpelling(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "A Long Target Name")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "f.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	want, err := realPath(target)
	if err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(base, "junction")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
	short := make([]uint16, 300)
	src, _ := windows.UTF16PtrFromString(target)
	n, err := windows.GetShortPathName(src, &short[0], uint32(len(short)))
	if err != nil || n == 0 {
		t.Fatalf("GetShortPathName: %v", err)
	}
	spellings := map[string]string{
		"junction":        junction,
		"junction below":  filepath.Join(junction, "f.txt"),
		"extended prefix": `\\?\` + target,
		"upper case":      strings.ToUpper(target),
		"8.3 short name":  windows.UTF16ToString(short[:n]),
		"forward slashes": filepath.ToSlash(target),
	}
	for name, spelling := range spellings {
		got, err := realPath(spelling)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		expect := want
		if name == "junction below" {
			expect = filepath.Join(want, "f.txt")
		}
		if got != expect {
			t.Errorf("%s: realPath(%q) = %q, want %q", name, spelling, got, expect)
		}
		if !readOutsideScope([]string{filepath.Join(base, "elsewhere")}, spelling) {
			t.Errorf("%s: %q was inside an unrelated scope", name, spelling)
		}
		if readOutsideScope([]string{want}, spelling) {
			t.Errorf("%s: %q resolved outside its own folder", name, spelling)
		}
	}
}
