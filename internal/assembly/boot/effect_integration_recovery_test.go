package boot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/worktree"
	"reasonix/internal/session/control"
)

func TestEffectRecoveryJournalFencesNewRuntimeWrites(t *testing.T) {
	home, root := statedBootHome(t), t.TempDir()
	writeFile(t, root, "reasonix.toml", `
default_model = "test-model"
[agent]
collaboration = "shared"
[[providers]]
name = "test-model"
kind = "integration-fence-effect"
model = "x"
`)
	mirrorToUserConfigAt(t, home, root)
	approveWorkspaceAt(t, home, root)
	gitInDir(t, root, "init", "-q")
	writeFile(t, root, "a.txt", "original a")
	writeFile(t, root, "b.txt", "original b")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "base")
	leaveInterruptedIntegration(t, root)
	p := &pathLeaseProvider{path: "unrelated.txt", written: make(chan struct{}), resume: make(chan struct{})}
	close(p.resume)
	var current provider.Provider = p
	provider.Register("integration-fence-effect", func(provider.Config) (provider.Provider, error) { return current, nil })
	var mu sync.Mutex
	code := ""
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.ToolResult {
			mu.Lock()
			code = e.Tool.RefusalCode
			mu.Unlock()
		}
	})
	ctrl, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	_ = ctrl.Run(ctx, "write unrelated.txt")
	mu.Lock()
	gotCode := code
	mu.Unlock()
	if gotCode != "isolation.recovery_required" {
		t.Fatalf("runtime refusal: %q", gotCode)
	}
	if _, err := os.Stat(filepath.Join(root, "unrelated.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime wrote past recovery fence: %v", err)
	}
	recoverer := &revisionLeaseProvider{calls: []provider.ToolCall{{ID: "recover", Name: "recover_integration", Arguments: `{}`}, revisionWriter("recovered.txt")},
		first: make(chan struct{}), advance: make(chan struct{}), last: make(chan provider.Message, 1), finish: make(chan struct{})}
	close(recoverer.advance)
	close(recoverer.finish)
	current = recoverer
	next, err := Build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: event.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	next.SetToolApprovalMode(control.ToolApprovalYolo)
	_ = next.Run(ctx, "recover integration and create recovered.txt")
	if _, err := os.Stat(filepath.Join(root, "recovered.txt")); err != nil {
		t.Fatalf("recovery did not unblock next write: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "a.txt")); err != nil || string(got) != "original a" {
		t.Fatalf("recovery preimage: %q %v", got, err)
	}
}

func leaveInterruptedIntegration(t *testing.T, root string) {
	t.Helper()
	repo, err := gitcmd.Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := worktree.TakeSnapshot(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := worktree.CreateCandidate(t.Context(), snap, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worktree.RemoveCandidate(context.Background(), snap, candidate) })
	writeFile(t, candidate.WorkspaceRoot, "a.txt", "candidate a")
	writeFile(t, candidate.WorkspaceRoot, "b.txt", "candidate b")
	tree, changes, err := worktree.CandidateTree(t.Context(), snap, candidate)
	if err != nil {
		t.Fatal(err)
	}
	old := fileutil.CrashPoint
	defer func() { fileutil.CrashPoint = old }()
	fileutil.CrashPoint = func(op, path string) {
		if op == "atomic-write" && path == filepath.Join(snap.RepoRoot, "b.txt") {
			panic("simulated interruption")
		}
	}
	crashed := false
	func() {
		defer func() { crashed = recover() != nil }()
		_ = worktree.ApplyPaths(t.Context(), snap, tree, changes)
	}()
	if !crashed {
		t.Fatal("integration did not reach crash boundary")
	}
}
