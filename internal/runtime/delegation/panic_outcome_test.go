package delegation

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/agentgraph"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/tools/jobs"
)

func TestProfilePanicOutcomes(t *testing.T) {
	for _, background := range []bool{false, true} {
		name := "foreground"
		if background {
			name = "background"
		}
		t.Run(name, func(t *testing.T) {
			runProfilePanicOutcome(t, background, errors.New("provider panic cause"))
		})
	}
}

func TestProfileNilPanicOutcomes(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"0", "1"} {
		t.Run("panicnil="+mode, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), exe, "-test.run=^TestProfileNilPanicChild$", "-test.count=1", "-test.v")
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "GODEBUG=") && !strings.HasPrefix(entry, "REASONIX_PROFILE_NIL_PANIC=") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, "GODEBUG=panicnil="+mode, "REASONIX_PROFILE_NIL_PANIC=1")
			output, err := cmd.CombinedOutput()
			t.Logf("explicit panicnil=%s child:\n%s", mode, output)
			if err != nil {
				t.Errorf("compiled profile probe failed: %v", err)
			}
		})
	}
}

func TestProfileNilPanicChild(t *testing.T) {
	if os.Getenv("REASONIX_PROFILE_NIL_PANIC") != "1" {
		return
	}
	for _, background := range []bool{false, true} {
		name := "foreground"
		if background {
			name = "background"
		}
		t.Run(name, func(t *testing.T) { runProfilePanicOutcome(t, background, nil) })
	}
}

type outcomePanicProvider struct{ value any }

func (outcomePanicProvider) Name() string { return "panic-outcome" }

func (p outcomePanicProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	panic(p.value)
}

type profilePanicRecorder struct{ done chan error }

func (*profilePanicRecorder) RecordStart(string, string, string) {}
func (r *profilePanicRecorder) RecordDone(_ string, _ jobs.Status, err error) {
	r.done <- err
}

func runProfilePanicOutcome(t *testing.T, background bool, value any) {
	t.Helper()
	rec := &recordSink{}
	manager := jobs.NewManager(event.Discard)
	defer manager.Close()
	recorder := &profilePanicRecorder{done: make(chan error, 1)}
	manager.SetTaskRecorder(recorder)
	ctx := jobs.WithManager(agent.WithCallContext(t.Context(), "panic-call", rec, nil, false), manager)
	ctx = jobs.WithSession(ctx, "panic-parent")
	ctx = agent.WithParentSession(ctx, "panic-parent")
	task := newTestTaskTool(t, outcomePanicProvider{value: value}, tool.NewRegistry(), "sys", "", "", nil)
	spec := ProfileExecSpec{
		Task: TaskSpec{Objective: "exercise callback failure"}, Grant: CapabilityGrant{AllowNoTools: true},
		Worker: WorkerSpec{Kind: "task", Name: "task", SystemPrompt: "sys"},
		Sched:  SchedulerPolicy{RunInBackground: background},
	}
	returned := false
	var recovered any
	var output string
	func() {
		defer func() { recovered = recover() }()
		var err error
		output, err = task.RunProfileSpec(ctx, spec)
		if err != nil {
			t.Errorf("unexpected synchronous return: %v", err)
		}
		returned = true
	}()
	if background {
		if !returned {
			t.Fatalf("background launch panicked: %v", recovered)
		}
		results, why := manager.Wait(ctx, []string{extractJobID(output)}, jobs.WaitOptions{Timeout: 5 * time.Second})
		if why != jobs.WaitFinished || len(results) != 1 || results[0].Status != jobs.Failed {
			t.Errorf("background panic outcome = %+v (%s), want failed", results, why)
		}
		cause := <-recorder.done
		if original, ok := value.(error); ok && !errors.Is(cause, original) {
			t.Errorf("background panic lost cause identity: %v", cause)
		}
	} else {
		if returned {
			t.Error("foreground panic was swallowed")
		}
		if cause, ok := value.(error); ok {
			if observed, ok := recovered.(error); !ok || !errors.Is(observed, cause) {
				t.Errorf("foreground panic identity = %v, want original cause", recovered)
			}
		}
	}
	graph := foldGraph(t, rec)
	requireState(t, graph, "panic-call/sub-1", agentgraph.StateFailed)
	terminal := 0
	for _, e := range rec.kinds(event.ToolProgress) {
		if progressName(e) == event.SubagentProgressStatusName && isTerminalStatusOutput(progressOutput(e)) {
			terminal++
			if progressOutput(e) != string(subagentPhaseFailed) {
				t.Errorf("panic progress = %q, want failed", progressOutput(e))
			}
		}
	}
	if terminal != 1 {
		t.Errorf("terminal progress count = %d, want one", terminal)
	}
	paths, err := filepath.Glob(filepath.Join(task.transcripts.dir, "*.meta.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("prepared metadata = %v, error = %v", paths, err)
	}
	ref := strings.TrimSuffix(filepath.Base(paths[0]), ".meta.json")
	meta, err := NewSubagentStore(task.transcripts.dir).LoadMeta(ref)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Status != sessionstore.SubagentFailed {
		t.Errorf("durable child status = %q, want failed", meta.Status)
	}
}

func TestForegroundCompletionReleasesTranscriptBeforePublishing(t *testing.T) {
	task := newTestTaskTool(t, reasoningTextProvider{}, tool.NewRegistry(), "sys", "", "", nil)
	var ref string
	var releaseReplacement func()
	defer func() {
		if releaseReplacement != nil {
			releaseReplacement()
		}
	}()
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind != event.GraphDelta || e.Graph == nil {
			return
		}
		for _, node := range e.Graph.Nodes {
			if node.ID != "completed-call/sub-1" || node.State != agentgraph.StateCompleted {
				continue
			}
			ref = node.Ref
			if ref == "" || task.transcripts.held(ref) {
				t.Error("completed graph published while transcript still held")
				continue
			}
			var err error
			releaseReplacement, err = task.transcripts.lock(ref)
			if err != nil {
				t.Errorf("terminal callback could not reserve the released reference: %v", err)
			}
		}
	})
	ctx := agent.WithParentSession(agent.WithCallContext(t.Context(), "completed-call", sink, nil, false), "completed-parent")
	_, err := task.RunProfileSpec(ctx, ProfileExecSpec{
		Task: TaskSpec{Objective: "ordinary successful work"}, Grant: CapabilityGrant{AllowNoTools: true},
		Worker: WorkerSpec{Kind: "task", Name: "task", SystemPrompt: "sys"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if releaseReplacement == nil || !task.transcripts.held(ref) {
		t.Fatal("old run cleanup retired the callback's replacement reservation")
	}
}
