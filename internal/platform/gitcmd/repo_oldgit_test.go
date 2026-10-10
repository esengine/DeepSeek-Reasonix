//go:build !windows

package gitcmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// preOptionGit puts first on PATH a git older than 2.31: rev-parse does not
// know --path-format and echoes it back as an extra output line.
func preOptionGit(t *testing.T) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	script := `#!/bin/sh
n=$#; i=0; echoed=
while [ $i -lt $n ]; do
  a=$1; shift
  case "$a" in --path-format=*) echoed=$a ;; *) set -- "$@" "$a" ;; esac
  i=$((i+1))
done
[ -n "$echoed" ] && echo "$echoed"
exec "` + real + `" "$@"
`
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestOpenOnGitWithoutPathFormat(t *testing.T) {
	for _, linked := range []bool{false, true} {
		name := "main"
		if linked {
			name = "linked"
		}
		t.Run(name, func(t *testing.T) {
			f := newRepoFixture(t, "", map[string]string{"f.txt": "one\n"})
			dir := f.dir
			if linked {
				dir = filepath.Join(t.TempDir(), "linked")
				f.plain("worktree", "add", "--quiet", dir)
			}
			alias := filepath.Join(t.TempDir(), "alias")
			if err := os.Symlink(dir, alias); err != nil {
				t.Fatal(err)
			}
			want := f.open(dir)
			preOptionGit(t)
			got, err := Open(f.ctx, alias)
			if errors.Is(err, ErrNotRepository) {
				t.Fatalf("Open on git without --path-format = %v, want the repository", err)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.GitDir != want.GitDir || got.CommonDir != want.CommonDir || got.WorkTree != want.WorkTree {
				t.Fatalf("identity = %+v, want %+v", got, want)
			}
		})
	}
}
