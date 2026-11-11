package delegation

import (
	"context"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

func TestTurnIdentityRidesTheContext(t *testing.T) {
	if got := TurnIdentity(context.Background()); got != "" {
		t.Fatalf("bare context carries turn %q", got)
	}
	if got := TurnIdentity(WithTurnIdentity(context.Background(), "turn-7")); got != "turn-7" {
		t.Fatalf("TurnIdentity = %q, want turn-7", got)
	}
}

func TestRunRecordNamesTheTurnItWasSpawnedIn(t *testing.T) {
	store := NewSubagentStore(testenv.TempDir(t))
	spec := testSubagentSpec(t, "audit")
	spec.ParentTurn = "turn-7"
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	defer run.Release()
	if err := store.MarkRunning(run); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	meta, err := store.LoadMeta(run.Ref)
	if err != nil || meta.ParentTurn != "turn-7" {
		t.Fatalf("stored parent turn = %q err=%v, want turn-7", meta.ParentTurn, err)
	}
	got, err := store.Transcript(run.Ref, spec.ParentSession, spec.WorkspaceRoot, 0, 0)
	if err != nil || got.Info.ParentTurn != "turn-7" {
		t.Fatalf("listed parent turn = %q err=%v, want turn-7", got.Info.ParentTurn, err)
	}
}

func TestParentTurnDoesNotChangeTheCapsuleIdentity(t *testing.T) {
	store := NewSubagentStore(testenv.TempDir(t))
	plain := testSubagentSpec(t, "audit")
	turned := plain
	turned.ParentTurn = "turn-9"
	a, err := store.PrepareFresh(plain)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	b, err := store.PrepareFresh(turned)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Release()
	if a.Meta.CapsuleHash != b.Meta.CapsuleHash {
		t.Fatal("a parent turn changed the capsule hash: two runs of the same work would differ on every turn")
	}
}

func BenchmarkPrepareFresh(b *testing.B) {
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	for _, turn := range []string{"", "turn-7"} {
		b.Run("turn="+turn, func(b *testing.B) {
			store := NewSubagentStore(testenv.TempDir(b))
			spec := SubagentSpec{ExecutionID: "e", Kind: "task", Name: "x", WorkspaceRoot: testenv.TempDir(b), ParentSession: "p", SystemPrompt: "s", Registry: reg, ParentTurn: turn}
			b.ReportAllocs()
			for range b.N {
				run, err := store.PrepareFresh(spec)
				if err != nil {
					b.Fatal(err)
				}
				run.Release()
			}
		})
	}
}

func TestLiveRunsListsOnlyWhatThisProcessIsExecuting(t *testing.T) {
	store, spec := viewFixture(t)
	spec.ParentTurn = "turn-3"
	running := startedRun(t, store, spec)
	done := startedRun(t, store, spec)
	if err := store.SaveCompleted(done); err != nil {
		t.Fatal(err)
	}
	done.Release()

	got, err := store.LiveRuns(spec.ParentSession, spec.WorkspaceRoot)
	if err != nil {
		t.Fatalf("LiveRuns: %v", err)
	}
	if len(got) != 1 || got[0].Ref != running.Ref || got[0].State != RunRunning || got[0].ParentTurn != "turn-3" {
		t.Fatalf("live runs = %+v, want only %s running under turn-3", got, running.Ref)
	}
	if other, _ := store.LiveRuns("another-session", spec.WorkspaceRoot); len(other) != 0 {
		t.Fatalf("another conversation sees %d live runs, want none", len(other))
	}
	running.Release()
	if after, _ := store.LiveRuns(spec.ParentSession, spec.WorkspaceRoot); len(after) != 0 {
		t.Fatalf("a released run is still live: %+v", after)
	}
}

func BenchmarkLiveRunsAgainstARunsScan(b *testing.B) {
	store := NewSubagentStore(testenv.TempDir(b) + "/subagents")
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "read_file", readOnly: true})
	spec := SubagentSpec{ExecutionID: "e", Kind: "task", Name: "x", WorkspaceRoot: testenv.TempDir(b), ParentSession: "p", SystemPrompt: "s", Registry: reg}
	for range 300 {
		run, err := store.PrepareFresh(spec)
		if err != nil {
			b.Fatal(err)
		}
		if err := store.MarkRunning(run); err != nil {
			b.Fatal(err)
		}
		if err := store.SaveCompleted(run); err != nil {
			b.Fatal(err)
		}
		run.Release()
	}
	for range 3 {
		run, err := store.PrepareFresh(spec)
		if err != nil {
			b.Fatal(err)
		}
		if err := store.MarkRunning(run); err != nil {
			b.Fatal(err)
		}
		defer run.Release()
	}
	b.Run("LiveRuns", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := store.LiveRuns("p", spec.WorkspaceRoot); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("Runs", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := store.Runs("p", spec.WorkspaceRoot); err != nil {
				b.Fatal(err)
			}
		}
	})
}
