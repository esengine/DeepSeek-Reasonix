package gitcmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func repoWithUnaddressableDriver(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	cfg := filepath.Join(dir, ".git", "config")
	f, err := os.OpenFile(cfg, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("[filter \"a=b\"]\n\tclean = cat\n"); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A driver name no -c entry can address refuses an invocation only when that
// invocation can run the driver; resolving the repository never does.
func TestDriverListingIsSkippedWhereNoDriverCanRun(t *testing.T) {
	dir := repoWithUnaddressableDriver(t)
	ctx := context.Background()

	for _, args := range [][]string{{"status", "--porcelain"}, {"add", "-A"}, {"diff", "HEAD"}} {
		if err := Command(ctx, dir, args...).Run(); !errors.Is(err, ErrRepositoryDrivers) {
			t.Fatalf("git %v err = %v, want ErrRepositoryDrivers", args, err)
		}
	}
	if _, err := Open(ctx, dir); err != nil {
		t.Fatalf("Open: %v, want the repository resolved without listing its drivers", err)
	}
	for _, args := range [][]string{{"rev-parse", "--git-path", "index"}, {"symbolic-ref", "--quiet", "--short", "HEAD"}, {"write-tree"}, {"ls-tree", "-z", "--full-tree", "-r", "HEAD"}} {
		if err := Command(ctx, dir, args...).Run(); errors.Is(err, ErrRepositoryDrivers) {
			t.Fatalf("git %v was held for the driver listing; it can run no driver", args)
		}
	}
}

// --attr-source takes the next argument as its value, so a tree-ish spelled
// like a driver-free subcommand must not be read as the subcommand.
func TestAttrSourceValueIsNotTheSubcommand(t *testing.T) {
	f := newRepoFixture(t, "f.txt filter=pwn\n", map[string]string{"f.txt": "hello\n"})
	f.plain("branch", "rev-parse")
	p := f.payload()
	f.appendConfig("config", "[filter \"pwn\"]\n\tclean = "+p+"\n\tsmudge = "+p+"\n\tprocess = "+p+"\n")
	f.makeStatDirty("f.txt", "hellx\n")

	_, _ = f.run("--attr-source", "rev-parse", "status", "--porcelain")
	f.assertNotExecuted()
}

// Every global option form git accepts must leave subcommandIndex on the
// subcommand git runs: an alias for the probe name runs only if git reached it.
func TestSubcommandIndexAgreesWithGitGlobalOptions(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	forms := [][]string{
		{"-C", dir}, {"-c", "x.y=z"}, {"--git-dir", filepath.Join(dir, ".git")}, {"--git-dir=" + filepath.Join(dir, ".git")},
		{"--work-tree", dir}, {"--work-tree=" + dir}, {"--namespace", "ns"}, {"--namespace=ns"},
		{"--config-env", "x.y=HOME"}, {"--config-env=x.y=HOME"}, {"--super-prefix", "sub/"}, {"--super-prefix=sub/"},
		{"--attr-source", "HEAD"}, {"--attr-source=HEAD"}, {"--exec-path"}, {"--exec-path=/nonexistent"},
		{"--no-pager"}, {"--paginate"}, {"--no-replace-objects"}, {"--no-lazy-fetch"}, {"--no-optional-locks"},
		{"--no-advice"}, {"--bare"}, {"--literal-pathspecs"}, {"--glob-pathspecs"}, {"--noglob-pathspecs"}, {"--icase-pathspecs"},
	}
	const probe = "zzprobe"
	ran := 0
	for _, form := range forms {
		args := append(slices.Clone(form), probe)
		cmd := exec.Command("git", append([]string{"-c", "alias." + probe + "=!echo reached"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil || !strings.Contains(string(out), "reached") {
			continue
		}
		ran++
		if got := subcommandIndex(args); got != len(form) {
			t.Errorf("subcommandIndex(%v) = %d, git runs the subcommand at %d", args, got, len(form))
		}
	}
	if ran == 0 {
		t.Fatal("git accepted none of the global option forms")
	}
}

// Signature formats shell out to the configured gpg program; none of the
// three may be a program the repository chose.
func TestSignatureFormatsDoNotRunRepositoryGpgPrograms(t *testing.T) {
	f := newRepoFixture(t, "", map[string]string{"f.txt": "hello\n"})
	p := f.payload()
	f.appendConfig("config", "[gpg]\n\tprogram = "+p+"\n[gpg \"ssh\"]\n\tprogram = "+p+"\n[gpg \"x509\"]\n\tprogram = "+p+"\n")
	raw := f.plain("cat-file", "commit", "HEAD")
	head, msg, _ := strings.Cut(raw, "\n\n")
	forged := head + "\ngpgsig -----BEGIN PGP SIGNATURE-----\n \n abc\n -----END PGP SIGNATURE-----\n\n" + msg
	hash := exec.CommandContext(f.ctx, "git", "-C", f.dir, "hash-object", "-t", "commit", "-w", "--stdin")
	hash.Stdin = strings.NewReader(forged)
	id, err := hash.Output()
	if err != nil {
		t.Fatalf("forge signed commit: %v", err)
	}
	f.plain("update-ref", "refs/heads/signed", strings.TrimSpace(string(id)))

	_, _ = f.run("rev-list", "--format=%G?", "-n1", "signed")
	_, _ = f.run("for-each-ref", "--format=%(signature)", "refs/heads/signed")
	_, _ = f.run("log", "--format=%G?", "-n1", "signed")
	f.assertNotExecuted()
}
