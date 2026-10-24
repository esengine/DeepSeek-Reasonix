package boot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/runtime/rootenv"
	"reasonix/internal/session/control"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
)

type rootForkAcceptanceProvider struct{ calls atomic.Int32 }

func (*rootForkAcceptanceProvider) Name() string { return "root-fork-acceptance" }

func (p *rootForkAcceptanceProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	step := p.calls.Add(1) - 1
	if step%2 == 0 {
		args := fmt.Sprintf(`{"path":"step-%d.md","content":"candidate document"}`, step/2)
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: fmt.Sprintf("write-%d", step), Name: "write_file", Arguments: args}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestEffectRootConversationForkReopensAnIndependentScene(t *testing.T) {
	home, source := statedBootHome(t), t.TempDir()
	writeFile(t, home, "config.toml", "[agent]\ncollaboration='shared'\nauto_integrate=false\n")
	writeFile(t, source, "base.txt", "source baseline")
	gitInDir(t, source, "init", "-q")
	gitInDir(t, source, "add", "-A")
	gitInDir(t, source, "commit", "-qm", "base")
	repo, err := gitcmd.Open(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := mutation.New(source, config.WorkspaceLeaseDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := rootenv.New(config.RootsForHome(home).DeliveryWorktreeDir(), source, repo, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	path := sessionstore.CanonicalSessionPath(filepath.Join(t.TempDir(), "parent.jsonl"))
	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	environment, err := store.Prepare(t.Context(), path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := lease.IssueWriteAuthority(sessionstore.NextSessionWriteGeneration())
	if err != nil {
		t.Fatal(err)
	}
	if err := environment.BindSession(t.Context(), path, authority); err != nil {
		t.Fatal(err)
	}
	parentRecord := environment.Record()
	environment.Close()
	p := &rootForkAcceptanceProvider{}
	opts := installationEffectOptions(t, home, source, p)
	opts.SessionDir, opts.SessionEnvironmentPath = filepath.Dir(path), path
	parent, err := Build(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := parent.Resume(sessionstore.NewSession("BASE"), path); err != nil {
		t.Fatal(err)
	}
	if err := parent.BindSessionWriteAuthority(lease); err != nil {
		t.Fatal(err)
	}
	parent.SetToolApprovalMode(control.ToolApprovalYolo)
	if err := parent.Run(t.Context(), "write in parent scene"); err != nil {
		t.Fatal(err)
	}
	turns := parent.CheckpointTurnsByMessageIndex()
	if len(turns) != 1 {
		t.Fatalf("parent did not establish one branch boundary: %+v", turns)
	}
	var turn int
	for _, value := range turns {
		turn = value
	}
	forkPath, err := parent.BranchAt(turn, "independent scene")
	if err != nil {
		t.Fatal(err)
	}
	binding, bound, err := sessionstore.LoadExecutionEnvironment(forkPath)
	if err != nil || !bound || binding.EnvironmentID == parentRecord.ID || binding.WorkspaceRoot == parent.WorkspaceRoot() || binding.SourceWorkspace != parentRecord.SourceWorkspace {
		t.Fatalf("conversation fork lost independent environment identity: %+v %v %v", binding, bound, err)
	}
	if err := parent.CheckSessionEnvironment(forkPath); err == nil {
		t.Fatal("parent runtime accepted the independent fork's transcript")
	}
	for range 2 {
		func() {
			childLease, err := sessionstore.TryAcquireSessionLease(forkPath)
			if err != nil {
				t.Fatal(err)
			}
			defer childLease.Release()
			opts.SessionEnvironmentPath = forkPath
			child, err := Build(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer child.Close()
			loaded, err := sessionstore.LoadSession(forkPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := child.Resume(loaded, forkPath); err != nil {
				t.Fatal(err)
			}
			if err := child.BindSessionWriteAuthority(childLease); err != nil {
				t.Fatal(err)
			}
			child.SetToolApprovalMode(control.ToolApprovalYolo)
			if child.WorkspaceRoot() != binding.WorkspaceRoot {
				t.Fatal("reopened fork moved to another scene")
			}
			if err := child.Run(t.Context(), "continue fork after open"); err != nil {
				t.Fatal(err)
			}
		}()
	}
	for step := range 3 {
		name := fmt.Sprintf("step-%d.md", step)
		if body, err := os.ReadFile(filepath.Join(binding.WorkspaceRoot, name)); err != nil || string(body) != "candidate document" {
			t.Fatalf("fork lost retained or resumed work: %s %q %v", name, body, err)
		}
		if _, err := os.Stat(filepath.Join(source, name)); !os.IsNotExist(err) {
			t.Fatal("fork wrote source", name, err)
		}
		if step > 0 {
			if _, err := os.Stat(filepath.Join(parent.WorkspaceRoot(), name)); !os.IsNotExist(err) {
				t.Fatal("fork wrote parent scene", name, err)
			}
		}
	}
	if err := parent.RootEnvironment().Validate(t.Context()); err != nil {
		t.Fatal("fork close retired the parent environment", err)
	}
}
