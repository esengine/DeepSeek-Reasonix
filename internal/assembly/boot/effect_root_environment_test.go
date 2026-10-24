package boot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/rootenv"
	"reasonix/internal/session/control"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
)

type persistentRootProvider struct{ calls atomic.Int32 }

func (*persistentRootProvider) Name() string { return "persistent-root" }

func (p *persistentRootProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	call := p.calls.Add(1) - 1
	if call%2 == 0 {
		path := "out.md"
		if call > 0 {
			path = "second.md"
		}
		raw, _ := json.Marshal(map[string]string{"path": path, "content": "isolated document\n"})
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "write", Name: "write_file", Arguments: string(raw)}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectRootEnvironmentRestoresBeforeBuildAndSurvivesRebuild(t *testing.T) {
	home, root := statedBootHome(t), testenv.TempDir(t)
	p := &persistentRootProvider{}
	provider.Register("boot-persistent-root-environment", func(provider.Config) (provider.Provider, error) { return p, nil })
	writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "BASE"
collaboration = "shared"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-persistent-root-environment"
model = "x"
`)
	denied, _ := json.Marshal(filepath.Join(root, "private.txt"))
	configPath := filepath.Join(root, "reasonix.toml")
	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configBytes = append(configBytes, []byte("\n[sandbox]\nforbid_read = ["+string(denied)+"]\n")...)
	if err := os.WriteFile(configPath, configBytes, 0600); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "private.txt", "forbidden\n")
	mirrorToUserConfigAt(t, home, root)
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "base")
	repo, err := gitcmd.Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	coord, err := mutation.New(root, config.WorkspaceLeaseDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := rootenv.New(config.RootsForHome(home).DeliveryWorktreeDir(), root, repo, coord)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(testenv.TempDir(t), "chat.jsonl")
	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	env, err := store.Prepare(t.Context(), sessionstore.CanonicalSessionPath(path), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := lease.IssueWriteAuthority(sessionstore.NextSessionWriteGeneration())
	if err != nil {
		t.Fatal(err)
	}
	if err := env.BindSession(t.Context(), path, auth); err != nil {
		t.Fatal(err)
	}
	r := env.Record()
	env.Close()
	opts := Options{Home: home, WorkspaceRoot: root, SessionDir: filepath.Dir(path), SessionEnvironmentPath: path, Sink: event.Discard}
	builder := newBuilder()
	firstBuild, err := builder.build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	first := firstBuild.Controller
	t.Cleanup(first.Close)
	host := agent.NewHostExecutor(builder.tools.reg, agent.Options{CheckTargetAccess: builder.tools.env.targetAccessCheck()}, event.Discard)
	defer host.Close()
	for _, base := range []string{root, r.Candidate.WorkspaceRoot} {
		raw, _ := json.Marshal(map[string]string{"path": filepath.Join(base, "private.txt")})
		result := host.Execute(t.Context(), provider.ToolCall{Name: "read_file", Arguments: string(raw)})
		if result.Executed || !result.Blocked {
			t.Fatalf("root environment lost source read denial: %+v", result)
		}
	}
	if first.WorkspaceRepo().WorkTree != r.Candidate.Repo.WorkTree {
		t.Fatalf("runtime at source: %+v", first.WorkspaceRepo())
	}
	if err := first.Resume(sessionstore.NewSession("BASE"), path); err != nil {
		t.Fatal(err)
	}
	if err := first.BindSessionWriteAuthority(lease); err != nil {
		t.Fatal(err)
	}
	first.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := first.Run(t.Context(), "write first"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "out.md")); !os.IsNotExist(err) {
		t.Fatal("root write escaped", err)
	}
	if _, err := os.Stat(filepath.Join(r.Candidate.WorktreeRoot, "out.md")); err != nil {
		t.Fatal("candidate write missing", err)
	}
	next, err := Rebuild(t.Context(), first, Options{Home: home, WorkspaceRoot: first.WorkspaceRoot(), SessionDir: filepath.Dir(path), Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(next.Controller.Close)
	first.Close()
	if err := next.Controller.RootEnvironment().Validate(t.Context()); err != nil {
		t.Fatal("old runtime released new owner:", err)
	}
	if err := next.Controller.BindSessionWriteAuthority(lease); err != nil {
		t.Fatal(err)
	}
	if err := next.Controller.Run(t.Context(), "write second"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.Candidate.WorktreeRoot, "second.md")); err != nil {
		t.Fatal(err)
	}
	next.Controller.Close()
	opened, err := store.Open(t.Context(), r.ID, sessionstore.CanonicalSessionPath(path))
	if err != nil {
		t.Fatal(err)
	}
	opened.Close()
	if _, err := os.Stat(filepath.Join(r.Candidate.WorktreeRoot, "out.md")); err != nil {
		t.Fatal("close deleted environment:", err)
	}
	if err := os.RemoveAll(r.Candidate.WorktreeRoot); err != nil {
		t.Fatal(err)
	}
	if bad, err := Build(t.Context(), opts); err == nil {
		bad.Close()
		t.Fatal("missing bound environment fell back to source")
	} else if !errors.Is(err, rootenv.ErrRecovery) {
		t.Fatalf("missing environment error: %v", err)
	}
}
