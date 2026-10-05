package gitcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A nested repository registered as a gitlink carries its own local filter;
// staging the superproject must neither run it nor lose the rest of the tree.
func TestStageAllDoesNotEnterNestedRepositories(t *testing.T) {
	f := newRepoFixture(t, "", map[string]string{"x.txt": "one\n"})
	sub := filepath.Join(f.dir, "sub")
	f.plainIn(f.dir, "init", "--quiet", "sub")
	f.write("sub/f.txt", "a\n")
	f.write("sub/.gitattributes", "f.txt filter=evil\n")
	f.plainIn(sub, "add", "-A")
	f.plainIn(sub, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "--quiet", "-m", "s")
	f.plainIn(sub, "config", "filter.evil.clean", f.payload())
	gitlink := strings.TrimSpace(f.plainIn(sub, "rev-parse", "HEAD"))
	f.plain("update-index", "--add", "--cacheinfo", "160000,"+gitlink+",sub")
	f.plain("commit", "--quiet", "-m", "sub")
	f.makeStatDirty("sub/f.txt", "b\n")
	f.plainIn(f.dir, "init", "--quiet", "nest")
	f.write("nest/q.txt", "q\n")
	f.makeStatDirty("x.txt", "two\n")
	f.write("y.txt", "new\n")

	repo := f.open(f.dir)
	index := filepath.Join(t.TempDir(), "index")
	raw, err := os.ReadFile(filepath.Join(f.dir, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(index, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	env := "GIT_INDEX_FILE=" + index
	if err := repo.StageAll(f.ctx, env); err != nil {
		t.Fatalf("StageAll: %v", err)
	}
	f.assertNotExecuted()
	cmd := repo.Command(f.ctx, "ls-files", "-s")
	cmd.Env = append(cmd.Env, env)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{"160000 " + gitlink + " 0\tsub\n", "\tx.txt\n", "\ty.txt\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("staged index:\n%s\nwant %q", got, want)
		}
	}
	if strings.Contains(got, "nest") {
		t.Fatalf("staged index:\n%s\nwant the untracked nested repository left out", got)
	}
	x := strings.TrimSpace(f.plain("hash-object", "x.txt"))
	if !strings.Contains(got, x+" 0\tx.txt") {
		t.Fatalf("staged index:\n%s\nwant x.txt at its working-tree content %s", got, x)
	}
}

// A user's own submodule.recurse=true must not carry host checkouts and resets
// into a submodule, where that submodule's config would name the filter.
func TestUserRecurseDoesNotCarryHostCheckoutIntoSubmodules(t *testing.T) {
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
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, []byte("[submodule]\n\trecurse = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	repo := f.open(f.dir)
	for _, args := range [][]string{{"checkout", "--quiet", "other"}, {"reset", "--quiet", "--hard", "main"}} {
		if out, err := repo.Command(f.ctx, args...).CombinedOutput(); err != nil {
			t.Fatalf("host %v: %v: %s", args, err, out)
		}
		f.assertNotExecuted()
	}
}
