package control

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestSavedAttachmentsStayOutOfGitStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := testenv.TempDir(t)
	// The machine's git config and ignore files are cut off: a global excludes
	// file that already ignores .reasonix would pass this for the wrong reason.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(testenv.TempDir(t), "none"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("XDG_CONFIG_HOME", testenv.TempDir(t))
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "core.excludesFile="}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	if _, err := SaveImageBytesInRoot(root, "image/png", mustBase64(t, tinyPNG)); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveAttachmentBytesInRoot(root, "notes.txt", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if got := git("status", "--porcelain", "--untracked-files=all"); got != "" {
		t.Fatalf("pasted attachments show up as changes to commit:\n%s", got)
	}
}
