package control

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/checkpoint"
	"reasonix/internal/event"
	"reasonix/internal/hook"
	"reasonix/internal/jobs"
	"reasonix/internal/permission"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
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
	case <-time.After(2 * time.Second):
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

func TestSubmitClearDiscardsCurrentContextWithoutSavingTranscript(t *testing.T) {
	dir := t.TempDir()
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "old context"})
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	path := filepath.Join(dir, "session.jsonl")
	c := New(Options{Executor: exec, SystemPrompt: "sys", SessionDir: dir, SessionPath: path, Label: "test"})
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

	c.submit("/clear", "")
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && c.SessionPath() == path {
		time.Sleep(time.Millisecond)
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
	case <-time.After(2 * time.Second):
		t.Fatal("ApprovalRequest was not emitted")
	}
	return ""
}

func waitPermissionHook(t *testing.T, payloads <-chan hook.Payload) hook.Payload {
	t.Helper()
	select {
	case payload := <-payloads:
		return payload
	case <-time.After(2 * time.Second):
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
	case <-time.After(2 * time.Second):
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
	case <-time.After(2 * time.Second):
		t.Fatal("memory approval stayed blocked after Approve")
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
	case <-time.After(2 * time.Second):
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
	c.mu.Lock()
	c.granted[permission.SessionGrantRuleForScope("bash", "go test ./...")] = true
	c.mu.Unlock()

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
	case <-time.After(2 * time.Second):
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
			case <-time.After(2 * time.Second):
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
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for TurnDone after panic")
	}

	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	if running {
		t.Fatal("c.running should be false after panic recovery")
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

	deadline := time.After(2 * time.Second)
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
	if got := c.Compose("继续"); got != "继续" {
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
	if got := c.Compose("先别继续"); got != "先别继续" {
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
