package boot

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/session/control"
	"reasonix/internal/state/mutation"
)

var registerGitCommonEffect = sync.OnceFunc(func() {
	provider.Register("git-common-effect", func(provider.Config) (provider.Provider, error) { return &effectRecordingProvider{}, nil })
})

func TestEffectLinkedWorktreeUserShellWaitsForSharedGitMetadata(t *testing.T) {
	for _, layout := range []string{"primary", "additional", "saved_extra"} {
		t.Run(layout, func(t *testing.T) { runLinkedWorktreeMutationCase(t, layout, "") })
	}
}

func TestEffectQueuedUserShellRefusesChangedGitDiscovery(t *testing.T) {
	for _, layout := range []string{"primary", "additional", "saved_extra"} {
		for _, changed := range []string{"gitdir", "commondir"} {
			t.Run(layout+"/"+changed, func(t *testing.T) { runLinkedWorktreeMutationCase(t, layout, changed) })
		}
	}
}

func runLinkedWorktreeMutationCase(t *testing.T, layout, changed string) {
	registerGitCommonEffect()
	home, root, linked := statedBootHome(t), t.TempDir(), t.TempDir()
	if layout == "saved_extra" {
		linked = extraRootFolder(t)
	}
	writeFile(t, root, "reasonix.toml", `default_model="test-model"
[agent]
collaboration="shared"
[codegraph]
enabled=false
[[providers]]
name="test-model"
kind="git-common-effect"
model="x"
`)
	mirrorToUserConfigAt(t, home, root)
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "fixture")
	gitInDir(t, root, "worktree", "add", "--detach", linked, "HEAD")
	t.Cleanup(func() { gitInDir(t, root, "worktree", "remove", "--force", linked) })
	approveWorkspaceAt(t, home, linked)
	activeRoot := linked
	var additional []string
	if layout != "primary" {
		activeRoot = t.TempDir()
		cfg, err := os.ReadFile(filepath.Join(root, "reasonix.toml"))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, activeRoot, "reasonix.toml", string(cfg))
		approveWorkspaceAt(t, home, activeRoot)
		if layout == "additional" {
			additional = []string{linked}
		}
	}
	waiting, done := make(chan event.Event, 8), make(chan event.Event, 1)
	c, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: activeRoot, AdditionalDirs: additional, Sink: event.FuncSink(func(e event.Event) {
		if e.WorkspaceLease != nil && e.WorkspaceLease.State == "waiting" {
			waiting <- e
		}
		if e.Kind == event.TurnDone {
			done <- e
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.EnsureSessionPath()
	if layout == "saved_extra" {
		if _, err := c.AddWorkspaceRoot(linked); err != nil {
			t.Fatal(err)
		}
	}
	linkedRepo, err := gitcmd.Open(t.Context(), linked)
	if err != nil {
		t.Fatal(err)
	}
	other, err := mutation.New(root, config.WorkspaceLeaseDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	held, err := other.Acquire(t.Context(), mutation.Intent{Effect: mutation.GitMetadata, Paths: []string{linkedRepo.CommonDir}})
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(mutation.NotExecuted)
	c.RunShellWith("echo fixture > shell-result.txt", control.ShellRun{LocalOnly: true})
	select {
	case e := <-waiting:
		if e.WorkspaceLease.HolderTransactionID != held.ID {
			t.Fatal("user shell waited for the wrong resource")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("linked worktree shell bypassed common metadata")
	}
	if _, err := os.Stat(filepath.Join(activeRoot, "shell-result.txt")); !os.IsNotExist(err) {
		t.Fatalf("shell ran before admission: %v", err)
	}
	independent, err := other.Acquire(t.Context(), mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{filepath.Join(root, "independent.txt")}})
	if err != nil {
		t.Fatal(err)
	}
	independent.End(mutation.NotExecuted)
	if changed != "" {
		redirectQueuedGitDiscovery(t, linkedRepo, changed)
	}
	held.End(mutation.NotExecuted)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("user shell did not continue")
	}
	_, err = os.Stat(filepath.Join(activeRoot, "shell-result.txt"))
	if changed != "" {
		if !os.IsNotExist(err) {
			t.Fatalf("shell executed after Git discovery changed: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
}

func redirectQueuedGitDiscovery(t *testing.T, repo gitcmd.Repo, changed string) {
	t.Helper()
	path := filepath.Join(repo.WorkTree, ".git")
	value := "gitdir: " + filepath.Join(repo.CommonDir, "worktrees", "replacement") + "\n"
	if changed == "commondir" {
		path = filepath.Join(repo.GitDir, "commondir")
		value = t.TempDir() + "\n"
	} else {
		dir := filepath.Join(repo.CommonDir, "worktrees", "replacement")
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(dir); err != nil {
				t.Error(err)
			}
		})
		for _, name := range []string{"HEAD", "gitdir", "commondir"} {
			data, err := os.ReadFile(filepath.Join(repo.GitDir, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(path, before, 0600); err != nil {
			t.Error(err)
		}
	})
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	if err := repo.Verify(); err != nil {
		t.Fatalf("fixture must preserve pinned identity: %v", err)
	}
}
