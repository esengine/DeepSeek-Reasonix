package gitcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Each probe builds a repository whose own config names a marker script, runs
// the host invocation the product uses, and fails if the marker appears —
// covering config keys beyond the driver list (signatures, lazy fetch, submodules).

func (f *repoFixture) payloadExit() string {
	f.t.Helper()
	path := filepath.Join(f.t.TempDir(), "payload-exit.sh")
	script := "#!/bin/sh\necho ran >> '" + f.marker + "'\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		f.t.Fatal(err)
	}
	return path
}

// worktree merge-back (merge_autocommit.go, merge_commit.go) and studio's
// candidate.go run commit-tree; commit.gpgSign from .git/config makes it sign.
func TestReviewGapCommitTreeHonoursRepoGpgSign(t *testing.T) {
	f := newRepoFixture(t, "", map[string]string{"f.txt": "one\n"})
	p := f.payloadExit()
	f.appendConfig("config", "[commit]\n\tgpgSign = true\n[gpg]\n\tprogram = "+p+"\n")
	tree := strings.TrimSpace(f.plain("rev-parse", "HEAD^{tree}"))
	_, _ = f.run("-c", "user.name=Reasonix", "-c", "user.email=r@local", "commit-tree", tree, "-p", "HEAD", "-m", "x")
	f.assertNotExecuted()
}

// merge_commit.go runs `merge --no-ff --no-commit --no-verify <worktreeHead>`;
// merge.verifySignatures from .git/config verifies the merged tip.
func TestReviewGapMergeHonoursRepoVerifySignatures(t *testing.T) {
	f := newRepoFixture(t, "", map[string]string{"f.txt": "one\n"})
	f.plain("checkout", "--quiet", "-b", "side")
	f.write("g.txt", "g\n")
	f.plain("add", "-A")
	f.plain("commit", "--quiet", "-m", "side")
	signCommitHeader(f)
	f.plain("checkout", "--quiet", "main")
	p := f.payloadExit()
	f.appendConfig("config", "[merge]\n\tverifySignatures = true\n[gpg]\n\tprogram = "+p+"\n")
	_, _ = f.run("-c", "user.name=Reasonix", "-c", "user.email=r@local", "merge", "--no-ff", "--no-commit", "--no-verify", "side")
	f.assertNotExecuted()
}

// A promisor remote in .git/config turns a missing blob into a lazy fetch, a
// network command run inside the repository with its transport config.
func TestReviewGapLazyFetchRunsRepoUploadPack(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(f *repoFixture)
	}{
		{"diff HEAD", func(f *repoFixture) { _, _ = f.run("diff", "HEAD", "--", "f.txt") }},
		{"diff --numstat HEAD", func(f *repoFixture) { _, _ = f.run("diff", "--numstat", "HEAD", "--") }},
		{"worktree add", func(f *repoFixture) {
			_, _ = f.run("worktree", "add", "--detach", filepath.Join(f.t.TempDir(), "wt"), "HEAD")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRepoFixture(t, "", map[string]string{"f.txt": "one\n"})
			blob := strings.TrimSpace(f.plain("rev-parse", "HEAD:f.txt"))
			if err := os.Remove(filepath.Join(f.dir, ".git", "objects", blob[:2], blob[2:])); err != nil {
				t.Fatal(err)
			}
			p := f.payloadExit()
			f.appendConfig("config", "[core]\n\trepositoryformatversion = 1\n[extensions]\n\tpartialClone = origin\n"+
				"[remote \"origin\"]\n\turl = "+t.TempDir()+"\n\tpromisor = true\n\tuploadpack = "+p+"\n")
			f.write("f.txt", "two\n")
			tc.run(f)
			f.assertNotExecuted()
		})
	}
}

// desktop workspace_git_branches.go runs `checkout <branch>`; submodule.recurse
// in .git/config makes it check out the submodule, whose own config is not
// listed.
func TestReviewGapCheckoutRecursesIntoSubmoduleDrivers(t *testing.T) {
	sub := newRepoFixture(t, "s.txt filter=pwn\n", map[string]string{"s.txt": "v1\n"})
	f := newRepoFixture(t, "", map[string]string{"top.txt": "top\n"})
	f.plain("-c", "protocol.file.allow=always", "submodule", "--quiet", "add", sub.dir, "sm")
	f.plain("commit", "--quiet", "-m", "add submodule")
	f.plain("checkout", "--quiet", "-b", "other")
	sub.write("s.txt", "v2\n")
	sub.plain("commit", "--quiet", "-am", "v2")
	f.plainIn(filepath.Join(f.dir, "sm"), "-c", "protocol.file.allow=always", "fetch", "--quiet")
	f.plainIn(filepath.Join(f.dir, "sm"), "checkout", "--quiet", "origin/main")
	f.plain("commit", "--quiet", "-am", "bump")
	f.plain("checkout", "--quiet", "main")
	f.plain("submodule", "--quiet", "update")
	f.appendConfig("modules/sm/config", "[filter \"pwn\"]\n\tsmudge = "+f.payload()+"\n\tclean = "+f.payload()+"\n")
	f.appendConfig("config", "[submodule]\n\trecurse = true\n")
	_, _ = f.run("checkout", "other")
	f.assertNotExecuted()
}

// diff.submodule=diff makes the superproject's diff start `git diff` inside
// the submodule, where that submodule's diff settings apply.
func TestReviewGapDiffSubmoduleInlineDiff(t *testing.T) {
	sub := newRepoFixture(t, "s.txt diff=tc\n", map[string]string{"s.txt": "v1\n"})
	f := newRepoFixture(t, "", map[string]string{"top.txt": "top\n"})
	f.plain("-c", "protocol.file.allow=always", "submodule", "--quiet", "add", sub.dir, "sm")
	f.plain("commit", "--quiet", "-m", "add submodule")
	sub.write("s.txt", "v2\n")
	sub.plain("commit", "--quiet", "-am", "v2")
	f.plainIn(filepath.Join(f.dir, "sm"), "-c", "protocol.file.allow=always", "fetch", "--quiet")
	f.plainIn(filepath.Join(f.dir, "sm"), "checkout", "--quiet", "origin/main")
	p := f.payload()
	f.appendConfig("modules/sm/config", "[diff]\n\texternal = "+p+"\n[diff \"tc\"]\n\ttextconv = "+p+"\n")
	f.appendConfig("config", "[diff]\n\tsubmodule = diff\n[status]\n\tsubmoduleSummary = true\n")
	out, _ := f.run("diff", "HEAD")
	t.Logf("diff output: %q", out)
	_, _ = f.run("diff", "--numstat", "HEAD", "--")
	_, _ = f.run("status", "--porcelain=v1")
	f.assertNotExecuted()
}

// Plugin install runs `git clone <github url> <tmp>` with dir "" (process cwd),
// not Detached. Probe whether clone reads the cwd repository's url/protocol.
func TestReviewGapPluginCloneFromRepositoryCwd(t *testing.T) {
	f := newRepoFixture(t, "", map[string]string{"f.txt": "one\n"})
	p := f.payloadExit()
	f.appendConfig("config", "[url \"ext::"+p+" \"]\n\tinsteadOf = https://github.com/\n[protocol \"ext\"]\n\tallow = always\n"+
		"[credential]\n\thelper = !"+p+"\n[core]\n\tsshCommand = "+p+"\n\taskPass = "+p+"\n")
	t.Chdir(f.dir)
	cmd := CommandWithConfig(f.ctx, "", []string{"core.autocrlf=false"}, "clone", "--depth=1", "https://github.com/example/none.git", filepath.Join(t.TempDir(), "c"))
	cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0")
	_, _ = cmd.CombinedOutput()
	f.assertNotExecuted()
}

func (f *repoFixture) plainIn(dir string, args ...string) string {
	f.t.Helper()
	old := f.dir
	f.dir = dir
	defer func() { f.dir = old }()
	return f.plain(args...)
}

func TestReviewGapSubmoduleSplit(t *testing.T) {
	for _, tc := range []struct {
		name, subcfg, supercfg string
		args                   []string
	}{
		{"diff external via diff.submodule=diff", "[diff]\n\texternal = %P\n", "[diff]\n\tsubmodule = diff\n", []string{"diff", "HEAD"}},
		{"textconv via diff.submodule=diff", "[diff \"tc\"]\n\ttextconv = %P\n", "[diff]\n\tsubmodule = diff\n", []string{"diff", "HEAD"}},
		{"status submoduleSummary gpg", "[log]\n\tshowSignature = true\n[gpg]\n\tprogram = %P\n", "[status]\n\tsubmoduleSummary = true\n", []string{"status", "--porcelain=v1"}},
		{"status submoduleSummary plain status", "[diff]\n\texternal = %P\n[diff \"tc\"]\n\ttextconv = %P\n", "[status]\n\tsubmoduleSummary = true\n", []string{"status"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sub := newRepoFixture(t, "s.txt diff=tc\n", map[string]string{"s.txt": "v1\n"})
			f := newRepoFixture(t, "", map[string]string{"top.txt": "top\n"})
			f.plain("-c", "protocol.file.allow=always", "submodule", "--quiet", "add", sub.dir, "sm")
			f.plain("commit", "--quiet", "-m", "add submodule")
			sub.write("s.txt", "v2\n")
			sub.plain("commit", "--quiet", "-am", "v2")
			f.plainIn(filepath.Join(f.dir, "sm"), "-c", "protocol.file.allow=always", "fetch", "--quiet")
			f.plainIn(filepath.Join(f.dir, "sm"), "checkout", "--quiet", "origin/main")
			p := f.payload()
			f.appendConfig("modules/sm/config", strings.ReplaceAll(tc.subcfg, "%P", p))
			f.appendConfig("config", tc.supercfg)
			out, _ := f.run(tc.args...)
			t.Logf("out: %q", out)
			f.assertNotExecuted()
		})
	}
}
