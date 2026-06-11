package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/checkpoint"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type typedNilControllerSink struct{}

func (*typedNilControllerSink) Emit(event.Event) {}

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

func TestNewTreatsTypedNilSinkAsDiscard(t *testing.T) {
	var sink *typedNilControllerSink
	c := New(Options{Sink: sink})

	c.notice("typed nil sink should not panic")
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
	c := New(Options{Executor: exec, SessionDir: dir, Label: "test"})
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
	dir := t.TempDir()
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
