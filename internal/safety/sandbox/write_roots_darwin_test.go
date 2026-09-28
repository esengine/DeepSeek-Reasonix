package sandbox

import (
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

// $TMPDIR sits in /private/var/folders, which every jailed command may write,
// so a command can replace it with a link; the next profile must not follow it.
func TestSeatbeltDoesNotFollowATempDirReplacedByALink(t *testing.T) {
	base := realDir(t, filepath.Join(testenv.TempDir(t), "fake"))
	if !strings.HasPrefix(base, "/private/var/folders/") {
		t.Skip("temp tree is not under /private/var/folders")
	}
	ws := testenv.TempDir(t)
	tmp := filepath.Join(base, "T")
	symlink(t, "/usr", tmp)
	t.Setenv("TMPDIR", tmp+"/")
	profile := seatbeltProfile(Spec{Mode: "enforce", WriteRoots: []string{ws}})
	if strings.Contains(profile, `(subpath "/usr")`) {
		t.Fatalf("profile follows the replaced temp dir:\n%s", profile)
	}
	var dropped bool
	for _, d := range DroppedWriteRoots(Spec{Mode: "enforce"}) {
		dropped = dropped || (d.Root == tmp+"/" && d.Code == WriteRootRedirectedCode)
	}
	if !dropped {
		t.Fatal("the replaced temp dir is not reported")
	}
}
