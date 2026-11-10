package boot

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
)

func TestWorkspaceBranchSummaryReadsOverlapThroughBuild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git wrapper uses POSIX shell")
	}
	dir := testenv.TempDir(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "base", "--allow-empty"},
		{"branch", "next"},
	} {
		cmd := exec.Command(realGit, append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: dir, Sink: event.Discard, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	bin, signals := testenv.TempDir(t), testenv.TempDir(t)
	wrapper := `#!/bin/sh
probe=
for arg in "$@"; do
 case "$arg" in
 symbolic-ref) probe=branch ;;
 diff) probe=diff ;;
 --untracked-files=normal) probe=status ;;
 esac
done
if [ -n "$probe" ]; then
 : > "$BRANCH_PROBE_SIGNALS/$probe"
 ready=
 attempt=0
 while [ "$attempt" -lt 200 ]; do
  attempt=$((attempt + 1))
  if [ -f "$BRANCH_PROBE_SIGNALS/branch" ] && [ -f "$BRANCH_PROBE_SIGNALS/diff" ] && [ -f "$BRANCH_PROBE_SIGNALS/status" ]; then
   ready=yes
   break
  fi
  sleep 0.01
 done
 [ "$ready" = yes ] || exit 86
fi
exec "$BRANCH_PROBE_GIT" "$@"
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BRANCH_PROBE_SIGNALS", signals)
	t.Setenv("BRANCH_PROBE_GIT", realGit)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.MkdirAll(filepath.Join(dir, "scratch"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scratch", "keep.txt"), []byte("local work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	info, ok, err := ctrl.SwitchWorkspaceBranch(ctx, "next")
	if err != nil || !ok || info.Branch != "next" || info.Detached || info.Untracked != 1 || info.Added != 0 || info.Removed != 0 {
		t.Fatalf("switch summary = %+v ok=%v err=%v; independent reads must not wait on one another", info, ok, err)
	}
	out, err := exec.Command(realGit, "-C", dir, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(out)) != "next" {
		t.Fatalf("HEAD = %s err=%v", out, err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "scratch", "keep.txt"))
	if err != nil || string(body) != "local work\n" {
		t.Fatalf("local work changed: %q %v", body, err)
	}
}
