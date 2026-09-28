package sandbox

import (
	"path/filepath"
	"strings"
	"testing"
)

// $TMPDIR sits in /private/var/folders, which every jailed command may write,
// so a command can replace it with a link; the next profile must not follow it.
func TestSeatbeltDoesNotFollowATempDirReplacedByALink(t *testing.T) {
	base := planDir(t, filepath.Join(t.TempDir(), "fake"))
	if !strings.HasPrefix(base, "/private/var/folders/") {
		t.Skip("temp tree is not under /private/var/folders")
	}
	ws := t.TempDir()
	tmp := filepath.Join(base, "T")
	planLink(t, "/usr", tmp)
	t.Setenv("TMPDIR", tmp+"/")
	if profile := seatbeltProfile(Spec{Mode: "enforce", WriteRoots: []string{ws}}); strings.Contains(profile, `(subpath "/usr")`) {
		t.Fatalf("profile follows the replaced temp dir:\n%s", profile)
	}
}
