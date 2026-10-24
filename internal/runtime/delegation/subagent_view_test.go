package delegation

import (
	"errors"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
)

func viewFixture(t *testing.T) (*SubagentStore, SubagentSpec) {
	t.Helper()
	sessionDir := testenv.TempDir(t)
	store := NewSubagentStore(sessionDir + "/subagents")
	spec := testSubagentSpec(t, "worker")
	spec.Kind = "task"
	return store, spec
}

func startedRun(t *testing.T, store *SubagentStore, spec SubagentSpec, msgs ...provider.Message) *SubagentRun {
	t.Helper()
	run, err := store.PrepareFresh(spec)
	if err != nil {
		t.Fatalf("PrepareFresh: %v", err)
	}
	for _, m := range msgs {
		run.Session.Add(m)
	}
	if err := store.MarkRunning(run); err != nil {
		t.Fatalf("MarkRunning: %v", err)
	}
	return run
}

func said(role provider.Role, text string) provider.Message {
	return provider.Message{Role: role, Content: text}
}

func TestRunningChildIsReadFromMemoryAndFinishedOneFromTheStore(t *testing.T) {
	store, spec := viewFixture(t)
	run := startedRun(t, store, spec, said(provider.RoleUser, "audit"), said(provider.RoleAssistant, "reading"))

	got, err := store.Transcript(run.Ref, spec.ParentSession, spec.WorkspaceRoot, 0, 0)
	if err != nil || !got.Live || got.Info.State != RunRunning || got.Total != 3 || len(got.Messages) != 3 {
		t.Fatalf("running read = %+v err=%v, want the system prompt and 2 live messages", got, err)
	}

	run.Session.Add(said(provider.RoleAssistant, "found it"))
	tail, err := store.Transcript(run.Ref, spec.ParentSession, spec.WorkspaceRoot, 3, 0)
	if err != nil || tail.Total != 4 || len(tail.Messages) != 1 || tail.Messages[0].Content != "found it" || tail.Next != 4 {
		t.Fatalf("tail = %+v err=%v, want only the third message", tail, err)
	}

	if err := store.SaveCompleted(run); err != nil {
		t.Fatalf("SaveCompleted: %v", err)
	}
	run.Release()
	done, err := store.Transcript(run.Ref, spec.ParentSession, spec.WorkspaceRoot, 0, 0)
	if err != nil || done.Live || done.Info.State != RunCompleted || done.Total != 4 || len(done.Messages) != 4 {
		t.Fatalf("finished read = %+v err=%v, want 4 stored messages and not live", done, err)
	}
	if page, _ := store.Transcript(run.Ref, spec.ParentSession, spec.WorkspaceRoot, 1, 1); len(page.Messages) != 1 || page.Next != 2 {
		t.Fatalf("limit 1 from 1 = %+v, want one message and next=2", page)
	}
}

func TestRunsListsLiveAndFinishedChildrenOfOneParentOnly(t *testing.T) {
	store, spec := viewFixture(t)
	live := startedRun(t, store, spec, said(provider.RoleUser, "a"))
	defer live.Release()
	finished := startedRun(t, store, spec, said(provider.RoleUser, "b"))
	if err := store.SaveCompleted(finished); err != nil {
		t.Fatal(err)
	}
	finished.Release()
	other := spec
	other.ParentSession = "someone-else"
	foreign := startedRun(t, store, other, said(provider.RoleUser, "c"))
	defer foreign.Release()

	runs, err := store.Runs(spec.ParentSession, spec.WorkspaceRoot)
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs = %+v err=%v, want this parent's two", runs, err)
	}
	states := map[string]RunState{}
	for _, r := range runs {
		states[r.Ref] = r.State
		if r.Model != spec.Model || r.Name != spec.Name || r.Kind != "task" {
			t.Fatalf("run info = %+v, want the spec's model, name and kind", r)
		}
	}
	if states[live.Ref] != RunRunning || states[finished.Ref] != RunCompleted {
		t.Fatalf("states = %v", states)
	}
}

func TestARunWhoseOwnerIsGoneReadsAsInterruptedNotRunning(t *testing.T) {
	store, spec := viewFixture(t)
	run := startedRun(t, store, spec, said(provider.RoleUser, "a"))
	run.Release()
	runs, err := store.Runs(spec.ParentSession, spec.WorkspaceRoot)
	if err != nil || len(runs) != 1 || runs[0].State != RunInterrupted {
		t.Fatalf("runs = %+v err=%v, want one interrupted run", runs, err)
	}
}

func TestTranscriptRefusesWhatTheCallerDoesNotOwn(t *testing.T) {
	store, spec := viewFixture(t)
	run := startedRun(t, store, spec, said(provider.RoleUser, "a"))
	defer run.Release()
	if _, err := store.Transcript(run.Ref, "someone-else", spec.WorkspaceRoot, 0, 0); !errors.Is(err, ErrSubagentNotOwned) {
		t.Fatalf("other parent = %v, want ErrSubagentNotOwned", err)
	}
	if _, err := store.Transcript(run.Ref, spec.ParentSession, testenv.TempDir(t), 0, 0); !errors.Is(err, ErrSubagentNotOwned) {
		t.Fatalf("other workspace = %v, want ErrSubagentNotOwned", err)
	}
	for _, ref := range []string{"sa_" + "0000000000000000", "../x", ""} {
		if _, err := store.Transcript(ref, spec.ParentSession, spec.WorkspaceRoot, 0, 0); !errors.Is(err, ErrSubagentNotFound) {
			t.Fatalf("ref %q = %v, want ErrSubagentNotFound", ref, err)
		}
	}
}

func TestReadingARunningChildWhileItWritesIsSafe(t *testing.T) {
	store, spec := viewFixture(t)
	run := startedRun(t, store, spec, said(provider.RoleUser, "a"))
	defer run.Release()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 200 {
			run.Session.Add(said(provider.RoleAssistant, "step"))
		}
	}()
	go func() {
		defer wg.Done()
		next := 0
		for range 200 {
			got, err := store.Transcript(run.Ref, spec.ParentSession, spec.WorkspaceRoot, next, 0)
			if err != nil {
				t.Errorf("Transcript: %v", err)
				return
			}
			next = got.Next
		}
	}()
	wg.Wait()
}

// A run is recorded complete a moment before its owner lets go of it, and a
// reader in that moment must not be told it is still being written.
func TestARecordedFinishedRunIsNotLiveWhileItsOwnerHasNotLetGo(t *testing.T) {
	store, spec := viewFixture(t)
	run := startedRun(t, store, spec, said(provider.RoleUser, "a"), said(provider.RoleAssistant, "done"))
	defer run.Release()
	if err := store.SaveCompleted(run); err != nil {
		t.Fatal(err)
	}
	got, err := store.Transcript(run.Ref, spec.ParentSession, spec.WorkspaceRoot, 0, 0)
	if err != nil || got.Live || got.Info.State != RunCompleted || len(got.Messages) != 3 {
		t.Fatalf("read between SaveCompleted and Release = %+v err=%v, want completed, not live, all messages", got, err)
	}
}

func TestARunNamesTheToolCallThatSpawnedIt(t *testing.T) {
	store, spec := viewFixture(t)
	spec.ParentToolCallID = "call_task_7"
	live := startedRun(t, store, spec, said(provider.RoleUser, "a"))
	defer live.Release()
	done := startedRun(t, store, spec, said(provider.RoleUser, "b"))
	if err := store.SaveCompleted(done); err != nil {
		t.Fatal(err)
	}
	done.Release()

	runs, err := store.Runs(spec.ParentSession, spec.WorkspaceRoot)
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs = %+v err=%v", runs, err)
	}
	for _, r := range runs {
		if r.ParentToolCallID != "call_task_7" {
			t.Fatalf("run %s parent call = %q, want the call that spawned it, live or finished", r.Ref, r.ParentToolCallID)
		}
	}
	page, err := store.Transcript(live.Ref, spec.ParentSession, spec.WorkspaceRoot, 0, 0)
	if err != nil || page.Info.ParentToolCallID != "call_task_7" {
		t.Fatalf("transcript info = %+v err=%v", page.Info, err)
	}
}
