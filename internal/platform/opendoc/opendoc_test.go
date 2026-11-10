package opendoc

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/internal/base/testenv"
)

// The file is one argument of an argument list on every system: nothing is
// built into a command line a shell would read, and nothing but the platform's
// own opener is ever started.
func TestCommandPassesThePathAsOneArgumentToThePlatformOpener(t *testing.T) {
	const path = `C:\Users\me\my site\a & b (1).html`
	for _, tc := range []struct {
		goos string
		name string
		args []string
	}{
		{"darwin", "open", []string{path}},
		{"windows", "rundll32", []string{"url.dll,FileProtocolHandler", path}},
		{"linux", "xdg-open", []string{path}},
		{"freebsd", "xdg-open", []string{path}},
	} {
		name, args := command(tc.goos, path)
		if name != tc.name || !reflect.DeepEqual(args, tc.args) {
			t.Errorf("%s: command = %s %q, want %s %q", tc.goos, name, args, tc.name, tc.args)
		}
	}
}

func TestOpenStartsTheOpenerAndReportsWhyItCouldNot(t *testing.T) {
	doc := filepath.Join(testenv.TempDir(t), "a.html")
	var gotName string
	var gotArgs []string
	t.Cleanup(func() { start = realStart })

	start = func(name string, args ...string) error { gotName, gotArgs = name, args; return nil }
	if err := Open(doc); err != nil || gotName == "" || gotArgs[len(gotArgs)-1] != doc {
		t.Fatalf("Open = %v, started %q %q", err, gotName, gotArgs)
	}

	boom := errors.New("no opener")
	start = func(string, ...string) error { return boom }
	if err := Open(doc); !errors.Is(err, boom) {
		t.Fatalf("a failed start was not reported: %v", err)
	}
}

func TestOpenRefusesWhatIsNotAnAbsolutePath(t *testing.T) {
	t.Cleanup(func() { start = realStart })
	start = func(string, ...string) error { t.Fatal("started with a relative path"); return nil }
	for _, path := range []string{"", "a.html", "../a.html", "-flag", "https://example.com/"} {
		if err := Open(path); err == nil {
			t.Errorf("Open(%q) succeeded", path)
		}
	}
}
