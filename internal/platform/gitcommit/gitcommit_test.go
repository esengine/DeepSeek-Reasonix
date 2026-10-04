package gitcommit

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/gitcmd"
)

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(k+"_NAME", "t")
		t.Setenv(k+"_EMAIL", "t@t")
	}
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) (string, gitcmd.Repo) {
	t.Helper()
	isolate(t)
	dir := testenv.TempDir(t)
	run(t, dir, "init", "-q")
	write(t, dir, "a.go", "package a\n")
	run(t, dir, "add", "a.go")
	run(t, dir, "commit", "-qm", "base")
	repo, err := gitcmd.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	return dir, repo
}

func TestStagedRefusesWithoutRepoOrChanges(t *testing.T) {
	if _, err := Staged(context.Background(), gitcmd.Repo{Dir: t.TempDir()}); !errors.Is(err, ErrNoRepository) {
		t.Fatalf("no repo: %v", err)
	}
	_, repo := fixture(t)
	if _, err := Staged(context.Background(), repo); !errors.Is(err, ErrNothingStaged) {
		t.Fatalf("clean index: %v", err)
	}
}

func TestStagedReadsOnlyTheIndex(t *testing.T) {
	dir, repo := fixture(t)
	write(t, dir, "a.go", "package a\n\nfunc A() {}\n")
	run(t, dir, "add", "a.go")
	write(t, dir, "b.go", "package b\n")
	write(t, dir, "a.go", "package a\n\nfunc A() {}\n\nfunc Unstaged() {}\n")

	snap, err := Staged(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Files) != 1 || snap.Files[0].Path != "a.go" || snap.Files[0].Status != "M" {
		t.Fatalf("files = %+v", snap.Files)
	}
	if n := snap.Files[0].Insertions; n == nil || *n != 2 {
		t.Fatalf("insertions = %v", n)
	}
	if !strings.Contains(snap.Diff, "func A()") || strings.Contains(snap.Diff, "Unstaged") {
		t.Fatalf("diff = %q", snap.Diff)
	}
	if len(snap.Recent) != 1 || snap.Recent[0] != "base" || snap.Fingerprint == "" {
		t.Fatalf("recent = %v fp = %q", snap.Recent, snap.Fingerprint)
	}
}

func TestStagedKeepsSecretFileBytesOutOfTheDiff(t *testing.T) {
	dir, repo := fixture(t)
	write(t, dir, ".env", "TOKEN=hunter2-plain\n")
	write(t, dir, "k.pem", "-----BEGIN PRIVATE KEY-----\n")
	write(t, dir, "ok.go", "package ok\n")
	run(t, dir, "add", ".env", "k.pem", "ok.go")

	snap, err := Staged(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(snap.SensitiveFiles(), ","); got != ".env,k.pem" {
		t.Fatalf("sensitive = %q", got)
	}
	if strings.Contains(snap.Diff, "hunter2") || strings.Contains(snap.Diff, "PRIVATE KEY") || !strings.Contains(snap.Diff, "package ok") {
		t.Fatalf("diff = %q", snap.Diff)
	}
}

func TestStagedFlagsCredentialShapedAddedLine(t *testing.T) {
	dir, repo := fixture(t)
	write(t, dir, "conf.go", "package a\n\nconst cfg = `api_key = \"abcd1234efgh5678ijkl9012\"`\n")
	run(t, dir, "add", "conf.go")
	snap, err := Staged(context.Background(), repo)
	if err != nil || !snap.ContentSecrets || !snap.Warned() {
		t.Fatalf("snap = %+v err = %v", snap, err)
	}
}

func TestCommitRecordsOnlyTheIndex(t *testing.T) {
	dir, repo := fixture(t)
	write(t, dir, "new.go", "package n\n")
	run(t, dir, "add", "new.go")
	write(t, dir, "a.go", "package a // edited\n")
	snap, _ := Staged(context.Background(), repo)

	res, err := Commit(context.Background(), repo, "feat: add n\n\nbody", snap.Fingerprint, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Subject != "feat: add n" || len(res.Hash) < 40 {
		t.Fatalf("result = %+v", res)
	}
	if got := strings.TrimSpace(run(t, dir, "log", "-1", "--format=%B")); got != "feat: add n\n\nbody" {
		t.Fatalf("message = %q", got)
	}
	if got := strings.TrimSpace(run(t, dir, "status", "--porcelain")); got != "M a.go" {
		t.Fatalf("status after commit = %q", got)
	}
}

func TestCommitInEmptyRepository(t *testing.T) {
	isolate(t)
	dir := testenv.TempDir(t)
	run(t, dir, "init", "-q")
	write(t, dir, "a.go", "package a\n")
	run(t, dir, "add", "a.go")
	repo, _ := gitcmd.Open(context.Background(), dir)
	snap, err := Staged(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(context.Background(), repo, "chore: init", snap.Fingerprint, false); err != nil {
		t.Fatal(err)
	}
}

func TestCommitRefusesSecretsUntilAcknowledged(t *testing.T) {
	dir, repo := fixture(t)
	write(t, dir, ".env", "A=1\n")
	run(t, dir, "add", ".env")
	snap, _ := Staged(context.Background(), repo)

	_, err := Commit(context.Background(), repo, "chore: env", snap.Fingerprint, false)
	var se *SensitiveError
	if !errors.Is(err, ErrSensitiveStaged) || !errors.As(err, &se) || len(se.Files) != 1 || se.Files[0] != ".env" {
		t.Fatalf("err = %v", err)
	}
	if got := strings.TrimSpace(run(t, dir, "rev-list", "--count", "HEAD")); got != "1" {
		t.Fatalf("a refused commit recorded something: %s commits", got)
	}
	if _, err := Commit(context.Background(), repo, "chore: env", snap.Fingerprint, true); err != nil {
		t.Fatal(err)
	}
}

func TestCommitRefusesAChangedIndex(t *testing.T) {
	dir, repo := fixture(t)
	write(t, dir, "x.go", "package x\n")
	run(t, dir, "add", "x.go")
	snap, _ := Staged(context.Background(), repo)
	write(t, dir, "y.go", "package y\n")
	run(t, dir, "add", "y.go")

	for _, fp := range []string{snap.Fingerprint, ""} {
		if _, err := Commit(context.Background(), repo, "feat: x", fp, false); !errors.Is(err, ErrStagedChanged) {
			t.Fatalf("fingerprint %q: %v", fp, err)
		}
	}
}

func TestCommitRejectsBadMessages(t *testing.T) {
	dir, repo := fixture(t)
	write(t, dir, "x.go", "package x\n")
	run(t, dir, "add", "x.go")
	snap, _ := Staged(context.Background(), repo)
	for msg, want := range map[string]error{
		"  \n ":                                ErrEmptyMessage,
		"a\x00b":                               ErrMessageInvalid,
		strings.Repeat("a", MaxMessageBytes+1): ErrMessageTooLong,
	} {
		if _, err := Commit(context.Background(), repo, msg, snap.Fingerprint, false); !errors.Is(err, want) {
			t.Fatalf("%.10q: got %v want %v", msg, err, want)
		}
	}
}

func TestCommitNeedsAnIdentity(t *testing.T) {
	dir, repo := fixture(t)
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL"} {
		if v, ok := os.LookupEnv(k); ok {
			_ = v
			os.Unsetenv(k)
		}
	}
	run(t, dir, "config", "user.useConfigOnly", "true")
	write(t, dir, "x.go", "package x\n")
	run(t, dir, "add", "x.go")
	snap, _ := Staged(context.Background(), repo)
	if _, err := Commit(context.Background(), repo, "feat: x", snap.Fingerprint, false); !errors.Is(err, ErrIdentityMissing) {
		t.Fatalf("err = %v", err)
	}
}

func TestCommitRunsNoRepositoryConfiguredProgram(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("payload script is POSIX shell")
	}
	dir, repo := fixture(t)
	marker := filepath.Join(testenv.TempDir(t), "executed")
	payload := filepath.Join(testenv.TempDir(t), "payload.sh")
	script := "#!/bin/sh\necho ran >> '" + marker + "'\ncat >/dev/null\n"
	if err := os.WriteFile(payload, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, hook := range []string{"pre-commit", "commit-msg", "post-commit", "prepare-commit-msg"} {
		if err := os.WriteFile(filepath.Join(dir, ".git", "hooks", hook), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, dir, "x.go", "package x\n")
	run(t, dir, "add", "x.go")
	run(t, dir, "config", "core.fsmonitor", payload)
	run(t, dir, "config", "commit.gpgsign", "true")
	run(t, dir, "config", "gpg.program", payload)
	run(t, dir, "config", "core.hooksPath", filepath.Join(dir, ".git", "hooks"))

	snap, err := Staged(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Commit(context.Background(), repo, "feat: x", snap.Fingerprint, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a program named by repository configuration ran")
	}
	if sig := run(t, dir, "log", "-1", "--format=%G?"); strings.TrimSpace(sig) != "N" {
		t.Fatalf("commit was signed: %q", sig)
	}
}

const openSSHKey = "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAABG5vbmU=\n-----END OPENSSH PRIVATE KEY-----\n"

func TestPrivateKeyContentMakesAnyFileSensitive(t *testing.T) {
	dir, repo := fixture(t)
	write(t, dir, "id_ed25519", openSSHKey)
	write(t, dir, "notes.txt", "see below\n"+openSSHKey)
	write(t, dir, ".env.production", "DB_PASSWORD=hunter2-plain\n")
	write(t, dir, "ok.go", "package ok\n")
	run(t, dir, "add", "id_ed25519", "notes.txt", ".env.production", "ok.go")

	snap, err := Staged(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(snap.SensitiveFiles(), ","); got != ".env.production,id_ed25519,notes.txt" {
		t.Fatalf("sensitive = %q", got)
	}
	for _, leak := range []string{"b3BlbnNzaC1r", "PRIVATE KEY", "hunter2"} {
		if strings.Contains(snap.Diff, leak) {
			t.Fatalf("%q reached the diff meant for a model", leak)
		}
	}
	if !strings.Contains(snap.Diff, "package ok") {
		t.Fatal("the ordinary file was dropped too")
	}
	if _, err := Commit(context.Background(), repo, "chore: keys", snap.Fingerprint, false); !errors.Is(err, ErrSensitiveStaged) {
		t.Fatalf("an unacknowledged private key was committed: %v", err)
	}
}

func TestScanJudgesTheWholeDiffNotOnlyItsRetainedPart(t *testing.T) {
	dir, repo := fixture(t)
	write(t, dir, "a_big.txt", strings.Repeat("filler line of text\n", MaxDiffBytes/10))
	write(t, dir, "z_late.txt", openSSHKey)
	run(t, dir, "add", "a_big.txt", "z_late.txt")
	snap, err := Staged(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Truncated || len(snap.Diff) > MaxDiffBytes {
		t.Fatalf("truncated=%v len=%d", snap.Truncated, len(snap.Diff))
	}
	if got := snap.SensitiveFiles(); len(got) != 1 || got[0] != "z_late.txt" {
		t.Fatalf("sensitive = %v", got)
	}
}

func TestIndexMovedAfterTheCheckCannotEnterTheCommit(t *testing.T) {
	dir, repo := fixture(t)
	write(t, dir, "x.go", "package x\n")
	run(t, dir, "add", "x.go")
	snap, _ := Staged(context.Background(), repo)
	write(t, dir, ".env", "TOKEN=late\n")
	afterCheck = func() { run(t, dir, "add", ".env") }
	defer func() { afterCheck = nil }()

	if _, err := Commit(context.Background(), repo, "feat: x", snap.Fingerprint, false); err != nil {
		t.Fatal(err)
	}
	if files := run(t, dir, "show", "--name-only", "--format=", "HEAD"); strings.TrimSpace(files) != "x.go" {
		t.Fatalf("commit holds %q, want only the reviewed x.go", files)
	}
	if st := run(t, dir, "status", "--porcelain"); !strings.Contains(st, "A  .env") {
		t.Fatalf("the late file must stay staged, not be committed or lost: %q", st)
	}
}
