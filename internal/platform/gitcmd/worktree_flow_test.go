package gitcmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Worktree creation adds with --no-checkout and populates with a reset run
// inside the new worktree, where the driver listing sees an includeIf keyed on
// that worktree's gitdir or branch.
func TestNoCheckoutThenResetSeesConditionalInclude(t *testing.T) {
	for _, tc := range []struct {
		name, cond string
		add        func(dst string) []string
	}{
		{"gitdir detached", "gitdir:**/worktrees/**", func(dst string) []string {
			return []string{"worktree", "add", "--no-checkout", "--detach", dst, "HEAD"}
		}},
		{"onbranch -b", "onbranch:reasonix/**", func(dst string) []string {
			return []string{"worktree", "add", "--no-checkout", "-b", "reasonix/delivery-x", dst, "HEAD"}
		}},
		{"worktree-scope include", "gitdir:**/worktrees/**", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRepoFixture(t, "f.txt filter=pwn\n", map[string]string{"f.txt": "hello\n"})
			p := f.payload()
			evil := filepath.Join(t.TempDir(), "evil.cfg")
			if err := os.WriteFile(evil, []byte("[filter \"pwn\"]\n\tsmudge = "+p+"\n\tclean = "+p+"\n\tprocess = "+p+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			f.appendConfig("config", "[includeIf \""+tc.cond+"\"]\n\tpath = "+evil+"\n")
			dst := filepath.Join(t.TempDir(), "linked")
			add := tc.add
			if add == nil {
				add = func(dst string) []string {
					return []string{"worktree", "add", "--no-checkout", "--detach", dst, "HEAD"}
				}
			}
			f.mustRun(add(dst)...)
			head := strings.TrimSpace(f.plain("rev-parse", "HEAD"))
			if out, err := Command(f.ctx, dst, "reset", "--hard", head).CombinedOutput(); err != nil {
				t.Fatalf("reset: %v %s", err, out)
			}
			if data, _ := os.ReadFile(filepath.Join(dst, "f.txt")); string(data) != "hello\n" {
				t.Fatalf("f.txt = %q", data)
			}
			if out, err := Command(f.ctx, dst, "status", "--porcelain=v1").CombinedOutput(); err != nil || strings.TrimSpace(string(out)) != "" {
				t.Fatalf("status after reset = %q %v", out, err)
			}
			f.assertNotExecuted()
		})
	}
}

// Removal runs a hardened status inside the linked worktree, then removes with
// --force so git starts no status of its own there.
func TestWorktreeRemoveForceSkipsLinkedConfig(t *testing.T) {
	f := newRepoFixture(t, "f.txt filter=pwn\n", map[string]string{"f.txt": "hello\n"})
	f.plain("config", "extensions.worktreeConfig", "true")
	dst := filepath.Join(t.TempDir(), "linked")
	f.plain("worktree", "add", "--quiet", "--detach", dst, "HEAD")
	gitDir := strings.TrimSpace(f.plainIn(dst, "rev-parse", "--absolute-git-dir"))
	if err := os.WriteFile(filepath.Join(gitDir, "config.worktree"), []byte("[filter \"pwn\"]\n\tclean = "+f.payload()+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "f.txt"), []byte("hellx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// the pre-remove clean check runs with dir=linked worktree
	_, _ = Command(f.ctx, dst, "status", "--porcelain=v1", "--untracked-files=all", "--ignored").CombinedOutput()
	_, _ = f.run("worktree", "remove", "--force", dst)
	f.assertNotExecuted()
}

// submodule.recurse is overridden only where the repository sets it, so a
// user's own global value is left alone elsewhere.
func TestSubmoduleRecursePinnedOnlyWhenRepositorySetsIt(t *testing.T) {
	f := newRepoFixture(t, "", map[string]string{"f.txt": "hello\n"})
	if args := Command(f.ctx, f.dir, "status").Args; slices.Contains(args, "submodule.recurse=false") {
		t.Fatalf("args = %v, want no submodule.recurse override for a repository that does not set it", args)
	}
	f.appendConfig("config", "[submodule]\n\trecurse = true\n")
	if args := Command(f.ctx, f.dir, "status").Args; !slices.Contains(args, "submodule.recurse=false") {
		t.Fatalf("args = %v, want submodule.recurse=false when the repository sets it", args)
	}
}

// A partial clone's absent objects are reported by git's own listing, so a
// caller can tell "not fetched" from any other failure.
func TestObjectsMissingReportsPartialCloneGaps(t *testing.T) {
	src := newRepoFixture(t, "", map[string]string{"a.txt": "1\n"})
	src.write("a.txt", "2\n")
	src.plain("commit", "--quiet", "-am", "two")
	src.write("a.txt", "3\n")
	src.plain("commit", "--quiet", "-am", "three")
	src.plain("config", "uploadpack.allowFilter", "true")
	clone := filepath.Join(t.TempDir(), "partial")
	if out, err := exec.CommandContext(src.ctx, "git", "clone", "--quiet", "--filter=blob:none", "file://"+src.dir, clone).CombinedOutput(); err != nil {
		t.Fatalf("partial clone: %v: %s", err, out)
	}
	repo, err := Open(src.ctx, clone)
	if err != nil {
		t.Fatal(err)
	}
	if repo.ObjectsMissing(src.ctx, "HEAD") {
		t.Fatal("HEAD's objects are checked out, want none missing")
	}
	if !repo.ObjectsMissing(src.ctx, "HEAD~1", "HEAD~1^") {
		t.Fatal("historical blobs were filtered out, want them reported missing")
	}
	if _, err := Command(src.ctx, clone, "show", "HEAD~1").Output(); err == nil {
		t.Fatal("show of an unfetched commit succeeded, want it to fail without fetching")
	}
}
