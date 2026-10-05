package builtin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRipgrepRestoresOnlyTheSearchRootPrefix(t *testing.T) {
	target := filepath.Join(t.TempDir(), "physical")
	requested := filepath.Join(filepath.Dir(target), "alias")
	file := filepath.Join(target, "nested", "hit.txt")
	text := ":12:keep " + target + "/../content unchanged"
	got := restoreRipgrepPath(file+text, target, requested)
	want := filepath.Join(requested, "nested", "hit.txt") + text
	if got != want {
		t.Fatalf("result=%q, want %q", got, want)
	}
	neighbor := filepath.Join(target+"-other", "hit.txt") + text
	if got := restoreRipgrepPath(neighbor, target, requested); got != neighbor {
		t.Fatalf("rewrote a neighboring root: %q", got)
	}
	rp := ResolvedPath{Root: requested, DisplayRoot: "external/source", External: true}
	if got := displayRipgrepLine(got, rp); got != "external/source/nested/hit.txt"+text {
		t.Fatalf("external projection changed the content or lost the token: %q", got)
	}
}

func TestRipgrepErrorsPreserveIdentityAndExternalPaths(t *testing.T) {
	target := filepath.Join(t.TempDir(), "physical")
	requested := filepath.Join(filepath.Dir(target), "alias")
	rp := ResolvedPath{Root: requested, DisplayRoot: "external/source", External: true}
	for _, cause := range []error{
		&os.PathError{Op: "chdir", Path: target, Err: os.ErrNotExist},
		errors.New(target + ": No such file or directory"),
	} {
		err := ripgrepDisplayError(cause, target, requested, rp)
		if !errors.Is(err, cause) || !strings.Contains(err.Error(), "external/source") {
			t.Fatalf("lost cause or display path: %v", err)
		}
		if strings.Contains(err.Error(), filepath.Dir(target)) {
			t.Fatalf("error leaked a local path: %v", err)
		}
	}
	missingBinary := &os.PathError{Op: "fork/exec", Path: "missing-rg", Err: os.ErrNotExist}
	if err := ripgrepDisplayError(missingBinary, target, requested, rp); !strings.Contains(err.Error(), "missing-rg") || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("executable failure was misattributed to the search root: %v", err)
	}
}
