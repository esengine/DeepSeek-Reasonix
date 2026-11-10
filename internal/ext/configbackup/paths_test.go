package configbackup

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
)

// The existence hint beside a command asks the disk only about plain absolute
// words: a backup from elsewhere must not be able to make this machine probe
// arbitrary places by climbing out of one.
func TestAbsPathsAsksTheDiskOnlyAboutPlainWords(t *testing.T) {
	dir := testenv.TempDir(t)
	real := filepath.Join(dir, "tool")
	if err := os.WriteFile(real, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		word string
		want bool
	}{{real, true}, {filepath.Join(dir, "nope"), false}, {dir + "/../" + filepath.Base(dir) + "/tool", false}, {real + "\x00", false}} {
		got := absPaths(tc.word)
		if len(got) != 1 || got[0].Exists != tc.want {
			t.Fatalf("%q = %+v, want exists=%v", tc.word, got, tc.want)
		}
	}
}
