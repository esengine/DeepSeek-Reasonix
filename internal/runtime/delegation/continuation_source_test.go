package delegation

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/jobs"
)

func TestAncestorContinuationPreparationHasOneCrossProcessOwner(t *testing.T) {
	dir, store, ref, spec := prepareCompletedSubagentForLineageTest(t, "parent")
	saveTestBranchMeta(t, dir, "parent", "")
	saveTestBranchMeta(t, dir, "child", "parent")
	first, err := store.continuationSource(t.Context(), ref, "child")
	if err != nil || first.Kind != EnvironmentCopy || !first.active() {
		t.Fatalf("ancestor source not reserved: %+v %v", first, err)
	}
	peer := NewSubagentStore(filepath.Join(dir, "subagents"))
	if _, err := peer.continuationSource(t.Context(), ref, "child"); err == nil {
		t.Fatal("another store prepared a duplicate alias")
	} else {
		var refusal tool.Refusal
		if !errors.As(err, &refusal) || refusal.Code != isolation.CodeBusy {
			t.Fatalf("unexpected duplicate result: %v", err)
		}
	}
	first.release()
	successor, err := peer.continuationSource(t.Context(), ref, "child")
	if err != nil {
		t.Fatal(err)
	}
	defer successor.release()
	first.release()
	if _, err := store.continuationSource(t.Context(), ref, "child"); err == nil {
		t.Fatal("old release removed successor's preparation owner")
	}
	successor.release()
	spec.ParentSession = "child"
	run, err := peer.PrepareContinue(ref, spec)
	if err != nil {
		t.Fatal(err)
	}
	defer run.Release()
	if _, err := store.continuationSource(t.Context(), ref, "child"); err == nil {
		t.Fatal("direct transcript caller bypassed common preparation owner")
	}
	if err := peer.MarkRunning(run); err != nil {
		t.Fatal(err)
	}
	resolved, err := store.continuationSource(t.Context(), ref, "child")
	if err != nil || resolved.Kind != EnvironmentResume || resolved.Reference != run.Ref || resolved.active() {
		t.Fatalf("published copy alias was not reused without a preparation lock: %+v %v", resolved, err)
	}
	if meta, err := store.LoadMeta(ref); err != nil || meta.ParentSession != "parent" || meta.Status != sessionstore.SubagentCompleted {
		t.Fatalf("copy preparation modified ancestor: %+v %v", meta, err)
	}
}

func TestAncestorContinuationCancelledQueueReleasesOnlyItsPreparation(t *testing.T) {
	for _, background := range []bool{false, true} {
		name := "foreground"
		if background {
			name = "background"
		}
		t.Run(name, func(t *testing.T) {
			dir, root := t.TempDir(), t.TempDir()
			store := NewSubagentStore(filepath.Join(dir, "subagents"))
			registry := tool.NewRegistry()
			registry.Add(fakeTool{name: "read_file", readOnly: true})
			options := TaskToolOptions{Provider: &mockProvider{chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "source"}, {Type: provider.ChunkDone}}}, ParentRegistry: registry, SysPrompt: "sys", MaxSteps: 20}
			source := NewTaskToolWithOptions(options).WithTranscripts(store, root, "deepseek", "max")
			out, err := source.RunProfileSpec(agent.WithParentSession(t.Context(), "ancestor"), ProfileExecSpec{Task: TaskSpec{Objective: "source"}})
			if err != nil {
				t.Fatal(err)
			}
			ref := subagentRefFromOutput(t, out)
			saveTestBranchMeta(t, dir, "ancestor", "")
			saveTestBranchMeta(t, dir, "parent-session", "ancestor")
			scheduler := writeclaim.NewSubagentScheduler(1, 1)
			release, err := scheduler.Acquire(t.Context(), writeclaim.AcquireRequest{})
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			options.Provider = &blockingProvider{started: make(chan struct{})}
			parent := NewTaskToolWithOptions(options).WithTranscripts(store, root, "deepseek", "max").WithScheduler(scheduler)
			manager := jobs.NewManager(event.Discard)
			defer manager.Close()
			ctx, cancel := context.WithCancel(jobs.WithSession(jobs.WithManager(testTaskContext(), manager), "parent-session"))
			defer cancel()
			spec := ProfileExecSpec{Task: TaskSpec{Objective: "continue"}, Context: ContextRequest{ContinueFrom: ref}, Sched: SchedulerPolicy{RunInBackground: background}}
			type result struct {
				out string
				err error
			}
			done := make(chan result, 1)
			go func() { out, err := parent.RunProfileSpec(ctx, spec); done <- result{out, err} }()
			var submitted result
			if background {
				submitted = <-done
				if submitted.err != nil {
					t.Fatal(submitted.err)
				}
			}
			waitForQueuedAcquires(t, scheduler, 1)
			if background {
				if !manager.KillForSession("parent-session", extractJobID(submitted.out)) {
					t.Fatal("queued job not cancelled")
				}
				manager.Close()
			} else {
				cancel()
				if finished := <-done; !errors.Is(finished.err, context.Canceled) {
					t.Fatalf("queue result: %v", finished.err)
				}
			}
			peer := NewSubagentStore(filepath.Join(dir, "subagents"))
			next, err := peer.continuationSource(t.Context(), ref, "parent-session")
			if err != nil {
				t.Fatalf("cancelled queue retained alias: %v", err)
			}
			next.release()
		})
	}
}

func TestAncestorContinuationCopyRetainsPersonaToolAndModelConstraints(t *testing.T) {
	dir, store, ref, spec := prepareCompletedSubagentForLineageTest(t, "ancestor")
	saveTestBranchMeta(t, dir, "ancestor", "")
	saveTestBranchMeta(t, dir, "child", "ancestor")
	source, err := store.continuationSource(t.Context(), ref, "child")
	if err != nil {
		t.Fatal(err)
	}
	defer source.release()
	meta, err := store.LoadMeta(ref)
	if err != nil {
		t.Fatal(err)
	}
	spec.ParentSession, spec.EnvironmentID, spec.WorkspaceRoot = "child", "iso_new", t.TempDir()
	spec.continuationSource = source
	if err := validateMeta(meta, spec); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*SubagentSpec){
		"persona": func(s *SubagentSpec) { s.SystemPrompt = "different persona" },
		"tools":   func(s *SubagentSpec) { s.Registry = tool.NewRegistry() },
		"model":   func(s *SubagentSpec) { s.Model = "different model" },
		"effort":  func(s *SubagentSpec) { s.Effort = "different effort" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := spec
			change(&changed)
			if err := validateMeta(meta, changed); err == nil {
				t.Fatal("copy bypassed continuation constraints")
			}
		})
	}
	source.release()
	if err := validateMeta(meta, spec); err == nil {
		t.Fatal("released preparation proof remained valid")
	}
}

func TestEnvironmentContinuationUsesCurrentCandidateWithoutAnotherFactory(t *testing.T) {
	root := t.TempDir()
	store := NewSubagentStore(filepath.Join(t.TempDir(), "subagents"))
	p := &mockProvider{chunks: []provider.Chunk{{Type: provider.ChunkText, Text: "same scene"}, {Type: provider.ChunkDone}}}
	task := NewTaskToolWithOptions(TaskToolOptions{Provider: p, ParentRegistry: tool.NewRegistry(), SysPrompt: "sys", MaxSteps: 20}).WithTranscripts(store, root, "model", "high")
	task.environmentID, task.environmentGeneration, task.environmentSource = "iso_current", "generation", root
	out, err := task.RunProfileSpec(testTaskContext(), ProfileExecSpec{Task: TaskSpec{Objective: "first"}})
	if err != nil {
		t.Fatal(err)
	}
	ref := subagentRefFromOutput(t, out)
	out, err = task.RunProfileSpec(testTaskContext(), ProfileExecSpec{Task: TaskSpec{Objective: "continue"}, Context: ContextRequest{ContinueFrom: ref}, Sched: SchedulerPolicy{Environment: "worktree"}})
	if err != nil || subagentRefFromOutput(t, out) != ref {
		t.Fatalf("existing candidate incorrectly required another factory: %s %v", out, err)
	}
}
