package gitcmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A Repo is resolved when the session opens; what the workspace gains
// afterwards must not change which repository, or whose config, host git reads.

func (f *repoFixture) open(dir string) Repo {
	f.t.Helper()
	repo, err := Open(f.ctx, dir)
	if err != nil {
		f.t.Fatalf("Open(%s): %v", dir, err)
	}
	return repo
}

func (f *repoFixture) repoOut(repo Repo, args ...string) (string, error) {
	f.t.Helper()
	out, err := repo.Command(f.ctx, args...).Output()
	return strings.TrimSpace(string(out)), err
}

// substituteConfig is a local config a later write points git at: a probe key
// to read the config's origin by, and a filter that records any run.
func (f *repoFixture) substituteConfig() string {
	return "[probe]\n\tx = substitute\n[filter \"evil\"]\n\tclean = " + f.payload() + "\n\tsmudge = " + f.payload() + "\n"
}

func (f *repoFixture) assertReadsOpenedRepository(repo Repo, wantGitDir string) {
	f.t.Helper()
	if got, err := f.repoOut(repo, "config", "--get", "probe.x"); err == nil || got != "" {
		f.t.Fatalf("config probe.x = %q (err %v), want the substitute config unread", got, err)
	}
	if got, err := f.repoOut(repo, "rev-parse", "--absolute-git-dir"); err != nil || filepath.Clean(got) != wantGitDir {
		f.t.Fatalf("git dir = %q (err %v), want %s", got, err, wantGitDir)
	}
	f.makeStatDirty("f.txt", "two\n")
	if _, err := f.repoOut(repo, "status", "--porcelain"); err != nil {
		f.t.Fatalf("status: %v", err)
	}
	if _, err := f.repoOut(repo, "diff", "HEAD"); err != nil {
		f.t.Fatalf("diff: %v", err)
	}
	f.assertNotExecuted()
}

func TestOpenResolvesLinkedWorktreeIdentity(t *testing.T) {
	f := newRepoFixture(t, "", map[string]string{"f.txt": "one\n"})
	linked := filepath.Join(t.TempDir(), "linked")
	f.plain("worktree", "add", "--quiet", linked)
	repo := f.open(linked)
	common := f.open(f.dir).GitDir
	real, err := filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatal(err)
	}
	if repo.CommonDir != common || repo.GitDir == common || repo.WorkTree != real {
		t.Fatalf("linked worktree resolved as %+v, want common dir %s", repo, common)
	}
	if _, err := Open(f.ctx, t.TempDir()); !errors.Is(err, ErrNotRepository) {
		t.Fatalf("Open(non-repo) = %v, want ErrNotRepository", err)
	}
	if err := (Repo{}).Command(f.ctx, "status").Run(); !errors.Is(err, ErrNotRepository) {
		t.Fatalf("unresolved Repo ran: %v, want ErrNotRepository", err)
	}
}

func TestOpenResolvesSymlinkedWorktreeIdentity(t *testing.T) {
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
			repo, direct := f.open(alias), f.open(dir)
			if repo.GitDir != direct.GitDir || repo.CommonDir != direct.CommonDir || repo.WorkTree != direct.WorkTree {
				t.Fatalf("alias identity = %+v, want %+v", repo, direct)
			}
			if (repo.GitDir != repo.CommonDir) != linked {
				t.Fatalf("linked=%v resolved as %+v", linked, repo)
			}
			if err := repo.Verify(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// A workspace that is a subdirectory of a repository gains its own .git.
func TestOpenedRepoIgnoresNestedRepositoryCreatedLater(t *testing.T) {
	f := newRepoFixture(t, "", map[string]string{"pkg/f.txt": "one\n", "f.txt": "one\n"})
	pkg := filepath.Join(f.dir, "pkg")
	repo := f.open(pkg)
	if top := f.open(f.dir); repo.GitDir != top.GitDir || repo.WorkTree != top.WorkTree {
		t.Fatalf("subdirectory resolved as %+v, want the enclosing repository %+v", repo, top)
	}
	f.plainIn(pkg, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(pkg, ".gitattributes"), []byte("*.txt filter=evil\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, ".git", "config"), []byte(f.substituteConfig()), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, _ := Command(f.ctx, pkg, "config", "--get", "probe.x").Output(); strings.TrimSpace(string(out)) != "substitute" {
		t.Fatalf("discovery from the workspace read %q, want the nested config (the case being guarded)", out)
	}
	f.assertReadsOpenedRepository(repo, repo.GitDir)
}

// A commondir file written into the git dir redirects discovery to another
// repository's config.
func TestOpenedRepoIgnoresCommondirWrittenLater(t *testing.T) {
	f := newRepoFixture(t, "", map[string]string{"f.txt": "one\n"})
	repo := f.open(f.dir)
	evil := filepath.Join(t.TempDir(), "evil")
	f.plain("clone", "--quiet", "--bare", f.dir, evil)
	if err := os.WriteFile(filepath.Join(evil, "config"), []byte("[core]\n\trepositoryformatversion = 0\n"+f.substituteConfig()), 0o644); err != nil {
		t.Fatal(err)
	}
	f.write(".gitattributes", "*.txt filter=evil\n")
	if err := os.WriteFile(filepath.Join(f.dir, ".git", "commondir"), []byte(evil+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, _ := Command(f.ctx, f.dir, "config", "--get", "probe.x").Output(); strings.TrimSpace(string(out)) != "substitute" {
		t.Fatalf("discovery read %q, want the commondir's config (the case being guarded)", out)
	}
	f.assertReadsOpenedRepository(repo, repo.GitDir)
}

// A damaged HEAD makes the git dir invalid; the workspace root dressed as an
// implicit bare repository must not stand in for it.
func TestOpenedRepoFailsClosedOnDamagedHead(t *testing.T) {
	f := newRepoFixture(t, "", map[string]string{"f.txt": "one\n"})
	repo := f.open(f.dir)
	for _, d := range []string{"objects", "refs"} {
		if err := os.MkdirAll(filepath.Join(f.dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.write("HEAD", "ref: refs/heads/main\n")
	f.write("config", "[core]\n\tbare = false\n\tworktree = "+f.dir+"\n"+f.substituteConfig())
	f.write(".gitattributes", "*.txt filter=evil\n")
	if err := os.WriteFile(filepath.Join(f.dir, ".git", "HEAD"), []byte("garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.makeStatDirty("f.txt", "two\n")
	for _, args := range [][]string{{"config", "--get", "probe.x"}, {"status", "--porcelain"}, {"diff", "HEAD"}} {
		if out, err := f.repoOut(repo, args...); err == nil || strings.Contains(out, "substitute") {
			t.Fatalf("%v = %q (err %v), want a failure naming no substitute", args, out, err)
		}
		if out, _ := f.run(args...); strings.Contains(out, "substitute") {
			t.Fatalf("discovery %v read the implicit bare repository: %q", args, out)
		}
	}
	f.assertNotExecuted()
}

// A git dir that belongs to another checkout makes host diffs read, and host
// writes land in, that checkout: Open refuses it as not a repository.
func TestOpenRefusesGitDirOfAnotherCheckout(t *testing.T) {
	other := newRepoFixture(t, "", map[string]string{"other.txt": "other\n"})
	t.Run("gitdir file", func(t *testing.T) {
		ws := t.TempDir()
		if err := os.WriteFile(filepath.Join(ws, ".git"), []byte("gitdir: "+filepath.Join(other.dir, ".git")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if out, _ := Command(other.ctx, ws, "ls-files").Output(); !strings.Contains(string(out), "other.txt") {
			t.Fatalf("discovery listed %q, want the other checkout (the case being guarded)", out)
		}
		if _, err := Open(other.ctx, ws); !errors.Is(err, ErrNotRepository) {
			t.Fatalf("Open = %v, want ErrNotRepository", err)
		}
	})
	t.Run("core.worktree", func(t *testing.T) {
		f := newRepoFixture(t, "", map[string]string{"f.txt": "one\n"})
		f.plain("config", "core.worktree", other.dir)
		if _, err := Open(f.ctx, f.dir); !errors.Is(err, ErrNotRepository) {
			t.Fatalf("Open = %v, want ErrNotRepository", err)
		}
	})
	t.Run("linked admin dir of another worktree", func(t *testing.T) {
		linked := filepath.Join(t.TempDir(), "linked")
		other.plain("worktree", "add", "--quiet", linked)
		ws := t.TempDir()
		admin := strings.TrimSpace(other.plainIn(linked, "rev-parse", "--absolute-git-dir"))
		if err := os.WriteFile(filepath.Join(ws, ".git"), []byte("gitdir: "+admin+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(other.ctx, ws); !errors.Is(err, ErrNotRepository) {
			t.Fatalf("Open = %v, want ErrNotRepository", err)
		}
	})
}

// A stored identity is held to Open's test when it is used again.
func TestVerifyRefusesStoredIdentityOfAnotherCheckout(t *testing.T) {
	f := newRepoFixture(t, "", map[string]string{"f.txt": "one\n"})
	other := newRepoFixture(t, "", map[string]string{"other.txt": "other\n"})
	repo := f.open(f.dir)
	if err := repo.Verify(); err != nil {
		t.Fatalf("Verify(opened) = %v", err)
	}
	forged := repo
	forged.GitDir = other.open(other.dir).GitDir
	forged.CommonDir = forged.GitDir
	if err := forged.Verify(); !errors.Is(err, ErrNotRepository) {
		t.Fatalf("Verify(forged) = %v, want ErrNotRepository", err)
	}
	if err := (Repo{}).Verify(); !errors.Is(err, ErrNotRepository) {
		t.Fatalf("Verify(zero) = %v, want ErrNotRepository", err)
	}
}
