//go:build !windows

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func requireConfinedGit(t *testing.T) {
	t.Helper()
	if !Available() {
		t.Skip("no usable OS sandbox backend")
	}
	requireGit(t)
}

// runConfined runs command under the real OS sandbox for spec, in dir.
func runConfined(t *testing.T, spec Spec, dir, command string) (string, error) {
	t.Helper()
	argv, wrapped := Command(spec, Shell{Kind: ShellBash, Path: "/bin/bash"}, command)
	if !wrapped {
		t.Fatal("expected a sandbox-wrapped command")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = isolatedGitEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func snapshotFiles(t *testing.T, paths ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			out[p] = "<absent>"
			continue
		}
		out[p] = string(data)
	}
	return out
}

func TestSandboxDeniesGitMetadataWrites(t *testing.T) {
	requireConfinedGit(t)
	root := realTempDir(t)
	ws := filepath.Join(root, "ws")
	newRepo(t, ws)
	lib := filepath.Join(root, "lib")
	newRepo(t, lib)
	gitIn(t, ws, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "vendor/lib")
	gitIn(t, ws, "commit", "-q", "-m", "submodule")
	gitIn(t, ws, "worktree", "add", "-q", "-b", "linked", filepath.Join(ws, ".wt", "linked"))
	git := filepath.Join(ws, ".git")
	module := filepath.Join(git, "modules", "vendor", "lib")
	wtMeta := filepath.Join(git, "worktrees", "linked")
	spec := Spec{Mode: "enforce", WriteRoots: []string{ws}}

	watched := []string{
		filepath.Join(git, "config"), filepath.Join(git, "config.worktree"), filepath.Join(git, "hooks", "pre-commit"),
		filepath.Join(wtMeta, "config"), filepath.Join(wtMeta, "config.worktree"),
		filepath.Join(module, "config"), filepath.Join(module, "hooks", "post-checkout"), filepath.Join(ws, "vendor", "lib", ".git"),
	}
	before := snapshotFiles(t, watched...)
	commands := []string{
		"git config core.fsmonitor /bin/sh",
		"printf '[core]\\n\\tfsmonitor = /bin/sh\\n' >> .git/config",
		"printf '#!/bin/sh\\n' > .git/hooks/pre-commit",
		"mv .git/hooks .git/hooks.moved",
		"cp .git/config /tmp/x-$$ && mv -f /tmp/x-$$ .git/config",
		"ln -s /tmp .git/hooks2 && mv -f .git/hooks2 .git/hooks",
		"printf '[core]\\n' >> .git/modules/vendor/lib/config",
		"printf '#!/bin/sh\\n' > .git/modules/vendor/lib/hooks/post-checkout",
		"mv .git .git.moved",
		"rm -rf .git",
	}
	// Seatbelt matches paths, so it also refuses creating an absent file;
	// bubblewrap can only mount over a path that exists.
	if runtime.GOOS == "darwin" {
		commands = append([]string{
			"printf '[core]\\n\\tfsmonitor = /bin/sh\\n' > .git/config.worktree",
			"printf '[core]\\n' > .git/worktrees/linked/config",
			"printf '[core]\\n' > .git/worktrees/linked/config.worktree",
		}, commands...)
	}
	for _, command := range commands {
		if out, err := runConfined(t, spec, ws, command); err == nil {
			t.Errorf("%q must be denied, got success: %s", command, out)
		}
	}
	if after := snapshotFiles(t, watched...); !mapsEqual(before, after) {
		t.Fatalf("protected git metadata changed:\nbefore %v\nafter  %v", before, after)
	}
	if info, err := os.Lstat(git); err != nil || !info.IsDir() {
		t.Fatalf("the .git entry must survive rm -rf and mv: %v", err)
	}
}

func TestSandboxDeniesRetargetingGitPointers(t *testing.T) {
	requireConfinedGit(t)
	root := realTempDir(t)
	main := filepath.Join(root, "main")
	newRepo(t, main)
	linked := filepath.Join(root, "linked")
	gitIn(t, main, "worktree", "add", "-q", "-b", "linked", linked)
	sep := filepath.Join(root, "sep")
	store := filepath.Join(root, "store.git")
	if err := os.Mkdir(sep, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, sep, "init", "-q", "--separate-git-dir", store)
	spec := Spec{Mode: "enforce", WriteRoots: []string{linked, sep, root}}

	for _, tc := range []struct{ dir, command string }{
		{linked, "printf 'gitdir: %s\\n' " + root + "/evil > .git"},
		{linked, "rm .git"},
		{linked, "printf '[core]\\n' >> " + filepath.Join(main, ".git", "config")},
		{linked, "printf '/tmp\\n' > " + filepath.Join(main, ".git", "worktrees", "linked", "commondir")},
		{sep, "printf 'gitdir: /tmp\\n' > .git"},
		{sep, "printf '[core]\\n' >> " + filepath.Join(store, "config")},
		{sep, "mv " + store + " " + store + ".moved && mkdir -p " + store},
		{sep, "printf '#!/bin/sh\\n' > " + filepath.Join(store, "hooks", "pre-commit")},
	} {
		if out, err := runConfined(t, spec, tc.dir, tc.command); err == nil {
			t.Errorf("in %s, %q must be denied, got success: %s", tc.dir, tc.command, out)
		}
	}
	if out, err := runConfined(t, spec, linked, "git status --porcelain && git -C "+sep+" status --porcelain"); err != nil {
		t.Fatalf("repositories must still resolve after the denied writes: %v: %s", err, out)
	}
}

// Ordinary git work writes objects, refs, index, logs and lock files under
// .git, never its config or hooks, so it keeps working confined.
func TestSandboxKeepsOrdinaryGitWorking(t *testing.T) {
	requireConfinedGit(t)
	ws := filepath.Join(realTempDir(t), "ws")
	newRepo(t, ws)
	spec := Spec{Mode: "enforce", WriteRoots: []string{ws}}
	for _, command := range []string{
		"echo one > a.txt && git add a.txt && git commit -q -m one",
		"git branch topic && git checkout -q -b feature",
		"echo two >> a.txt && git commit -q -am two",
		"git checkout -q main && git merge -q --no-edit feature",
		"git checkout -q topic && echo t > t.txt && git add t.txt && git commit -q -m t && git rebase -q main",
		"echo wip >> a.txt && git stash -q && git stash pop -q && git checkout -q -- a.txt",
		"git tag -a v1 -m v1 && git tag light",
		"git branch -D feature",
		"git worktree add -q -b side .wt/side && git worktree remove .wt/side",
		"git cherry-pick --no-edit main~1 || git cherry-pick --abort",
		"git gc -q",
		"mkdir fresh && cd fresh && git init -q && echo f > f && git add f && git commit -q -m fresh",
		"touch . && chmod 755 . && touch .git",
	} {
		if out, err := runConfined(t, spec, ws, command); err != nil {
			t.Fatalf("%q must keep working confined: %v: %s", command, err, out)
		}
	}
}

// These agent git operations write repository config or hooks and so fail
// confined. The error names the protected path, which is what lets the host
// attribute the denial.
func TestSandboxGitOperationsThatWriteMetadata(t *testing.T) {
	requireConfinedGit(t)
	ws := filepath.Join(realTempDir(t), "ws")
	newRepo(t, ws)
	gitIn(t, ws, "branch", "old")
	gitIn(t, ws, "branch", "topic")
	lib := filepath.Join(filepath.Dir(ws), "lib")
	newRepo(t, lib)
	gitIn(t, ws, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "vendor/lib")
	gitIn(t, ws, "commit", "-q", "-m", "submodule")
	gitIn(t, ws, "submodule", "deinit", "-q", "--all")
	if err := os.RemoveAll(filepath.Join(ws, ".git", "modules")); err != nil {
		t.Fatal(err)
	}
	spec := Spec{Mode: "enforce", WriteRoots: []string{ws}}
	for _, command := range []string{
		"git config user.name Someone",
		"git config --unset core.bare",
		"git remote add origin https://example.invalid/repo.git",
		"git branch -m old renamed",
		"git config core.hooksPath .githooks",
		"printf '#!/bin/sh\\n' > .git/hooks/pre-commit && chmod +x .git/hooks/pre-commit",
		"git -c protocol.file.allow=always submodule update --init",
	} {
		out, err := runConfined(t, spec, ws, command)
		if err == nil {
			t.Errorf("%q must fail confined: %s", command, out)
			continue
		}
		if !strings.Contains(out, ".git/config") && !strings.Contains(out, ".git/hooks/") {
			t.Errorf("%q failure must name the protected path: %s", command, out)
		}
	}
	// git reports the refused write but exits 0 and claims the upstream is set.
	out, err := runConfined(t, spec, ws, "git branch --set-upstream-to=main topic")
	if err != nil || !strings.Contains(out, ".git/config") {
		t.Fatalf("set-upstream-to: %v: %s", err, out)
	}
	if out, err := runConfined(t, spec, ws, "git config branch.topic.remote"); err == nil {
		t.Fatalf("upstream must not have been recorded: %s", out)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// Submodule gitdirs are covered by pattern on macOS, so one created or moved
// after the rules were written is covered too.
func TestSeatbeltCoversSubmoduleGitDirsByPattern(t *testing.T) {
	requireConfinedGit(t)
	if runtime.GOOS != "darwin" {
		t.Skip("patterns are the Seatbelt backend's")
	}
	ws := filepath.Join(realTempDir(t), "ws")
	newRepo(t, ws)
	if err := os.MkdirAll(filepath.Join(ws, ".git", "modules", "old", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".git", "modules", "old", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := Spec{Mode: "enforce", WriteRoots: []string{ws}}
	for _, command := range []string{
		"mkdir -p .git/modules/new/deeper && printf '[core]\\n' > .git/modules/new/deeper/config",
		"mkdir -p .git/modules/new && printf '#!/bin/sh\\n' > .git/modules/new/hooks",
		"printf '#!/bin/sh\\n' > .git/modules/old/hooks/post-checkout",
		"mv .git/modules/old .git/modules/moved",
		"ln -s " + ws + " .git/modules/planted",
		"printf '[core]\\n' > .git/worktrees/x/config.worktree || (mkdir -p .git/worktrees/x && printf '[core]\\n' > .git/worktrees/x/config.worktree)",
	} {
		if out, err := runConfined(t, spec, ws, command); err == nil {
			t.Errorf("%q must be denied, got success: %s", command, out)
		}
	}
	if out, err := runConfined(t, spec, ws, "mkdir -p .git/modules/new/objects && echo x > .git/modules/new/objects/o && rm .git/modules/new/objects/o"); err != nil {
		t.Fatalf("other submodule gitdir contents stay writable: %v: %s", err, out)
	}
}

// The reviewer's case: a `.git` file reaching its gitdir through a symlink that
// lives in the workspace. Swapping the symlink must be refused.
func TestSeatbeltDeniesSwappingASymlinkOnThePointerPath(t *testing.T) {
	requireConfinedGit(t)
	if runtime.GOOS != "darwin" {
		t.Skip("a mount cannot pin a symlink; the host pins GIT_DIR instead")
	}
	base := realTempDir(t)
	root := filepath.Join(base, "ws")
	code := filepath.Join(root, "code")
	store := filepath.Join(root, "store")
	if err := os.MkdirAll(code, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, code, "init", "-q", "-b", "main", "--separate-git-dir", store)
	if err := os.Symlink("store", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(code, ".git"), []byte("gitdir: ../link\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := Spec{Mode: "enforce", WriteRoots: []string{code, root}}
	out, err := runConfined(t, spec, code, "cd .. && mkdir evil && cp -R store/. evil/ && printf '[probe]\\n\\tx = 1\\n' >> evil/config && rm link && ln -s evil link")
	if err == nil {
		t.Fatalf("swapping the pointer's symlink must be denied: %s", out)
	}
	if target, err := os.Readlink(filepath.Join(root, "link")); err != nil || target != "store" {
		t.Fatalf("link changed: %q %v", target, err)
	}
}

// Rules must not grow with the number of submodule gitdirs: a repository, or a
// confined command planting HEAD files, would otherwise overflow the argument
// list and break every command.
func TestSandboxRuleSizeIsBoundedBySubmoduleCount(t *testing.T) {
	requireConfinedGit(t)
	size := func(ws string) int {
		argv, _ := Command(Spec{Mode: "enforce", WriteRoots: []string{ws}}, Shell{Kind: ShellBash, Path: "/bin/bash"}, "true")
		n := 0
		for _, a := range argv {
			n += len(a)
		}
		return n
	}
	ws := filepath.Join(realTempDir(t), "ws")
	newRepo(t, ws)
	plant := func(from, to int) {
		for i := from; i < to; i++ {
			m := filepath.Join(ws, ".git", "modules", fmt.Sprintf("g%d", i%20), fmt.Sprintf("m%d", i))
			if err := os.MkdirAll(filepath.Join(m, "hooks"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(m, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(m, "config"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	plant(0, 200)
	atLimit := size(ws)
	plant(200, 2000)
	if after := size(ws); after > atLimit || after > 256<<10 {
		t.Fatalf("sandbox arguments must stop growing past the limit: %d bytes at 200 gitdirs, %d at 2000", atLimit, after)
	}
	start := time.Now()
	if out, err := runConfined(t, Spec{Mode: "enforce", WriteRoots: []string{ws}}, ws, "echo ok > f && git add f && git commit -q -m f"); err != nil {
		t.Fatalf("commands keep running: %v: %s", err, out)
	}
	t.Logf("confined git commit with 2000 submodule gitdirs took %v", time.Since(start))
	if out, err := runConfined(t, Spec{Mode: "enforce", WriteRoots: []string{ws}}, ws, "printf '[core]\\n' >> .git/modules/g1/m1/config"); err == nil {
		t.Fatalf("a planted submodule config stays protected: %s", out)
	}
}

// The host adds its own linked worktrees under the workspace's .git/worktrees.
// Rewriting such an entry's commondir would point the host's git at a config
// the sandbox wrote, so existing entries' commondir stays protected.
func TestSandboxProtectsHostLinkedWorktreeCommondir(t *testing.T) {
	requireConfinedGit(t)
	base := realTempDir(t)
	ws := filepath.Join(base, "ws")
	newRepo(t, ws)
	for i, linked := range []string{filepath.Join(base, "managed", "cand"), filepath.Join(ws, ".reasonix", "wt", "cand2")} {
		gitIn(t, ws, "worktree", "add", "-q", "--detach", linked)
		id := filepath.Base(linked)
		evil := filepath.Join(ws, fmt.Sprintf("evil%d", i))
		command := "mkdir -p " + evil + " && cp -R .git/. " + evil + "/ && printf '[probe]\\n\\tx = 1\\n' >> " + evil + "/config && printf '" + evil + "\\n' > .git/worktrees/" + id + "/commondir"
		if out, err := runConfined(t, Spec{Mode: "enforce", WriteRoots: []string{ws}}, ws, command); err == nil {
			t.Fatalf("rewriting %s's commondir must be denied: %s", id, out)
		}
		cmd := exec.Command("git", "-C", linked, "config", "--get", "probe.x")
		cmd.Env = isolatedGitEnv()
		if out, _ := cmd.Output(); strings.TrimSpace(string(out)) != "" {
			t.Fatalf("host git in %s reads a sandbox-written config", linked)
		}
	}
	if out, err := runConfined(t, Spec{Mode: "enforce", WriteRoots: []string{ws}}, ws, "git worktree add -q -b fresh .wt/fresh && git worktree remove .wt/fresh"); err != nil {
		t.Fatalf("new worktrees stay creatable: %v: %s", err, out)
	}
}

// Refs and reflogs inside a submodule gitdir are not config: a branch or tag
// named config or hooks is ordinary git work.
func TestSandboxLeavesSubmoduleRefsNamedLikeMetadataWritable(t *testing.T) {
	requireConfinedGit(t)
	base := realTempDir(t)
	ws := filepath.Join(base, "ws")
	newRepo(t, ws)
	lib := filepath.Join(base, "lib")
	newRepo(t, lib)
	gitIn(t, ws, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "vendor/lib")
	gitIn(t, ws, "commit", "-q", "-m", "submodule")
	spec := Spec{Mode: "enforce", WriteRoots: []string{ws}}
	for _, command := range []string{
		"git -C vendor/lib branch config",
		"git -C vendor/lib branch hooks",
		"git -C vendor/lib branch feature/hooks/x",
		"git -C vendor/lib tag config",
		"git -C vendor/lib branch feature/y && git -C vendor/lib branch -D feature/y",
		"git -C vendor/lib checkout -q -b topic && git -C vendor/lib commit -q --allow-empty -m t && git -C vendor/lib checkout -q -",
		"git -C vendor/lib gc -q --prune=now",
	} {
		if out, err := runConfined(t, spec, ws, command); err != nil {
			t.Errorf("%q must keep working confined: %v: %s", command, err, out)
		}
	}
	if out, err := runConfined(t, spec, ws, "printf '[core]\\n' >> .git/modules/vendor/lib/config"); err == nil {
		t.Fatalf("the submodule config stays protected: %s", out)
	}
}
