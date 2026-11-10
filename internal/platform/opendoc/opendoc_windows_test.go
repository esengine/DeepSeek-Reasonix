package opendoc

import (
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"reasonix/internal/base/testenv"
)

var awkwardPaths = []string{
	`C:\Users\me\my site\index.html`,
	`C:\work\a & b\x.html`,
	`C:\work\100%\%PATH%.html`,
	`C:\work\a^b\c^^d.html`,
	`C:\work\中文 目录\页面 é ü.html`,
	`C:\work\say "hi" (1).html`,
	`C:\work\trailing backslash\` + strings.Repeat(`very long segment name `, 14) + `\deep\x.html`,
}

// The helper is a real child process: it reports the argument list Windows
// hands to a program started the way Open starts its opener.
func TestHelperReportsItsArguments(t *testing.T) {
	out := os.Getenv("OPENDOC_ARGS_OUT")
	if out == "" {
		t.Skip("helper for TestOpenerArgumentsArriveWhole")
	}
	i := 0
	for i < len(os.Args) && os.Args[i] != "--" {
		i++
	}
	data, _ := json.Marshal(os.Args[i+1:])
	if err := os.WriteFile(out, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOpenerArgumentsArriveWhole(t *testing.T) {
	for _, path := range awkwardPaths {
		_, args := command("windows", path)
		if len(args) != 2 || args[1] != path {
			t.Fatalf("command split the path: %q", args)
		}
		if got, err := windows.DecomposeCommandLine(windows.ComposeCommandLine(args)); err != nil || !reflect.DeepEqual(got, args) {
			t.Errorf("%q: the command line decomposes to %q, want %q", path, got, args)
		}
		out := testenv.TempDir(t) + `\args.json`
		child := exec.Command(os.Args[0], append([]string{"-test.run=^TestHelperReportsItsArguments$", "--"}, args...)...)
		child.Env = append(os.Environ(), "OPENDOC_ARGS_OUT="+out)
		if b, err := child.CombinedOutput(); err != nil {
			t.Fatalf("%q: helper failed: %v\n%s", path, err, b)
		}
		var got []string
		data, err := os.ReadFile(out)
		if err != nil || json.Unmarshal(data, &got) != nil {
			t.Fatalf("%q: no report from the helper: %v", path, err)
		}
		if !reflect.DeepEqual(got, args) {
			t.Errorf("%q: the child received %q, want %q", path, got, args)
		}
	}
}
