package control

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/hook"
	"reasonix/internal/provider"
	"reasonix/internal/store"
	"reasonix/internal/tool"
)

type shutdownBlockingRunner struct {
	started chan struct{}
	done    chan struct{}
	once    sync.Once
	clean   bool
}

type shutdownPromptRunner struct {
	controller *Controller
	ask        bool
	allowed    bool
	answers    []event.AskAnswer
	err        error
}

type immediateCountingRunner struct{ calls atomic.Int32 }

type markerObservingProvider struct {
	path    string
	streams [][]provider.Chunk
	mu      sync.Mutex
	starts  []time.Time
}

func (p *markerObservingProvider) Name() string { return "marker-observer" }

func (p *markerObservingProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	meta, ok, err := agent.LoadBranchMeta(p.path)
	if err != nil || !ok || meta.InFlightTurn == nil {
		return nil, errors.New("provider ran without a durable in-flight marker")
	}
	p.mu.Lock()
	index := len(p.starts)
	p.starts = append(p.starts, meta.InFlightTurn.StartedAt)
	p.mu.Unlock()
	if index >= len(p.streams) {
		index = len(p.streams) - 1
	}
	ch := make(chan provider.Chunk, len(p.streams[index]))
	for _, chunk := range p.streams[index] {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

type durableBlockingProvider struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

type displayOrderingProvider struct {
	displayRecorded *atomic.Bool
	calls           atomic.Int32
}

func (p *displayOrderingProvider) Name() string { return "display-ordering" }

func (p *displayOrderingProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.calls.Add(1)
	if !p.displayRecorded.Load() {
		return nil, errors.New("provider started before durable display mapping")
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func newDurableBlockingProvider() *durableBlockingProvider {
	return &durableBlockingProvider{started: make(chan struct{}), release: make(chan struct{})}
}

func (p *durableBlockingProvider) Name() string { return "durable-blocking" }

func (p *durableBlockingProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	p.once.Do(func() { close(p.started) })
	<-p.release
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "finished"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (r *immediateCountingRunner) Run(context.Context, string) error {
	r.calls.Add(1)
	return nil
}

func (r *shutdownPromptRunner) Run(ctx context.Context, _ string) error {
	if r.ask {
		r.answers, r.err = r.controller.Ask(ctx, []event.AskQuestion{{
			ID: "shutdown-choice", Prompt: "Pick one", Options: []event.AskOption{{Label: "A"}, {Label: "B"}},
		}})
		return r.err
	}
	r.allowed, _, r.err = gateApprover{c: r.controller}.Approve(ctx, "bash", "go test ./...", nil)
	return r.err
}

func newShutdownBlockingRunner() *shutdownBlockingRunner {
	return &shutdownBlockingRunner{started: make(chan struct{}), done: make(chan struct{})}
}

func (r *shutdownBlockingRunner) Run(ctx context.Context, _ string) error {
	r.once.Do(func() { close(r.started) })
	<-ctx.Done()
	close(r.done)
	if r.clean {
		return nil
	}
	return ctx.Err()
}

func waitForInFlightMarker(t *testing.T, path string, present bool) *agent.InFlightTurnMeta {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		meta, ok, err := agent.LoadBranchMeta(path)
		if err == nil && ok && (meta.InFlightTurn != nil) == present {
			return meta.InFlightTurn
		}
		time.Sleep(5 * time.Millisecond)
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	t.Fatalf("in-flight marker present=%v, want %v (meta ok=%v err=%v value=%+v)", meta.InFlightTurn != nil, present, ok, err, meta.InFlightTurn)
	return nil
}

func newPersistedRuntimeController(t *testing.T, runner *shutdownBlockingRunner) (*Controller, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return New(Options{Runner: runner, Sink: event.Discard, SessionDir: dir, SessionPath: path}), path
}

func TestPrepareRuntimeShutdownPreservesMarkerWithoutUserCancel(t *testing.T) {
	runner := newShutdownBlockingRunner()
	controller, path := newPersistedRuntimeController(t, runner)

	controller.SubmitUserTurn("keep the accepted prompt", "keep the accepted prompt")
	select {
	case <-runner.started:
	case <-time.After(3 * time.Second):
		t.Fatal("turn did not start")
	}
	marker := waitForInFlightMarker(t, path, true)
	if !marker.PreserveUser {
		t.Fatalf("in-flight marker = %+v, want visible-user preservation", marker)
	}

	controller.PrepareRuntimeShutdown()
	select {
	case <-runner.done:
	default:
		t.Fatal("runtime shutdown returned before its turn stack stopped")
	}
	if controller.CancelRequested() {
		t.Fatal("runtime shutdown was misclassified as an explicit user Cancel")
	}
	waitForInFlightMarker(t, path, true)
	controller.Close()
	waitForInFlightMarker(t, path, true)
}

func TestDurableTurnAdmissionSnapshotFailureStopsProvider(t *testing.T) {
	providerValue := &recordingProvider{streams: [][]provider.Chunk{textTurn("must not run")}}
	executor := agent.New(providerValue, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	// A directory cannot be atomically replaced by the transcript file, giving
	// the admission hook a deterministic persistence failure.
	unwritableSessionPath := t.TempDir()
	controller := New(Options{
		Runner: executor, Executor: executor, Sink: event.Discard,
		SessionDir: filepath.Dir(unwritableSessionPath), SessionPath: unwritableSessionPath,
	})
	if err := controller.EnableDurableTurnAdmission(); err != nil {
		t.Fatal(err)
	}
	_, err := controller.PrepareDurableTurn(DurableTurnInput{Input: "accepted only if durable", DisplayText: "accepted only if durable"})
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("PrepareDurableTurn error = %v, want precommit failure", err)
	}
	controller.Close()
	if len(providerValue.requests) != 0 {
		t.Fatalf("provider calls = %d, want 0 before durable admission", len(providerValue.requests))
	}
	history := controller.History()
	if len(history) != 1 || history[0].Role != provider.RoleSystem {
		t.Fatalf("failed admission history = %+v, want exact pre-admission transcript", history)
	}
}

func TestDurableTurnAdmissionListingMetaFailureContinuesProviderExactlyOnce(t *testing.T) {
	providerValue := &recordingProvider{streams: [][]provider.Chunk{textTurn("completed once")}}
	executor := agent.New(providerValue, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "listing-failure.jsonl")
	controller := New(Options{
		Runner: executor, Executor: executor, Sink: event.Discard,
		SessionDir: dir, SessionPath: sessionPath,
	})
	listingErr := errors.New("injected listing meta failure")
	controller.sessionMetaUpdate = func(string, string, string, int, bool) error { return listingErr }
	if err := controller.EnableDurableTurnAdmission(); err != nil {
		t.Fatal(err)
	}
	complete, err := controller.PrepareDurableTurn(DurableTurnInput{Input: "committed exactly once", DisplayText: "committed exactly once"})
	if err != nil {
		t.Fatal(err)
	}
	controller.SubmitUserTurn("committed exactly once", "committed exactly once")
	result := complete()
	if !result.Claimed || !result.SemanticCommit || result.Err != nil {
		t.Fatalf("durable listing failure result = %+v", result)
	}
	deadline := time.Now().Add(3 * time.Second)
	for controller.Running() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if controller.Running() {
		t.Fatal("Turn did not finish after committed listing failure")
	}
	controller.Close()
	if len(providerValue.requests) != 1 {
		t.Fatalf("provider calls = %d, want 1 after committed metadata failure", len(providerValue.requests))
	}
	history := controller.History()
	if len(history) != 3 || history[0].Role != provider.RoleSystem || history[1].Role != provider.RoleUser || history[1].Content != "committed exactly once" ||
		history[2].Role != provider.RoleAssistant || history[2].Content != "completed once" {
		t.Fatalf("committed admission history = %+v", history)
	}
	loaded, err := agent.LoadSession(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if disk := loaded.Snapshot(); len(disk) != 3 || disk[1].Role != provider.RoleUser || disk[1].Content != "committed exactly once" ||
		disk[2].Role != provider.RoleAssistant || disk[2].Content != "completed once" {
		t.Fatalf("committed admission disk history = %+v", disk)
	}
	waitForInFlightMarker(t, sessionPath, false)
}

func TestDurableTurnAdmissionMarkerFailureStopsBeforeUserAndProvider(t *testing.T) {
	providerValue := &recordingProvider{streams: [][]provider.Chunk{textTurn("must not run")}}
	executor := agent.New(providerValue, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "marker-failure.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// A directory at the metadata path makes the crash-recovery marker fail
	// while leaving the transcript path itself writable.
	if err := os.Mkdir(agent.BranchMetaPath(sessionPath), 0o700); err != nil {
		t.Fatal(err)
	}
	controller := New(Options{
		Runner: executor, Executor: executor, Sink: event.Discard,
		SessionDir: dir, SessionPath: sessionPath,
	})
	if err := controller.EnableDurableTurnAdmission(); err != nil {
		t.Fatal(err)
	}
	_, err := controller.PrepareDurableTurn(DurableTurnInput{Input: "must have a recovery marker", DisplayText: "must have a recovery marker"})
	if err == nil || !strings.Contains(err.Error(), "persist in-flight Turn marker") {
		t.Fatalf("PrepareDurableTurn marker error = %v", err)
	}
	controller.Close()
	if len(providerValue.requests) != 0 {
		t.Fatalf("provider calls = %d, want 0 before durable marker", len(providerValue.requests))
	}
	if history := controller.History(); len(history) != 1 || history[0].Role != provider.RoleSystem {
		t.Fatalf("marker failure history = %+v, want exact pre-admission transcript", history)
	}
	info, err := os.Stat(sessionPath)
	if err != nil || info.Size() != 0 {
		t.Fatalf("marker failure transcript stat = %+v, %v; want zero-byte anchor", info, err)
	}
}

func TestDurableTurnAdmissionMissingSystemStopsBeforeProvider(t *testing.T) {
	providerValue := &recordingProvider{streams: [][]provider.Chunk{textTurn("must not run")}}
	executor := agent.New(providerValue, tool.NewRegistry(), agent.NewSession(""), agent.Options{}, event.Discard)
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "missing-system.jsonl")
	if err := os.WriteFile(sessionPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	controller := New(Options{
		Runner: executor, Executor: executor, Sink: event.Discard,
		SessionDir: dir, SessionPath: sessionPath,
	})
	if err := controller.EnableDurableTurnAdmission(); err != nil {
		t.Fatal(err)
	}
	_, err := controller.PrepareDurableTurn(DurableTurnInput{Input: "must have a system contract", DisplayText: "must have a system contract"})
	if err == nil || !strings.Contains(err.Error(), "no leading system message") {
		t.Fatalf("PrepareDurableTurn missing-system error = %v", err)
	}
	controller.Close()
	if len(providerValue.requests) != 0 {
		t.Fatalf("provider calls = %d, want 0 without a system contract", len(providerValue.requests))
	}
	if history := controller.History(); len(history) != 0 {
		t.Fatalf("missing-system failure history = %+v, want empty pre-admission transcript", history)
	}
	info, err := os.Stat(sessionPath)
	if err != nil || info.Size() != 0 {
		t.Fatalf("missing-system transcript stat = %+v, %v; want zero-byte anchor", info, err)
	}
}

func TestPrepareDurableTurnCommitsBeforeHookGoalAndCheckpointWork(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ordered.jsonl")
	var displayRecorded atomic.Bool
	providerValue := &displayOrderingProvider{displayRecorded: &displayRecorded}
	executor := agent.New(providerValue, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	hookObserved := make(chan error, 1)
	hooks := hook.NewRunner([]hook.ResolvedHook{{
		HookConfig: hook.HookConfig{Command: "observe"}, Event: hook.UserPromptSubmit, Scope: hook.ScopeProject,
	}}, "", func(context.Context, hook.SpawnInput) hook.SpawnResult {
		loaded, err := agent.LoadSession(path)
		if err == nil {
			messages := loaded.Snapshot()
			if len(messages) != 2 || messages[1].Role != provider.RoleUser || messages[1].Content != "visible anchor" {
				err = errors.New("hook did not observe the committed stable user anchor")
			}
		}
		if err == nil {
			meta, ok, metaErr := agent.LoadBranchMeta(path)
			if metaErr != nil || !ok || meta.InFlightTurn == nil {
				err = errors.New("hook did not observe the durable marker")
			}
		}
		hookObserved <- err
		return hook.SpawnResult{ExitCode: 0}
	}, nil)
	turnDone := make(chan event.Event, 1)
	controller := New(Options{
		Runner: executor, Executor: executor, Hooks: hooks,
		Sink: event.FuncSink(func(value event.Event) {
			if value.Kind == event.TurnDone {
				turnDone <- value
			}
		}),
		SessionDir: dir, SessionPath: path,
	})
	var recordedContent, recordedDisplay string
	controller.SetDisplayRecorder(func(content, display string) {
		recordedContent, recordedDisplay = content, display
		displayRecorded.Store(true)
	})
	if err := controller.EnableDurableTurnAdmission(); err != nil {
		t.Fatal(err)
	}
	complete, err := controller.PrepareDurableTurn(DurableTurnInput{
		Input: "raw model input", DisplayText: "visible anchor", EditedOriginal: "before edit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if providerValue.calls.Load() != 0 || displayRecorded.Load() || len(controller.Checkpoints()) != 0 || controller.Goal() != "" {
		t.Fatal("PrepareDurableTurn executed provider, checkpoint, or Goal work")
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	messages := loaded.Snapshot()
	if len(messages) != 2 || messages[1].Content != "visible anchor" || !messages[1].Edited || messages[1].Original != "before edit" {
		t.Fatalf("prepared disk anchor = %+v", messages)
	}

	controller.SubmitUserTurn("raw model input", "visible anchor")
	if result := complete(); !result.Claimed || !result.SemanticCommit || result.Err != nil {
		t.Fatalf("prepared admission result = %+v", result)
	}
	select {
	case err := <-hookObserved:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("prompt hook did not run")
	}
	select {
	case done := <-turnDone:
		if done.Err != nil {
			t.Fatalf("TurnDone error = %v", done.Err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("TurnDone not emitted")
	}
	if !displayRecorded.Load() || recordedContent != "raw model input" || recordedDisplay != "visible anchor" {
		t.Fatalf("durable display mapping = (%q, %q), want composed content before provider", recordedContent, recordedDisplay)
	}
	turns := controller.CheckpointTurnsByMessageIndex()
	if len(turns) != 1 || turns[1] != 0 {
		t.Fatalf("prepared checkpoint boundaries = %v, want {1:0}", turns)
	}
}

func TestPreparedGoalContinuationOwnsOneTopLevelMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goal-marker.jsonl")
	providerValue := &markerObservingProvider{path: path, streams: [][]provider.Chunk{
		textTurn("working\n\n[goal:continue]"),
		textTurn("done\n\n[goal:complete]"),
	}}
	executor := agent.New(providerValue, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	turnDone := make(chan event.Event, 1)
	controller := New(Options{
		Runner: executor, Executor: executor, SessionDir: dir, SessionPath: path,
		Sink: event.FuncSink(func(value event.Event) {
			if value.Kind == event.TurnDone {
				turnDone <- value
			}
		}),
	})
	controller.SetGoal("finish two rounds")
	if err := controller.EnableDurableTurnAdmission(); err != nil {
		t.Fatal(err)
	}
	complete, err := controller.PrepareDurableTurn(DurableTurnInput{Input: "start", DisplayText: "start"})
	if err != nil {
		t.Fatal(err)
	}
	controller.SubmitUserTurn("start", "start")
	if result := complete(); result.Err != nil || !result.Claimed || !result.SemanticCommit {
		t.Fatalf("prepared admission result = %+v", result)
	}
	select {
	case done := <-turnDone:
		if done.Err != nil {
			t.Fatal(done.Err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("goal Turn did not finish")
	}
	providerValue.mu.Lock()
	starts := append([]time.Time(nil), providerValue.starts...)
	providerValue.mu.Unlock()
	if len(starts) != 2 || !starts[0].Equal(starts[1]) {
		t.Fatalf("goal marker starts = %v, want one unchanged top-level marker", starts)
	}
	waitForInFlightMarker(t, path, false)
	if turns := controller.CheckpointTurnsByMessageIndex(); len(turns) != 1 || turns[1] != 0 {
		t.Fatalf("goal checkpoint boundaries = %v, want only outer boundary {1:0}", turns)
	}
}

func TestPreparedDurableTurnClearFailureBecomesTurnErrorAndPoisonsController(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clear-failure.jsonl")
	providerValue := newDurableBlockingProvider()
	executor := agent.New(providerValue, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	turnDone := make(chan event.Event, 1)
	controller := New(Options{
		Runner: executor, Executor: executor, SessionDir: dir, SessionPath: path,
		Sink: event.FuncSink(func(value event.Event) {
			if value.Kind == event.TurnDone {
				turnDone <- value
			}
		}),
	})
	if err := controller.EnableDurableTurnAdmission(); err != nil {
		t.Fatal(err)
	}
	complete, err := controller.PrepareDurableTurn(DurableTurnInput{Input: "run", DisplayText: "run"})
	if err != nil {
		t.Fatal(err)
	}
	controller.SubmitUserTurn("run", "run")
	if result := complete(); result.Err != nil || !result.SemanticCommit {
		t.Fatalf("prepared admission result = %+v", result)
	}
	select {
	case <-providerValue.started:
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not start")
	}
	clearErr := errors.New("injected marker clear failure")
	controller.mu.Lock()
	controller.sessionInFlightClear = func(string) error { return clearErr }
	controller.mu.Unlock()
	close(providerValue.release)
	select {
	case done := <-turnDone:
		if done.Err == nil || !strings.Contains(done.Err.Error(), clearErr.Error()) || done.Outcome == "completed" {
			t.Fatalf("TurnDone = %+v, want marker-clear failure", done)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("TurnDone not emitted")
	}
	waitForInFlightMarker(t, path, true)
	if _, err := controller.PrepareDurableTurn(DurableTurnInput{Input: "must reject"}); err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("Prepare after clear failure = %v, want poisoned rejection", err)
	}
}

func TestPreparedDurableTurnFinalSnapshotFailureBecomesTurnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "final-snapshot-error.jsonl")
	providerValue := newDurableBlockingProvider()
	executor := agent.New(providerValue, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	turnDone := make(chan event.Event, 1)
	controller := New(Options{
		Runner: executor, Executor: executor, SessionDir: dir, SessionPath: path,
		Sink: event.FuncSink(func(value event.Event) {
			if value.Kind == event.TurnDone {
				turnDone <- value
			}
		}),
	})
	if err := controller.EnableDurableTurnAdmission(); err != nil {
		t.Fatal(err)
	}
	complete, err := controller.PrepareDurableTurn(DurableTurnInput{Input: "run", DisplayText: "run"})
	if err != nil {
		t.Fatal(err)
	}
	controller.SubmitUserTurn("run", "run")
	if result := complete(); result.Err != nil || !result.SemanticCommit {
		t.Fatalf("prepared admission result = %+v", result)
	}
	select {
	case <-providerValue.started:
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not start")
	}
	logPath := store.SessionEventLog(path)
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(logPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	close(providerValue.release)
	select {
	case done := <-turnDone:
		if done.Err == nil || !strings.Contains(done.Err.Error(), "persist final durable Turn transcript") || done.Outcome == "completed" {
			t.Fatalf("TurnDone = %+v, want final transcript error", done)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("TurnDone not emitted")
	}
	waitForInFlightMarker(t, path, true)
	if _, err := controller.PrepareDurableTurn(DurableTurnInput{Input: "must reject"}); err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("Prepare after final snapshot failure = %v, want poisoned rejection", err)
	}
}

func TestPreparePrecommitClearFailurePoisonsController(t *testing.T) {
	unwritablePath := t.TempDir()
	executor := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	controller := New(Options{Executor: executor, SessionDir: filepath.Dir(unwritablePath), SessionPath: unwritablePath})
	controller.mu.Lock()
	controller.sessionInFlightClear = func(string) error { return errors.New("clear unavailable") }
	controller.mu.Unlock()
	if err := controller.EnableDurableTurnAdmission(); err != nil {
		t.Fatal(err)
	}
	if _, err := controller.PrepareDurableTurn(DurableTurnInput{Input: "first"}); err == nil || !strings.Contains(err.Error(), "clear unavailable") {
		t.Fatalf("precommit failure = %v, want joined clear error", err)
	}
	if _, err := controller.PrepareDurableTurn(DurableTurnInput{Input: "second"}); err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("Prepare after poisoned rollback = %v", err)
	}
}

func TestPrepareRuntimeShutdownDoesNotMisreportConcurrentSuccessfulTurn(t *testing.T) {
	runner := newShutdownBlockingRunner()
	runner.clean = true
	controller, path := newPersistedRuntimeController(t, runner)

	controller.SubmitUserTurn("finishes at shutdown", "finishes at shutdown")
	select {
	case <-runner.started:
	case <-time.After(3 * time.Second):
		t.Fatal("turn did not start")
	}
	waitForInFlightMarker(t, path, true)
	controller.PrepareRuntimeShutdown()
	waitForInFlightMarker(t, path, false)
	controller.Close()
}

func TestPrepareRuntimeShutdownTerminatesPromptsWithoutAnswering(t *testing.T) {
	for _, test := range []struct {
		name string
		ask  bool
	}{
		{name: "approval"},
		{name: "ask", ask: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "prompt.jsonl")
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			promptSeen := make(chan struct{}, 1)
			controller := New(Options{SessionDir: dir, SessionPath: path, Sink: event.FuncSink(func(value event.Event) {
				if value.Kind == event.ApprovalRequest || value.Kind == event.AskRequest {
					promptSeen <- struct{}{}
				}
			})})
			runner := &shutdownPromptRunner{controller: controller, ask: test.ask}
			controller.runner = runner
			controller.EnableInteractiveApproval()
			controller.SubmitUserTurn("wait for a user decision", "wait for a user decision")
			select {
			case <-promptSeen:
			case <-time.After(3 * time.Second):
				t.Fatal("prompt was not emitted")
			}
			waitForInFlightMarker(t, path, true)
			if status := controller.RuntimeStatus(); !status.PendingPrompt {
				t.Fatalf("status before shutdown = %+v, want pending Prompt", status)
			}

			controller.PrepareRuntimeShutdown()
			if !errors.Is(runner.err, context.Canceled) || runner.allowed || len(runner.answers) != 0 {
				t.Fatalf("shutdown prompt result allowed=%v answers=%+v err=%v", runner.allowed, runner.answers, runner.err)
			}
			if status := controller.RuntimeStatus(); status.PendingPrompt || status.CancelRequested {
				t.Fatalf("status after shutdown = %+v, want no prompt and no user-cancel classification", status)
			}
			waitForInFlightMarker(t, path, true)
			controller.Close()
		})
	}
}

func TestPrepareRuntimeShutdownSealsFinishingBeforeParkedTurnCanSpawn(t *testing.T) {
	runner := &immediateCountingRunner{}
	turnDoneEntered := make(chan struct{})
	releaseTurnDone := make(chan struct{})
	var once sync.Once
	controller := New(Options{Runner: runner, Sink: event.FuncSink(func(value event.Event) {
		if value.Kind == event.TurnDone {
			once.Do(func() {
				close(turnDoneEntered)
				<-releaseTurnDone
			})
		}
	})})
	controller.Send("first")
	select {
	case <-turnDoneEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("first turn did not enter the finishing window")
	}
	// Admission during TurnDone delivery parks this body. Shutdown must clear it
	// while holding the same run-state lock before waiting on turnWG.
	controller.Send("must never start")
	prepared := make(chan struct{})
	go func() {
		controller.PrepareRuntimeShutdown()
		close(prepared)
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		controller.mu.Lock()
		closed := controller.closed
		parked := len(controller.parkedTurns)
		controller.mu.Unlock()
		if closed {
			if parked != 0 {
				t.Fatalf("PrepareRuntimeShutdown left %d parked turns after sealing admission", parked)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("PrepareRuntimeShutdown did not seal admission")
		}
		time.Sleep(time.Millisecond)
	}
	close(releaseTurnDone)
	select {
	case <-prepared:
	case <-time.After(3 * time.Second):
		t.Fatal("PrepareRuntimeShutdown did not finish after TurnDone delivery")
	}
	if got := runner.calls.Load(); got != 1 {
		t.Fatalf("runner calls = %d, parked turn spawned after runtime shutdown", got)
	}
	controller.Close()
}

func TestPrepareRuntimeShutdownRacesInitialAdmissionWithoutWaitGroupAdd(t *testing.T) {
	for iteration := 0; iteration < 200; iteration++ {
		runner := newShutdownBlockingRunner()
		controller := New(Options{Runner: runner, Sink: event.Discard})
		start := make(chan struct{})
		var workers sync.WaitGroup
		workers.Add(2)
		go func() {
			defer workers.Done()
			<-start
			controller.SubmitUserTurn("race admission", "race admission")
		}()
		go func() {
			defer workers.Done()
			<-start
			controller.PrepareRuntimeShutdown()
		}()
		close(start)
		workers.Wait()
		controller.Close()
	}
}

func TestPrepareRuntimeShutdownWaitsForBlockedDurablePreparation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blocked-prepare.jsonl")
	executor := agent.New(nil, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	controller := New(Options{Executor: executor, SessionDir: dir, SessionPath: path})
	metaStarted := make(chan struct{})
	releaseMeta := make(chan struct{})
	controller.sessionMetaUpdate = func(string, string, string, int, bool) error {
		close(metaStarted)
		<-releaseMeta
		return nil
	}
	if err := controller.EnableDurableTurnAdmission(); err != nil {
		t.Fatal(err)
	}
	type prepareResult struct {
		complete func() DurableTurnAdmissionResult
		err      error
	}
	prepared := make(chan prepareResult, 1)
	go func() {
		complete, err := controller.PrepareDurableTurn(DurableTurnInput{Input: "accepted while shutdown races"})
		prepared <- prepareResult{complete: complete, err: err}
	}()
	select {
	case <-metaStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("durable preparation did not reach blocked metadata write")
	}
	shutdownDone := make(chan struct{})
	go func() {
		controller.PrepareRuntimeShutdown()
		close(shutdownDone)
	}()
	select {
	case <-shutdownDone:
		t.Fatal("runtime shutdown returned while durable preparation was still in flight")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseMeta)
	var result prepareResult
	select {
	case result = <-prepared:
		if result.err != nil || result.complete == nil {
			t.Fatalf("PrepareDurableTurn = %v", result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("durable preparation did not finish")
	}
	select {
	case <-shutdownDone:
	case <-time.After(3 * time.Second):
		t.Fatal("runtime shutdown did not finish after durable preparation")
	}
	admission := result.complete()
	if admission.SemanticCommit != true || admission.Claimed || admission.Err == nil {
		t.Fatalf("shutdown-raced unclaimed admission = %+v", admission)
	}
	waitForInFlightMarker(t, path, true)
}

func TestExplicitUserCancelStillClearsMarker(t *testing.T) {
	runner := newShutdownBlockingRunner()
	controller, path := newPersistedRuntimeController(t, runner)

	controller.SubmitUserTurn("cancel this turn", "cancel this turn")
	select {
	case <-runner.started:
	case <-time.After(3 * time.Second):
		t.Fatal("turn did not start")
	}
	waitForInFlightMarker(t, path, true)
	if got := controller.TryCancel(); got != CancelRequestedNow {
		t.Fatalf("TryCancel = %q, want %q", got, CancelRequestedNow)
	}
	select {
	case <-runner.done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled turn did not stop")
	}
	waitForInFlightMarker(t, path, false)
	controller.Close()
}

func TestResumeWithRecoveryReportsRepairAndDoesNotRepeat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interrupted.jsonl")
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "accepted user prompt"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "partial assistant tail"})
	if err := session.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkSessionInFlightTurn(path, 1, true); err != nil {
		t.Fatal(err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	executor := agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard)
	controller := New(Options{Executor: executor, SessionDir: dir, SessionPath: path})
	state, err := controller.ResumeWithRecovery(loaded, path)
	if err != nil {
		t.Fatal(err)
	}
	if !state.PreviousTurnInterrupted {
		t.Fatal("ResumeWithRecovery did not report the consumed in-flight marker")
	}
	messages := executor.Session().Snapshot()
	if len(messages) != 2 || messages[1].Role != provider.RoleUser || messages[1].Content != "accepted user prompt" {
		t.Fatalf("repaired transcript = %+v, want system plus preserved real user prompt", messages)
	}
	waitForInFlightMarker(t, path, false)

	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if repeated, err := controller.ResumeWithRecovery(reloaded, path); err != nil || repeated.PreviousTurnInterrupted {
		t.Fatal("already-consumed recovery marker was reported a second time")
	}
}

func TestResumeWithRecoveryClearsMarkerBeforeUserWithoutInterruptedOutcome(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marker-only.jsonl")
	session := agent.NewSession("system")
	// Persist an ordinary transcript, then simulate a kill in the exact window
	// after marker write and before the stable user append/snapshot.
	if err := session.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkSessionInFlightTurn(path, session.Len(), true); err != nil {
		t.Fatal(err)
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	executor := agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard)
	controller := New(Options{Executor: executor, SessionDir: dir, SessionPath: path})
	state, err := controller.ResumeWithRecovery(loaded, path)
	if err != nil {
		t.Fatal(err)
	}
	if state.PreviousTurnInterrupted {
		t.Fatal("marker-before-user crash was reported as an accepted interrupted Turn")
	}
	waitForInFlightMarker(t, path, false)
}

func TestResumeWithRecoveryClearFailureRejectsRuntimeAndKeepsMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recovery-clear-failure.jsonl")
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "accepted"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "partial"})
	if err := session.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkSessionInFlightTurn(path, 1, true); err != nil {
		t.Fatal(err)
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	executor := agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard)
	controller := New(Options{Executor: executor, SessionDir: dir, SessionPath: path})
	clearErr := errors.New("injected recovery clear failure")
	controller.sessionInFlightClear = func(string) error { return clearErr }
	state, err := controller.ResumeWithRecovery(loaded, path)
	if !state.PreviousTurnInterrupted || err == nil || !strings.Contains(err.Error(), clearErr.Error()) {
		t.Fatalf("ResumeWithRecovery = %+v, %v", state, err)
	}
	waitForInFlightMarker(t, path, true)

	// A fresh runtime can retry the idempotent repair and reports the accepted
	// interruption exactly once only after marker clear succeeds.
	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	retryExecutor := agent.New(nil, nil, agent.NewSession("system"), agent.Options{}, event.Discard)
	retry := New(Options{Executor: retryExecutor, SessionDir: dir, SessionPath: path})
	retryState, err := retry.ResumeWithRecovery(reloaded, path)
	if err != nil || !retryState.PreviousTurnInterrupted {
		t.Fatalf("retry ResumeWithRecovery = %+v, %v", retryState, err)
	}
	waitForInFlightMarker(t, path, false)
}

func TestFinalDurableTurnSnapshotPrecedesMarkerClear(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "final-durable.jsonl")
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "accepted"})
	if err := session.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkSessionInFlightTurn(path, 1, true); err != nil {
		t.Fatal(err)
	}
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "complete answer"})
	executor := agent.New(nil, nil, session, agent.Options{}, event.Discard)
	controller := New(Options{Executor: executor, SessionDir: dir, SessionPath: path})
	controller.mu.Lock()
	controller.durableTurnAdmission = true
	controller.mu.Unlock()

	controller.finishInFlightTurn(nil)
	waitForInFlightMarker(t, path, false)
	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if messages := reloaded.Snapshot(); len(messages) != 3 || messages[2].Content != "complete answer" {
		t.Fatalf("final durable transcript = %+v", messages)
	}
}

func TestFinalDurableTurnSnapshotFailureKeepsMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "final-durable-failure.jsonl")
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "accepted"})
	if err := session.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkSessionInFlightTurn(path, 1, true); err != nil {
		t.Fatal(err)
	}
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "not durable"})
	logPath := store.SessionEventLog(path)
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(logPath, 0o700); err != nil {
		t.Fatal(err)
	}
	// A non-native event-log occupant deliberately falls back to the JSONL
	// anchor, so block that authoritative path too. This makes the failure
	// genuinely pre-commit instead of a successful checkpoint-only save.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	executor := agent.New(nil, nil, session, agent.Options{}, event.Discard)
	controller := New(Options{Executor: executor, SessionDir: dir, SessionPath: path})
	controller.mu.Lock()
	controller.durableTurnAdmission = true
	controller.mu.Unlock()

	controller.finishInFlightTurn(nil)
	waitForInFlightMarker(t, path, true)
}

func TestInterruptedRecoverySnapshotFailureKeepsMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "interrupted-repair-failure.jsonl")
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "accepted"})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "partial tail"})
	if err := session.SaveSnapshot(path); err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkSessionInFlightTurn(path, 1, true); err != nil {
		t.Fatal(err)
	}
	logPath := store.SessionEventLog(path)
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(logPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	loaded := agent.NewSession("system")
	loaded.Add(provider.Message{Role: provider.RoleUser, Content: "accepted"})
	loaded.Add(provider.Message{Role: provider.RoleAssistant, Content: "partial tail"})
	executor := agent.New(nil, nil, loaded, agent.Options{}, event.Discard)
	controller := New(Options{Executor: executor, SessionDir: dir, SessionPath: path})

	if interrupted, err := controller.recoverInterruptedTurn(path); !interrupted || err == nil {
		t.Fatal("recovery marker was not recognized")
	}
	waitForInFlightMarker(t, path, true)
	if messages := executor.Session().Snapshot(); len(messages) != 2 || messages[1].Role != provider.RoleUser {
		t.Fatalf("in-memory recovery did not strip partial tail: %+v", messages)
	}
}
