package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/checkpoint"
	"reasonix/internal/command"
	"reasonix/internal/event"
	"reasonix/internal/guardian"
	"reasonix/internal/hook"
	"reasonix/internal/i18n"
	"reasonix/internal/jobs"
	"reasonix/internal/permission"
	"reasonix/internal/plugin"
	"reasonix/internal/pluginpkg"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

type typedNilControllerSink struct{}

func (*typedNilControllerSink) Emit(event.Event) {}

func isolateControlConfigHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	t.Chdir(t.TempDir())
	return home
}

type appendingRunner struct {
	session *agent.Session
}

func (r appendingRunner) Run(_ context.Context, input string) error {
	r.session.Add(provider.Message{Role: provider.RoleUser, Content: input})
	return nil
}

type handoffRunner struct {
	session *agent.Session
}

func (r handoffRunner) Run(_ context.Context, input string) error {
	r.session.Add(provider.Message{Role: provider.RoleUser, Content: "handoff: " + input})
	return nil
}

type sessionContextRunner struct {
	parentSession string
	jobSession    string
}

func (r *sessionContextRunner) Run(ctx context.Context, input string) error {
	r.parentSession = agent.ParentSession(ctx)
	r.jobSession = jobs.SessionFromContext(ctx)
	return nil
}

type cancelingRunner struct {
	cancel context.CancelFunc
}

func (r cancelingRunner) Run(_ context.Context, _ string) error {
	r.cancel()
	return nil
}

func TestContextSnapshotIncludesCompletionTokens(t *testing.T) {
	prov := &scriptedTurns{turns: [][]provider.Chunk{{
		{Type: provider.ChunkText, Text: "ok"},
		{Type: provider.ChunkUsage, Usage: &provider.Usage{
			PromptTokens:     6840,
			CompletionTokens: 48,
			TotalTokens:      6888,
			ReasoningTokens:  48,
		}},
		{Type: provider.ChunkDone},
	}}}
	ag := agent.New(prov, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{ContextWindow: 1_000_000}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag})

	if err := c.Run(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}

	used, window := c.ContextSnapshot()
	if used != 6888 || window != 1_000_000 {
		t.Fatalf("ContextSnapshot() = (%d, %d), want (6888, 1000000)", used, window)
	}
}

type fakeControlTool struct{ name string }

func (t fakeControlTool) Name() string { return t.name }
func (fakeControlTool) Description() string {
	return "fake"
}
func (fakeControlTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (fakeControlTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}
func (fakeControlTool) ReadOnly() bool { return true }

type startBackgroundJobTool struct {
	started chan string
	release chan struct{}
}

func (t startBackgroundJobTool) Name() string        { return "start_background_job" }
func (t startBackgroundJobTool) Description() string { return "start background job" }
func (t startBackgroundJobTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t startBackgroundJobTool) ReadOnly() bool { return false }
func (t startBackgroundJobTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	jm, ok := jobs.FromContext(ctx)
	if !ok {
		return "", nil
	}
	j := jm.StartForSession(jobs.SessionFromContext(ctx), "bash", "controller", func(_ context.Context, out io.Writer) (string, error) {
		_, _ = io.WriteString(out, "before\n")
		<-t.release
		_, _ = io.WriteString(out, "after\n")
		return "", nil
	})
	t.started <- j.ID
	return "started " + j.ID, nil
}

type recordingProvider struct {
	name     string
	streams  [][]provider.Chunk
	requests []provider.Request
}

func (p *recordingProvider) Name() string {
	if p.name != "" {
		return p.name
	}
	return "recording"
}

func (p *recordingProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests = append(p.requests, req)
	i := len(p.requests) - 1
	if i >= len(p.streams) {
		i = len(p.streams) - 1
	}
	chunks := p.streams[i]
	ch := make(chan provider.Chunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func requestMessagesText(messages []provider.Message) string {
	var b strings.Builder
	for _, m := range messages {
		b.WriteString(string(m.Role))
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

func lastUserMessage(messages []provider.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == provider.RoleUser {
			return messages[i].Content
		}
	}
	return ""
}

func TestNewTreatsTypedNilSinkAsDiscard(t *testing.T) {
	var sink *typedNilControllerSink
	c := New(Options{Sink: sink})

	c.notice("typed nil sink should not panic")
}

func TestClearSessionMarksCleanupPendingBeforeReturningForRunningJobs(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.jsonl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte(`{"role":"user","content":"old"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	jm := jobs.NewManager(event.Discard)
	release := make(chan struct{})
	started := make(chan struct{})
	defer func() {
		close(release)
		jm.Close()
	}()
	jm.StartForSession(agent.BranchID(oldPath), "task", "stuck clear", func(ctx context.Context, _ io.Writer) (string, error) {
		close(started)
		<-ctx.Done()
		<-release
		return "", ctx.Err()
	})
	select {
	case <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("background job never started")
	}

	ctrl := New(Options{Executor: exec, SessionDir: dir, SessionPath: oldPath, Label: "test", Jobs: jm})
	if err := ctrl.ClearSession(); err != nil {
		t.Fatalf("ClearSession: %v", err)
	}
	if !agent.IsCleanupPending(oldPath) {
		t.Fatalf("old session should be cleanup-pending before ClearSession returns")
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("old session file should remain until delayed cleanup: %v", err)
	}
	sessions, err := agent.ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range sessions {
		if filepath.Clean(session.Path) == filepath.Clean(oldPath) {
			t.Fatalf("cleanup-pending old session still listed: %+v", sessions)
		}
	}
}

func TestClearSessionQueuesSessionStartHookContext(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.jsonl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte(`{"role":"user","content":"old"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	hooks := hook.NewRunner([]hook.ResolvedHook{{
		HookConfig: hook.HookConfig{Command: "session-start"},
		Event:      hook.SessionStart,
	}}, dir, func(context.Context, hook.SpawnInput) hook.SpawnResult {
		return hook.SpawnResult{ExitCode: 0, Stdout: "clear session context"}
	}, nil)
	c := New(Options{Executor: exec, SystemPrompt: "sys", SessionDir: dir, SessionPath: oldPath, Label: "test", Hooks: hooks})

	if err := c.ClearSession(); err != nil {
		t.Fatalf("ClearSession: %v", err)
	}
	got := c.Compose("next")
	if !strings.Contains(got, `<hook-context event="SessionStart">`) || !strings.Contains(got, "clear session context") || !strings.HasSuffix(got, "next") {
		t.Fatalf("clear session did not queue SessionStart hook context: %q", got)
	}
}

func TestReconcileCleanupPendingRemovesOrphanedArtifacts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "orphan.jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"orphan"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{Name: "orphan"}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(jobs.ArtifactDir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobs.ArtifactDir(path), "job.log"), []byte("job output"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ckptDir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkCleanupPending(path, "delete"); err != nil {
		t.Fatal(err)
	}

	if err := ReconcileCleanupPending(dir); err != nil {
		t.Fatalf("ReconcileCleanupPending: %v", err)
	}
	for _, p := range []string{path, agent.BranchMetaPath(path), jobs.ArtifactDir(path), ckptDir(path), agent.CleanupPendingPath(path)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after reconciliation (err=%v)", p, err)
		}
	}
}

func TestTurnOutcomeClassifiesFinalReadiness(t *testing.T) {
	err := &agent.FinalReadinessError{Attempts: 3, Reason: "missing verification"}
	if got := turnOutcome(err); got != event.TurnOutcomeFinalReadiness {
		t.Fatalf("turnOutcome() = %q, want %q", got, event.TurnOutcomeFinalReadiness)
	}
	if got := turnOutcome(errors.New("provider failed")); got != "" {
		t.Fatalf("ordinary turn outcome = %q, want empty", got)
	}
}

func TestRunTurnSnapshotsActivityWhenTranscriptChanges(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	path := filepath.Join(dir, "session.jsonl")
	c := New(Options{Runner: appendingRunner{session: sess}, Executor: exec, SessionDir: dir, SessionPath: path, Label: "test"})

	if err := c.runTurn(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 2 {
		t.Fatalf("saved messages = %d, want system + user", len(loaded.Messages))
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("load activity meta ok=%v err=%v", ok, err)
	}
	if meta.UpdatedAt.IsZero() {
		t.Fatal("activity meta should be marked")
	}
}

func TestRunInjectsParentSessionForJobs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	runner := &sessionContextRunner{}
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Runner: runner, Executor: exec, SessionDir: dir, SessionPath: path, Label: "test"})

	if err := c.Run(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	want := agent.BranchID(path)
	if runner.parentSession != want {
		t.Fatalf("ParentSession = %q, want %q", runner.parentSession, want)
	}
	if runner.jobSession != want {
		t.Fatalf("jobs session = %q, want %q", runner.jobSession, want)
	}
}

func TestRunStopHookIgnoresCanceledCallerContext(t *testing.T) {
	runCtx, cancel := context.WithCancel(context.Background())
	var stopCalls int
	var stopErr error
	hooks := hook.NewRunner([]hook.ResolvedHook{{
		HookConfig: hook.HookConfig{Command: "record-stop"},
		Event:      hook.Stop,
		Scope:      hook.ScopeProject,
	}}, "", func(ctx context.Context, in hook.SpawnInput) hook.SpawnResult {
		stopCalls++
		stopErr = ctx.Err()
		return hook.SpawnResult{ExitCode: 0}
	}, nil)
	c := New(Options{
		Runner: cancelingRunner{cancel: cancel},
		Hooks:  hooks,
	})

	if err := c.Run(runCtx, "hello"); err != nil {
		t.Fatal(err)
	}

	if runCtx.Err() != context.Canceled {
		t.Fatalf("caller context err = %v, want %v", runCtx.Err(), context.Canceled)
	}
	if stopCalls != 1 {
		t.Fatalf("Stop hook calls = %d, want 1", stopCalls)
	}
	if stopErr != nil {
		t.Fatalf("Stop hook context err = %v, want nil", stopErr)
	}
}

func TestSetSessionPathAdoptsTemporaryBackgroundJobs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	started := make(chan string, 1)
	release := make(chan struct{})
	jm := jobs.NewManager(event.Discard)
	reg := tool.NewRegistry()
	reg.Add(startBackgroundJobTool{started: started, release: release})
	prov := &scriptedTurns{turns: [][]provider.Chunk{
		toolCallTurn("call-1", "start_background_job", `{}`),
		textTurn("done"),
	}}
	ag := agent.New(prov, reg, agent.NewSession("sys"), agent.Options{Jobs: jm}, event.Discard)
	c := New(Options{Runner: ag, Executor: ag, SessionDir: dir, Label: "test", Jobs: jm})
	defer c.Close()

	if err := c.Run(context.Background(), "start background job"); err != nil {
		t.Fatal(err)
	}
	jobID := <-started
	c.SetSessionPath(path)
	close(release)

	parentSession := agent.BranchID(path)
	res := c.jobs.WaitForSession(context.Background(), parentSession, []string{jobID}, 1)
	if len(res) != 1 || !strings.Contains(res[0].Output, "before\n") || !strings.Contains(res[0].Output, "after\n") {
		t.Fatalf("adopted controller job = %+v, want before/after output", res)
	}
	if _, err := os.Stat(filepath.Join(jobs.ArtifactDir(path), jobID+".log")); err != nil {
		t.Fatalf("controller job artifact should be under persistent sidecar: %v", err)
	}
}

func TestGoalStatePersistsNextToSessionPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test"})

	c.SetGoalWithResearchMode("fix the typo", GoalResearchOn)
	c.GoalStrict(true)

	data, err := os.ReadFile(goalStatePath(path))
	if err != nil {
		t.Fatal(err)
	}
	var state goalState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	if state.Goal != "fix the typo" || state.Status != GoalStatusRunning || state.ResearchMode != GoalResearchOn || !state.Strict {
		t.Fatalf("goal state = %+v, want running strict research goal", state)
	}
}

func TestResumeRestoresTerminalGoalTodosFromSidecar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	loaded := agent.NewSession("sys")
	loaded.Add(provider.Message{
		Role: provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{{
			ID:        "todo-1",
			Name:      "todo_write",
			Arguments: `{"todos":[{"content":"Step 1","status":"in_progress"}]}`,
		}},
	})
	loaded.Add(provider.Message{
		Role:       provider.RoleTool,
		ToolCallID: "todo-1",
		Name:       "todo_write",
		Content:    "ok",
	})
	if err := os.WriteFile(goalStatePath(path), []byte(`{"status":"complete","todos":[{"content":"Step 1","status":"completed"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test"})
	c.Resume(loaded, path)

	got := c.Todos()
	if len(got) != 1 || got[0].Content != "Step 1" || got[0].Status != "completed" {
		t.Fatalf("Todos() after resume = %+v, want completed todos from goal-state sidecar", got)
	}
}

func TestResumeKeepsTranscriptTodosForRunningGoalSidecar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	loaded := agent.NewSession("sys")
	loaded.Add(provider.Message{
		Role: provider.RoleAssistant,
		ToolCalls: []provider.ToolCall{{
			ID:        "todo-1",
			Name:      "todo_write",
			Arguments: `{"todos":[{"content":"Step 1","status":"in_progress"}]}`,
		}},
	})
	loaded.Add(provider.Message{
		Role:       provider.RoleTool,
		ToolCallID: "todo-1",
		Name:       "todo_write",
		Content:    "ok",
	})
	if err := os.WriteFile(goalStatePath(path), []byte(`{"status":"running","todos":[{"content":"Step 1","status":"completed"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test"})
	c.Resume(loaded, path)

	got := c.Todos()
	if len(got) != 1 || got[0].Content != "Step 1" || got[0].Status != "in_progress" {
		t.Fatalf("Todos() after resume = %+v, want transcript todos while goal state is running", got)
	}
}

func TestResumeRestoresRunningAutoResearchGoalFromSidecar(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	path := filepath.Join(root, "session.jsonl")
	taskID := "investigate-runtime-resume"
	if err := os.MkdirAll(filepath.Join(root, ".reasonix", "autoresearch", taskID, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".reasonix", "autoresearch", taskID, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".reasonix", "autoresearch", taskID, "state", "task_spec.json"), []byte(`{"id":"investigate-runtime-resume","goal":"investigate runtime resume","status":"running","created_at":"2026-06-30T00:00:00Z","updated_at":"2026-06-30T00:00:00Z","success_criteria":[{"id":"criterion-1","description":"resume keeps AutoResearch active","required":true}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".reasonix", "autoresearch", taskID, "state", "progress.json"), []byte(`{"task_id":"investigate-runtime-resume","iteration":2,"current_direction":"verify resume","stale_count":1,"pivot_count":0,"updated_at":"2026-06-30T00:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".reasonix", "autoresearch", taskID, "state", "directions_tried.json"), []byte(`{"task_id":"investigate-runtime-resume","directions":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".reasonix", "autoresearch", taskID, "state", "findings.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".reasonix", "autoresearch", taskID, "logs", "heartbeat.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goalStatePath(path), []byte(`{"goal":"investigate runtime resume","status":"running","researchMode":1,"autoResearchTaskID":"investigate-runtime-resume"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded := agent.NewSession("sys")
	exec := agent.New(nil, nil, loaded, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, WorkspaceRoot: root, SessionDir: root, Label: "test"})
	c.Resume(loaded, path)

	if got := c.Goal(); got != "investigate runtime resume" {
		t.Fatalf("Goal() after resume = %q, want running goal from sidecar", got)
	}
	composed := c.Compose("continue")
	if !strings.Contains(composed, "<autoresearch-runtime>") || !strings.Contains(composed, "task_id: "+taskID) {
		t.Fatalf("Compose after resume missing AutoResearch runtime for %q:\n%s", taskID, composed)
	}
}

func TestRunTurnRecordsDisplayForPersistedUserMessage(t *testing.T) {
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Runner: handoffRunner{session: sess}, Executor: exec})
	var gotContent, gotDisplay string
	c.SetDisplayRecorder(func(content, display string) {
		gotContent = content
		gotDisplay = display
	})

	if err := c.runTurnWithRawDisplay(context.Background(), "expanded prompt", "raw prompt", "visible prompt"); err != nil {
		t.Fatal(err)
	}

	if gotContent != "handoff: expanded prompt" {
		t.Fatalf("display recorded against %q, want persisted user message", gotContent)
	}
	if gotDisplay != "visible prompt" {
		t.Fatalf("display = %q, want visible prompt", gotDisplay)
	}
}

func TestSnapshotDoesNotRefreshSessionActivity(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test", ModelRef: "provider/model-a"})
	c.SetSessionPath(filepath.Join(dir, "session.jsonl"))

	if err := c.SnapshotActivity(); err != nil {
		t.Fatal(err)
	}
	first, ok, err := agent.LoadBranchMeta(c.SessionPath())
	if err != nil || !ok {
		t.Fatalf("load initial meta ok=%v err=%v", ok, err)
	}

	time.Sleep(10 * time.Millisecond)
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "saved without activity"})
	if err := c.Snapshot(); err != nil {
		t.Fatal(err)
	}
	second, ok, err := agent.LoadBranchMeta(c.SessionPath())
	if err != nil || !ok {
		t.Fatalf("load second meta ok=%v err=%v", ok, err)
	}
	if !second.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("Snapshot refreshed activity: first=%s second=%s", first.UpdatedAt, second.UpdatedAt)
	}
	if second.Model != "provider/model-a" {
		t.Fatalf("snapshot model = %q, want provider/model-a", second.Model)
	}
}

func TestSnapshotAdoptsNewerDiskForPureStalePrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	staleSess := agent.NewSession("sys")
	staleSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	staleSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	staleExec := agent.New(nil, nil, staleSess, agent.Options{}, event.Discard)
	stale := New(Options{Executor: staleExec, SessionDir: dir, SessionPath: path, Label: "test"})

	currentSess := agent.NewSession("sys")
	currentSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	currentSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	currentSess.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	currentSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "two"})
	currentExec := agent.New(nil, nil, currentSess, agent.Options{}, event.Discard)
	current := New(Options{Executor: currentExec, SessionDir: dir, SessionPath: path, Label: "test"})
	if err := current.SnapshotActivity(); err != nil {
		t.Fatalf("SnapshotActivity current: %v", err)
	}

	if err := stale.Snapshot(); err != nil {
		t.Fatalf("Snapshot stale prefix: %v", err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := len(loaded.Messages); got != 5 {
		t.Fatalf("message count after stale snapshot = %d, want 5", got)
	}
	if got := loaded.Messages[4].Content; got != "two" {
		t.Fatalf("last message after stale snapshot = %q, want %q", got, "two")
	}
	if got := len(stale.executor.Session().Snapshot()); got != 5 {
		t.Fatalf("stale controller adopted message count = %d, want 5", got)
	}
}

func TestSnapshotRecoversDivergedControllerTranscript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	staleSess := agent.NewSession("sys")
	staleSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	staleSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	staleSess.Add(provider.Message{Role: provider.RoleUser, Content: "local second"})
	staleExec := agent.New(nil, nil, staleSess, agent.Options{}, event.Discard)
	stale := New(Options{Executor: staleExec, SessionDir: dir, SessionPath: path, Label: "test"})

	currentSess := agent.NewSession("sys")
	currentSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	currentSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	currentSess.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	currentExec := agent.New(nil, nil, currentSess, agent.Options{}, event.Discard)
	current := New(Options{Executor: currentExec, SessionDir: dir, SessionPath: path, Label: "test"})
	if err := current.SnapshotActivity(); err != nil {
		t.Fatalf("SnapshotActivity current: %v", err)
	}

	if err := stale.Snapshot(); err != nil {
		t.Fatalf("Snapshot stale diverged: %v", err)
	}
	recoveryPath := stale.SessionPath()
	if recoveryPath == path || recoveryPath == "" {
		t.Fatalf("stale session path after recovery = %q, want recovery path", recoveryPath)
	}
	recovered, err := agent.LoadSession(recoveryPath)
	if err != nil {
		t.Fatalf("LoadSession recovery: %v", err)
	}
	if got := recovered.Messages[len(recovered.Messages)-1].Content; got != "local second" {
		t.Fatalf("recovery tail = %q, want local second", got)
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession original: %v", err)
	}
	if got := loaded.Messages[len(loaded.Messages)-1].Content; got != "disk second" {
		t.Fatalf("original tail = %q, want disk second", got)
	}
}

// TestSnapshotConflictRecoveryTransplantsInFlightTurnMarker: when a snapshot
// conflict forks the running turn onto a recovery branch, the in-flight-turn
// marker must move with it. Left on the original branch, the stale marker
// makes the next open of that branch strip messages from a turn that in fact
// kept running (and completed) on the recovery branch.
func TestSnapshotConflictRecoveryTransplantsInFlightTurnMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	staleSess := agent.NewSession("sys")
	staleSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	staleSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	staleSess.Add(provider.Message{Role: provider.RoleUser, Content: "local second"})
	staleExec := agent.New(nil, nil, staleSess, agent.Options{}, event.Discard)
	stale := New(Options{Executor: staleExec, SessionDir: dir, SessionPath: path, Label: "test"})

	currentSess := agent.NewSession("sys")
	currentSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	currentSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	currentSess.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	currentExec := agent.New(nil, nil, currentSess, agent.Options{}, event.Discard)
	current := New(Options{Executor: currentExec, SessionDir: dir, SessionPath: path, Label: "test"})
	if err := current.SnapshotActivity(); err != nil {
		t.Fatalf("SnapshotActivity current: %v", err)
	}

	// The stale runtime had a foreground turn running when the conflict fired.
	if err := agent.MarkSessionInFlightTurn(path, 2, true); err != nil {
		t.Fatalf("MarkSessionInFlightTurn: %v", err)
	}

	if err := stale.Snapshot(); err != nil {
		t.Fatalf("Snapshot stale diverged: %v", err)
	}
	recoveryPath := stale.SessionPath()
	if recoveryPath == path || recoveryPath == "" {
		t.Fatalf("stale session path after recovery = %q, want recovery path", recoveryPath)
	}

	origMeta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta original ok=%v err=%v", ok, err)
	}
	if origMeta.InFlightTurn != nil {
		t.Fatal("in-flight turn marker left on the forked-from branch; reopening it would strip the turn")
	}
	recMeta, ok, err := agent.LoadBranchMeta(recoveryPath)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta recovery ok=%v err=%v", ok, err)
	}
	if recMeta.InFlightTurn == nil {
		t.Fatal("in-flight turn marker not transplanted to the recovery branch")
	}
	if recMeta.InFlightTurn.StartMessageIndex != 2 || !recMeta.InFlightTurn.PreserveUser {
		t.Fatalf("transplanted marker = %+v, want start index 2 with preserve_user", recMeta.InFlightTurn)
	}
}

// TestRecoverInterruptedTurnSparesTurnContinuedOnRecoveryBranch covers the
// legacy leftovers of the marker transplant: runtimes predating it forked a
// recovery branch mid-turn and left the in-flight marker on the original
// branch. Opening the original must clear the stale marker without stripping
// a turn that in fact kept running on the recovery branch.
func TestRecoverInterruptedTurnSparesTurnContinuedOnRecoveryBranch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	orig := agent.NewSession("sys")
	orig.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	orig.Add(provider.Message{Role: provider.RoleAssistant, Content: "partial"})
	if err := orig.Save(path); err != nil {
		t.Fatalf("Save original: %v", err)
	}
	if err := agent.MarkSessionInFlightTurn(path, 1, true); err != nil {
		t.Fatalf("MarkSessionInFlightTurn: %v", err)
	}
	// A legacy runtime forked the running turn onto a recovery branch and
	// left the marker behind on the original.
	forked := agent.NewSession("sys")
	forked.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	forked.Add(provider.Message{Role: provider.RoleAssistant, Content: "continued elsewhere"})
	if _, err := forked.SaveRecoveryBranch(agent.RecoveryBranchOptions{OriginalPath: path}); err != nil {
		t.Fatalf("SaveRecoveryBranch: %v", err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	exec := agent.New(nil, nil, loaded, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test"})
	c.recoverInterruptedTurn(path)

	if got := c.executor.Session().Len(); got != 3 {
		t.Fatalf("message count after reopening forked-from branch = %d, want 3 (turn stripped despite recovery child)", got)
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	if meta.InFlightTurn != nil {
		t.Fatal("fork-orphaned in-flight marker not cleared")
	}
}

// TestRecoverInterruptedTurnStripsGenuineCrash pins the crash-recovery
// behavior the recovery-child guard must not swallow: with no recovery branch
// in sight, an in-flight marker means the runtime died mid-turn and the
// partial tail is stripped (preserving the user prompt).
func TestRecoverInterruptedTurnStripsGenuineCrash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	orig := agent.NewSession("sys")
	orig.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	orig.Add(provider.Message{Role: provider.RoleAssistant, Content: "partial"})
	if err := orig.Save(path); err != nil {
		t.Fatalf("Save original: %v", err)
	}
	if err := agent.MarkSessionInFlightTurn(path, 1, true); err != nil {
		t.Fatalf("MarkSessionInFlightTurn: %v", err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	exec := agent.New(nil, nil, loaded, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test"})
	c.recoverInterruptedTurn(path)

	if got := c.executor.Session().Len(); got != 2 {
		t.Fatalf("message count after crash recovery = %d, want 2 (sys + preserved user prompt)", got)
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	if meta.InFlightTurn != nil {
		t.Fatal("in-flight marker not cleared after crash recovery")
	}
}

func TestSnapshotRewriteRecoversStaleControllerTranscript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	staleSess := agent.NewSession("sys")
	staleSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	staleSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	staleExec := agent.New(nil, nil, staleSess, agent.Options{}, event.Discard)
	stale := New(Options{Executor: staleExec, SessionDir: dir, SessionPath: path, Label: "test"})

	currentSess := agent.NewSession("sys")
	currentSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	currentSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	currentSess.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	currentSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "two"})
	currentExec := agent.New(nil, nil, currentSess, agent.Options{}, event.Discard)
	current := New(Options{Executor: currentExec, SessionDir: dir, SessionPath: path, Label: "test"})
	if err := current.SnapshotActivity(); err != nil {
		t.Fatalf("SnapshotActivity current: %v", err)
	}

	staleSess.Replace([]provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "summarized first"},
	})
	if err := stale.SnapshotRewrite(); err != nil {
		t.Fatalf("SnapshotRewrite stale: %v", err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := len(loaded.Messages); got != 5 {
		t.Fatalf("message count after stale rewrite = %d, want 5", got)
	}
	if got := loaded.Messages[4].Content; got != "two" {
		t.Fatalf("last message after stale rewrite = %q, want %q", got, "two")
	}
	recoveryPath := stale.SessionPath()
	if recoveryPath == path || recoveryPath == "" {
		t.Fatalf("stale session path after rewrite recovery = %q, want recovery path", recoveryPath)
	}
	recovered, err := agent.LoadSession(recoveryPath)
	if err != nil {
		t.Fatalf("LoadSession recovery: %v", err)
	}
	if got := recovered.Messages[1].Content; got != "summarized first" {
		t.Fatalf("recovery content = %q, want summarized first", got)
	}
	meta, ok, err := agent.LoadBranchMeta(recoveryPath)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta recovery ok=%v err=%v", ok, err)
	}
	if !meta.Recovered || meta.ParentID != agent.BranchID(path) {
		t.Fatalf("recovery meta = %+v, want recovered parent", meta)
	}
}

func TestSnapshotActivityPersistsOwnedCompactionRewrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	if err := sess.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	exec := agent.New(nil, nil, loaded, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test"})

	// A mid-turn autosave can persist the pre-compaction prefix. Auto-compaction
	// then rewrites older history inside the same turn; the final activity
	// snapshot must persist that owned rewrite in place instead of branching.
	loaded.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	if err := c.Snapshot(); err != nil {
		t.Fatalf("Snapshot pre-compaction: %v", err)
	}
	loaded.Replace([]provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "<compaction-summary>\nSummary of earlier conversation: first -> one\n</compaction-summary>"},
		{Role: provider.RoleUser, Content: "second"},
	})
	loaded.IncrementRewrite()

	if err := c.SnapshotActivity(); err != nil {
		t.Fatalf("SnapshotActivity after compaction: %v", err)
	}
	if got := c.SessionPath(); got != path {
		t.Fatalf("session path after owned compaction = %q, want original %q", got, path)
	}
	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession rewritten: %v", err)
	}
	if got := len(reloaded.Messages); got != 3 {
		t.Fatalf("message count after compaction rewrite = %d, want 3: %+v", got, reloaded.Messages)
	}
	if got := reloaded.Messages[1].Content; !strings.Contains(got, "compaction-summary") {
		t.Fatalf("compaction summary was not persisted: %+v", reloaded.Messages)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("recovery branches after owned compaction rewrite = %v err=%v, want none", matches, err)
	}
}

func TestRecoveryBranchPersistsLaterOwnedCompactionRewrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	currentSess := agent.NewSession("sys")
	currentSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	currentSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "disk"})
	if err := currentSess.Save(path); err != nil {
		t.Fatalf("Save current: %v", err)
	}

	localSess := agent.NewSession("sys")
	localSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	localSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "local"})
	localExec := agent.New(nil, nil, localSess, agent.Options{}, event.Discard)
	sink := &noticeSink{}
	c := New(Options{Executor: localExec, SessionDir: dir, SessionPath: path, Label: "test", Sink: sink})

	if err := c.Snapshot(); err != nil {
		t.Fatalf("Snapshot initial recovery: %v", err)
	}
	recoveryPath := c.SessionPath()
	if recoveryPath == "" || recoveryPath == path {
		t.Fatalf("recovery path = %q, want distinct path", recoveryPath)
	}
	notices := sink.notices()
	if len(notices) == 0 {
		t.Fatal("initial recovery emitted no user notice")
	}
	if got := notices[len(notices)-1]; strings.Contains(got, agent.BranchID(recoveryPath)) || strings.Contains(got, "recovery branch") {
		t.Fatalf("initial recovery notice exposed internal branch detail: %q", got)
	}

	localSess.Add(provider.Message{Role: provider.RoleUser, Content: "continue"})
	if err := c.Snapshot(); err != nil {
		t.Fatalf("Snapshot recovery append: %v", err)
	}
	localSess.Replace([]provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "<compaction-summary>\nSummary of recovery branch work: first -> local\n</compaction-summary>"},
		{Role: provider.RoleUser, Content: "continue"},
	})
	localSess.IncrementRewrite()

	if err := c.SnapshotActivity(); err != nil {
		t.Fatalf("SnapshotActivity recovery compaction: %v", err)
	}
	if got := c.SessionPath(); got != recoveryPath {
		t.Fatalf("session path after recovery compaction = %q, want recovery %q", got, recoveryPath)
	}
	reloaded, err := agent.LoadSession(recoveryPath)
	if err != nil {
		t.Fatalf("LoadSession recovery: %v", err)
	}
	if got := len(reloaded.Messages); got != 3 {
		t.Fatalf("message count after recovery compaction = %d, want 3: %+v", got, reloaded.Messages)
	}
	if got := reloaded.Messages[1].Content; !strings.Contains(got, "compaction-summary") {
		t.Fatalf("recovery compaction summary was not persisted: %+v", reloaded.Messages)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, "*-recovery-*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("nested recovery branches after owned recovery compaction = %v err=%v, want none", matches, err)
	}
}

func TestConcurrentSnapshotsShareSingleRecoveryHandoff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	currentSess := agent.NewSession("sys")
	currentSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	currentSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "disk"})
	if err := currentSess.Save(path); err != nil {
		t.Fatalf("Save current: %v", err)
	}

	localSess := agent.NewSession("sys")
	localSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	localSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "local"})
	localExec := agent.New(nil, nil, localSess, agent.Options{}, event.Discard)

	entered := make(chan controlRecoveryInfo, 16)
	release := make(chan struct{})
	c := New(Options{
		Executor:    localExec,
		SessionDir:  dir,
		SessionPath: path,
		Label:       "test",
		OnSessionRecovered: func(info SessionRecoveryInfo) error {
			entered <- controlRecoveryInfo{originalPath: info.OriginalPath, recoveryPath: info.RecoveryPath}
			<-release
			return nil
		},
	})

	firstDone := make(chan error, 1)
	go func() { firstDone <- c.Snapshot() }()
	first := <-entered
	if first.originalPath != path || first.recoveryPath == "" || first.recoveryPath == path {
		t.Fatalf("first recovery info = %+v, want distinct recovery from original", first)
	}

	const racingSnapshots = 8
	var wg sync.WaitGroup
	errs := make(chan error, racingSnapshots)
	for i := 0; i < racingSnapshots; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- c.Snapshot()
		}()
	}

	select {
	case extra := <-entered:
		t.Fatalf("concurrent snapshot entered recovery while first handoff was blocked: %+v", extra)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Snapshot: %v", err)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("racing Snapshot: %v", err)
		}
	}
	select {
	case extra := <-entered:
		t.Fatalf("unexpected additional recovery after handoff completed: %+v", extra)
	default:
	}

	if got := c.SessionPath(); got != first.recoveryPath {
		t.Fatalf("controller session path = %q, want recovery %q", got, first.recoveryPath)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl"))
	if err != nil {
		t.Fatalf("glob recovery branches: %v", err)
	}
	recoveries := recoveryTranscriptPaths(matches)
	if len(recoveries) != 1 || recoveries[0] != first.recoveryPath {
		t.Fatalf("recovery branches = %v err=%v, want only %q", matches, err, first.recoveryPath)
	}
}

func recoveryTranscriptPaths(paths []string) []string {
	out := paths[:0]
	for _, path := range paths {
		if !strings.HasSuffix(path, ".events.jsonl") {
			out = append(out, path)
		}
	}
	return out
}

type controlRecoveryInfo struct {
	originalPath string
	recoveryPath string
}

// blockedRecoveryHandoff is a controller whose first Snapshot has entered the
// recovery handoff and is parked inside OnSessionRecovered until release is
// closed, so tests can race other controller operations against an in-flight
// handoff.
type blockedRecoveryHandoff struct {
	c         *Controller
	dir       string
	path      string
	local     *agent.Session
	first     controlRecoveryInfo
	release   chan struct{}
	firstDone chan error
}

func startBlockedRecoveryHandoff(t *testing.T) *blockedRecoveryHandoff {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	currentSess := agent.NewSession("sys")
	currentSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	currentSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "disk"})
	if err := currentSess.Save(path); err != nil {
		t.Fatalf("Save current: %v", err)
	}

	localSess := agent.NewSession("sys")
	localSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	localSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "local"})
	localExec := agent.New(nil, nil, localSess, agent.Options{}, event.Discard)

	entered := make(chan controlRecoveryInfo, 1)
	release := make(chan struct{})
	c := New(Options{
		Executor:    localExec,
		SessionDir:  dir,
		SessionPath: path,
		Label:       "test",
		OnSessionRecovered: func(info SessionRecoveryInfo) error {
			entered <- controlRecoveryInfo{originalPath: info.OriginalPath, recoveryPath: info.RecoveryPath}
			<-release
			return nil
		},
	})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})

	firstDone := make(chan error, 1)
	go func() { firstDone <- c.Snapshot() }()
	first := <-entered
	if first.originalPath != path || first.recoveryPath == "" || first.recoveryPath == path {
		t.Fatalf("recovery info = %+v, want distinct recovery from original", first)
	}
	return &blockedRecoveryHandoff{
		c: c, dir: dir, path: path, local: localSess,
		first: first, release: release, firstDone: firstDone,
	}
}

// TestSessionSwapWaitsForRecoveryHandoff guards the swap side of the snapshot
// serialization: moves of controller-owned session state must wait for an
// in-flight save/recovery handoff instead of interleaving with it. A swap that
// lands mid-handoff pairs the old path with the new session, which either
// writes one transcript's messages into another's file or manufactures another
// bogus conflict on the next save.
func TestSessionSwapWaitsForRecoveryHandoff(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T, h *blockedRecoveryHandoff) (done chan struct{}, wantPath string)
		// during runs while the handoff is still blocked; verify after the
		// racing operation completed.
		during func(t *testing.T, h *blockedRecoveryHandoff)
		verify func(t *testing.T, h *blockedRecoveryHandoff)
	}{
		{name: "SetSessionPath", run: func(t *testing.T, h *blockedRecoveryHandoff) (chan struct{}, string) {
			other := filepath.Join(h.dir, "other.jsonl")
			done := make(chan struct{})
			go func() { h.c.SetSessionPath(other); close(done) }()
			return done, other
		}},
		{name: "Resume", run: func(t *testing.T, h *blockedRecoveryHandoff) (chan struct{}, string) {
			other := filepath.Join(h.dir, "resumed.jsonl")
			sess := agent.NewSession("sys")
			sess.Add(provider.Message{Role: provider.RoleUser, Content: "resumed"})
			if err := sess.Save(other); err != nil {
				t.Fatalf("Save resumed: %v", err)
			}
			done := make(chan struct{})
			go func() { h.c.Resume(sess, other); close(done) }()
			return done, other
		}},
		{
			name: "CancelFlush",
			run: func(t *testing.T, h *blockedRecoveryHandoff) (chan struct{}, string) {
				// Truncate the cancelled turn: drop the assistant reply, the
				// same shape stripTurnMessagesAfter feeds this helper.
				truncated := []provider.Message{
					{Role: provider.RoleSystem, Content: "sys"},
					{Role: provider.RoleUser, Content: "first"},
				}
				done := make(chan struct{})
				go func() { h.c.replaceSessionAfterCancel(truncated); close(done) }()
				return done, h.first.recoveryPath
			},
			during: func(t *testing.T, h *blockedRecoveryHandoff) {
				// The in-memory truncation itself must wait for the handoff: an
				// early Replace would let the blocked save capture the shortened
				// transcript, read the longer on-disk partial as a stale-prefix
				// conflict, and adopt it back over the cancel cleanup.
				if got := len(h.local.Snapshot()); got != 3 {
					t.Fatalf("session truncated to %d messages while the handoff was still in flight, want 3", got)
				}
			},
			verify: func(t *testing.T, h *blockedRecoveryHandoff) {
				if got := len(h.local.Snapshot()); got != 2 {
					t.Fatalf("session = %d messages after cancel flush, want 2", got)
				}
				loaded, err := agent.LoadSession(h.first.recoveryPath)
				if err != nil {
					t.Fatalf("LoadSession recovery: %v", err)
				}
				if got := len(loaded.Messages); got != 2 {
					t.Fatalf("recovery transcript = %d messages after cancel flush, want 2", got)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := startBlockedRecoveryHandoff(t)
			done, wantPath := tc.run(t, h)
			select {
			case <-done:
				t.Fatal("session state moved while the recovery handoff was still in flight")
			case <-time.After(100 * time.Millisecond):
			}
			if tc.during != nil {
				tc.during(t, h)
			}
			close(h.release)
			if err := <-h.firstDone; err != nil {
				t.Fatalf("first Snapshot: %v", err)
			}
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("session state move did not finish after the handoff completed")
			}
			if got := h.c.SessionPath(); got != wantPath {
				t.Fatalf("controller session path = %q, want %q", got, wantPath)
			}
			matches, err := filepath.Glob(filepath.Join(h.dir, "*-recovery-*.jsonl"))
			if err != nil {
				t.Fatalf("glob recovery branches: %v", err)
			}
			recoveries := recoveryTranscriptPaths(matches)
			if len(recoveries) != 1 || recoveries[0] != h.first.recoveryPath {
				t.Fatalf("recovery branches = %v, want only %q", matches, h.first.recoveryPath)
			}
			if tc.verify != nil {
				tc.verify(t, h)
			}
		})
	}
}

// TestSnapshotConflictAdoptionResetsRewriteBaseline guards the baseline
// handoff on the adopt path: adopting a newer on-disk transcript installs a
// freshly loaded session, and the replaced session's rewrite version must not
// leak onto it. A leaked (higher) baseline would make the adopted session's
// own compactions look already persisted, so the next autosave would take the
// snapshot path, conflict, and fork a spurious recovery branch.
func TestSnapshotConflictAdoptionResetsRewriteBaseline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	base := agent.NewSession("sys")
	base.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	if err := base.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}
	stale, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession stale: %v", err)
	}
	other, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession other: %v", err)
	}
	other.Add(provider.Message{Role: provider.RoleAssistant, Content: "newer"})
	if err := other.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot other: %v", err)
	}

	exec := agent.New(nil, nil, stale, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test"})
	// Rewrites the stale controller never persisted before it noticed the
	// newer transcript; adoption must discard this counter with the session.
	for i := 0; i < 3; i++ {
		stale.IncrementRewrite()
	}

	if err := c.Snapshot(); err != nil {
		t.Fatalf("Snapshot adopt: %v", err)
	}
	if got := c.SessionPath(); got != path {
		t.Fatalf("session path after adoption = %q, want original %q", got, path)
	}
	adopted := exec.Session()
	if adopted == stale {
		t.Fatal("expected adoption to replace the stale session")
	}
	if adopted.NeedsRewriteSave() {
		t.Fatal("adopted session should carry a persisted rewrite baseline; the stale session's counter must not leak onto it")
	}

	// The adopted session's first compaction must persist in place.
	msgs := adopted.Snapshot()
	adopted.Replace([]provider.Message{
		msgs[0],
		{Role: provider.RoleUser, Content: "<compaction-summary>\nfirst -> newer\n</compaction-summary>"},
	})
	adopted.IncrementRewrite()
	if err := c.SnapshotActivity(); err != nil {
		t.Fatalf("SnapshotActivity after adopted compaction: %v", err)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("recovery branches after adopted compaction = %v err=%v, want none", matches, err)
	}
	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession rewritten: %v", err)
	}
	if got := len(reloaded.Messages); got != 2 {
		t.Fatalf("message count after adopted compaction = %d, want 2: %+v", got, reloaded.Messages)
	}
}

// TestConcurrentCompactionAndAutosaveNeverBranch drives the real shape of the
// bug: a mid-turn autosave goroutine saving while the turn goroutine compacts.
// Whatever the interleaving, an owned single-process session must never fork a
// recovery branch — the decision/mark pipeline in snapshot() has to capture
// the rewrite version it actually persisted, and retry as an owned rewrite
// when a compaction slips in between.
func TestConcurrentCompactionAndAutosaveNeverBranch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "turn-0"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test"})
	if err := c.Snapshot(); err != nil {
		t.Fatalf("initial snapshot: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				// Errors surface as recovery branches, asserted below.
				_ = c.SnapshotActivity()
			}
		}
	}()
	for i := 1; i <= 40; i++ {
		sess.Add(provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("turn-%d", i)})
		if i%4 == 0 {
			msgs := sess.Snapshot()
			sess.Replace([]provider.Message{
				msgs[0],
				{Role: provider.RoleUser, Content: fmt.Sprintf("<compaction-summary>\nrounds through %d\n</compaction-summary>", i)},
				msgs[len(msgs)-1],
			})
			sess.IncrementRewrite()
		}
	}
	close(stop)
	wg.Wait()
	if err := c.SnapshotActivity(); err != nil {
		t.Fatalf("final snapshot: %v", err)
	}

	if matches, err := filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("compaction racing autosave created recovery branches: %v err=%v", matches, err)
	}
	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession final: %v", err)
	}
	if got, want := len(reloaded.Messages), sess.Len(); got != want {
		t.Fatalf("persisted %d messages, memory has %d", got, want)
	}
}

func TestAdoptHistoryPreservesRewriteBaseline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	s := agent.NewSession("old sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "tool-1", Name: "read_file", Arguments: "{}"}}})
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "tool-1", Name: "read_file", Content: strings.Repeat("detail ", 100)})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	if err := s.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	msgs := loaded.Snapshot()
	msgs[0].Content = "new sys"

	exec := agent.New(nil, nil, agent.NewSession("new sys"), agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test", DisableColdResumePrune: true})
	c.AdoptHistory(msgs, path)
	rewrite := exec.Session().Snapshot()
	rewrite[3].Content = "[elided tool result]"
	exec.Session().Replace(rewrite)
	if err := c.SnapshotRewrite(); err != nil {
		t.Fatalf("SnapshotRewrite adopted history: %v", err)
	}

	if got := c.SessionPath(); got != path {
		t.Fatalf("SessionPath after adopted rewrite = %q, want %q", got, path)
	}
	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession rewritten: %v", err)
	}
	if got := reloaded.Messages[0].Content; got != "new sys" {
		t.Fatalf("system prompt after rewrite = %q, want new sys", got)
	}
	if got := reloaded.Messages[3].Content; got != "[elided tool result]" {
		t.Fatalf("tool result after rewrite = %q, want elided", got)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("recovery branches after adopted rewrite = %v err=%v, want none", matches, err)
	}
}

func TestAdoptHistoryRejectsStaleCarriedHistoryBaseline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	current := agent.NewSession("sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	current.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "disk two"})
	if err := current.Save(path); err != nil {
		t.Fatalf("Save current: %v", err)
	}

	stale := []provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "first"},
		{Role: provider.RoleAssistant, Content: "one"},
	}
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test", DisableColdResumePrune: true})
	c.AdoptHistory(stale, path)
	if err := c.SnapshotRewrite(); err != nil {
		t.Fatalf("SnapshotRewrite stale adopted history: %v", err)
	}

	if got := c.SessionPath(); got != path {
		t.Fatalf("SessionPath after stale adopted rewrite = %q, want original path", got)
	}
	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession original: %v", err)
	}
	if got := reloaded.Messages[len(reloaded.Messages)-1].Content; got != "disk two" {
		t.Fatalf("original tail after stale adopted rewrite = %q, want disk two", got)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("recovery branches after prefix stale adopted rewrite = %v err=%v, want none", matches, err)
	}
}

func TestCancelFlushRejectsStaleControllerOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	staleSess := agent.NewSession("sys")
	staleSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	staleSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	staleSess.Add(provider.Message{Role: provider.RoleUser, Content: "partial"})
	staleExec := agent.New(nil, nil, staleSess, agent.Options{}, event.Discard)
	stale := New(Options{Executor: staleExec, SessionDir: dir, SessionPath: path, Label: "test"})

	currentSess := agent.NewSession("sys")
	currentSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	currentSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	currentSess.Add(provider.Message{Role: provider.RoleUser, Content: "second"})
	currentSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "two"})
	currentExec := agent.New(nil, nil, currentSess, agent.Options{}, event.Discard)
	current := New(Options{Executor: currentExec, SessionDir: dir, SessionPath: path, Label: "test"})
	if err := current.SnapshotActivity(); err != nil {
		t.Fatalf("SnapshotActivity current: %v", err)
	}

	stale.replaceSessionAfterCancel([]provider.Message{
		{Role: provider.RoleSystem, Content: "sys"},
		{Role: provider.RoleUser, Content: "first"},
	})

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := len(loaded.Messages); got != 5 {
		t.Fatalf("message count after stale cancel flush = %d, want 5", got)
	}
	if got := loaded.Messages[4].Content; got != "two" {
		t.Fatalf("last message after stale cancel flush = %q, want %q", got, "two")
	}
}

func TestSnapshotActivityRefreshesSessionActivity(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test"})
	c.SetSessionPath(filepath.Join(dir, "session.jsonl"))

	if err := c.SnapshotActivity(); err != nil {
		t.Fatal(err)
	}
	first, _, err := agent.LoadBranchMeta(c.SessionPath())
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(10 * time.Millisecond)
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "activity"})
	if err := c.SnapshotActivity(); err != nil {
		t.Fatal(err)
	}
	second, _, err := agent.LoadBranchMeta(c.SessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if !second.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("SnapshotActivity did not refresh activity: first=%s second=%s", first.UpdatedAt, second.UpdatedAt)
	}
}

func TestSnapshotActivitySavesTranscriptBeforeModelMeta(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "must persist"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test", ModelRef: "provider/model-a"})
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agent.BranchMetaPath(path), []byte("{bad json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := c.SnapshotActivity(); err == nil {
		t.Fatal("SnapshotActivity should report malformed branch metadata")
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("transcript was not saved before metadata error: %v", err)
	}
	if len(loaded.Messages) == 0 || loaded.Messages[len(loaded.Messages)-1].Content != "must persist" {
		t.Fatalf("saved transcript = %+v, want persisted user message", loaded.Messages)
	}
}

func TestNewSessionStartsFreshContextAndSavesTranscript(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "old context"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	path := filepath.Join(dir, "session.jsonl")
	c := New(Options{Executor: exec, SystemPrompt: "sys", SessionDir: dir, SessionPath: path, Label: "test"})

	if err := c.NewSession(); err != nil {
		t.Fatal(err)
	}
	if c.SessionPath() == path {
		t.Fatal("/new did not rotate to a fresh session path")
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Messages) != 2 || loaded.Messages[1].Content != "old context" {
		t.Fatalf("previous transcript was not saved: %+v", loaded.Messages)
	}
	current := exec.Session().Snapshot()
	if len(current) != 1 || current[0].Role != provider.RoleSystem || current[0].Content != "sys" {
		t.Fatalf("fresh context = %+v, want only system prompt", current)
	}
}

func TestSnapshotConflictLogAttrsCarryRevisionLedger(t *testing.T) {
	conflict := &agent.SessionSnapshotConflictError{
		Path:             "/tmp/session.jsonl",
		Kind:             agent.SessionSnapshotConflictDiverged,
		ExistingMessages: 7,
		SnapshotMessages: 5,
		BaseRevision:     3,
		DiskRevision:     9,
	}
	attrs := snapshotConflictLogAttrs(fmt.Errorf("save: %w", conflict), "/tmp/session.jsonl", "rewrite")
	got := map[string]any{}
	for i := 0; i+1 < len(attrs); i += 2 {
		key, ok := attrs[i].(string)
		if !ok {
			t.Fatalf("attr key %v is not a string", attrs[i])
		}
		got[key] = attrs[i+1]
	}
	if got["mode"] != "rewrite" || got["kind"] != "diverged" {
		t.Fatalf("attrs = %v, want mode=rewrite kind=diverged", got)
	}
	if got["base_revision"] != int64(3) || got["disk_revision"] != int64(9) {
		t.Fatalf("attrs = %v, want base_revision=3 disk_revision=9", got)
	}
	if got["disk_messages"] != 7 || got["snapshot_messages"] != 5 {
		t.Fatalf("attrs = %v, want disk_messages=7 snapshot_messages=5", got)
	}

	// A conflict error without the typed detail still logs path and mode.
	plain := snapshotConflictLogAttrs(agent.ErrSessionSnapshotConflict, "/tmp/session.jsonl", "snapshot")
	if len(plain) != 4 {
		t.Fatalf("plain attrs = %v, want only path and mode", plain)
	}
}

type noticeSink struct {
	mu     sync.Mutex
	events []event.Event
}

func (s *noticeSink) Emit(e event.Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
}

func (s *noticeSink) notices() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, e := range s.events {
		if e.Kind == event.Notice {
			out = append(out, e.Text)
		}
	}
	return out
}

func TestSnapshotConflictAtRecoveryDepthCapForceSavesCurrentBranch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	disk := agent.NewSession("sys")
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	disk.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	if err := disk.Save(path); err != nil {
		t.Fatalf("Save disk: %v", err)
	}
	meta, ok, err := agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta ok=%v err=%v", ok, err)
	}
	meta.Recovered = true
	meta.RecoveryDepth = agent.SessionRecoveryMaxDepth
	if err := agent.SaveBranchMeta(path, meta); err != nil {
		t.Fatalf("SaveBranchMeta: %v", err)
	}

	stale := agent.NewSession("sys")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local second"})
	exec := agent.New(nil, nil, stale, agent.Options{}, event.Discard)
	sink := &noticeSink{}
	c := New(Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test", Sink: sink})
	stale.IncrementRewrite()

	if err := c.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if got := c.SessionPath(); got != path {
		t.Fatalf("session path = %q, want unchanged %q (no new fork)", got, path)
	}
	forks, err := filepath.Glob(filepath.Join(dir, "*-recovery-*.jsonl"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(forks) != 0 {
		t.Fatalf("depth cap still forked: %v", forks)
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got := loaded.Messages[len(loaded.Messages)-1].Content; got != "local second" {
		t.Fatalf("disk tail = %q, want force-saved local transcript", got)
	}
	notices := sink.notices()
	if len(notices) == 0 || !strings.Contains(notices[len(notices)-1], "saved the current conflict copy in place") {
		t.Fatalf("notices = %v, want depth-cap notice", notices)
	}
	if stale.NeedsRewriteSave() {
		t.Fatal("rewrite baseline not re-anchored by depth-cap force save")
	}

	foreign := agent.NewSession("sys")
	foreign.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	foreign.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	foreign.Add(provider.Message{Role: provider.RoleUser, Content: "foreign second"})
	if err := foreign.Save(path); err != nil {
		t.Fatalf("Save foreign: %v", err)
	}
	meta, ok, err = agent.LoadBranchMeta(path)
	if err != nil || !ok {
		t.Fatalf("LoadBranchMeta foreign ok=%v err=%v", ok, err)
	}
	meta.Recovered = true
	meta.RecoveryDepth = agent.SessionRecoveryMaxDepth
	if err := agent.SaveBranchMeta(path, meta); err != nil {
		t.Fatalf("SaveBranchMeta foreign: %v", err)
	}
	if err := c.Snapshot(); err != nil {
		t.Fatalf("repeated depth-cap Snapshot: %v", err)
	}
	if got := sink.notices(); len(got) != len(notices) {
		t.Fatalf("repeated depth-cap snapshot emitted duplicate notice: %v", got)
	}

	// The force save re-anchored the baseline: the next snapshot must not
	// conflict again.
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
	if err := c.Snapshot(); err != nil {
		t.Fatalf("follow-up Snapshot: %v", err)
	}
	if got := sink.notices(); len(got) != len(notices) {
		t.Fatalf("follow-up snapshot emitted more notices: %v", got)
	}
}

func TestNewSessionRefusesWhileTurnRunning(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "old context"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	path := filepath.Join(dir, "session.jsonl")
	c := New(Options{Executor: exec, SystemPrompt: "sys", SessionDir: dir, SessionPath: path, Label: "test"})

	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	if err := c.NewSession(); err == nil {
		t.Fatal("NewSession while running = nil error, want refusal")
	}
	if got := c.SessionPath(); got != path {
		t.Fatalf("session path = %q, want unrotated %q", got, path)
	}
	if snap := exec.Session().Snapshot(); len(snap) != 2 {
		t.Fatalf("running session was reset out from under the turn: %+v", snap)
	}

	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
	if err := c.NewSession(); err != nil {
		t.Fatalf("NewSession after the turn stopped: %v", err)
	}
	if c.SessionPath() == path {
		t.Fatal("session path did not rotate once the turn stopped")
	}
}

// TestNewSessionRefusesTurnStartedDuringSnapshot forces the TOCTOU interleaving
// the running guard alone missed: a turn starts while NewSession is mid-Snapshot
// (running was false at the entry check), and must be refused so the executor
// session is not swapped out from under a live run loop.
func TestNewSessionRefusesTurnStartedDuringSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	// A diverged on-disk transcript makes Snapshot enter the recovery callback,
	// where the test parks NewSession mid-rotation.
	diskSess := agent.NewSession("sys")
	diskSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	diskSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "disk"})
	if err := diskSess.Save(path); err != nil {
		t.Fatalf("Save disk: %v", err)
	}
	localSess := agent.NewSession("sys")
	localSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	localSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "local"})
	localExec := agent.New(nil, nil, localSess, agent.Options{}, event.Discard)

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	c := New(Options{
		Executor:     localExec,
		SystemPrompt: "sys",
		SessionDir:   dir,
		SessionPath:  path,
		Label:        "test",
		OnSessionRecovered: func(SessionRecoveryInfo) error {
			entered <- struct{}{}
			<-release
			return nil
		},
	})

	newSessionDone := make(chan error, 1)
	go func() { newSessionDone <- c.NewSession() }()

	// NewSession is now parked inside Snapshot, still holding the rotation gate.
	<-entered

	// A turn tries to start in exactly the window the bare running check left
	// open. It must be refused rather than flip running=true and read the
	// session NewSession is about to replace.
	if err := c.RunTurn(context.Background(), "hello"); !errors.Is(err, ErrTurnRunning) {
		close(release)
		<-newSessionDone
		t.Fatalf("RunTurn during rotation = %v, want ErrTurnRunning", err)
	}
	if c.Running() {
		close(release)
		<-newSessionDone
		t.Fatal("RunTurn set running=true during a rotation")
	}
	// The live session must be untouched while the refused turn could have read
	// it: NewSession has not swapped yet (still parked before the swap).
	if snap := localExec.Session().Snapshot(); len(snap) != 3 {
		close(release)
		<-newSessionDone
		t.Fatalf("session mutated during rotation window: %+v", snap)
	}

	close(release)
	if err := <-newSessionDone; err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	// Rotation completed: a fresh session with only the system prompt, on a new
	// path, and a turn may start again.
	if c.SessionPath() == path {
		t.Fatal("session path did not rotate")
	}
	if snap := localExec.Session().Snapshot(); len(snap) != 1 || snap[0].Role != provider.RoleSystem {
		t.Fatalf("post-rotation session = %+v, want only system prompt", snap)
	}
	if c.Running() {
		t.Fatal("rotation gate leaked: controller still marked running")
	}
}

// TestSessionMutationsRefuseWhileRotating proves every session-mutating entry
// point is wired to the same rotation gate: while a rotation is in progress
// (c.rotating held), each refuses instead of swapping/rewriting the live
// session, and a turn cannot start either. This is the TOCTOU class the bare
// Running() checks left open — a mutation slipping in mid-rotation.
func TestSessionMutationsRefuseWhileRotating(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hi"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "there"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SystemPrompt: "sys", SessionDir: dir, SessionPath: path, Label: "test"})

	// Simulate a rotation already in progress (as NewSession/ClearSession hold
	// it across their snapshot-then-swap window).
	if err := c.beginRotation(); err != nil {
		t.Fatalf("beginRotation: %v", err)
	}

	if err := c.NewSession(); !errors.Is(err, errRotationInProgress) {
		t.Fatalf("NewSession while rotating = %v, want errRotationInProgress", err)
	}
	if err := c.ClearSession(); !errors.Is(err, errRotationInProgress) {
		t.Fatalf("ClearSession while rotating = %v, want errRotationInProgress", err)
	}
	if _, err := c.Branch("x"); !errors.Is(err, errRotationInProgress) {
		t.Fatalf("Branch while rotating = %v, want errRotationInProgress", err)
	}
	if _, err := c.ForkNamed(1, "x"); !errors.Is(err, errRotationInProgress) {
		t.Fatalf("ForkNamed while rotating = %v, want errRotationInProgress", err)
	}
	if _, err := c.SwitchBranch("x"); !errors.Is(err, errRotationInProgress) {
		t.Fatalf("SwitchBranch while rotating = %v, want errRotationInProgress", err)
	}
	if err := c.Compact(context.Background(), ""); !errors.Is(err, errRotationInProgress) {
		t.Fatalf("Compact while rotating = %v, want errRotationInProgress", err)
	}
	if err := c.Rewind(0, RewindConversation); !errors.Is(err, errRotationInProgress) {
		t.Fatalf("Rewind while rotating = %v, want errRotationInProgress", err)
	}
	if err := c.SummarizeFrom(context.Background(), 1); !errors.Is(err, errRotationInProgress) {
		t.Fatalf("SummarizeFrom while rotating = %v, want errRotationInProgress", err)
	}
	if err := c.SummarizeUpTo(context.Background(), 1); !errors.Is(err, errRotationInProgress) {
		t.Fatalf("SummarizeUpTo while rotating = %v, want errRotationInProgress", err)
	}
	// A turn must not start while a rotation holds the gate.
	if err := c.RunTurn(context.Background(), "hello"); !errors.Is(err, ErrTurnRunning) {
		t.Fatalf("RunTurn while rotating = %v, want ErrTurnRunning", err)
	}
	// The live session was never touched by any refused mutation.
	if snap := exec.Session().Snapshot(); len(snap) != 3 {
		t.Fatalf("session mutated during rotation = %+v, want untouched", snap)
	}

	c.endRotation()

	// Conversely: while a turn runs, every mutation is refused with its own
	// message and the gate cannot be claimed.
	c.mu.Lock()
	c.running = true
	c.mu.Unlock()
	if err := c.beginRotation(); !errors.Is(err, errTurnRunningRotation) {
		t.Fatalf("beginRotation while running = %v, want errTurnRunningRotation", err)
	}
	if err := c.Compact(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "cannot compact while a turn is running") {
		t.Fatalf("Compact while running = %v, want 'cannot compact' message", err)
	}
	if err := c.Rewind(0, RewindConversation); err == nil || !strings.Contains(err.Error(), "cannot rewind while a turn is running") {
		t.Fatalf("Rewind while running = %v, want 'cannot rewind' message", err)
	}
	if err := c.SummarizeFrom(context.Background(), 1); err == nil || !strings.Contains(err.Error(), "cannot summarize while a turn is running") {
		t.Fatalf("SummarizeFrom while running = %v, want 'cannot summarize' message", err)
	}
}

func TestNewSessionQueuesSessionStartHookContext(t *testing.T) {
	dir := t.TempDir()
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	path := filepath.Join(dir, "session.jsonl")
	hooks := hook.NewRunner([]hook.ResolvedHook{{
		HookConfig: hook.HookConfig{Command: "session-start"},
		Event:      hook.SessionStart,
	}}, dir, func(context.Context, hook.SpawnInput) hook.SpawnResult {
		return hook.SpawnResult{ExitCode: 0, Stdout: "new session context"}
	}, nil)
	c := New(Options{Executor: exec, SystemPrompt: "sys", SessionDir: dir, SessionPath: path, Label: "test", Hooks: hooks})

	if err := c.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	got := c.Compose("next")
	if !strings.Contains(got, `<hook-context event="SessionStart">`) || !strings.Contains(got, "new session context") || !strings.HasSuffix(got, "next") {
		t.Fatalf("new session did not queue SessionStart hook context: %q", got)
	}
}

func TestNewSessionResetsTwoModelPlannerContext(t *testing.T) {
	dir := t.TempDir()
	planner := &recordingProvider{name: "planner", streams: [][]provider.Chunk{
		textTurn("OLD PLAN: inspect alpha.go"),
		textTurn("NEW PLAN: inspect beta.go"),
	}}
	execProv := &recordingProvider{name: "executor", streams: [][]provider.Chunk{
		textTurn("old done"),
		textTurn("new done"),
	}}
	exec := agent.New(execProv, tool.NewRegistry(), agent.NewSession("exec sys"), agent.Options{}, event.Discard)
	plannerSess := agent.NewSession("planner sys")
	coord := agent.NewCoordinator(planner, plannerSess, nil, tool.NewRegistry(), agent.Options{}, exec, 0, event.Discard, nil)
	path := filepath.Join(dir, "session.jsonl")
	c := New(Options{Runner: coord, Executor: exec, SystemPrompt: "exec sys", SessionDir: dir, SessionPath: path, Label: "test"})

	if err := c.Run(context.Background(), "old task alpha"); err != nil {
		t.Fatal(err)
	}
	if err := c.NewSession(); err != nil {
		t.Fatal(err)
	}
	if err := c.Run(context.Background(), "new task beta"); err != nil {
		t.Fatal(err)
	}

	if len(planner.requests) != 2 {
		t.Fatalf("planner requests = %d, want 2", len(planner.requests))
	}
	second := requestMessagesText(planner.requests[1].Messages)
	if strings.Contains(second, "old task alpha") || strings.Contains(second, "OLD PLAN") {
		t.Fatalf("new planner request leaked previous session context:\n%s", second)
	}
	if !strings.Contains(second, "new task beta") {
		t.Fatalf("new planner request missing current task:\n%s", second)
	}
}

func TestTwoModelPlannerApprovalUsesHostGate(t *testing.T) {
	dir := t.TempDir()
	planner := &recordingProvider{name: "planner", streams: [][]provider.Chunk{
		textTurn("Plan:\n1. Edit main.go\n\n是否批准这个方案？"),
	}}
	execProv := &recordingProvider{name: "executor", streams: [][]provider.Chunk{
		textTurn("approved execution complete"),
	}}
	exec := agent.New(execProv, tool.NewRegistry(), agent.NewSession("exec sys"), agent.Options{}, event.Discard)
	coord := agent.NewCoordinator(planner, agent.NewSession("planner sys"), nil, tool.NewRegistry(), agent.Options{}, exec, 0, event.Discard, nil)

	ids := make(chan string, 1)
	var prompts int
	c := New(Options{
		Runner:       coord,
		Executor:     exec,
		SystemPrompt: "exec sys",
		SessionDir:   dir,
		SessionPath:  filepath.Join(dir, "session.jsonl"),
		Label:        "test",
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind != event.ApprovalRequest {
				return
			}
			prompts++
			if e.Approval.Tool != planApprovalTool {
				t.Errorf("approval tool = %q, want %q", e.Approval.Tool, planApprovalTool)
			}
			if !strings.Contains(e.Approval.Reason, "Planner requested") {
				t.Errorf("approval reason = %q, want planner source", e.Approval.Reason)
			}
			ids <- e.Approval.ID
		}),
	})
	c.EnableInteractiveApproval()

	done := make(chan error, 1)
	go func() {
		done <- c.Run(context.Background(), "fix the planner approval bug")
	}()
	id := waitApprovalID(t, ids)
	if got := len(execProv.requests); got != 0 {
		t.Fatalf("executor requests before approval = %d, want 0", got)
	}
	c.Approve(id, true, false, false)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("approved two-model turn did not finish")
	}
	if prompts != 1 {
		t.Fatalf("approval prompts = %d, want 1", prompts)
	}
	if got := len(execProv.requests); got == 0 {
		t.Fatal("executor did not run after approval")
	}
	reqText := requestMessagesText(execProv.requests[0].Messages)
	if !strings.Contains(reqText, "Reasonix executor handoff") || !strings.Contains(reqText, "Edit main.go") {
		t.Fatalf("approved executor request missing planner handoff:\n%s", reqText)
	}
}

func TestTwoModelPlannerUserDecisionUsesAskGate(t *testing.T) {
	dir := t.TempDir()
	planner := &recordingProvider{name: "planner", streams: [][]provider.Chunk{
		textTurn("需要用户选择方案：\n方案一：小改当前逻辑\n方案二：重构控制流\n请选择哪个方案。"),
	}}
	execProv := &recordingProvider{name: "executor", streams: [][]provider.Chunk{
		textTurn("selected execution complete"),
	}}
	exec := agent.New(execProv, tool.NewRegistry(), agent.NewSession("exec sys"), agent.Options{}, event.Discard)
	coord := agent.NewCoordinator(planner, agent.NewSession("planner sys"), nil, tool.NewRegistry(), agent.Options{}, exec, 0, event.Discard, nil)

	asks := make(chan event.Ask, 1)
	c := New(Options{
		Runner:       coord,
		Executor:     exec,
		SystemPrompt: "exec sys",
		SessionDir:   dir,
		SessionPath:  filepath.Join(dir, "session.jsonl"),
		Label:        "test",
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.AskRequest {
				asks <- e.Ask
			}
		}),
	})
	c.EnableInteractiveApproval()

	done := make(chan error, 1)
	go func() {
		done <- c.Run(context.Background(), "fix the planner decision bug")
	}()
	var ask event.Ask
	select {
	case ask = <-asks:
	case <-time.After(30 * time.Second):
		t.Fatal("AskRequest was not emitted")
	}
	if got := len(execProv.requests); got != 0 {
		t.Fatalf("executor requests before user decision = %d, want 0", got)
	}
	if len(ask.Questions) != 1 || ask.Questions[0].ID != "planner_user_decision" {
		t.Fatalf("ask questions = %+v, want planner decision question", ask.Questions)
	}
	c.AnswerQuestion(ask.ID, []event.AskAnswer{{QuestionID: "planner_user_decision", Selected: []string{"方案二：重构控制流"}}})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("answered two-model turn did not finish")
	}
	if got := len(execProv.requests); got == 0 {
		t.Fatal("executor did not run after user decision")
	}
	reqText := requestMessagesText(execProv.requests[0].Messages)
	if !strings.Contains(reqText, "Host user answer to planner question") || !strings.Contains(reqText, "方案二") {
		t.Fatalf("executor request missing host user answer:\n%s", reqText)
	}
}

func TestResumeResetsTwoModelPlannerContext(t *testing.T) {
	dir := t.TempDir()
	planner := &recordingProvider{name: "planner", streams: [][]provider.Chunk{
		textTurn("OLD PLAN: inspect alpha.go"),
		textTurn("RESUMED PLAN: inspect gamma.go"),
	}}
	execProv := &recordingProvider{name: "executor", streams: [][]provider.Chunk{
		textTurn("old done"),
		textTurn("resumed done"),
	}}
	exec := agent.New(execProv, tool.NewRegistry(), agent.NewSession("exec sys"), agent.Options{}, event.Discard)
	plannerSess := agent.NewSession("planner sys")
	coord := agent.NewCoordinator(planner, plannerSess, nil, tool.NewRegistry(), agent.Options{}, exec, 0, event.Discard, nil)
	c := New(Options{Runner: coord, Executor: exec, SystemPrompt: "exec sys", SessionDir: dir, SessionPath: filepath.Join(dir, "old.jsonl"), Label: "test"})

	if err := c.Run(context.Background(), "old task alpha"); err != nil {
		t.Fatal(err)
	}
	resumed := agent.NewSession("exec sys")
	resumed.Add(provider.Message{Role: provider.RoleUser, Content: "saved task gamma"})
	c.Resume(resumed, filepath.Join(dir, "resumed.jsonl"))
	if err := c.Run(context.Background(), "continue gamma"); err != nil {
		t.Fatal(err)
	}

	if len(planner.requests) != 2 {
		t.Fatalf("planner requests = %d, want 2", len(planner.requests))
	}
	second := requestMessagesText(planner.requests[1].Messages)
	if strings.Contains(second, "old task alpha") || strings.Contains(second, "OLD PLAN") {
		t.Fatalf("resumed planner request leaked previous session context:\n%s", second)
	}
	if !strings.Contains(second, "continue gamma") {
		t.Fatalf("resumed planner request missing current task:\n%s", second)
	}
}

func TestResetPlannerSessionClearsPlannerHistory(t *testing.T) {
	dir := t.TempDir()
	planner := &recordingProvider{name: "planner", streams: [][]provider.Chunk{
		textTurn("FIRST PLAN: inspect alpha.go"),
		textTurn("SECOND PLAN: inspect beta.go"),
	}}
	execProv := &recordingProvider{name: "executor", streams: [][]provider.Chunk{
		textTurn("first done"),
		textTurn("second done"),
	}}
	exec := agent.New(execProv, tool.NewRegistry(), agent.NewSession("exec sys"), agent.Options{}, event.Discard)
	plannerSess := agent.NewSession("planner sys")
	coord := agent.NewCoordinator(planner, plannerSess, nil, tool.NewRegistry(), agent.Options{}, exec, 0, event.Discard, nil)
	path := filepath.Join(dir, "session.jsonl")
	c := New(Options{Runner: coord, Executor: exec, SystemPrompt: "exec sys", SessionDir: dir, SessionPath: path, Label: "test"})

	if err := c.Run(context.Background(), "first task"); err != nil {
		t.Fatal(err)
	}
	// Explicitly reset the planner session (simulates a tab switch).
	c.ResetPlannerSession()
	if err := c.Run(context.Background(), "second task"); err != nil {
		t.Fatal(err)
	}

	if len(planner.requests) != 2 {
		t.Fatalf("planner requests = %d, want 2", len(planner.requests))
	}
	second := requestMessagesText(planner.requests[1].Messages)
	if strings.Contains(second, "first task") || strings.Contains(second, "FIRST PLAN") {
		t.Fatalf("planner request after reset leaked previous session context:\n%s", second)
	}
	if !strings.Contains(second, "second task") {
		t.Fatalf("planner request after reset missing current task:\n%s", second)
	}
}

func TestTwoModelShortChoiceReplySkipsPlanner(t *testing.T) {
	dir := t.TempDir()
	planner := &recordingProvider{name: "planner", streams: [][]provider.Chunk{
		textTurn("planner should not run for a context-dependent choice reply"),
	}}
	execProv := &recordingProvider{name: "executor", streams: [][]provider.Chunk{
		textTurn("selected option 1"),
	}}
	execSess := agent.NewSession("exec sys")
	execSess.Add(provider.Message{Role: provider.RoleUser, Content: "先给我两个执行方案"})
	execSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "两个执行方式可选：\n\n1. Subagent-Driven（推荐）\n2. 当前会话执行\n\n你选哪种？"})
	exec := agent.New(execProv, tool.NewRegistry(), execSess, agent.Options{}, event.Discard)
	coord := agent.NewCoordinator(planner, agent.NewSession("planner sys"), nil, tool.NewRegistry(), agent.Options{}, exec, 0, event.Discard, NewPlannerGate(nil))
	c := New(Options{Runner: coord, Executor: exec, SystemPrompt: "exec sys", SessionDir: dir, SessionPath: filepath.Join(dir, "session.jsonl"), Label: "test"})

	if err := c.Run(context.Background(), "1"); err != nil {
		t.Fatal(err)
	}

	if len(planner.requests) != 0 {
		t.Fatalf("planner requests = %d, want 0 for a short context-dependent choice reply", len(planner.requests))
	}
	if len(execProv.requests) != 1 {
		t.Fatalf("executor requests = %d, want 1", len(execProv.requests))
	}
	reqText := requestMessagesText(execProv.requests[0].Messages)
	if !strings.Contains(reqText, "1. Subagent-Driven") {
		t.Fatalf("executor request lost the previous assistant options:\n%s", reqText)
	}
	if strings.Contains(reqText, "Reasonix executor handoff") {
		t.Fatalf("short choice reply should not be wrapped as a planner handoff:\n%s", reqText)
	}
	if got := lastUserMessage(execProv.requests[0].Messages); got != "1" {
		t.Fatalf("executor last user = %q, want raw choice reply", got)
	}
}

func TestSubmitClearDiscardsCurrentContextWithoutSavingTranscript(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "old context"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	path := filepath.Join(dir, "session.jsonl")
	cleared := make(chan struct{})
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Text == "context cleared" {
			close(cleared)
		}
	})
	c := New(Options{Executor: exec, SystemPrompt: "sys", SessionDir: dir, SessionPath: path, Label: "test", Sink: sink})
	if err := c.Snapshot(); err != nil {
		t.Fatal(err)
	}
	ckpt := ckptDir(path)
	if err := os.MkdirAll(ckpt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ckpt, "turn-0.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	c.submit("/clear", "", "")
	select {
	case <-cleared:
	case <-time.After(30 * time.Second):
		t.Fatal("/clear did not finish")
	}
	if c.SessionPath() == path {
		t.Fatal("/clear did not rotate to a fresh session path")
	}
	for _, p := range []string{path, agent.BranchMetaPath(path), ckpt} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("discarded artifact %s still exists or stat failed with %v", p, err)
		}
	}
	if _, err := os.Stat(c.SessionPath()); !os.IsNotExist(err) {
		t.Fatalf("fresh empty session should not be saved yet; stat err=%v", err)
	}
	current := exec.Session().Snapshot()
	if len(current) != 1 || current[0].Role != provider.RoleSystem || current[0].Content != "sys" {
		t.Fatalf("cleared context = %+v, want only system prompt", current)
	}
}

func TestDisconnectMCPServerRemovesLazyPlaceholder(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeControlTool{name: "mcp__mock__connect"})
	c := New(Options{Host: plugin.NewHost(), Registry: reg})

	if ok := c.DisconnectMCPServer("mock"); !ok {
		t.Fatal("DisconnectMCPServer returned false for a registered lazy placeholder")
	}
	if _, found := reg.Get("mcp__mock__connect"); found {
		t.Fatalf("lazy placeholder still registered after disconnect; names=%v", reg.Names())
	}
}

func TestUnregisterMCPServerToolsBlocksLateSharedHostSwap(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeControlTool{name: "mcp__mock__connect"})
	c := New(Options{Host: plugin.NewHost(), Registry: reg})

	if ok := c.UnregisterMCPServerTools("mock"); !ok {
		t.Fatal("UnregisterMCPServerTools returned false")
	}
	reg.Add(fakeControlTool{name: "mcp__mock__echo"})
	if _, found := reg.Get("mcp__mock__echo"); found {
		t.Fatalf("late shared-host tool swap was accepted after unregister; names=%v", reg.Names())
	}
	reg.Add(fakeControlTool{name: "mcp__other__echo"})
	if _, found := reg.Get("mcp__other__echo"); !found {
		t.Fatalf("unregister blocked unrelated MCP tools; names=%v", reg.Names())
	}
}

func TestRemoveMCPServerRemovesUnconnectedLazyPlaceholder(t *testing.T) {
	isolateControlConfigHome(t)
	dir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData", "Roaming"))
	t.Chdir(dir)
	if err := os.WriteFile("reasonix.toml", []byte(`
[[plugins]]
name = "mock"
command = "mock-mcp"
tier = "lazy"
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	reg := tool.NewRegistry()
	reg.Add(fakeControlTool{name: "mcp__mock__connect"})
	c := New(Options{Host: plugin.NewHost(), Registry: reg})

	disconnected, err := c.RemoveMCPServer("mock")
	if err != nil {
		t.Fatalf("RemoveMCPServer: %v", err)
	}
	if disconnected {
		t.Fatal("RemoveMCPServer reported a live disconnect for an unconnected lazy placeholder")
	}
	if _, found := reg.Get("mcp__mock__connect"); found {
		t.Fatalf("lazy placeholder still registered after remove; names=%v", reg.Names())
	}
	if names := c.ConfiguredMCPNames(); len(names) != 0 {
		t.Fatalf("ConfiguredMCPNames() = %v, want empty after remove", names)
	}
}

func TestRemoveMCPServerKeepsRuntimeOnlyToolsWhenPersistenceRemovalFails(t *testing.T) {
	isolateControlConfigHome(t)
	reg := tool.NewRegistry()
	reg.Add(fakeControlTool{name: "mcp__runtime_only__echo"})
	c := New(Options{Host: plugin.NewHost(), Registry: reg})

	if disconnected, err := c.RemoveMCPServer("runtime_only"); err == nil || disconnected || !strings.Contains(err.Error(), "no removable MCP server") {
		t.Fatalf("RemoveMCPServer(runtime_only) = (%v, %v)", disconnected, err)
	}
	if _, found := reg.Get("mcp__runtime_only__echo"); !found {
		t.Fatalf("runtime-only tool was removed despite failed persistence; names=%v", reg.Names())
	}
}

func TestRemoveMCPServerRejectsPluginManagedTools(t *testing.T) {
	home := isolateControlConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	root := filepath.Join(reasonixHome, "plugins", "superpowers")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, pluginpkg.NativeManifest), []byte(`{
  "name": "superpowers",
  "version": "1.0.0",
  "mcpServers": {
    "helper": { "command": "bin/helper" }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pluginpkg.Upsert(reasonixHome, pluginpkg.InstalledPlugin{
		Name:         "superpowers",
		Root:         "plugins/superpowers",
		Version:      "1.0.0",
		ManifestKind: "reasonix",
		Enabled:      true,
	}); err != nil {
		t.Fatal(err)
	}

	reg := tool.NewRegistry()
	reg.Add(fakeControlTool{name: "mcp__helper__echo"})
	c := New(Options{Host: plugin.NewHost(), Registry: reg})
	disconnected, err := c.RemoveMCPServer("helper")
	if err == nil || disconnected || !strings.Contains(err.Error(), "managed by plugin") || !strings.Contains(err.Error(), "superpowers") {
		t.Fatalf("RemoveMCPServer(plugin-managed) = (%v, %v)", disconnected, err)
	}
	if _, found := reg.Get("mcp__helper__echo"); !found {
		t.Fatalf("plugin-managed tool was removed despite rejected removal; names=%v", reg.Names())
	}
	if disconnected := c.DisconnectMCPServer("helper"); !disconnected {
		t.Fatal("session-only disconnect should be allowed for a plugin-managed MCP")
	}
	if _, found := reg.Get("mcp__helper__echo"); found {
		t.Fatalf("plugin-managed tool survived session-only disconnect; names=%v", reg.Names())
	}
}

func TestRemoveMCPServerDeletesProjectMCPJSONSource(t *testing.T) {
	isolateControlConfigHome(t)
	if err := os.WriteFile(".mcp.json", []byte(`{
  "mcpServers": {
    "mock": { "command": "mock-mcp" },
    "keep": { "command": "keep-mcp" }
  }
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := tool.NewRegistry()
	reg.Add(fakeControlTool{name: "mcp__mock__connect"})
	c := New(Options{Host: plugin.NewHost(), Registry: reg})
	disconnected, err := c.RemoveMCPServer("mock")
	if err != nil {
		t.Fatalf("RemoveMCPServer(.mcp.json): %v", err)
	}
	if disconnected {
		t.Fatal("RemoveMCPServer reported a live disconnect for an idle .mcp.json server")
	}
	if _, found := reg.Get("mcp__mock__connect"); found {
		t.Fatalf(".mcp.json placeholder survived removal; names=%v", reg.Names())
	}
	raw, err := os.ReadFile(".mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"mock"`) || !strings.Contains(string(raw), `"keep"`) {
		t.Fatalf(".mcp.json removal did not preserve unrelated servers:\n%s", raw)
	}
}

// approvalIDs returns a Controller whose Sink forwards each ApprovalRequest's ID
// onto the channel, plus a counter of how many requests it emitted.
func approvalIDs() (*Controller, chan string, *int) {
	ids := make(chan string, 8)
	prompts := 0
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			prompts++
			ids <- e.Approval.ID
		}
	})})
	return c, ids, &prompts
}

func permissionHookController(t *testing.T, match string) (*Controller, chan string, chan hook.Payload) {
	t.Helper()
	ids := make(chan string, 8)
	payloads := make(chan hook.Payload, 8)
	spawner := func(_ context.Context, in hook.SpawnInput) hook.SpawnResult {
		var payload hook.Payload
		if err := json.Unmarshal([]byte(in.Stdin), &payload); err != nil {
			t.Errorf("permission hook payload json: %v", err)
		}
		payloads <- payload
		return hook.SpawnResult{ExitCode: 0}
	}
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				ids <- e.Approval.ID
			}
		}),
		Hooks: hook.NewRunner([]hook.ResolvedHook{{
			HookConfig: hook.HookConfig{Command: "notify", Match: match},
			Event:      hook.PermissionRequest,
			Scope:      hook.ScopeGlobal,
		}}, "/tmp", spawner, nil),
	})
	return c, ids, payloads
}

func waitApprovalID(t *testing.T, ids <-chan string) string {
	t.Helper()
	select {
	case id := <-ids:
		return id
	case <-time.After(30 * time.Second):
		t.Fatal("ApprovalRequest was not emitted")
	}
	return ""
}

func waitPermissionHook(t *testing.T, payloads <-chan hook.Payload) hook.Payload {
	t.Helper()
	select {
	case payload := <-payloads:
		return payload
	case <-time.After(30 * time.Second):
		t.Fatal("PermissionRequest hook did not fire")
	}
	return hook.Payload{}
}

func assertNoPermissionHook(t *testing.T, payloads <-chan hook.Payload) {
	t.Helper()
	select {
	case payload := <-payloads:
		t.Fatalf("PermissionRequest hook fired unexpectedly: %+v", payload)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestApprovalAllowOnce drives the happy path: the gate emits an ApprovalRequest,
// the (fake) frontend answers allow, and the gate returns allow with no grant.
func TestApprovalAllowOnce(t *testing.T) {
	c, ids, _ := approvalIDs()
	go func() { c.Approve(<-ids, true, false, false) }()

	allow, remember, err := gateApprover{c}.Approve(context.Background(), "bash", "go test", nil)
	if err != nil || !allow || remember {
		t.Fatalf("Approve = (%v,%v,%v), want allow once", allow, remember, err)
	}
}

func TestMemoryApprovalRequestShowsRememberPayload(t *testing.T) {
	approvals := make(chan event.Approval, 1)
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			approvals <- e.Approval
		}
	})})

	args := json.RawMessage(`{
		"name": "stable-retrieval-conclusion",
		"description": "History retrieval should reuse stable synthesized conclusions.",
		"type": "feedback",
		"body": "**Why:** repeated history scans are expensive.\n\n**How to apply:** save the stable summary as a memory document."
	}`)
	result := make(chan string, 1)
	go func() {
		allow, _, err := gateApprover{c}.Approve(context.Background(), "remember", "", args)
		if err != nil {
			result <- err.Error()
			return
		}
		if !allow {
			result <- "memory approval denied"
			return
		}
		result <- ""
	}()

	var approval event.Approval
	select {
	case approval = <-approvals:
	case <-time.After(30 * time.Second):
		t.Fatal("memory approval request was not emitted")
	}
	for _, want := range []string{
		`Save/update memory "stable-retrieval-conclusion"`,
		"[feedback]",
		"History retrieval should reuse stable synthesized conclusions.",
		"repeated history scans are expensive",
		"save the stable summary",
	} {
		if !strings.Contains(approval.Subject, want) {
			t.Fatalf("approval subject %q does not contain %q", approval.Subject, want)
		}
	}
	if strings.Contains(approval.Subject, "\n") {
		t.Fatalf("approval subject should be compact for TUI rendering, got %q", approval.Subject)
	}

	c.Approve(approval.ID, true, true, true)
	select {
	case msg := <-result:
		if msg != "" {
			t.Fatalf("Approve returned %s", msg)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("memory approval stayed blocked after Approve")
	}
}

func TestGuardianCannotAutoAllowFreshHumanApprovalTools(t *testing.T) {
	guardianProv := &recordingProvider{
		name:    "guardian",
		streams: [][]provider.Chunk{textTurn(`{"risk_level":"low","user_authorization":"high","outcome":"allow","rationale":"authorized memory update"}`)},
	}
	guardianSess := guardian.NewSession(guardianProv, tool.NewRegistry(), guardian.PolicyPrompt(), "guardian-test", 0, nil, event.Discard)
	exec := agent.New(&recordingProvider{name: "executor"}, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)

	approvals := make(chan event.Approval, 1)
	c := New(Options{
		Executor: exec,
		Guardian: guardianSess,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvals <- e.Approval
			}
		}),
	})

	args := json.RawMessage(`{"name":"prefers-vitest","description":"Preferred test framework","body":"Use vitest for frontend tests."}`)
	type approveResult struct {
		allow    bool
		remember bool
		err      error
	}
	done := make(chan approveResult, 1)
	go func() {
		allow, remember, err := gateApprover{c}.Approve(context.Background(), "remember", "", args)
		done <- approveResult{allow: allow, remember: remember, err: err}
	}()

	var approval event.Approval
	select {
	case approval = <-approvals:
	case <-time.After(30 * time.Second):
		t.Fatal("memory approval request was not emitted after Guardian allow")
	}
	if approval.Tool != "remember" {
		t.Fatalf("approval tool = %q, want remember", approval.Tool)
	}
	if len(guardianProv.requests) != 1 {
		t.Fatalf("guardian reviews = %d, want 1", len(guardianProv.requests))
	}
	select {
	case got := <-done:
		t.Fatalf("Guardian must not auto-allow remember, got %+v", got)
	case <-time.After(50 * time.Millisecond):
	}

	c.Approve(approval.ID, true, true, true)
	select {
	case got := <-done:
		if got.err != nil || !got.allow || got.remember {
			t.Fatalf("Approve = (%v,%v,%v), want manual allow without remember", got.allow, got.remember, got.err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("memory approval stayed blocked after manual Approve")
	}
}

func TestHeadlessGateRefusesFreshHumanApprovalTools(t *testing.T) {
	gate := NewHeadlessPermissionGate(permission.New("ask", nil, nil, nil))

	for _, toolName := range []string{"remember", "forget"} {
		allow, reason, err := gate.Check(context.Background(), toolName, json.RawMessage(`{}`), false)
		if err != nil || allow || !strings.Contains(reason, "fresh human approval") {
			t.Fatalf("%s headless check = (%v,%q,%v), want fresh-human refusal", toolName, allow, reason, err)
		}
	}

	allow, reason, err := gate.Check(context.Background(), "bash", json.RawMessage(`{"command":"go test ./..."}`), false)
	if err != nil || !allow || reason != "" {
		t.Fatalf("ordinary headless ask = (%v,%q,%v), want autonomous allow", allow, reason, err)
	}
}

func TestMemoryApprovalSubjectsAndNotifications(t *testing.T) {
	forgetSubject := approvalDisplaySubject("forget", "", json.RawMessage(`{"name":"wrong-memory"}`))
	if forgetSubject != `Archive memory "wrong-memory"` {
		t.Fatalf("forget approval subject = %q", forgetSubject)
	}
	if got := approvalNotificationText("remember", "Save/update memory with private details"); got != "approval needed: remember" {
		t.Fatalf("remember notification = %q", got)
	}
	if got := approvalNotificationText("forget", `Archive memory "wrong-memory"`); got != "approval needed: forget" {
		t.Fatalf("forget notification = %q", got)
	}
	if got := approvalNotificationText("bash", "go test ./..."); got != "approval needed: bash go test ./..." {
		t.Fatalf("bash notification = %q", got)
	}
	moveSubject := approvalDisplaySubject("move_file", "src/a.md", json.RawMessage(`{"source_path":"src/a.md","destination_path":"docs/a.md"}`))
	if moveSubject != "src/a.md -> docs/a.md" {
		t.Fatalf("move_file approval subject = %q", moveSubject)
	}
}

func TestPermissionRequestHookFiresForToolApproval(t *testing.T) {
	c, ids, payloads := permissionHookController(t, "bash")
	args := json.RawMessage(`{"command":"go test ./..."}`)
	type approveResult struct {
		allow    bool
		remember bool
		err      error
	}
	done := make(chan approveResult, 1)
	go func() {
		allow, remember, err := gateApprover{c}.Approve(context.Background(), "bash", "go test ./...", args)
		done <- approveResult{allow: allow, remember: remember, err: err}
	}()

	id := waitApprovalID(t, ids)
	payload := waitPermissionHook(t, payloads)
	if payload.Event != hook.PermissionRequest {
		t.Fatalf("payload event = %q, want PermissionRequest", payload.Event)
	}
	if payload.ToolName != "bash" {
		t.Fatalf("payload tool = %q, want bash", payload.ToolName)
	}
	if payload.Subject != "go test ./..." {
		t.Fatalf("payload subject = %q, want command subject", payload.Subject)
	}
	if string(payload.ToolArgs) != string(args) {
		t.Fatalf("payload args = %s, want %s", payload.ToolArgs, args)
	}

	c.Approve(id, true, false, false)
	select {
	case got := <-done:
		if got.err != nil || !got.allow || got.remember {
			t.Fatalf("Approve = (%v,%v,%v), want allow once", got.allow, got.remember, got.err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("approval stayed blocked")
	}
}

func TestPermissionRequestHookDoesNotFireForPolicyAllow(t *testing.T) {
	c, _, payloads := permissionHookController(t, "bash")
	g := permission.NewGate(permission.New("ask", []string{"bash(go test*)"}, nil, nil), gateApprover{c})

	allow, _, err := g.Check(context.Background(), "bash", json.RawMessage(`{"command":"go test ./..."}`), false)
	if err != nil || !allow {
		t.Fatalf("allow-listed call = (%v,%v), want allowed", allow, err)
	}
	assertNoPermissionHook(t, payloads)
}

func TestPermissionRequestHookDoesNotFireForAutoApprovalMode(t *testing.T) {
	c, _, payloads := permissionHookController(t, "bash")
	c.SetToolApprovalMode(ToolApprovalAuto)
	g := c.newInteractiveGate()

	allow, _, err := g.Check(context.Background(), "bash", json.RawMessage(`{"command":"go test ./..."}`), false)
	if err != nil || !allow {
		t.Fatalf("auto-approved call = (%v,%v), want allowed", allow, err)
	}
	assertNoPermissionHook(t, payloads)
}

func TestPermissionRequestHookDoesNotFireForSessionGrant(t *testing.T) {
	c, _, payloads := permissionHookController(t, "bash")
	c.approval.grantSession("bash", "go test ./...")

	allow, _, err := c.requestApproval(context.Background(), "bash", "go test ./...", nil)
	if err != nil || !allow {
		t.Fatalf("session-granted approval = (%v,%v), want allowed", allow, err)
	}
	assertNoPermissionHook(t, payloads)
}

func TestPermissionRequestHookDoesNotFireForYolo(t *testing.T) {
	c, _, payloads := permissionHookController(t, "bash")
	c.SetToolApprovalMode(ToolApprovalYolo)

	allow, _, err := c.requestApproval(context.Background(), "bash", "go test ./...", nil)
	if err != nil || !allow {
		t.Fatalf("YOLO approval = (%v,%v), want allowed", allow, err)
	}
	assertNoPermissionHook(t, payloads)
}

func TestPermissionRequestHookDoesNotFireForPlanApproval(t *testing.T) {
	c, ids, payloads := permissionHookController(t, ".*")
	done := make(chan bool, 1)
	errs := make(chan error, 1)
	go func() {
		allow, _, err := c.requestApproval(context.Background(), planApprovalTool, "", nil)
		if err != nil {
			errs <- err
			return
		}
		done <- allow
	}()

	id := waitApprovalID(t, ids)
	assertNoPermissionHook(t, payloads)
	c.Approve(id, true, false, false)

	select {
	case err := <-errs:
		t.Fatalf("plan approval: %v", err)
	case allow := <-done:
		if !allow {
			t.Fatal("manual plan approval should allow")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("plan approval stayed blocked")
	}
}

func TestPermissionRequestHookRedactsMemoryApprovalPayload(t *testing.T) {
	cases := []struct {
		tool string
		args json.RawMessage
	}{
		{
			tool: "remember",
			args: json.RawMessage(`{"name":"private-memory","description":"private description","body":"private memory body"}`),
		},
		{
			tool: "forget",
			args: json.RawMessage(`{"name":"private-memory"}`),
		},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			c, ids, payloads := permissionHookController(t, tc.tool)
			done := make(chan string, 1)
			go func() {
				allow, _, err := gateApprover{c}.Approve(context.Background(), tc.tool, "", tc.args)
				if err != nil {
					done <- err.Error()
					return
				}
				if !allow {
					done <- tc.tool + " approval denied"
					return
				}
				done <- ""
			}()

			id := waitApprovalID(t, ids)
			payload := waitPermissionHook(t, payloads)
			if payload.ToolName != tc.tool {
				t.Fatalf("payload tool = %q, want %s", payload.ToolName, tc.tool)
			}
			if payload.Subject != "" {
				t.Fatalf("memory PermissionRequest subject = %q, want redacted", payload.Subject)
			}
			if len(payload.ToolArgs) != 0 {
				t.Fatalf("memory PermissionRequest args = %s, want redacted", payload.ToolArgs)
			}

			c.Approve(id, true, false, false)
			select {
			case msg := <-done:
				if msg != "" {
					t.Fatal(msg)
				}
			case <-time.After(30 * time.Second):
				t.Fatal("memory approval stayed blocked")
			}
		})
	}
}

// TestApprovalDeny confirms a declined call returns allow=false.
func TestApprovalDeny(t *testing.T) {
	c, ids, _ := approvalIDs()
	go func() { c.Approve(<-ids, false, false, false) }()

	allow, _, err := gateApprover{c}.Approve(context.Background(), "bash", "rm -rf /", nil)
	if err != nil || allow {
		t.Fatalf("Approve = (%v,%v), want deny", allow, err)
	}
}

// TestApprovalSessionGrantScopesBashToCommand proves an "allow this session"
// answer short-circuits later prompts for the same bash command, but a different
// command still reaches the frontend.
func TestApprovalSessionGrantScopesBashToCommand(t *testing.T) {
	c, ids, prompts := approvalIDs()
	go func() {
		c.Approve(<-ids, true, true, false) // grant go build for this session
		c.Approve(<-ids, true, false, false)
	}()

	for i, subject := range []string{"go build", "go build", "go test ./..."} {
		allow, _, err := gateApprover{c}.Approve(context.Background(), "bash", subject, nil)
		if err != nil || !allow {
			t.Fatalf("call %d = (%v,%v), want allow", i, allow, err)
		}
	}
	if *prompts != 2 {
		t.Errorf("prompted %d times, want 2 (same command granted, different command prompts)", *prompts)
	}
}

func TestApprovalSessionGrantCanScopeBashToCommandPrefix(t *testing.T) {
	c, ids, prompts := approvalIDs()
	go func() {
		c.Approve(<-ids, true, true, false) // grant bash session (prefix preferred)
		c.Approve(<-ids, true, false, false)
	}()

	for i, subject := range []string{"go test ./...", "go test ./internal/control", "go build ./..."} {
		allow, _, err := gateApprover{c}.Approve(context.Background(), "bash", subject, nil)
		if err != nil || !allow {
			t.Fatalf("call %d = (%v,%v), want allow", i, allow, err)
		}
	}
	if *prompts != 2 {
		t.Errorf("prompted %d times, want 2 (prefix grant should cover similar command only)", *prompts)
	}
}

func TestApprovalPersistentBashPrefixRememberRule(t *testing.T) {
	ids := make(chan string, 1)
	var remembered string
	var notices []string
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				ids <- e.Approval.ID
			}
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
		}),
		OnRemember: func(rule string) RememberResult {
			remembered = rule
			return RememberResult{Rule: rule, Path: "reasonix.toml", Saved: true}
		},
	})
	go func() {
		c.Approve(<-ids, true, true, true)
	}()

	allow, remember, err := gateApprover{c}.Approve(context.Background(), "bash", "go test ./...", nil)
	if err != nil || !allow || remember {
		t.Fatalf("Approve = (%v,%v,%v), want allow with controller-managed persistence", allow, remember, err)
	}
	if remembered != "Bash(go test:*)" {
		t.Fatalf("remembered rule = %q, want Bash(go test:*)", remembered)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "Bash(go test:*)") || !strings.Contains(notices[0], "reasonix.toml") {
		t.Fatalf("notices = %v, want saved rule notice", notices)
	}
}

func TestPlanModeReadOnlyTrustApprovalPersistsMCPTrust(t *testing.T) {
	ids := make(chan string, 2)
	var approval event.Approval
	var notices []string
	var rememberedServer, rememberedTool string
	prompts := 0
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				prompts++
				approval = e.Approval
				ids <- e.Approval.ID
			}
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
		}),
		OnRememberMCPReadOnlyTrust: func(serverName, rawToolName string) MCPReadOnlyTrustResult {
			rememberedServer, rememberedTool = serverName, rawToolName
			return MCPReadOnlyTrustResult{Server: serverName, Tool: rawToolName, Path: "reasonix.toml", Saved: true}
		},
	})

	go func() {
		c.Approve(<-ids, true, true, true)
	}()
	req := agent.PlanModeReadOnlyTrustRequest{
		ToolName:    "mcp__github__issue_read",
		ServerName:  "github",
		RawToolName: "issue/read",
		Args:        json.RawMessage(`{"issue":1}`),
	}
	allow, reason, err := planModeReadOnlyTrustApprover{c}.CheckPlanModeReadOnlyTrust(context.Background(), req)
	if err != nil || !allow || reason != "" {
		t.Fatalf("CheckPlanModeReadOnlyTrust = (%v,%q,%v), want allow", allow, reason, err)
	}
	if approval.Tool != "mcp__github__issue_read" || !strings.Contains(approval.Subject, "github/issue/read") || !strings.Contains(approval.Reason, "read-only") {
		t.Fatalf("approval = %+v, want MCP read-only trust prompt", approval)
	}
	if rememberedServer != "github" || rememberedTool != "issue/read" {
		t.Fatalf("remembered MCP trust = %s/%s, want github/issue/read", rememberedServer, rememberedTool)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "github/issue/read") {
		t.Fatalf("notices = %v, want MCP trust saved notice", notices)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	allow, reason, err = planModeReadOnlyTrustApprover{c}.CheckPlanModeReadOnlyTrust(ctx, req)
	if err != nil || !allow || reason != "" {
		t.Fatalf("second CheckPlanModeReadOnlyTrust = (%v,%q,%v), want session grant", allow, reason, err)
	}
	if prompts != 1 {
		t.Fatalf("approval prompts = %d, want 1", prompts)
	}
}

func TestPlanModeReadOnlyTrustApprovalPersistsBashCommandTrust(t *testing.T) {
	ids := make(chan string, 2)
	var approval event.Approval
	var notices []string
	var rememberedPrefix string
	prompts := 0
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				prompts++
				approval = e.Approval
				ids <- e.Approval.ID
			}
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
		}),
		OnRememberPlanModeReadOnlyCommand: func(prefix string) PlanModeReadOnlyCommandTrustResult {
			rememberedPrefix = prefix
			return PlanModeReadOnlyCommandTrustResult{Prefix: prefix, Path: "reasonix.toml", Saved: true}
		},
	})

	go func() {
		c.Approve(<-ids, true, true, true)
	}()
	req := agent.PlanModeReadOnlyTrustRequest{
		ToolName: agent.PlanModeReadOnlyCommandApprovalTool,
		Command:  "gh issue view 5867 --json title",
		Prefix:   "gh issue view",
		Args:     json.RawMessage(`{"command":"gh issue view 5867 --json title"}`),
	}
	allow, reason, err := planModeReadOnlyTrustApprover{c}.CheckPlanModeReadOnlyTrust(context.Background(), req)
	if err != nil || !allow || reason != "" {
		t.Fatalf("CheckPlanModeReadOnlyTrust = (%v,%q,%v), want allow", allow, reason, err)
	}
	if approval.Tool != agent.PlanModeReadOnlyCommandApprovalTool || !strings.Contains(approval.Subject, `Trust "gh issue view"`) || !strings.Contains(approval.Subject, "gh issue view 5867") || !strings.Contains(approval.Reason, "Auto/YOLO") {
		t.Fatalf("approval = %+v, want plan-mode bash read-only command trust prompt", approval)
	}
	if rememberedPrefix != "gh issue view" {
		t.Fatalf("remembered prefix = %q, want gh issue view", rememberedPrefix)
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "gh issue view") {
		t.Fatalf("notices = %v, want read-only command trust saved notice", notices)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	allow, reason, err = planModeReadOnlyTrustApprover{c}.CheckPlanModeReadOnlyTrust(ctx, req)
	if err != nil || !allow || reason != "" {
		t.Fatalf("second CheckPlanModeReadOnlyTrust = (%v,%q,%v), want session grant", allow, reason, err)
	}
	if prompts != 1 {
		t.Fatalf("approval prompts = %d, want 1", prompts)
	}
}

func TestApprovalSubjectsUseChineseCatalog(t *testing.T) {
	i18n.DetectLanguage("zh")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	rememberArgs := json.RawMessage(`{"name":"prefers-vitest","type":"user","description":"Preferred test framework","body":"Use Vitest for frontend tests."}`)
	if got := approvalDisplaySubject(memoryRememberTool, "", rememberArgs); !strings.Contains(got, "保存/更新记忆") || !strings.Contains(got, "正文: Use Vitest") {
		t.Fatalf("remember approval subject = %q, want Chinese labels", got)
	}

	forgetArgs := json.RawMessage(`{"name":"old-fact"}`)
	if got := approvalDisplaySubject(memoryForgetTool, "", forgetArgs); got != `归档记忆 "old-fact"` {
		t.Fatalf("forget approval subject = %q, want Chinese archive label", got)
	}
}

func TestPlanModeReadOnlyTrustApprovalUsesChineseCatalog(t *testing.T) {
	i18n.DetectLanguage("zh")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})
	done := make(chan struct {
		allow  bool
		reason string
		err    error
	}, 1)
	req := agent.PlanModeReadOnlyTrustRequest{
		ToolName: agent.PlanModeReadOnlyCommandApprovalTool,
		Command:  "gh issue view 5867 --json title",
		Prefix:   "gh issue view",
		Args:     json.RawMessage(`{"command":"gh issue view 5867 --json title"}`),
	}
	go func() {
		allow, reason, err := planModeReadOnlyTrustApprover{c}.CheckPlanModeReadOnlyTrust(context.Background(), req)
		done <- struct {
			allow  bool
			reason string
			err    error
		}{allow: allow, reason: reason, err: err}
	}()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(30 * time.Second):
		t.Fatal("plan-mode bash trust approval request was not emitted")
	}
	if !strings.Contains(approval.Subject, "在计划模式中信任") || !strings.Contains(approval.Subject, "gh issue view 5867") {
		t.Fatalf("approval subject = %q, want Chinese plan-mode trust subject", approval.Subject)
	}
	if !strings.Contains(approval.Reason, "不在 Reasonix 内置只读集合中") {
		t.Fatalf("approval reason = %q, want Chinese plan-mode trust reason", approval.Reason)
	}

	c.Approve(approval.ID, false, false, false)
	select {
	case got := <-done:
		if got.err != nil || got.allow || !strings.Contains(got.reason, "用户拒绝") {
			t.Fatalf("rejected trust result = %+v, want Chinese denial", got)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("plan-mode bash trust approval stayed blocked after rejection")
	}
}

func TestPlanModeReadOnlyTrustApprovalIgnoresToolAutoApproval(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})
	c.SetAutoApproveTools(true)

	type trustResult struct {
		allow  bool
		reason string
		err    error
	}
	done := make(chan trustResult, 1)
	req := agent.PlanModeReadOnlyTrustRequest{
		ToolName:    "mcp__github__issue_read",
		ServerName:  "github",
		RawToolName: "issue/read",
	}
	go func() {
		allow, reason, err := planModeReadOnlyTrustApprover{c}.CheckPlanModeReadOnlyTrust(context.Background(), req)
		done <- trustResult{allow: allow, reason: reason, err: err}
	}()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(30 * time.Second):
		t.Fatal("MCP read-only trust prompt was not emitted under tool auto-approval")
	}
	if approval.Tool != "mcp__github__issue_read" || !strings.Contains(approval.Subject, "github/issue/read") {
		t.Fatalf("approval = %+v, want MCP read-only trust prompt", approval)
	}
	select {
	case got := <-done:
		t.Fatalf("tool auto-approval must not answer MCP read-only trust, got %+v", got)
	case <-time.After(50 * time.Millisecond):
	}

	c.Approve(approval.ID, true, true, false)
	select {
	case got := <-done:
		if got.err != nil || !got.allow || got.reason != "" {
			t.Fatalf("CheckPlanModeReadOnlyTrust after approval = %+v, want allow", got)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("MCP read-only trust prompt stayed blocked after Approve")
	}

	allow, reason, err := planModeReadOnlyTrustApprover{c}.CheckPlanModeReadOnlyTrust(context.Background(), req)
	if err != nil || !allow || reason != "" {
		t.Fatalf("session-granted MCP read-only trust under YOLO = (%v,%q,%v), want allow", allow, reason, err)
	}
}

func TestPlanModeReadOnlyCommandTrustApprovalIgnoresToolAutoApproval(t *testing.T) {
	approvalRequests := make(chan event.Approval, 1)
	c := New(Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.ApprovalRequest {
				approvalRequests <- e.Approval
			}
		}),
	})
	c.SetAutoApproveTools(true)

	type trustResult struct {
		allow  bool
		reason string
		err    error
	}
	done := make(chan trustResult, 1)
	req := agent.PlanModeReadOnlyTrustRequest{
		ToolName: agent.PlanModeReadOnlyCommandApprovalTool,
		Command:  "gh issue view 5867",
		Prefix:   "gh issue view",
		Args:     json.RawMessage(`{"command":"gh issue view 5867"}`),
	}
	go func() {
		allow, reason, err := planModeReadOnlyTrustApprover{c}.CheckPlanModeReadOnlyTrust(context.Background(), req)
		done <- trustResult{allow: allow, reason: reason, err: err}
	}()

	var approval event.Approval
	select {
	case approval = <-approvalRequests:
	case <-time.After(30 * time.Second):
		t.Fatal("plan-mode bash read-only command trust prompt was not emitted under tool auto-approval")
	}
	if approval.Tool != agent.PlanModeReadOnlyCommandApprovalTool || !strings.Contains(approval.Subject, `Trust "gh issue view"`) {
		t.Fatalf("approval = %+v, want plan-mode bash read-only command trust prompt", approval)
	}
	select {
	case got := <-done:
		t.Fatalf("tool auto-approval must not answer plan-mode bash read-only command trust, got %+v", got)
	case <-time.After(50 * time.Millisecond):
	}

	c.Approve(approval.ID, true, true, false)
	select {
	case got := <-done:
		if got.err != nil || !got.allow || got.reason != "" {
			t.Fatalf("CheckPlanModeReadOnlyTrust after approval = %+v, want allow", got)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("plan-mode bash read-only command trust prompt stayed blocked after Approve")
	}

	allow, reason, err := planModeReadOnlyTrustApprover{c}.CheckPlanModeReadOnlyTrust(context.Background(), req)
	if err != nil || !allow || reason != "" {
		t.Fatalf("session-granted plan-mode bash read-only command trust under YOLO = (%v,%q,%v), want allow", allow, reason, err)
	}
}

func TestApprovalSessionGrantGroupsFileMutationTools(t *testing.T) {
	c, ids, prompts := approvalIDs()
	go func() { c.Approve(<-ids, true, true, false) }()

	for i, call := range []struct {
		tool    string
		subject string
	}{
		{"edit_file", "src/a.go"},
		{"write_file", "src/b.go"},
		{"multi_edit", "src/c.go"},
		{"move_file", "src/d.go"},
	} {
		allow, _, err := gateApprover{c}.Approve(context.Background(), call.tool, call.subject, nil)
		if err != nil || !allow {
			t.Fatalf("call %d = (%v,%v), want allow", i, allow, err)
		}
	}
	if *prompts != 1 {
		t.Errorf("prompted %d times, want 1 (file mutation session grant should short-circuit)", *prompts)
	}
}

func TestApprovalSessionGrantKeepsPolicyDenyPrecedence(t *testing.T) {
	c, ids, prompts := approvalIDs()
	g := permission.NewGate(permission.New("ask", nil, nil, []string{"bash(rm*)"}), gateApprover{c})
	go func() { c.Approve(<-ids, true, true, false) }()

	allow, _, err := g.Check(context.Background(), "bash", json.RawMessage(`{"command":"go build"}`), false)
	if err != nil || !allow {
		t.Fatalf("first approved call = (%v,%v), want allow", allow, err)
	}
	allow, _, err = g.Check(context.Background(), "bash", json.RawMessage(`{"command":"go build"}`), false)
	if err != nil || !allow {
		t.Fatalf("same-command call after session grant = (%v,%v), want allow", allow, err)
	}
	allow, reason, err := g.Check(context.Background(), "bash", json.RawMessage(`{"command":"rm -rf /tmp/x"}`), false)
	if err != nil || allow || reason == "" {
		t.Fatalf("deny-listed call = (%v,%q,%v), want blocked with reason", allow, reason, err)
	}
	if *prompts != 1 {
		t.Errorf("prompted %d times, want 1", *prompts)
	}
}

// TestApprovalCtxCancel ensures a cancelled turn unblocks the gate with an error
// (rather than hanging) when no one answers.
func TestApprovalCtxCancel(t *testing.T) {
	c := New(Options{Sink: event.Discard})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	allow, _, err := gateApprover{c}.Approve(ctx, "bash", "x", nil)
	if err == nil || allow {
		t.Fatalf("Approve on cancelled ctx = (%v,%v), want (false, error)", allow, err)
	}
}

func TestParseRewind(t *testing.T) {
	cps := []checkpoint.Meta{
		{Turn: 0, Prompt: "first"},
		{Turn: 1, Prompt: "second"},
		{Turn: 2, Prompt: "third"},
	}
	cases := []struct {
		args    string
		wantT   int
		wantS   RewindScope
		wantErr bool
	}{
		{"", 2, RewindBoth, false},                       // no args -> latest turn, both
		{"1", 1, RewindBoth, false},                      // turn only
		{"0 code", 0, RewindCode, false},                 // turn + code
		{"1 conversation", 1, RewindConversation, false}, // turn + conversation
		{"2 both", 2, RewindBoth, false},                 // turn + both
		{"abc", 0, RewindBoth, true},                     // invalid turn
		{"0 unknown", 0, RewindBoth, true},               // unknown scope
	}
	for _, tc := range cases {
		t.Run(tc.args, func(t *testing.T) {
			gotT, gotS, err := parseRewind(tc.args, cps)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseRewind(%q) err=%v, wantErr=%v", tc.args, err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if gotT != tc.wantT || gotS != tc.wantS {
				t.Fatalf("parseRewind(%q) = (%d,%d), want (%d,%d)", tc.args, gotT, gotS, tc.wantT, tc.wantS)
			}
		})
	}
}

func TestParseRewindEmptyCheckpoints(t *testing.T) {
	_, _, err := parseRewind("", nil)
	if err == nil {
		t.Fatal("expected error when no checkpoints")
	}
}

func TestRunGuardedPanicEmitsTurnDone(t *testing.T) {
	sess := agent.NewSession("sys")
	events := make(chan event.Event, 4)
	c := New(Options{
		Runner: appendingRunner{session: sess},
		Sink:   event.FuncSink(func(e event.Event) { events <- e }),
	})

	go func() {
		c.runGuarded(func(ctx context.Context) error {
			panic("boom")
		})
	}()

	select {
	case e := <-events:
		if e.Kind != event.TurnDone {
			t.Fatalf("expected TurnDone after panic, got %v", e.Kind)
		}
		if e.Err == nil || !strings.Contains(e.Err.Error(), "boom") {
			t.Fatalf("expected TurnDone.Err to contain panic message, got %v", e.Err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for TurnDone after panic")
	}

	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	if running {
		t.Fatal("c.running should be false after panic recovery")
	}
}

// TestRunGuardedParksReplacementUntilTurnDoneReturns pins the finishing-window
// admission contract from both sides: a replacement turn arriving while
// TurnDone is being delivered must NOT start inside the window (the original
// transport-crosstalk guarantee this window exists for), and it must START —
// exactly once — when the window closes. The second half replaces the old
// silent-drop behavior, which lost real input: every caller that submits upon
// seeing turn_done (a frontend's queued auto-send, a bot, a fast Enter) raced
// this window, observed as a CI-flaky lost turn and worked around in
// Composer.tsx by gating auto-send on submitDisabled instead of turn_done.
func TestRunGuardedParksReplacementUntilTurnDoneReturns(t *testing.T) {
	firstTurnDone := make(chan struct{})
	releaseTurnDone := make(chan struct{})
	firstBodyDone := make(chan struct{})
	secondBodyRan := make(chan struct{}, 2)
	var turnDones int32
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.TurnDone {
			if atomic.AddInt32(&turnDones, 1) == 1 {
				close(firstTurnDone)
				<-releaseTurnDone
			}
		}
	})})

	if got := c.runGuarded(func(context.Context) error {
		close(firstBodyDone)
		return nil
	}); got != turnStarted {
		t.Fatalf("first admission = %v, want turnStarted", got)
	}
	<-firstBodyDone
	select {
	case <-firstTurnDone:
	case <-time.After(time.Second):
		t.Fatal("TurnDone delivery did not start")
	}
	if !c.RuntimeStatus().Running {
		t.Fatal("controller reported idle while TurnDone was still being delivered")
	}
	if got := c.runGuarded(func(context.Context) error {
		secondBodyRan <- struct{}{}
		return nil
	}); got != turnParked {
		t.Fatalf("finishing-window admission = %v, want turnParked", got)
	}
	select {
	case <-secondBodyRan:
		t.Fatal("replacement turn started before TurnDone delivery completed")
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseTurnDone)
	select {
	case <-secondBodyRan:
	case <-time.After(30 * time.Second):
		t.Fatal("parked turn was never started after the finishing window closed")
	}
	deadline := time.Now().Add(30 * time.Second)
	for c.Running() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if c.Running() {
		t.Fatal("controller remained busy after the parked turn completed")
	}
	if got := atomic.LoadInt32(&turnDones); got != 2 {
		t.Fatalf("TurnDone emitted %d times, want 2 (one per turn)", got)
	}
	select {
	case <-secondBodyRan:
		t.Fatal("parked turn ran more than once")
	default:
	}
}

func TestRunGuardedPanicDoesNotDoubleEmitTurnDone(t *testing.T) {
	sess := agent.NewSession("sys")
	var count int32
	events := make(chan event.Event, 8)
	c := New(Options{
		Runner: appendingRunner{session: sess},
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.TurnDone {
				atomic.AddInt32(&count, 1)
			}
			events <- e
		}),
	})

	go func() {
		c.runGuarded(func(ctx context.Context) error {
			panic("boom")
		})
	}()

	deadline := time.After(30 * time.Second)
	for {
		select {
		case <-events:
			n := atomic.LoadInt32(&count)
			if n >= 1 {
				time.Sleep(50 * time.Millisecond)
				n2 := atomic.LoadInt32(&count)
				if n2 > 1 {
					t.Fatalf("TurnDone emitted %d times, expected 1", n2)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for TurnDone")
		}
	}
}

type blockingRunner struct {
	session *agent.Session
	release chan struct{}
}

func (r blockingRunner) Run(_ context.Context, input string) error {
	r.session.Add(provider.Message{Role: provider.RoleUser, Content: input})
	<-r.release
	return nil
}

func TestRunTurnReportsErrTurnRunning(t *testing.T) {
	sess := agent.NewSession("sys")
	release := make(chan struct{})
	c := New(Options{Runner: blockingRunner{session: sess, release: release}})

	done := make(chan error, 1)
	go func() {
		done <- c.RunTurn(context.Background(), "first")
	}()
	waitForRunning(t, c)

	if err := c.RunTurn(context.Background(), "second"); err != ErrTurnRunning {
		t.Fatalf("RunTurn while running error = %v, want ErrTurnRunning", err)
	}

	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("first RunTurn returned %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("first RunTurn did not finish after release")
	}
}

func TestSendWhileRunningDoesNotInterleaveTurns(t *testing.T) {
	sess := agent.NewSession("sys")
	release := make(chan struct{})
	events := make(chan event.Event, 4)
	c := New(Options{
		Runner: blockingRunner{session: sess, release: release},
		Sink: event.FuncSink(func(e event.Event) {
			events <- e
		}),
	})
	defer c.autosaveWG.Wait()

	c.Send("first")
	waitForRunning(t, c)
	c.Send("second")
	close(release)
	waitForTurnDone(t, events)

	var users []string
	for _, m := range sess.Messages {
		if m.Role == provider.RoleUser {
			users = append(users, m.Content)
		}
	}
	if len(users) != 1 || users[0] != "first" {
		t.Fatalf("user turns = %v, want only first turn recorded", users)
	}
}

func waitForRunning(t *testing.T, c *Controller) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.Running() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("controller did not enter running state")
}

func TestMidTurnAutosavePersistsDuringLongTurn(t *testing.T) {
	old := midTurnSnapshotInterval.Load()
	midTurnSnapshotInterval.Store(int64(10 * time.Millisecond))
	defer midTurnSnapshotInterval.Store(old)

	dir := t.TempDir()
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	path := filepath.Join(dir, "session.jsonl")
	release := make(chan struct{})
	c := New(Options{Runner: blockingRunner{session: sess, release: release}, Executor: exec, SessionDir: dir, SessionPath: path, Label: "test"})
	// Unblock the turn and wait for the autosaver to exit before TempDir
	// cleanup, which fails on Windows while a snapshot tmp write is in flight.
	defer c.autosaveWG.Wait()
	defer close(release)

	c.Send("hello mid-turn persistence")

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && strings.Contains(string(b), "hello mid-turn persistence") {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("session file was not written while the turn was still running")
}

type scriptedRunner struct {
	exec    *agent.Agent
	scripts []func(input string)
}

func (r *scriptedRunner) Run(_ context.Context, input string) error {
	if len(r.scripts) == 0 {
		return nil
	}
	next := r.scripts[0]
	r.scripts = r.scripts[1:]
	next(input)
	return nil
}

func TestApprovedPlanAutoApproveEndsWithExecutionTurn(t *testing.T) {
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	runner := &scriptedRunner{exec: exec}

	var c *Controller
	approvalPrompts := 0
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind != event.ApprovalRequest {
			return
		}
		approvalPrompts++
		if e.Approval.Tool == planApprovalTool {
			go c.Approve(e.Approval.ID, true, false, false)
			return
		}
		go c.Approve(e.Approval.ID, false, false, false)
	})
	c = New(Options{Runner: runner, Executor: exec, Sink: sink})
	c.SetPlanMode(true)

	runner.scripts = append(runner.scripts,
		func(input string) {
			exec.Session().Add(provider.Message{Role: provider.RoleAssistant, Content: "1. Create the file\n2. Update the file"})
		},
		func(input string) {
			if input != planApprovedMessage {
				t.Fatalf("approved execution input = %q, want planApprovedMessage", input)
			}
			exec.Session().Add(provider.Message{Role: provider.RoleAssistant, Content: "first step done; paused for review", ToolCalls: []provider.ToolCall{{
				ID: "todo-1", Name: "todo_write", Arguments: `{"todos":[{"content":"Create the file","status":"completed"},{"content":"Update the file","status":"in_progress"}]}`,
			}}})
		},
	)

	if err := c.runTurn(context.Background(), "plan this"); err != nil {
		t.Fatal(err)
	}
	if approvalPrompts != 1 {
		t.Fatalf("approval prompts after plan = %d, want 1", approvalPrompts)
	}

	// The plan approval auto-approves writers for the execution turn only. A later
	// turn does not inherit it, and "继续" carries no special meaning — Compose must
	// not inject any marker, and the next writer falls back to per-tool approval.
	if got := c.Compose("继续"); StripComposePrefixes(got) != "继续" {
		t.Fatalf("a paused approved plan must not marker-prefix the next turn, got %q", got)
	}
	allow, _, err := gateApprover{c}.Approve(context.Background(), "write_file", "/tmp/a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if allow {
		t.Fatal("writer after the execution turn should return to per-tool approval, not auto-allow")
	}
	if approvalPrompts != 2 {
		t.Fatalf("writer after the execution turn should prompt, prompts=%d", approvalPrompts)
	}
}

func TestApprovedPlanDoesNotAutoApproveNonContinuationTurn(t *testing.T) {
	exec := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	runner := &scriptedRunner{exec: exec}

	var c *Controller
	approvalPrompts := 0
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind != event.ApprovalRequest {
			return
		}
		approvalPrompts++
		if e.Approval.Tool == planApprovalTool {
			go c.Approve(e.Approval.ID, true, false, false)
			return
		}
		go c.Approve(e.Approval.ID, false, false, false)
	})
	c = New(Options{Runner: runner, Executor: exec, Sink: sink})
	c.SetPlanMode(true)

	runner.scripts = append(runner.scripts,
		func(input string) {
			exec.Session().Add(provider.Message{Role: provider.RoleAssistant, Content: "1. Create the file\n2. Update the file"})
		},
		func(input string) {
			exec.Session().Add(provider.Message{Role: provider.RoleAssistant, Content: "paused", ToolCalls: []provider.ToolCall{{
				ID: "todo-1", Name: "todo_write", Arguments: `{"todos":[{"content":"Create the file","status":"completed"},{"content":"Update the file","status":"in_progress"}]}`,
			}}})
		},
	)

	if err := c.runTurn(context.Background(), "plan this"); err != nil {
		t.Fatal(err)
	}
	if got := c.Compose("先别继续"); StripComposePrefixes(got) != "先别继续" {
		t.Fatalf("non-continuation input should not be marker-prefixed, got %q", got)
	}

	allow, _, err := gateApprover{c}.Approve(context.Background(), "write_file", "/tmp/a", nil)
	if err != nil {
		t.Fatal(err)
	}
	if allow {
		t.Fatal("non-continuation turn should not inherit approved-plan auto approval")
	}
	if approvalPrompts != 2 {
		t.Fatalf("non-continuation writer should prompt after plan approval, prompts=%d", approvalPrompts)
	}
}

// writeCmdFile creates a command .md file with frontmatter under dir.
func writeCmdFile(t *testing.T, dir, name, description, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("---\ndescription: %s\n---\n%s\n", description, body)
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCommandsAtomicPointer verifies that commands passed via Options.Commands are
// correctly exposed through the atomic-pointer Commands() getter and that
// CustomCommand resolves and renders them. Missing commands return found=false.
func TestCommandsAtomicPointer(t *testing.T) {
	cmds := []command.Command{
		{Name: "review", Description: "Review code", Body: "Review $1"},
		{Name: "test", Description: "Run tests", Body: "Test $1"},
	}
	c := New(Options{
		Commands: cmds,
		Sink:     &typedNilControllerSink{},
		Registry: tool.NewRegistry(),
	})

	// Commands() returns what was passed via Options
	got := c.Commands()
	if len(got) != 2 {
		t.Fatalf("Commands() = %d, want 2", len(got))
	}
	// Check retrieval via CustomCommand
	sent, ok := c.CustomCommand("/review myfile.go")
	if !ok {
		t.Error("/review should be found")
	}
	if !strings.Contains(sent, "Review myfile.go") {
		t.Errorf("unexpected render: %q", sent)
	}
	_, ok = c.CustomCommand("/missing")
	if ok {
		t.Error("/missing should not be found")
	}

	// Commands() getter uses atomic.Pointer internally
	if cmds2 := c.Commands(); len(cmds2) != 2 {
		t.Errorf("Commands() = %d after change, want 2", len(cmds2))
	}
}

// TestReloadCommandsFromFilesystem exercises ReloadCommands against real .md
// files in a temp workspace: initial load, hot-reload with a new file, and
// hot-reload after modifying an existing file. Also verifies that skills are
// preserved across the reload.
func TestReloadCommandsFromFilesystem(t *testing.T) {
	// Isolate HOME so CommandDirsForRoot does not pick up global .md command files.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	wsRoot := t.TempDir()
	cmdDir := filepath.Join(wsRoot, ".reasonix", "commands")
	writeCmdFile(t, cmdDir, "review", "Review code", "Review $1")
	writeCmdFile(t, cmdDir, "test", "Run tests", "Test $1")

	// Create a minimal in-memory skill to verify skills are preserved across reload.
	sk := skill.Skill{
		Name:        "myskill",
		Description: "Test skill",
		Body:        "You are a test skill. User says: {{.Input}}",
	}

	reg := tool.NewRegistry()
	c := New(Options{
		Sink:          &typedNilControllerSink{},
		Registry:      reg,
		WorkspaceRoot: wsRoot,
		Skills:        []skill.Skill{sk},
	})

	// ReloadCommands should pick up the two .md files and preserve the skill.
	if err := c.ReloadCommands(context.Background()); err != nil {
		t.Fatalf("ReloadCommands: %v", err)
	}
	cmds := c.Commands()
	if len(cmds) != 2 {
		t.Fatalf("Commands() = %d after reload, want 2", len(cmds))
	}

	// CustomCommand should resolve through the hot-swapped getter
	sent, ok := c.CustomCommand("/review hello.go")
	if !ok {
		t.Fatal("/review should be found after reload")
	}
	if !strings.Contains(sent, "Review hello.go") {
		t.Errorf("render = %q, want Review hello.go", sent)
	}

	// Missing command still not found
	if _, ok := c.CustomCommand("/nope"); ok {
		t.Error("/nope should not be found")
	}

	// Skill should appear in the slash_command tool's description after reload.
	if tool, found := reg.Get("slash_command"); found {
		if !strings.Contains(tool.Description(), "myskill") {
			t.Error("skill 'myskill' should appear in slash_command tool Description after ReloadCommands")
		}
	} else {
		t.Error("slash_command tool should be registered after ReloadCommands")
	}

	// Skill should still be callable via RunSkill after reload.
	if _, ok := c.RunSkill("/myskill"); !ok {
		t.Error("RunSkill(/myskill) should find the skill after ReloadCommands")
	}

	// Hot-reload: add a new command file
	writeCmdFile(t, cmdDir, "count", "Count to N", "Count from 1 to $1")
	if err := c.ReloadCommands(context.Background()); err != nil {
		t.Fatalf("ReloadCommands (add): %v", err)
	}
	if cmds := c.Commands(); len(cmds) != 3 {
		t.Fatalf("Commands() = %d after add, want 3", len(cmds))
	}

	// Hot-reload: modify an existing command
	writeCmdFile(t, cmdDir, "review", "Review code (friendly)", "Kindly review $1")
	if err := c.ReloadCommands(context.Background()); err != nil {
		t.Fatalf("ReloadCommands (modify): %v", err)
	}
	if cmds := c.Commands(); len(cmds) != 3 {
		t.Fatalf("Commands() = %d after modify, want 3", len(cmds))
	}
	sent, ok = c.CustomCommand("/review world.go")
	if !ok {
		t.Fatal("/review should be found after modify")
	}
	if !strings.Contains(sent, "Kindly review world.go") {
		t.Errorf("render after modify = %q, want Kindly review world.go", sent)
	}

	// The slash_command tool should be registered and updated
	if _, found := reg.Get("slash_command"); !found {
		t.Error("slash_command tool should be registered after ReloadCommands")
	}
}

// TestReloadCommandsDeleteFile verifies that removing a command .md file and
// reloading causes the command to disappear from both Commands() and
// CustomCommand(), while other commands remain intact.
func TestReloadCommandsDeleteFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	wsRoot := t.TempDir()
	cmdDir := filepath.Join(wsRoot, ".reasonix", "commands")
	writeCmdFile(t, cmdDir, "alpha", "Alpha cmd", "Alpha $1")
	writeCmdFile(t, cmdDir, "beta", "Beta cmd", "Beta $1")

	reg := tool.NewRegistry()
	c := New(Options{
		Sink:          &typedNilControllerSink{},
		Registry:      reg,
		WorkspaceRoot: wsRoot,
	})

	if err := c.ReloadCommands(context.Background()); err != nil {
		t.Fatalf("initial reload: %v", err)
	}
	if got := len(c.Commands()); got != 2 {
		t.Fatalf("Commands() = %d, want 2", got)
	}

	// Delete alpha.md
	if err := os.Remove(filepath.Join(cmdDir, "alpha.md")); err != nil {
		t.Fatal(err)
	}

	if err := c.ReloadCommands(context.Background()); err != nil {
		t.Fatalf("reload after delete: %v", err)
	}
	if got := len(c.Commands()); got != 1 {
		t.Fatalf("Commands() = %d after delete, want 1", got)
	}

	// /alpha should no longer be found
	if _, ok := c.CustomCommand("/alpha x"); ok {
		t.Error("/alpha should NOT be found after deletion")
	}
	// /beta should still work
	sent, ok := c.CustomCommand("/beta y")
	if !ok {
		t.Error("/beta should still be found")
	}
	if !strings.Contains(sent, "Beta y") {
		t.Errorf("render = %q, want Beta y", sent)
	}
}

// TestReloadCommandsMalformedFile verifies that a malformed .md file causes
// ReloadCommands to return an error but does not prevent other valid commands
// from loading.
func TestReloadCommandsMalformedFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	wsRoot := t.TempDir()
	cmdDir := filepath.Join(wsRoot, ".reasonix", "commands")
	writeCmdFile(t, cmdDir, "good", "Good cmd", "Good $1")

	// Write a malformed file (no valid frontmatter)
	if err := os.MkdirAll(cmdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(cmdDir, "broken.md")
	if err := os.WriteFile(broken, []byte("this is not valid yaml\n---\nrandom\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := tool.NewRegistry()
	c := New(Options{
		Sink:          &typedNilControllerSink{},
		Registry:      reg,
		WorkspaceRoot: wsRoot,
	})

	err := c.ReloadCommands(context.Background())
	// We expect some error from the malformed file
	if err == nil {
		t.Log("ReloadCommands returned nil error despite malformed file — command.Load may tolerate it")
	} else {
		t.Logf("ReloadCommands returned error (expected): %v", err)
	}

	// The valid command should still be loadable
	cmds := c.Commands()
	foundGood := false
	for _, cmd := range cmds {
		if cmd.Name == "good" {
			foundGood = true
		}
	}
	if !foundGood {
		t.Errorf("valid command 'good' should be present, got commands: %v", cmdNames(cmds))
	}
}

// TestReloadCommandsSameNameAcrossDirs verifies that when the same command
// name exists in multiple convention directories, the later-scanned directory
// (higher priority) wins. ConventionDirs = [".reasonix", ".agents", ".agent",
// ".claude"], scanned in reverse, so .reasonix is highest priority.
func TestReloadCommandsSameNameAcrossDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	wsRoot := t.TempDir()

	// Lower priority: .claude/commands
	claudeDir := filepath.Join(wsRoot, ".claude", "commands")
	writeCmdFile(t, claudeDir, "greet", "Claude greet", "Hello from Claude: $1")

	// Higher priority: .reasonix/commands
	reasonixDir := filepath.Join(wsRoot, ".reasonix", "commands")
	writeCmdFile(t, reasonixDir, "greet", "Reasonix greet", "Hello from Reasonix: $1")

	reg := tool.NewRegistry()
	c := New(Options{
		Sink:          &typedNilControllerSink{},
		Registry:      reg,
		WorkspaceRoot: wsRoot,
	})

	if err := c.ReloadCommands(context.Background()); err != nil {
		t.Fatalf("reload: %v", err)
	}

	// There should be exactly 1 command named "greet"
	cmds := c.Commands()
	count := 0
	for _, cmd := range cmds {
		if cmd.Name == "greet" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 'greet' command, got %d", count)
	}

	// The winning version should be from .reasonix (highest priority)
	sent, ok := c.CustomCommand("/greet world")
	if !ok {
		t.Fatal("/greet should be found")
	}
	if !strings.Contains(sent, "Hello from Reasonix") {
		t.Errorf("expected .reasonix version to win, got render: %q", sent)
	}
}

func TestReloadCommandsUsesCanonicalPluginNameAlongsideProjectShortName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)

	pluginRoot := filepath.Join(reasonixHome, "plugins", "pwf")
	if err := os.MkdirAll(filepath.Join(pluginRoot, ".claude-plugin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginRoot, pluginpkg.ClaudeManifest), []byte(`{"name":"pwf"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCmdFile(t, filepath.Join(pluginRoot, "commands"), "plan", "Plugin plan", "PLUGIN $1")
	writeCmdFile(t, filepath.Join(pluginRoot, "commands"), "status", "Plugin status", "STATUS $1")
	if err := pluginpkg.Upsert(reasonixHome, pluginpkg.InstalledPlugin{Name: "pwf", Root: "plugins/pwf", ManifestKind: "claude", Enabled: true}); err != nil {
		t.Fatal(err)
	}

	workspace := t.TempDir()
	writeCmdFile(t, filepath.Join(workspace, ".reasonix", "commands"), "plan", "Project plan", "PROJECT $1")
	c := New(Options{Sink: &typedNilControllerSink{}, Registry: tool.NewRegistry(), WorkspaceRoot: workspace})
	if err := c.ReloadCommands(context.Background()); err != nil {
		t.Fatalf("ReloadCommands: %v", err)
	}

	if got, ok := c.CustomCommand("/plan task"); !ok || got != "PROJECT task" {
		t.Fatalf("short command = %q, %v; want project winner", got, ok)
	}
	if got, ok := c.CustomCommand("/pwf:plan task"); !ok || got != "PLUGIN task" {
		t.Fatalf("qualified plugin command = %q, %v", got, ok)
	}
	if got, ok := c.CustomCommand("/status now"); !ok || got != "STATUS now" {
		t.Fatalf("hidden compatible short command = %q, %v", got, ok)
	}
	if got, ok := c.CustomCommand("/pwf:status now"); !ok || got != "STATUS now" {
		t.Fatalf("canonical plugin status command = %q, %v", got, ok)
	}
	cmds := c.Commands()
	canonicalFound := false
	hiddenFound := false
	for _, cmd := range cmds {
		if cmd.Name == "pwf:plan" && cmd.Plugin == "pwf" && cmd.ShortName == "plan" && !cmd.Hidden {
			canonicalFound = true
		}
		if cmd.Name == "status" && cmd.Plugin == "pwf" && cmd.ShortName == "status" && cmd.Hidden {
			hiddenFound = true
		}
	}
	if !canonicalFound || !hiddenFound {
		t.Fatalf("plugin command metadata missing: %+v", cmds)
	}
}

// TestReloadCommandsEmptySet verifies that deleting all command files and
// reloading results in an empty Commands() slice, while the slash_command tool
// still exists in the Registry (containing only Skills).
func TestReloadCommandsEmptySet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	wsRoot := t.TempDir()
	cmdDir := filepath.Join(wsRoot, ".reasonix", "commands")
	writeCmdFile(t, cmdDir, "temp", "Temp cmd", "Temp $1")

	sk := skill.Skill{
		Name:        "preserved",
		Description: "A skill to keep",
		Body:        "Skill body: {{.Input}}",
	}

	reg := tool.NewRegistry()
	c := New(Options{
		Sink:          &typedNilControllerSink{},
		Registry:      reg,
		WorkspaceRoot: wsRoot,
		Skills:        []skill.Skill{sk},
	})

	if err := c.ReloadCommands(context.Background()); err != nil {
		t.Fatalf("initial reload: %v", err)
	}
	if got := len(c.Commands()); got != 1 {
		t.Fatalf("Commands() = %d, want 1", got)
	}

	// Delete all command files
	if err := os.Remove(filepath.Join(cmdDir, "temp.md")); err != nil {
		t.Fatal(err)
	}

	if err := c.ReloadCommands(context.Background()); err != nil {
		t.Fatalf("reload after delete all: %v", err)
	}
	if got := len(c.Commands()); got != 0 {
		t.Fatalf("Commands() = %d after delete all, want 0", got)
	}

	// /temp should no longer be found
	if _, ok := c.CustomCommand("/temp x"); ok {
		t.Error("/temp should NOT be found after deletion")
	}

	// slash_command tool should still exist (for Skills)
	slashTool, found := reg.Get("slash_command")
	if !found {
		t.Fatal("slash_command tool should still exist even with 0 commands")
	}
	// It should still contain the skill
	if !strings.Contains(slashTool.Description(), "preserved") {
		t.Error("skill 'preserved' should still appear in slash_command tool Description")
	}
}

// TestReloadCommandsDesktopManagementNotice verifies the desktop/HTTP path:
// when the frontend submits "/reload-cmd" as raw input, Submit → managementNotice
// handles it and emits a Notice event with the correct count.
func TestReloadCommandsDesktopManagementNotice(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))

	wsRoot := t.TempDir()
	cmdDir := filepath.Join(wsRoot, ".reasonix", "commands")
	writeCmdFile(t, cmdDir, "hello", "Greet", "Hello $1")
	writeCmdFile(t, cmdDir, "review", "Review code", "Review $1")

	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})

	reg := tool.NewRegistry()
	c := New(Options{
		Sink:          sink,
		Registry:      reg,
		WorkspaceRoot: wsRoot,
	})

	// Initial load so Commands() is populated.
	if err := c.ReloadCommands(context.Background()); err != nil {
		t.Fatalf("initial reload: %v", err)
	}
	notices = nil // reset

	// Desktop path: managementNotice("/reload-cmd") should emit a notice.
	handled := c.managementNotice("/reload-cmd")
	if !handled {
		t.Fatal("managementNotice(/reload-cmd) should return true")
	}
	if len(notices) != 1 {
		t.Fatalf("expected 1 notice, got %d: %v", len(notices), notices)
	}
	if !strings.Contains(notices[0], "commands reloaded") {
		t.Errorf("notice = %q, want 'commands reloaded'", notices[0])
	}
	if !strings.Contains(notices[0], "2 available") {
		t.Errorf("notice = %q, want '2 available'", notices[0])
	}

	// Delete one command file and reload again.
	if err := os.Remove(filepath.Join(cmdDir, "hello.md")); err != nil {
		t.Fatal(err)
	}
	notices = nil
	handled = c.managementNotice("/reload-cmd")
	if !handled {
		t.Fatal("managementNotice(/reload-cmd) after delete should return true")
	}
	if len(notices) != 1 {
		t.Fatalf("expected 1 notice after delete, got %d: %v", len(notices), notices)
	}
	if !strings.Contains(notices[0], "1 available") {
		t.Errorf("notice after delete = %q, want '1 available'", notices[0])
	}

	// Delete all and verify empty-set notice.
	if err := os.Remove(filepath.Join(cmdDir, "review.md")); err != nil {
		t.Fatal(err)
	}
	notices = nil
	handled = c.managementNotice("/reload-cmd")
	if !handled {
		t.Fatal("managementNotice(/reload-cmd) empty set should return true")
	}
	if len(notices) != 1 {
		t.Fatalf("expected 1 notice for empty set, got %d: %v", len(notices), notices)
	}
	if !strings.Contains(notices[0], "0 available") {
		t.Errorf("notice for empty set = %q, want '0 available'", notices[0])
	}
}

// cmdNames is a test helper that extracts command names from a slice.
func cmdNames(cmds []command.Command) []string {
	names := make([]string, len(cmds))
	for i, c := range cmds {
		names[i] = c.Name
	}
	return names
}
