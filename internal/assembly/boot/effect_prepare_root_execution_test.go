package boot

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

func TestEffectRootAutoSelectionBuildsIndependentPersistentEnvironment(t *testing.T) {
	home, root := statedBootHome(t), testenv.TempDir(t)
	p := &presenceWaitingProvider{entered: make(chan struct{}), leave: make(chan struct{})}
	provider.Register("boot-root-auto-selection", func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
collaboration = "auto"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-root-auto-selection"
model = "x"
`)
	mirrorToUserConfigAt(t, home, root)
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "config", "core.autocrlf", "false")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "base")
	writeFile(t, root, "dirty.txt", "untracked source input\n")
	opts := Options{Home: home, WorkspaceRoot: root, Sink: event.Discard}
	first, err := Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	bindPresenceSession(t, first)
	done, finished := make(chan error, 1), make(chan struct{})
	var leaveOnce sync.Once
	defer func() { leaveOnce.Do(func() { close(p.leave) }); <-finished }()
	go func() { defer close(finished); done <- first.Run(t.Context(), "think") }()
	select {
	case <-p.entered:
	case err := <-done:
		t.Fatalf("first model did not start: %v", err)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	second, err := Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	lease := bindPresenceSession(t, second)
	release, err := second.BeginRuntimeReplacement()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	prepared, err := PrepareRootExecution(t.Context(), second, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	cfg, err := config.RootsForHome(home).LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if automaticIsolationAvailability(opts, cfg) != nil {
		if prepared.Result != nil || prepared.Controller != second {
			t.Fatal("unavailable automatic environment did not fall back before moving the chat")
		}
		if _, bound, err := sessionstore.LoadExecutionEnvironment(second.SessionPath()); err != nil || bound {
			t.Fatalf("fallback bound an unusable environment: %v %v", bound, err)
		}
		return
	}
	if prepared.Result == nil || prepared.Controller.WorkspaceRepo().WorkTree == second.WorkspaceRepo().WorkTree {
		t.Fatal("parallel auto execution retained source runtime")
	}
	actual := prepared.Controller.WorkspaceRoot()
	if raw, err := os.ReadFile(filepath.Join(actual, "dirty.txt")); err != nil || string(raw) != "untracked source input\n" {
		t.Fatalf("dirty snapshot = %q %v", raw, err)
	}
	if _, bound, err := sessionstore.LoadExecutionEnvironment(second.SessionPath()); err != nil || bound {
		t.Fatalf("binding published before adoption: %v %v", bound, err)
	}
	if err := prepared.Publish(func(*BuildResult) error { return nil }); err == nil {
		t.Fatal("unadopted root was published")
	}
	if err := prepared.Adopt(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Controller.BindSessionWriteAuthority(lease); err != nil {
		t.Fatal(err)
	}
	if err := prepared.Publish(func(*BuildResult) error { return nil }); err != nil {
		t.Fatal(err)
	}
	second.Close()
	prepared.Close()
	if err := prepared.Controller.CheckSessionEnvironment(prepared.Controller.SessionPath()); err != nil {
		t.Fatal("published root lost environment ownership:", err)
	}
	prepared.Controller.Close()
	if _, err := os.Stat(actual); err != nil {
		t.Fatal("root settlement removed persistent files:", err)
	}
	leaveOnce.Do(func() { close(p.leave) })
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	third, err := Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer third.Close()
	bindPresenceSession(t, third)
	releaseThird, err := third.BeginRuntimeReplacement()
	if err != nil {
		t.Fatal(err)
	}
	defer releaseThird()
	shared, err := PrepareRootExecution(t.Context(), third, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	if shared.Result != nil || shared.Controller != third {
		t.Fatal("idle source created unnecessary root environment")
	}
}
