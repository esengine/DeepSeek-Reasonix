package sessioncatalog

import (
	"context"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

func recoveryHeavyDirectory(t *testing.T, branches int) (string, DirectoryTarget) {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "root.jsonl")
	saveLineageSession(t, root, "question", "answer", "continued", "done")
	for range branches {
		session := agent.NewSession("sys")
		session.Add(provider.Message{Role: provider.RoleUser, Content: "question"})
		session.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
		if _, err := session.SaveConflictRecoveryBranch(agent.RecoveryBranchOptions{OriginalPath: root}); err != nil {
			t.Fatal(err)
		}
	}
	return dir, DirectoryTarget{Path: dir, Scope: "global"}
}

func TestRecoveryDirectoryPreferredBranchLoadsOnceThenIdleLoadsNone(t *testing.T) {
	ctx := context.Background()
	dir, target := recoveryHeavyDirectory(t, 8)
	ordered, err := agent.ListSessionOrder(dir)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(ordered))
	preferred := ""
	for _, info := range ordered {
		paths = append(paths, info.Path)
		if info.Recovered && preferred == "" {
			preferred = info.Path
		}
	}
	if preferred == "" {
		t.Fatal("recovery-heavy fixture did not create a recovery branch")
	}
	if err := agent.SetRecoveryPreferred(paths, preferred); err != nil {
		t.Fatal(err)
	}
	catalog, err := Open(ctx, Options{Path: filepath.Join(t.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = catalog.Close(context.Background()) })
	loads := map[string]int{}
	catalog.testSessionContentLoadHook = func(path string) { loads[catalog.pathKey(path)]++ }
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		t.Fatal(err)
	}
	if len(loads) != 9 {
		t.Fatalf("loaded transcripts = %d, want root + 8 branches", len(loads))
	}
	for path, count := range loads {
		if count != 1 {
			t.Fatalf("%s loaded %d times in one wave", path, count)
		}
	}
	loads = map[string]int{}
	if err := catalog.ReconcileDirectory(ctx, DirectoryTarget{Path: dir, Scope: "global"}); err != nil {
		t.Fatal(err)
	}
	if len(loads) != 0 {
		t.Fatalf("unchanged verified reconcile decoded %d transcripts", len(loads))
	}
}

func BenchmarkRecoveryDirectoryVerifiedIdleReconcile(b *testing.B) {
	ctx := context.Background()
	dir := b.TempDir()
	root := filepath.Join(dir, "root.jsonl")
	s := agent.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "question"})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	if err := s.Save(root); err != nil {
		b.Fatal(err)
	}
	for range 128 {
		branch := agent.NewSession("sys")
		branch.Add(provider.Message{Role: provider.RoleUser, Content: "question"})
		if _, err := branch.SaveConflictRecoveryBranch(agent.RecoveryBranchOptions{OriginalPath: root}); err != nil {
			b.Fatal(err)
		}
	}
	target := DirectoryTarget{Path: dir, Scope: "global"}
	catalog, err := Open(ctx, Options{Path: filepath.Join(b.TempDir(), "catalog.sqlite"), DisableRepair: true})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = catalog.Close(context.Background()) })
	if err := catalog.ReconcileDirectory(ctx, target); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		if err := catalog.ReconcileDirectory(ctx, target); err != nil {
			b.Fatal(err)
		}
	}
}
