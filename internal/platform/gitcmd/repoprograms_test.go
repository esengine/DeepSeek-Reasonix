package gitcmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Host git never runs a program named by the repository's own configuration.
// Each probe below runs the payload under stock git; each asserts the marker
// stays absent and the output still reports the change.

type repoFixture struct {
	t      *testing.T
	dir    string
	marker string
	ctx    context.Context
}

func requirePOSIXGit(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("payload script is POSIX shell")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

// newRepoFixture commits files (path -> content) with the given
// .gitattributes, using plain git: the fixture is setup, not the subject.
func newRepoFixture(t *testing.T, attributes string, files map[string]string) *repoFixture {
	t.Helper()
	requirePOSIXGit(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	f := &repoFixture{t: t, dir: t.TempDir(), marker: filepath.Join(t.TempDir(), "executed"), ctx: ctx}
	f.plain("init", "--quiet", "-b", "main")
	f.plain("config", "user.email", "test@example.com")
	f.plain("config", "user.name", "test")
	f.plain("config", "commit.gpgsign", "false")
	if attributes != "" {
		f.write(".gitattributes", attributes)
	}
	for path, content := range files {
		f.write(path, content)
	}
	f.plain("add", "-A")
	f.plain("commit", "--quiet", "-m", "initial")
	return f
}

func (f *repoFixture) plain(args ...string) string {
	f.t.Helper()
	cmd := exec.CommandContext(f.ctx, "git", append([]string{"-C", f.dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		f.t.Fatalf("setup git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func (f *repoFixture) write(rel, content string) {
	f.t.Helper()
	path := filepath.Join(f.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *repoFixture) appendConfig(rel, text string) {
	f.t.Helper()
	path := filepath.Join(f.dir, ".git", filepath.FromSlash(rel))
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		f.t.Fatal(err)
	}
	defer fh.Close()
	if _, err := fh.WriteString(text); err != nil {
		f.t.Fatal(err)
	}
}

// payload writes a script that records its run and passes stdin through, so a
// run that is not stopped still produces plausible git output.
func (f *repoFixture) payload() string {
	f.t.Helper()
	path := filepath.Join(f.t.TempDir(), "payload.sh")
	script := "#!/bin/sh\necho ran >> '" + f.marker + "'\ncat\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		f.t.Fatal(err)
	}
	return path
}

// makeStatDirty rewrites rel with same-length content and an old mtime, which
// forces git to re-hash the file — through its filter — to decide the answer.
func (f *repoFixture) makeStatDirty(rel, content string) {
	f.t.Helper()
	f.write(rel, content)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(f.dir, rel), old, old); err != nil {
		f.t.Fatal(err)
	}
}

func (f *repoFixture) run(args ...string) (string, error) {
	f.t.Helper()
	out, err := Command(f.ctx, "", append([]string{"-C", f.dir}, args...)...).CombinedOutput()
	return string(out), err
}

func (f *repoFixture) mustRun(args ...string) string {
	f.t.Helper()
	out, err := f.run(args...)
	if err != nil {
		f.t.Fatalf("host git %v: %v: %s", args, err, out)
	}
	return out
}

func (f *repoFixture) assertNotExecuted() {
	f.t.Helper()
	if data, err := os.ReadFile(f.marker); err == nil {
		f.t.Fatalf("host git ran a repository-configured program (%d runs)", strings.Count(string(data), "ran"))
	} else if !os.IsNotExist(err) {
		f.t.Fatalf("stat marker: %v", err)
	}
}

func TestHostStatusAndDiffDoNotRunRepositoryFilters(t *testing.T) {
	for _, tt := range []struct {
		name   string
		config func(f *repoFixture, payload string)
	}{
		{"subsection", func(f *repoFixture, p string) {
			f.appendConfig("config", "[filter \"pwn\"]\n\tclean = "+p+"\n\tprocess = "+p+"\n\trequired = true\n")
		}},
		{"legacy dotted section", func(f *repoFixture, p string) {
			f.appendConfig("config", "[filter.pwn]\n\tclean = "+p+"\n")
		}},
		{"included file", func(f *repoFixture, p string) {
			f.appendConfig("extra.cfg", "[filter \"pwn\"]\n\tclean = "+p+"\n")
			f.appendConfig("config", "[include]\n\tpath = extra.cfg\n")
		}},
		{"worktree scope", func(f *repoFixture, p string) {
			f.plain("config", "extensions.worktreeConfig", "true")
			f.appendConfig("config.worktree", "[filter \"pwn\"]\n\tclean = "+p+"\n")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newRepoFixture(t, "f.txt filter=pwn\n", map[string]string{"f.txt": "hello\n", "sub/g.txt": "g\n"})
			tt.config(f, f.payload())
			f.makeStatDirty("f.txt", "hellx\n")

			if out := f.mustRun("status", "--porcelain=v1"); !strings.Contains(out, " M f.txt") {
				t.Fatalf("status = %q, want the same-size edit reported", out)
			}
			if out := f.mustRun("diff", "--numstat", "HEAD", "--"); !strings.Contains(out, "1\t1\tf.txt") {
				t.Fatalf("numstat = %q, want 1/1 for f.txt", out)
			}
			if out, err := Command(f.ctx, f.dir, "diff", "HEAD", "--", "f.txt").CombinedOutput(); err != nil || !strings.Contains(string(out), "+hellx") {
				t.Fatalf("diff = %q, %v; want the working-tree line", out, err)
			}
			if out, err := Command(f.ctx, filepath.Join(f.dir, "sub"), "status", "--porcelain=v1").CombinedOutput(); err != nil || !strings.Contains(string(out), "f.txt") {
				t.Fatalf("status from a subdirectory = %q, %v", out, err)
			}
			f.mustRun("ls-files", "--modified")
			f.assertNotExecuted()
		})
	}
}

func TestHostHistoryReadsDoNotRunDiffDriversOrSignatureVerifiers(t *testing.T) {
	f := newRepoFixture(t, "f.txt diff=tc\n", map[string]string{"f.txt": "one\n"})
	p := f.payload()
	f.appendConfig("config", "[diff \"tc\"]\n\ttextconv = "+p+"\n\tcommand = "+p+"\n[diff]\n\texternal = "+p+
		"\n[log]\n\tshowSignature = true\n[gpg]\n\tprogram = "+p+"\n[gpg \"ssh\"]\n\tprogram = "+p+"\n")
	f.write("f.txt", "two\n")
	f.plain("-c", "diff.external=", "commit", "--quiet", "-am", "second")
	signCommitHeader(f)

	for _, args := range [][]string{
		{"log", "-p", "-1"},
		{"show", "HEAD"},
		{"diff", "HEAD~1", "HEAD"},
	} {
		if out := f.mustRun(args...); !strings.Contains(out, "+two") {
			t.Fatalf("git %v = %q, want the committed change", args, out)
		}
	}
	f.mustRun("log", "--format=%H %s", "-1")
	f.assertNotExecuted()
}

// signCommitHeader rewrites HEAD to carry a gpgsig header, the input that makes
// log.showSignature call the configured verifier.
func signCommitHeader(f *repoFixture) {
	f.t.Helper()
	raw := f.plain("cat-file", "commit", "HEAD")
	header, body, _ := strings.Cut(raw, "\n\n")
	signed := header + "\ngpgsig -----BEGIN PGP SIGNATURE-----\n \n AAAA\n -----END PGP SIGNATURE-----\n\n" + body
	cmd := exec.CommandContext(f.ctx, "git", "-C", f.dir, "hash-object", "-t", "commit", "-w", "--stdin")
	cmd.Stdin = strings.NewReader(signed)
	out, err := cmd.Output()
	if err != nil {
		f.t.Fatalf("hash signed commit: %v", err)
	}
	f.plain("update-ref", "HEAD", strings.TrimSpace(string(out)))
}

func TestHostSuperprojectInspectionDoesNotRunSubmoduleDrivers(t *testing.T) {
	sub := newRepoFixture(t, "s.txt filter=pwn\n", map[string]string{"s.txt": "hello\n"})
	f := newRepoFixture(t, "", map[string]string{"top.txt": "top\n"})
	f.plain("-c", "protocol.file.allow=always", "submodule", "--quiet", "add", sub.dir, "sm")
	f.plain("commit", "--quiet", "-m", "add submodule")
	f.appendConfig("modules/sm/config", "[filter \"pwn\"]\n\tclean = "+f.payload()+"\n")
	f.makeStatDirty("sm/s.txt", "hellx\n")
	f.write("top.txt", "changed\n")

	if out := f.mustRun("status", "--porcelain=v1"); !strings.Contains(out, " M top.txt") {
		t.Fatalf("status = %q, want the superproject change", out)
	}
	f.mustRun("diff", "--numstat", "HEAD", "--")
	f.assertNotExecuted()
}

func TestHostRepositoryMutationsDoNotRunRepositoryPrograms(t *testing.T) {
	f := newRepoFixture(t, "f.txt filter=pwn merge=pwn\n", map[string]string{"f.txt": "a\nb\nc\n"})
	f.plain("checkout", "--quiet", "-b", "side")
	f.write("f.txt", "a\nb\nX\n")
	f.plain("commit", "--quiet", "-am", "side")
	f.plain("checkout", "--quiet", "main")
	f.write("f.txt", "Y\nb\nc\n")
	f.plain("commit", "--quiet", "-am", "main")
	p := f.payload()
	hooks := filepath.Join(f.dir, ".git", "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, hook := range []string{"post-checkout", "reference-transaction", "post-merge", "pre-merge-commit", "pre-commit", "pre-auto-gc"} {
		if err := os.Symlink(p, filepath.Join(hooks, hook)); err != nil {
			t.Fatal(err)
		}
	}
	f.appendConfig("config", "[filter \"pwn\"]\n\tclean = "+p+"\n\tsmudge = "+p+"\n\tprocess = "+p+
		"\n[merge \"pwn\"]\n\tdriver = "+p+" %O %A %B\n")

	linked := filepath.Join(t.TempDir(), "linked")
	f.mustRun("worktree", "add", "--quiet", "--detach", linked, "HEAD")
	if data, err := os.ReadFile(filepath.Join(linked, "f.txt")); err != nil || string(data) != "Y\nb\nc\n" {
		t.Fatalf("worktree checkout = %q, %v; want the committed bytes", data, err)
	}
	f.write("new.txt", "new\n")
	f.mustRun("add", "-A")
	cmd := Command(f.ctx, f.dir, "hash-object", "--stdin-paths")
	cmd.Stdin = strings.NewReader("f.txt\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("hash-object: %v: %s", err, out)
	}
	f.mustRun("commit", "--quiet", "-m", "add new")
	_, _ = f.run("merge", "--no-edit", "side")
	_, _ = f.run("merge", "--abort")
	f.mustRun("update-ref", "refs/heads/probe", "HEAD")
	f.assertNotExecuted()
}

// A driver name git's -c syntax cannot address must stop the invocation rather
// than let it run with that driver live.
func TestUnaddressableDriverNameFailsClosed(t *testing.T) {
	f := newRepoFixture(t, "f.txt filter=a=b\n", map[string]string{"f.txt": "hello\n"})
	f.appendConfig("config", "[filter \"a=b\"]\n\tclean = "+f.payload()+"\n")
	f.makeStatDirty("f.txt", "hellx\n")

	_, err := f.run("status", "--porcelain=v1")
	if !errors.Is(err, ErrRepositoryDrivers) {
		t.Fatalf("status err = %v, want ErrRepositoryDrivers", err)
	}
	f.assertNotExecuted()
}

func TestRepositoryWithoutDriversAddsNoOverrides(t *testing.T) {
	f := newRepoFixture(t, "", map[string]string{"f.txt": "hello\n"})
	cmd := Command(f.ctx, f.dir, "status", "--porcelain=v1")
	for _, arg := range cmd.Args {
		// The baseline pins merge.verifySignatures etc.; only per-driver
		// overrides are what a driver-free repository must not add.
		if strings.HasSuffix(arg, ".clean=") || strings.HasSuffix(arg, ".process=") || strings.HasSuffix(arg, ".driver=") {
			t.Fatalf("args = %v, want no driver overrides for a driver-free repository", cmd.Args)
		}
	}
	if cmd.Err != nil {
		t.Fatalf("cmd.Err = %v", cmd.Err)
	}
}
