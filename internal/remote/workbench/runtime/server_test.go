package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/checkpoint"
	"reasonix/internal/command"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/memory"
	"reasonix/internal/plugin"
	"reasonix/internal/provider"
	remotebroker "reasonix/internal/remote/broker"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
	"reasonix/internal/skill"
	"reasonix/internal/store"
)

type fakeController struct {
	model         string
	history       []provider.Message
	status        control.RuntimeStatus
	steerAccepted bool
	steers        []string
}

type blockingController struct {
	*fakeController
	started chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	submits int
	closes  int
}

func newBlockingController() *blockingController {
	return &blockingController{
		fakeController: &fakeController{model: "local/blocking"},
		started:        make(chan struct{}),
		release:        make(chan struct{}),
	}
}

func (c *blockingController) Submit(input string) {
	c.mu.Lock()
	c.submits++
	c.mu.Unlock()
	c.once.Do(func() { close(c.started) })
	<-c.release
	c.fakeController.Submit(input)
}

func (c *blockingController) Close() {
	c.mu.Lock()
	c.closes++
	c.mu.Unlock()
}

func (c *blockingController) counts() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.submits, c.closes
}

func TestSocketPathStaysWithinPortableUnixLimitForLongHome(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), strings.Repeat("h", 55))
	workspace := filepath.Join(home, "project")
	got := SocketPath(home, workspace)
	if len(got) >= 104 {
		t.Fatalf("socket path length = %d, want less than portable AF_UNIX limit: %q", len(got), got)
	}
	wantPrefix := filepath.Join(home, ".reasonix") + string(filepath.Separator)
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("socket path %q escaped runtime home %q", got, home)
	}
}

type persistentFakeController struct {
	*fakeController
	sessionDir  string
	sessionPath string
	closed      bool
}

type profileFakeController struct {
	*persistentFakeController
	planMode     bool
	approvalMode string
	goal         string
	goalStatus   string
	goalWriteErr error
}

type rotatingFakeController struct {
	*persistentFakeController
	newPath    string
	clearPath  string
	newCalls   int
	clearCalls int
}

func (c *rotatingFakeController) NewSession() error {
	c.newCalls++
	c.history = nil
	c.sessionPath = c.newPath
	return nil
}

func (c *rotatingFakeController) ClearSession() error {
	c.clearCalls++
	c.history = nil
	c.sessionPath = c.clearPath
	return nil
}

func (c *profileFakeController) SetPlanMode(enabled bool) { c.planMode = enabled }
func (c *profileFakeController) SetToolApprovalMode(mode string) {
	c.approvalMode = mode
}
func (c *profileFakeController) SetGoal(goal string) {
	c.goal = strings.TrimSpace(goal)
	if c.goal == "" {
		c.goalStatus = ""
	} else {
		c.goalStatus = string(protocol.GoalRunning)
	}
}
func (c *profileFakeController) SetGoalDurable(goal, _ string) error {
	if c.goalWriteErr != nil {
		return c.goalWriteErr
	}
	if c.sessionPath == "" {
		return errors.New("missing session path")
	}
	if err := os.WriteFile(store.SessionGoalState(c.sessionPath), []byte(strings.TrimSpace(goal)+"\n"), 0o644); err != nil {
		return err
	}
	c.SetGoal(goal)
	return nil
}
func (c *profileFakeController) ResumeGoal() bool {
	if c.goal == "" {
		return false
	}
	c.goalStatus = string(protocol.GoalRunning)
	return true
}
func (c *profileFakeController) Goal() string       { return c.goal }
func (c *profileFakeController) GoalStatus() string { return c.goalStatus }

func (c *persistentFakeController) SessionPath() string { return c.sessionPath }
func (c *persistentFakeController) SetSessionPath(path string) {
	c.sessionPath = path
}
func (c *persistentFakeController) EnsureSessionPath() {
	if c.sessionPath == "" {
		c.sessionPath = filepath.Join(c.sessionDir, "remote-session.jsonl")
	}
}
func (c *persistentFakeController) AdoptHistory(history []provider.Message, path string) {
	c.history = append([]provider.Message(nil), history...)
	c.sessionPath = path
}
func (c *persistentFakeController) Close() { c.closed = true }

type projectionController struct {
	*fakeController
	checkpoints []checkpoint.Meta
	todos       []evidence.TodoItem
	goal        string
	goalStatus  string
	rewoundTurn int
	rewound     control.RewindScope
}

type catalogController struct {
	*fakeController
	commands     []command.Command
	skills       []skill.Skill
	disabled     []skill.Skill
	configured   []string
	disconnected []string
	memory       *memory.Set
}

func (*catalogController) Host() *plugin.Host { return nil }
func (c *catalogController) Commands() []command.Command {
	return append([]command.Command(nil), c.commands...)
}
func (c *catalogController) Skills() []skill.Skill {
	return append([]skill.Skill(nil), c.skills...)
}
func (c *catalogController) SlashSkills() []skill.Skill {
	return append([]skill.Skill(nil), c.skills...)
}
func (c *catalogController) DisabledSkills() []skill.Skill {
	return append([]skill.Skill(nil), c.disabled...)
}
func (c *catalogController) ConfiguredMCPNames() []string {
	return append([]string(nil), c.configured...)
}
func (c *catalogController) DisconnectedMCPNames() []string {
	return append([]string(nil), c.disconnected...)
}
func (c *catalogController) Memory() *memory.Set { return c.memory }

func (c *projectionController) Checkpoints() []checkpoint.Meta {
	return append([]checkpoint.Meta(nil), c.checkpoints...)
}
func (c *projectionController) CheckpointHasBoundary(turn int) bool {
	return turn == 1
}
func (c *projectionController) Todos() []evidence.TodoItem {
	return append([]evidence.TodoItem(nil), c.todos...)
}
func (c *projectionController) SetGoal(goal string) {
	c.goal = strings.TrimSpace(goal)
	if c.goal == "" {
		c.goalStatus = ""
	} else {
		c.goalStatus = string(protocol.GoalRunning)
	}
}
func (c *projectionController) ResumeGoal() bool {
	if c.goal == "" {
		return false
	}
	c.goalStatus = string(protocol.GoalRunning)
	return true
}
func (c *projectionController) Goal() string       { return c.goal }
func (c *projectionController) GoalStatus() string { return c.goalStatus }
func (c *projectionController) Rewind(turn int, scope control.RewindScope) error {
	c.rewoundTurn, c.rewound = turn, scope
	return nil
}

type brokerStubProvider struct {
	requests chan provider.Request
	mu       sync.Mutex
	calls    int
}

func (p *brokerStubProvider) Name() string { return "desktop-stub" }
func (p *brokerStubProvider) Stream(ctx context.Context, request provider.Request) (<-chan provider.Chunk, error) {
	p.requests <- request
	p.mu.Lock()
	call := p.calls
	p.calls++
	p.mu.Unlock()

	chunks := make(chan provider.Chunk, 3)
	switch call {
	case 0:
		chunks <- provider.Chunk{Type: provider.ChunkReasoning, Text: "inspect the remote workspace first"}
		chunks <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: "remote-read-1", Name: "read_file", Arguments: `{"path":"broker-proof.txt"}`,
		}}
		chunks <- provider.Chunk{Type: provider.ChunkDone}
	case 1:
		chunks <- provider.Chunk{Type: provider.ChunkText, Text: "hello from desktop after the Host tool result"}
		chunks <- provider.Chunk{Type: provider.ChunkDone}
	default:
		chunks <- provider.Chunk{Type: provider.ChunkError, Err: fmt.Errorf("unexpected Broker provider call %d", call+1)}
	}
	close(chunks)
	return chunks, nil
}

func (p *brokerStubProvider) RequiresToolCallReasoning() bool      { return true }
func (p *brokerStubProvider) WarnOnMissingToolCallReasoning() bool { return false }

func (c *fakeController) ModelRef() string { return c.model }
func (c *fakeController) Label() string    { return c.model }
func (c *fakeController) History() []provider.Message {
	return append([]provider.Message(nil), c.history...)
}
func (c *fakeController) Turn() int     { return len(c.history) }
func (c *fakeController) Running() bool { return c.status.Running }
func (c *fakeController) RuntimeStatus() control.RuntimeStatus {
	return c.status
}
func (c *fakeController) Submit(input string) {
	c.history = append(c.history, provider.Message{Role: provider.RoleUser, Content: input})
}
func (c *fakeController) TrySteer(text string) bool {
	if !c.steerAccepted {
		return false
	}
	c.steers = append(c.steers, text)
	return true
}
func (c *fakeController) Cancel()               {}
func (c *fakeController) Close()                {}
func (c *fakeController) SessionPath() string   { return "" }
func (c *fakeController) SetSessionPath(string) {}
func (c *fakeController) EnsureSessionPath()    {}
func (c *fakeController) AdoptHistory(h []provider.Message, _ string) {
	c.history = append([]provider.Message(nil), h...)
}

func TestSessionCatalogAndSlashArgsUseHostControllerCapabilities(t *testing.T) {
	store := memory.Store{Dir: t.TempDir()}
	saved, err := store.SaveWithOptions(memory.Memory{
		Name: "remote-policy", Title: "Remote policy", Description: "remote-only fact",
		Type: memory.TypeProject, Body: "Use the remote workspace policy.",
	}, memory.SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctrl := &catalogController{
		fakeController: &fakeController{model: "local/model"},
		commands: []command.Command{
			{Name: "review", Description: "Review this workspace"},
			{Name: "hidden", Hidden: true},
		},
		skills:     []skill.Skill{{Name: "explore", Description: "Explore the Host", Scope: skill.ScopeProject}},
		configured: []string{"connected", "offline"}, disconnected: []string{"offline"},
		memory: &memory.Set{Store: store},
	}
	catalog := buildSessionCatalog(context.Background(), ctrl)
	if len(catalog.Commands) != 1 || catalog.Commands[0].Name != "review" {
		t.Fatalf("commands = %+v", catalog.Commands)
	}
	if len(catalog.Skills) != 1 || catalog.Skills[0].Name != "explore" || catalog.Skills[0].Scope != "project" {
		t.Fatalf("skills = %+v", catalog.Skills)
	}
	if len(catalog.MCPServers) != 2 || catalog.MCPServers[0].Available || catalog.MCPServers[1].Available {
		t.Fatalf("MCP servers = %+v", catalog.MCPServers)
	}

	server := New(Options{Workspace: t.TempDir(), Version: "test"})
	target := protocol.RuntimeTarget{WorkspaceID: server.workspaceID, SessionID: "session_catalog"}
	server.sessions[target.SessionID] = &session{id: target.SessionID, ctrl: ctrl, model: ctrl.model, runtimeEpoch: "runtime_catalog"}
	result, err := server.composerSlashArgs(protocol.ComposerSlashArgsParams{
		RuntimeQuery: protocol.RuntimeQuery{ExpectedHostEpoch: server.hostEpoch, Target: target, ExpectedRuntimeEpoch: "runtime_catalog"},
		Input:        "/skill disable ",
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range result.Items {
		found = found || item.Label == "explore"
	}
	if !found {
		t.Fatalf("slash args = %+v", result.Items)
	}

	result, err = server.composerSlashArgs(protocol.ComposerSlashArgsParams{
		RuntimeQuery: protocol.RuntimeQuery{ExpectedHostEpoch: server.hostEpoch, Target: target, ExpectedRuntimeEpoch: "runtime_catalog"},
		Input:        "/memory revisions ",
	})
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, item := range result.Items {
		found = found || item.Label == saved.Memory.ID
	}
	if !found {
		t.Fatalf("remote /memory completion did not use the Host memory catalog: %+v", result.Items)
	}
}

func TestHistoryPageCapsAndPaginatesByVisibleUserTurns(t *testing.T) {
	history := []provider.Message{{Role: provider.RoleSystem, Content: "system"}}
	for turn := 0; turn < 250; turn++ {
		history = append(history,
			provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("user-%d", turn)},
			provider.Message{Role: provider.RoleAssistant, Content: fmt.Sprintf("assistant-%d", turn)},
		)
	}
	sess := &session{ctrl: &fakeController{model: "model", history: history}}
	latest := historyPage(sess, "snapshot_test", 0, protocol.HistoryMaxTurns)
	if latest.StartTurn != 50 || latest.EndTurn != 250 || latest.ActualTurns != 200 || !latest.HasOlder || latest.NextCursor != "turn:50" {
		t.Fatalf("latest = %+v", latest)
	}
	if len(latest.Messages) != 400 || latest.Messages[0].Content == nil || *latest.Messages[0].Content != "user-50" {
		t.Fatalf("latest messages = %d first=%+v", len(latest.Messages), latest.Messages[0])
	}
	before, err := historyCursorTurn(latest.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	older := historyPage(sess, "snapshot_test", before, protocol.HistoryMaxTurns)
	if older.StartTurn != 0 || older.EndTurn != 50 || older.ActualTurns != 50 || older.HasOlder || older.NextCursor != "" {
		t.Fatalf("older = %+v", older)
	}
	if len(older.Messages) != 101 || older.Messages[0].Role != "system" {
		t.Fatalf("older messages = %d first=%+v", len(older.Messages), older.Messages[0])
	}
}

func TestHistoryPagePreservesResolvedCapabilityMetadata(t *testing.T) {
	resolvedReadOnly := false
	history := []provider.Message{
		{Role: provider.RoleUser, Content: "update the database"},
		{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{
			ID: "call-1", Name: "use_capability",
			Arguments:        `{"action":"call","capability_id":"mcp-tool:db/write"}`,
			ResolvedName:     "mcp__db__write",
			CapabilityID:     "mcp-tool:db/write",
			ResolvedReadOnly: &resolvedReadOnly,
		}}},
		{Role: provider.RoleTool, ToolCallID: "call-1", Name: "use_capability", Content: "done"},
	}
	page := historyPage(&session{ctrl: &fakeController{model: "model", history: history}}, "snapshot_test", 0, protocol.HistoryMaxTurns)
	if len(page.Messages) != 3 || len(page.Messages[1].ToolCalls) != 1 {
		t.Fatalf("history page = %+v", page)
	}
	call := page.Messages[1].ToolCalls[0]
	if call.ResolvedName != "mcp__db__write" || call.CapabilityID != "mcp-tool:db/write" ||
		call.ResolvedReadOnly == nil || *call.ResolvedReadOnly {
		t.Fatalf("resolved capability metadata = %+v", call)
	}
}

func TestRuntimeOperationKeepsDetachedServerBusy(t *testing.T) {
	server := New(Options{Workspace: t.TempDir(), Version: "test"})
	server.sessions["session_operation"] = &session{
		id: "session_operation", ctrl: &fakeController{model: "model"},
		currentOp: &protocol.OperationState{OperationID: "operation_test", Kind: protocol.OperationCompact},
	}
	server.mu.Lock()
	busy := server.hasBusyLocked()
	server.mu.Unlock()
	if !busy {
		t.Fatal("detached runtime considered an active operation idle")
	}
}

func TestRuntimeMutationRequestIDReplaysConcurrentSubmitOnce(t *testing.T) {
	srv := New(Options{Workspace: t.TempDir(), Version: "test"})
	ctrl := newBlockingController()
	target := srv.installTestSession(ctrl)
	handler := committedTestHandlers(srv, 1)[protocol.MethodSessionSubmit]
	params := protocol.SessionSubmitParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "request-replay", ExpectedHostEpoch: srv.hostEpoch,
			Target: target, ExpectedRuntimeEpoch: "runtime_test",
		},
		Input: "hello", DisplayText: "hello",
	}

	type outcome struct {
		result protocol.SessionSubmitResult
		err    error
	}
	results := make(chan outcome, 2)
	run := func() {
		value, err := handler(context.Background(), params)
		if err != nil {
			results <- outcome{err: err}
			return
		}
		results <- outcome{result: value.(protocol.SessionSubmitResult)}
	}
	go run()
	<-ctrl.started
	go run()
	close(ctrl.release)
	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("submit errors = %v, %v", first.err, second.err)
	}
	if first.result.TurnID == "" || first.result != second.result {
		t.Fatalf("replayed results differ: first=%+v second=%+v", first.result, second.result)
	}
	if submits, _ := ctrl.counts(); submits != 1 {
		t.Fatalf("controller submits = %d, want exactly 1", submits)
	}
}

func TestRuntimeMutationRequestIDConflictPrecedesEpochChecks(t *testing.T) {
	srv := New(Options{Workspace: t.TempDir(), Version: "test"})
	ctrl := &fakeController{model: "local/stub"}
	target := srv.installTestSession(ctrl)
	handler := committedTestHandlers(srv, 1)[protocol.MethodSessionSubmit]
	params := protocol.SessionSubmitParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "request-conflict", ExpectedHostEpoch: srv.hostEpoch,
			Target: target, ExpectedRuntimeEpoch: "runtime_test",
		},
		Input: "first", DisplayText: "first",
	}
	if _, err := handler(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	params.Input = "changed"
	params.DisplayText = "changed"
	params.ExpectedRuntimeEpoch = "runtime-stale"
	_, err := handler(context.Background(), params)
	var remoteErr *protocol.RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Code != protocol.ErrRequestIDConflict {
		t.Fatalf("conflicting request error = %v, want REQUEST_ID_CONFLICT", err)
	}
}

func TestRuntimeSteerReportsTurnExitRaceInsteadOfFalseAcceptance(t *testing.T) {
	srv := New(Options{Workspace: t.TempDir(), Version: "test"})
	ctrl := &fakeController{model: "local/stub"}
	target := srv.installTestSession(ctrl)
	turnID := protocol.TurnID("turn_test")
	srv.mu.Lock()
	srv.sessions[target.SessionID].currentTurn = turnID
	srv.mu.Unlock()
	params := protocol.TurnSteerParams{
		SessionMutation: protocol.SessionMutation{
			RequestID:            "request-steer",
			ExpectedHostEpoch:    srv.hostEpoch,
			Target:               target,
			ExpectedRuntimeEpoch: "runtime_test",
		},
		ExpectedTurnID: turnID,
		Text:           "use plan B",
	}

	if _, err := srv.steer(params); err == nil {
		t.Fatal("rejected steer returned false acceptance")
	} else {
		var remoteErr *protocol.RemoteError
		if !errors.As(err, &remoteErr) || remoteErr.Code != protocol.ErrTurnNotActive {
			t.Fatalf("rejected steer error = %v, want TURN_NOT_ACTIVE", err)
		}
	}

	ctrl.steerAccepted = true
	result, err := srv.steer(params)
	if err != nil {
		t.Fatalf("accepted steer: %v", err)
	}
	if !result.Accepted || result.TurnID != turnID || len(ctrl.steers) != 1 || ctrl.steers[0] != params.Text {
		t.Fatalf("accepted steer result=%+v steers=%v", result, ctrl.steers)
	}
}

func TestRuntimeProfileWaitsForSubmitAdmissionAndRejectsBusy(t *testing.T) {
	srv := New(Options{Workspace: t.TempDir(), Version: "test"})
	ctrl := newBlockingController()
	target := srv.installTestSession(ctrl)
	handlers := committedTestHandlers(srv, 1)
	submitDone := make(chan error, 1)
	go func() {
		_, err := handlers[protocol.MethodSessionSubmit](context.Background(), protocol.SessionSubmitParams{
			SessionMutation: protocol.SessionMutation{
				RequestID: "request-submit", ExpectedHostEpoch: srv.hostEpoch,
				Target: target, ExpectedRuntimeEpoch: "runtime_test",
			},
			Input: "hello", DisplayText: "hello",
		})
		submitDone <- err
	}()
	<-ctrl.started

	profileDone := make(chan error, 1)
	go func() {
		collaboration := protocol.CollaborationPlan
		_, err := handlers[protocol.MethodSessionProfileSet](context.Background(), protocol.SessionProfileSetParams{
			SessionMutation: protocol.SessionMutation{
				RequestID: "request-profile", ExpectedHostEpoch: srv.hostEpoch,
				Target: target, ExpectedRuntimeEpoch: "runtime_test",
			},
			Patch: protocol.ProfilePatch{CollaborationMode: &collaboration},
		})
		profileDone <- err
	}()
	select {
	case err := <-profileDone:
		t.Fatalf("profile raced submit admission: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(ctrl.release)
	if err := <-submitDone; err != nil {
		t.Fatal(err)
	}
	err := <-profileDone
	var remoteErr *protocol.RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Code != protocol.ErrSessionBusy {
		t.Fatalf("profile error = %v, want SESSION_BUSY", err)
	}
	if submits, closes := ctrl.counts(); submits != 1 || closes != 0 {
		t.Fatalf("controller lifecycle submits=%d closes=%d", submits, closes)
	}
}

func TestRuntimeSessionCreateAndFileList(t *testing.T) {
	ws := t.TempDir()
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	registryPath := filepath.Join(t.TempDir(), "remote-sessions.json")
	// Short absolute socket path — macOS rejects long unix paths.
	sock := filepath.Join(t.TempDir(), "r.sock")
	if len(sock) > 100 {
		tempBase := os.TempDir()
		if goruntime.GOOS == "darwin" {
			tempBase = "/tmp"
		}
		shortDir, err := os.MkdirTemp(tempBase, "rx-wb-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(shortDir) })
		sock = filepath.Join(shortDir, "r.sock")
	}
	srv := New(Options{Workspace: ws, Version: "test", SessionDir: sessionDir, RegistryPath: registryPath, BuildController: func(_ context.Context, model string, _ *string, _ event.Sink) (SessionController, error) {
		return &persistentFakeController{fakeController: &fakeController{model: model}, sessionDir: sessionDir}, nil
	}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx, sock) }()

	// Wait for socket.
	var conn net.Conn
	var err error
	for i := 0; i < 50; i++ {
		select {
		case e := <-errCh:
			t.Fatalf("listen failed: %v", e)
		default:
		}
		conn, err = net.Dial("unix", sock)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	wire := rpcwire.NewConn(conn, conn, rpcwire.Options{
		Name: "test", StrictJSONRPC: true,
		MaxInboundBytes: protocol.FrameBytes, MaxOutboundBytes: protocol.FrameBytes,
	})
	go wire.Serve(ctx)

	buildID := srv.buildID
	raw, err := wire.Request(ctx, string(protocol.MethodRemoteInitialize), protocol.InitializeParams{BuildID: buildID, ClientInstanceID: "desktop-test", Workspace: ws})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("empty initialize result")
	}

	var initialized protocol.InitializeResult
	if err := json.Unmarshal(raw, &initialized); err != nil {
		t.Fatal(err)
	}
	raw, err = wire.Request(ctx, string(protocol.MethodWorkspaceList), protocol.WorkspaceListParams{ExpectedHostEpoch: initialized.HostEpoch})
	if err != nil {
		t.Fatalf("workspace list: %v", err)
	}
	var workspaces protocol.WorkspaceListResult
	if err := json.Unmarshal(raw, &workspaces); err != nil || len(workspaces.Items) != 1 {
		t.Fatalf("workspace list = %s err=%v", raw, err)
	}
	model := "stub/model"
	raw, err = wire.Request(ctx, string(protocol.MethodSessionCreate), protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "request-1", ExpectedHostEpoch: initialized.HostEpoch},
		WorkspaceID:  workspaces.Items[0].WorkspaceID, AdditionalDirectoryRefs: []protocol.DirectoryRef{},
		Topic: protocol.TopicSelection{Kind: protocol.TopicNew}, Profile: protocol.ProfileSelection{Model: &model},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var created protocol.SessionCreateResult
	if err := json.Unmarshal(raw, &created); err != nil || created.Target.SessionID == "" {
		t.Fatalf("create = %s err=%v", raw, err)
	}

	raw, err = wire.Request(ctx, string(protocol.MethodFileList), protocol.FileListParams{
		RuntimeQuery: protocol.RuntimeQuery{ExpectedHostEpoch: initialized.HostEpoch, Target: created.Target, ExpectedRuntimeEpoch: created.RuntimeEpoch},
		Path:         "",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var listed protocol.FileListResult
	_ = json.Unmarshal(raw, &listed)
	if listed.Entries == nil {
		t.Fatalf("list = %s", raw)
	}
	srv.Close()
}

func TestRuntimeGraceDetach(t *testing.T) {
	srv := New(Options{Workspace: t.TempDir(), Version: "t"})
	srv.ForceDetachForTest()
	if srv.Attached() {
		t.Fatal("expected detached")
	}
}

func TestRuntimeRestoresSessionRegistryAfterProcessRestart(t *testing.T) {
	workspace := t.TempDir()
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	registryPath := filepath.Join(t.TempDir(), "remote-sessions.json")
	build := func(_ context.Context, model string, _ *string, _ event.Sink) (SessionController, error) {
		return &persistentFakeController{
			fakeController: &fakeController{model: model},
			sessionDir:     sessionDir,
		}, nil
	}
	first := New(Options{
		Workspace: workspace, SessionDir: sessionDir, RegistryPath: registryPath,
		BuildController: build,
	})
	model := "local/test-model"
	created, err := first.create(context.Background(), protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{ExpectedHostEpoch: first.hostEpoch},
		WorkspaceID:  first.workspaceID,
		Topic:        protocol.TopicSelection{Kind: protocol.TopicNew, Title: "Persisted Remote chat"},
		Profile:      protocol.ProfileSelection{Model: &model},
	})
	if err != nil {
		t.Fatalf("create first runtime session: %v", err)
	}
	first.mu.Lock()
	firstSession := first.sessions[created.Target.SessionID]
	first.mu.Unlock()
	if firstSession == nil {
		t.Fatal("created session missing from first runtime")
	}
	transcript := agent.NewSession("system")
	transcript.Add(provider.Message{Role: provider.RoleUser, Content: "resume me after restart"})
	if err := transcript.Save(firstSession.ctrl.SessionPath()); err != nil {
		t.Fatalf("save transcript: %v", err)
	}
	first.snapshotAndClose()

	second := New(Options{
		Workspace: workspace, SessionDir: sessionDir, RegistryPath: registryPath,
		BuildController: build,
	})
	listed, err := second.list(context.Background(), protocol.SessionListParams{
		ExpectedHostEpoch: second.hostEpoch,
		WorkspaceID:       second.workspaceID,
	})
	if err != nil {
		t.Fatalf("list second runtime sessions: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].Target.SessionID != created.Target.SessionID {
		t.Fatalf("restored sessions = %+v, want %s", listed.Items, created.Target.SessionID)
	}
	if listed.Items[0].Runtime == nil || listed.Items[0].Runtime.RuntimeEpoch == created.RuntimeEpoch {
		t.Fatalf("restored runtime epoch = %+v, want a rebuilt runtime", listed.Items[0].Runtime)
	}
	second.mu.Lock()
	restored := second.sessions[created.Target.SessionID]
	second.mu.Unlock()
	if restored == nil || restored.title != "Persisted Remote chat" {
		t.Fatalf("restored session = %+v", restored)
	}
	history := restored.ctrl.History()
	if len(history) != 2 || history[1].Content != "resume me after restart" {
		t.Fatalf("restored history = %+v", history)
	}
	second.snapshotAndClose()
	second.snapshotAndClose()

	data, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatalf("read registry after repeated shutdown: %v", err)
	}
	var registry runtimeSessionRegistry
	if err := json.Unmarshal(data, &registry); err != nil || len(registry.Sessions) != 1 {
		t.Fatalf("registry after shutdown = %s err=%v", data, err)
	}
}

func TestRuntimeSessionLeaseBlocksConcurrentRestoreAndReleasesOnClose(t *testing.T) {
	workspace := t.TempDir()
	sessionDir := filepath.Join(t.TempDir(), "sessions")
	registryPath := filepath.Join(t.TempDir(), "remote-sessions.json")
	build := func(_ context.Context, model string, _ *string, _ event.Sink) (SessionController, error) {
		return &persistentFakeController{
			fakeController: &fakeController{model: model},
			sessionDir:     sessionDir,
		}, nil
	}
	first := New(Options{
		Workspace: workspace, SessionDir: sessionDir, RegistryPath: registryPath,
		BuildController: build,
	})
	model := "local/test-model"
	created, err := first.create(context.Background(), protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{ExpectedHostEpoch: first.hostEpoch},
		WorkspaceID:  first.workspaceID,
		Topic:        protocol.TopicSelection{Kind: protocol.TopicNew},
		Profile:      protocol.ProfileSelection{Model: &model},
	})
	if err != nil {
		t.Fatalf("create first runtime session: %v", err)
	}
	first.mu.Lock()
	firstSession := first.sessions[created.Target.SessionID]
	first.mu.Unlock()
	if firstSession == nil {
		t.Fatal("created session missing from first runtime")
	}
	path := firstSession.ctrl.SessionPath()
	if lease, leaseErr := agent.TryAcquireSessionLease(path); !errors.Is(leaseErr, agent.ErrSessionLeaseHeld) {
		if lease != nil {
			lease.Release()
		}
		t.Fatalf("concurrent lease acquire err = %v, want ErrSessionLeaseHeld", leaseErr)
	}

	second := New(Options{
		Workspace: workspace, SessionDir: sessionDir, RegistryPath: registryPath,
		BuildController: build,
	})
	listed, err := second.list(context.Background(), protocol.SessionListParams{
		ExpectedHostEpoch: second.hostEpoch,
		WorkspaceID:       second.workspaceID,
	})
	if err != nil {
		t.Fatalf("list while first runtime owns lease: %v", err)
	}
	if len(listed.Items) != 0 {
		t.Fatalf("concurrent restore published sessions = %+v, want none", listed.Items)
	}
	if _, ok := second.dormant[created.Target.SessionID]; !ok {
		t.Fatal("lease-blocked restore was not kept dormant")
	}

	first.snapshotAndClose()
	listed, err = second.list(context.Background(), protocol.SessionListParams{
		ExpectedHostEpoch: second.hostEpoch,
		WorkspaceID:       second.workspaceID,
	})
	if err != nil {
		t.Fatalf("list after first runtime released lease: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].Target.SessionID != created.Target.SessionID {
		t.Fatalf("restored sessions = %+v, want %s", listed.Items, created.Target.SessionID)
	}
	second.mu.Lock()
	restored := second.sessions[created.Target.SessionID]
	second.mu.Unlock()
	if restored == nil {
		t.Fatal("second runtime did not restore the released session")
	}

	closed, err := second.closeSession(protocol.SessionCloseParams{SessionMutation: protocol.SessionMutation{
		ExpectedHostEpoch:    second.hostEpoch,
		Target:               second.target(created.Target.SessionID),
		ExpectedRuntimeEpoch: restored.runtimeEpoch,
	}})
	if err != nil {
		t.Fatalf("close restored runtime session: %v", err)
	}
	if closed.Disposition != protocol.SessionReleased {
		t.Fatalf("close disposition = %q, want released", closed.Disposition)
	}
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("lease was not released on close: %v", err)
	}
	lease.Release()
}

func TestRuntimeDefersRestoreWhenControllerBuilderReturnsTypedNil(t *testing.T) {
	workspace := t.TempDir()
	sessionDir := t.TempDir()
	registryPath := filepath.Join(t.TempDir(), "remote-sessions.json")
	log := &strings.Builder{}
	var typedNil *persistentFakeController
	srv := New(Options{
		Workspace: workspace, SessionDir: sessionDir, RegistryPath: registryPath, Logger: log,
		BuildController: func(context.Context, string, *string, event.Sink) (SessionController, error) {
			return typedNil, errors.New("provider removed")
		},
	})
	srv.registryRead = true
	record := runtimeSessionRecord{
		ID: "session_removed_provider", Path: filepath.Join(sessionDir, "removed.jsonl"),
		Model: "removed/model", TopicID: "topic_removed_provider",
	}
	srv.dormant[record.ID] = record

	if err := srv.ensureSessionsRestored(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(srv.sessions) != 0 {
		t.Fatalf("restored sessions = %d, want zero", len(srv.sessions))
	}
	if _, ok := srv.dormant[record.ID]; !ok {
		t.Fatal("failed restore was not kept dormant for a later Provider catalog")
	}
	if !strings.Contains(log.String(), "defer session restore") {
		t.Fatalf("restore failure was not logged: %q", log.String())
	}
}

func TestRuntimeEarlyShutdownDoesNotOverwriteUnreadRegistry(t *testing.T) {
	registryPath := filepath.Join(t.TempDir(), "remote-sessions.json")
	original := []byte(`{"version":99,"workspace":"future","sessions":[{"future":true}]}` + "\n")
	if err := os.WriteFile(registryPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	srv := New(Options{Workspace: t.TempDir(), RegistryPath: registryPath})
	srv.Close()
	after, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatalf("early shutdown replaced unread registry:\n%s", after)
	}
}

func TestSetProfileRegistryFailurePreservesUsableSession(t *testing.T) {
	newServer := func(t *testing.T) (*Server, *session, *profileFakeController, *[]*profileFakeController) {
		t.Helper()
		workspace := t.TempDir()
		sessionDir := t.TempDir()
		registryPath := t.TempDir() // AtomicWriteFile cannot replace this directory.
		built := &[]*profileFakeController{}
		srv := New(Options{
			Workspace: workspace, SessionDir: sessionDir, RegistryPath: registryPath,
			BuildController: func(_ context.Context, model string, _ *string, _ event.Sink) (SessionController, error) {
				ctrl := &profileFakeController{persistentFakeController: &persistentFakeController{
					fakeController: &fakeController{model: model}, sessionDir: sessionDir,
				}}
				*built = append(*built, ctrl)
				return ctrl, nil
			},
		})
		srv.registryRead = true
		old := &profileFakeController{persistentFakeController: &persistentFakeController{
			fakeController: &fakeController{model: "local/old"}, sessionDir: sessionDir,
			sessionPath: filepath.Join(sessionDir, "session.jsonl"),
		}}
		old.approvalMode = string(protocol.ToolApprovalAsk)
		now := time.Now().UnixMilli()
		sess := &session{
			id: "session_profile", ctrl: old, model: old.model, effort: "high",
			collaboration: protocol.CollaborationNormal, tokenMode: protocol.TokenFull,
			toolApproval: protocol.ToolApprovalAsk, topicID: "topic_profile", title: "Profile",
			runtimeEpoch: "runtime_profile", createdAt: now, updatedAt: now,
		}
		srv.sessions[sess.id] = sess
		return srv, sess, old, built
	}

	t.Run("metadata update rolls back", func(t *testing.T) {
		srv, sess, old, _ := newServer(t)
		old.SetGoal("keep the existing goal")
		collaboration := protocol.CollaborationPlan
		approval := protocol.ToolApprovalYOLO
		goal := "replace the goal"
		_, err := srv.setProfile(context.Background(), protocol.SessionProfileSetParams{
			SessionMutation: protocol.SessionMutation{
				ExpectedHostEpoch: srv.hostEpoch, Target: srv.target(sess.id), ExpectedRuntimeEpoch: sess.runtimeEpoch,
			},
			Patch: protocol.ProfilePatch{CollaborationMode: &collaboration, ToolApprovalMode: &approval, Goal: &goal},
		})
		if err == nil {
			t.Fatal("profile update succeeded despite registry failure")
		}
		if sess.collaboration != protocol.CollaborationNormal || sess.toolApproval != protocol.ToolApprovalAsk || old.planMode || old.approvalMode != string(protocol.ToolApprovalAsk) {
			t.Fatalf("profile after failed persist = collaboration=%q approval=%q controller=(%v,%q)", sess.collaboration, sess.toolApproval, old.planMode, old.approvalMode)
		}
		if old.Goal() != "keep the existing goal" {
			t.Fatalf("goal changed despite failed profile transaction: %q", old.Goal())
		}
	})

	t.Run("controller rebuild rolls back", func(t *testing.T) {
		srv, sess, old, built := newServer(t)
		model := "local/new"
		previousEpoch := sess.runtimeEpoch
		_, err := srv.setProfile(context.Background(), protocol.SessionProfileSetParams{
			SessionMutation: protocol.SessionMutation{
				ExpectedHostEpoch: srv.hostEpoch, Target: srv.target(sess.id), ExpectedRuntimeEpoch: sess.runtimeEpoch,
			},
			Patch: protocol.ProfilePatch{Model: &model},
		})
		if err == nil {
			t.Fatal("profile rebuild succeeded despite registry failure")
		}
		if sess.ctrl != old || sess.model != "local/old" || sess.runtimeEpoch != previousEpoch || old.closed {
			t.Fatalf("session was not restored: ctrlOld=%v model=%q epoch=%q oldClosed=%v", sess.ctrl == old, sess.model, sess.runtimeEpoch, old.closed)
		}
		if len(*built) != 1 || !(*built)[0].closed {
			t.Fatalf("replacement controllers = %d closed=%v", len(*built), len(*built) == 1 && (*built)[0].closed)
		}
	})
}

func TestSetProfileAppliesGoalInSameTransaction(t *testing.T) {
	workspace := t.TempDir()
	sessionDir := t.TempDir()
	registryPath := filepath.Join(t.TempDir(), "sessions.json")
	srv := New(Options{Workspace: workspace, SessionDir: sessionDir, RegistryPath: registryPath})
	srv.registryRead = true
	ctrl := &profileFakeController{persistentFakeController: &persistentFakeController{
		fakeController: &fakeController{model: "local/model"}, sessionDir: sessionDir,
		sessionPath: filepath.Join(sessionDir, "session.jsonl"),
	}}
	target := srv.installTestSession(ctrl)
	if err := srv.persistSessionRegistry(); err != nil {
		t.Fatal(err)
	}
	collaboration := protocol.CollaborationGoal
	approval := protocol.ToolApprovalAuto
	goal := "finish the remote profile migration"

	result, err := srv.setProfile(context.Background(), protocol.SessionProfileSetParams{
		SessionMutation: protocol.SessionMutation{
			ExpectedHostEpoch: srv.hostEpoch, Target: target, ExpectedRuntimeEpoch: "runtime_test",
		},
		Patch: protocol.ProfilePatch{
			CollaborationMode: &collaboration,
			ToolApprovalMode:  &approval,
			Goal:              &goal,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ResolvedProfile.CollaborationMode != collaboration || result.ResolvedProfile.ToolApprovalMode != approval {
		t.Fatalf("resolved profile = %+v", result.ResolvedProfile)
	}
	if ctrl.planMode || ctrl.approvalMode != string(approval) || ctrl.Goal() != goal || ctrl.GoalStatus() != string(protocol.GoalRunning) {
		t.Fatalf("controller profile = plan:%v approval:%q goal:%q status:%q", ctrl.planMode, ctrl.approvalMode, ctrl.Goal(), ctrl.GoalStatus())
	}
	if _, err := os.Stat(srv.profileTransactionPath()); !os.IsNotExist(err) {
		t.Fatalf("profile transaction journal remains after commit: %v", err)
	}
}

func TestSetProfileRebuildAppliesGoalInSameTransaction(t *testing.T) {
	workspace := t.TempDir()
	sessionDir := t.TempDir()
	registryPath := filepath.Join(t.TempDir(), "sessions.json")
	var replacement *profileFakeController
	srv := New(Options{
		Workspace: workspace, SessionDir: sessionDir, RegistryPath: registryPath,
		BuildController: func(_ context.Context, model string, _ *string, _ event.Sink) (SessionController, error) {
			replacement = &profileFakeController{persistentFakeController: &persistentFakeController{
				fakeController: &fakeController{model: model}, sessionDir: sessionDir,
			}}
			return replacement, nil
		},
	})
	srv.registryRead = true
	old := &profileFakeController{persistentFakeController: &persistentFakeController{
		fakeController: &fakeController{model: "local/old"}, sessionDir: sessionDir,
		sessionPath: filepath.Join(sessionDir, "session.jsonl"),
	}, approvalMode: string(protocol.ToolApprovalAsk)}
	target := srv.installTestSession(old)
	if err := srv.persistSessionRegistry(); err != nil {
		t.Fatal(err)
	}
	model := "local/new"
	collaboration := protocol.CollaborationGoal
	approval := protocol.ToolApprovalAuto
	goal := "finish the rebuilt remote profile"

	result, err := srv.setProfile(context.Background(), protocol.SessionProfileSetParams{
		SessionMutation: protocol.SessionMutation{
			ExpectedHostEpoch: srv.hostEpoch, Target: target, ExpectedRuntimeEpoch: "runtime_test",
		},
		Patch: protocol.ProfilePatch{
			Model:             &model,
			CollaborationMode: &collaboration,
			ToolApprovalMode:  &approval,
			Goal:              &goal,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Disposition != protocol.ProfileRebuilt || replacement == nil {
		t.Fatalf("rebuild result = %+v replacement=%v", result, replacement != nil)
	}
	if !old.closed || replacement.closed || replacement.Goal() != goal || replacement.GoalStatus() != string(protocol.GoalRunning) {
		t.Fatalf("controller lifecycle oldClosed=%v newClosed=%v goal=%q status=%q", old.closed, replacement.closed, replacement.Goal(), replacement.GoalStatus())
	}
	if _, err := os.Stat(srv.profileTransactionPath()); !os.IsNotExist(err) {
		t.Fatalf("profile transaction journal remains after rebuild commit: %v", err)
	}
}

func TestSetProfileGoalWriteFailureRollsBackRegistry(t *testing.T) {
	workspace := t.TempDir()
	sessionDir := t.TempDir()
	registryPath := filepath.Join(t.TempDir(), "sessions.json")
	srv := New(Options{Workspace: workspace, SessionDir: sessionDir, RegistryPath: registryPath})
	srv.registryRead = true
	ctrl := &profileFakeController{persistentFakeController: &persistentFakeController{
		fakeController: &fakeController{model: "local/model"}, sessionDir: sessionDir,
		sessionPath: filepath.Join(sessionDir, "session.jsonl"),
	}, approvalMode: string(protocol.ToolApprovalAsk), goalWriteErr: errors.New("injected Goal write failure")}
	target := srv.installTestSession(ctrl)
	if err := srv.persistSessionRegistry(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	collaboration := protocol.CollaborationGoal
	approval := protocol.ToolApprovalAuto
	goal := "must not commit"
	_, err = srv.setProfile(context.Background(), protocol.SessionProfileSetParams{
		SessionMutation: protocol.SessionMutation{
			ExpectedHostEpoch: srv.hostEpoch, Target: target, ExpectedRuntimeEpoch: "runtime_test",
		},
		Patch: protocol.ProfilePatch{
			CollaborationMode: &collaboration,
			ToolApprovalMode:  &approval,
			Goal:              &goal,
		},
	})
	var remoteErr *protocol.RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Code != protocol.ErrSessionPersistFailed {
		t.Fatalf("setProfile error = %v, want SESSION_PERSIST_FAILED", err)
	}
	after, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("registry changed after failed Goal write:\nbefore=%s\nafter=%s", before, after)
	}
	sess := srv.sessions[target.SessionID]
	if sess.collaboration != protocol.CollaborationNormal || sess.toolApproval != protocol.ToolApprovalAsk {
		t.Fatalf("session profile changed after failed Goal write: %+v", resolvedProfile(sess))
	}
	if ctrl.planMode || ctrl.approvalMode != string(protocol.ToolApprovalAsk) || ctrl.Goal() != "" {
		t.Fatalf("controller changed after failed Goal write: plan=%v approval=%q goal=%q", ctrl.planMode, ctrl.approvalMode, ctrl.Goal())
	}
	if _, err := os.Stat(srv.profileTransactionPath()); !os.IsNotExist(err) {
		t.Fatalf("profile transaction journal remains after rollback: %v", err)
	}
}

func TestProfileTransactionRecoveryFencesPartialDurableUpdates(t *testing.T) {
	writeRegistry := func(t *testing.T, path, workspace, model string) []byte {
		t.Helper()
		body, err := json.Marshal(runtimeSessionRegistry{
			Version: runtimeSessionRegistryVersion, Workspace: canonicalRegistryWorkspace(workspace),
			Sessions: []runtimeSessionRecord{{ID: "session_test", Path: "session.jsonl", Model: model, TopicID: "topic_test"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		body = append(body, '\n')
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		return body
	}
	assertBytes := func(t *testing.T, path string, want []byte) {
		t.Helper()
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s contents = %q, want %q", path, got, want)
		}
	}

	for _, tc := range []struct {
		name      string
		commit    bool
		wantModel string
		wantGoal  string
	}{
		{name: "prepared rolls back", wantModel: "local/old", wantGoal: "old goal\n"},
		{name: "committed stays applied", commit: true, wantModel: "local/new", wantGoal: "new goal\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			sessionDir := t.TempDir()
			registryPath := filepath.Join(t.TempDir(), "sessions.json")
			sessionPath := filepath.Join(sessionDir, "session.jsonl")
			oldRegistry := writeRegistry(t, registryPath, workspace, "local/old")
			goalPath := store.SessionGoalState(sessionPath)
			oldGoal := []byte("old goal\n")
			if err := os.WriteFile(goalPath, oldGoal, 0o644); err != nil {
				t.Fatal(err)
			}
			srv := New(Options{Workspace: workspace, SessionDir: sessionDir, RegistryPath: registryPath})
			txn, err := srv.beginProfileGoalTransaction(sessionPath)
			if err != nil {
				t.Fatal(err)
			}
			newRegistry := writeRegistry(t, registryPath, workspace, "local/new")
			newGoal := []byte("new goal\n")
			if err := os.WriteFile(goalPath, newGoal, 0o644); err != nil {
				t.Fatal(err)
			}
			if tc.commit {
				if err := srv.commitProfileGoalTransaction(txn); err != nil {
					t.Fatal(err)
				}
			}

			restarted := New(Options{Workspace: workspace, SessionDir: sessionDir, RegistryPath: registryPath})
			if err := restarted.recoverProfileTransaction(); err != nil {
				t.Fatal(err)
			}
			wantRegistry := oldRegistry
			if tc.commit {
				wantRegistry = newRegistry
			}
			assertBytes(t, registryPath, wantRegistry)
			assertBytes(t, goalPath, []byte(tc.wantGoal))
			if _, err := os.Stat(restarted.profileTransactionPath()); !os.IsNotExist(err) {
				t.Fatalf("profile transaction journal remains after recovery: %v", err)
			}
			var registry runtimeSessionRegistry
			if err := json.Unmarshal(wantRegistry, &registry); err != nil {
				t.Fatal(err)
			}
			if registry.Sessions[0].Model != tc.wantModel {
				t.Fatalf("recovered model = %q, want %q", registry.Sessions[0].Model, tc.wantModel)
			}
		})
	}
}

func TestProfileTransactionRecoveryOwnsAutoResearchSideEffects(t *testing.T) {
	for _, tc := range []struct {
		name          string
		commit        bool
		wantTaskCount int
		wantGoal      bool
	}{
		{name: "prepared removes created task"},
		{name: "committed preserves created task", commit: true, wantTaskCount: 1, wantGoal: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace := t.TempDir()
			sessionDir := t.TempDir()
			registryPath := filepath.Join(t.TempDir(), "sessions.json")
			sessionPath := filepath.Join(sessionDir, "session.jsonl")
			srv := New(Options{Workspace: workspace, SessionDir: sessionDir, RegistryPath: registryPath})
			txn, err := srv.beginProfileGoalTransaction(sessionPath)
			if err != nil {
				t.Fatal(err)
			}
			if txn.journal.AutoResearchCreateToken == "" {
				t.Fatal("prepared profile transaction has no AutoResearch create token")
			}
			ctrl := control.New(control.Options{
				SessionDir: sessionDir, SessionPath: sessionPath,
				WorkspaceRoot: workspace, Label: "profile-recovery",
			})
			t.Cleanup(ctrl.Close)
			goal := "investigate the root cause, implement the fix, and verify the performance regression"
			if err := ctrl.SetGoalDurable(goal, txn.journal.AutoResearchCreateToken); err != nil {
				t.Fatal(err)
			}
			taskRoot := filepath.Join(workspace, ".reasonix", "autoresearch")
			entries, err := os.ReadDir(taskRoot)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Fatalf("AutoResearch task count before recovery = %d, want 1", len(entries))
			}
			tokenPath := filepath.Join(taskRoot, entries[0].Name(), ".create_token")
			token, err := os.ReadFile(tokenPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(string(token)) != txn.journal.AutoResearchCreateToken {
				t.Fatalf("AutoResearch create token = %q, want transaction token", token)
			}
			if tc.commit {
				if err := srv.commitProfileGoalTransaction(txn); err != nil {
					t.Fatal(err)
				}
			}

			restarted := New(Options{Workspace: workspace, SessionDir: sessionDir, RegistryPath: registryPath})
			if err := restarted.recoverProfileTransaction(); err != nil {
				t.Fatal(err)
			}
			entries, err = os.ReadDir(taskRoot)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(entries) != tc.wantTaskCount {
				t.Fatalf("AutoResearch task count after recovery = %d, want %d", len(entries), tc.wantTaskCount)
			}
			_, goalErr := os.Stat(store.SessionGoalState(sessionPath))
			if tc.wantGoal && goalErr != nil {
				t.Fatalf("committed Goal sidecar missing after recovery: %v", goalErr)
			}
			if !tc.wantGoal && !os.IsNotExist(goalErr) {
				t.Fatalf("prepared Goal sidecar remains after recovery: %v", goalErr)
			}
			if _, err := os.Stat(restarted.profileTransactionPath()); !os.IsNotExist(err) {
				t.Fatalf("profile transaction journal remains after recovery: %v", err)
			}
		})
	}
}

func TestSetProfileRejectsAllControllerActiveWork(t *testing.T) {
	tests := []struct {
		name   string
		status control.RuntimeStatus
	}{
		{name: "foreground turn", status: control.RuntimeStatus{Running: true}},
		{name: "pending prompt", status: control.RuntimeStatus{PendingPrompt: true}},
		{name: "background job", status: control.RuntimeStatus{BackgroundJobs: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			built := false
			srv := New(Options{
				Workspace: t.TempDir(), SessionDir: t.TempDir(),
				BuildController: func(context.Context, string, *string, event.Sink) (SessionController, error) {
					built = true
					return nil, errors.New("replacement must not be built while work is active")
				},
			})
			old := &profileFakeController{persistentFakeController: &persistentFakeController{
				fakeController: &fakeController{model: "local/old", status: tc.status}, sessionDir: t.TempDir(),
			}}
			target := srv.installTestSession(old)
			model := "local/new"
			_, err := srv.setProfile(context.Background(), protocol.SessionProfileSetParams{
				SessionMutation: protocol.SessionMutation{
					ExpectedHostEpoch: srv.hostEpoch, Target: target, ExpectedRuntimeEpoch: "runtime_test",
				},
				Patch: protocol.ProfilePatch{Model: &model},
			})
			var remoteErr *protocol.RemoteError
			if !errors.As(err, &remoteErr) || remoteErr.Code != protocol.ErrSessionBusy {
				t.Fatalf("setProfile error = %v, want SESSION_BUSY", err)
			}
			if built || old.closed {
				t.Fatalf("active controller lifecycle changed: built=%v oldClosed=%v", built, old.closed)
			}
		})
	}
}

func TestSetProfileRechecksActiveWorkBeforeSwap(t *testing.T) {
	var old *profileFakeController
	var replacement *profileFakeController
	sessionDir := t.TempDir()
	srv := New(Options{
		Workspace: t.TempDir(), SessionDir: sessionDir,
		BuildController: func(_ context.Context, model string, _ *string, _ event.Sink) (SessionController, error) {
			old.status.BackgroundJobs = 1
			replacement = &profileFakeController{persistentFakeController: &persistentFakeController{
				fakeController: &fakeController{model: model}, sessionDir: sessionDir,
			}}
			return replacement, nil
		},
	})
	old = &profileFakeController{persistentFakeController: &persistentFakeController{
		fakeController: &fakeController{model: "local/old"}, sessionDir: sessionDir,
	}}
	target := srv.installTestSession(old)
	model := "local/new"
	_, err := srv.setProfile(context.Background(), protocol.SessionProfileSetParams{
		SessionMutation: protocol.SessionMutation{
			ExpectedHostEpoch: srv.hostEpoch, Target: target, ExpectedRuntimeEpoch: "runtime_test",
		},
		Patch: protocol.ProfilePatch{Model: &model},
	})
	var remoteErr *protocol.RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Code != protocol.ErrSessionBusy {
		t.Fatalf("setProfile error = %v, want SESSION_BUSY", err)
	}
	srv.mu.Lock()
	sess := srv.sessions[target.SessionID]
	srv.mu.Unlock()
	if replacement == nil || !replacement.closed || sess == nil || sess.ctrl != old || old.closed {
		t.Fatalf("swap was not failure-atomic: replacementClosed=%v oldInstalled=%v oldClosed=%v", replacement != nil && replacement.closed, sess != nil && sess.ctrl == old, old.closed)
	}
}

func TestCloseSessionRetainsPromptAndBackgroundWork(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status control.RuntimeStatus
	}{
		{name: "pending prompt", status: control.RuntimeStatus{PendingPrompt: true}},
		{name: "background job", status: control.RuntimeStatus{BackgroundJobs: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := New(Options{Workspace: t.TempDir(), SessionDir: t.TempDir()})
			old := &profileFakeController{persistentFakeController: &persistentFakeController{
				fakeController: &fakeController{model: "local/old", status: tc.status}, sessionDir: t.TempDir(),
			}}
			target := srv.installTestSession(old)
			result, err := srv.closeSession(protocol.SessionCloseParams{SessionMutation: protocol.SessionMutation{
				ExpectedHostEpoch: srv.hostEpoch, Target: target, ExpectedRuntimeEpoch: "runtime_test",
			}})
			if err != nil {
				t.Fatal(err)
			}
			if result.Disposition != protocol.SessionRetainedActive || old.closed {
				t.Fatalf("close result = %+v, oldClosed=%v", result, old.closed)
			}
			srv.mu.Lock()
			installed := srv.sessions[target.SessionID] != nil
			srv.mu.Unlock()
			if !installed {
				t.Fatal("active session was removed")
			}
		})
	}
}

func TestSessionRotationRegistryFailureReturnsCommittedEpoch(t *testing.T) {
	newFixture := func(t *testing.T) (*Server, *session, *rotatingFakeController, protocol.RuntimeTarget, *strings.Builder) {
		t.Helper()
		sessionDir := t.TempDir()
		log := &strings.Builder{}
		srv := New(Options{
			Workspace: t.TempDir(), SessionDir: sessionDir,
			RegistryPath: t.TempDir(), // AtomicWriteFile cannot replace this directory.
			Logger:       log,
		})
		t.Cleanup(srv.snapshotAndClose)
		ctrl := &rotatingFakeController{
			persistentFakeController: &persistentFakeController{
				fakeController: &fakeController{model: "local/test", history: []provider.Message{{Role: provider.RoleUser, Content: "old"}}},
				sessionDir:     sessionDir, sessionPath: filepath.Join(sessionDir, "old.jsonl"),
			},
			newPath: filepath.Join(sessionDir, "new.jsonl"), clearPath: filepath.Join(sessionDir, "cleared.jsonl"),
		}
		target := srv.installTestSession(ctrl)
		srv.registryRead = true
		srv.mu.Lock()
		sess := srv.sessions[target.SessionID]
		srv.mu.Unlock()
		return srv, sess, ctrl, target, log
	}

	t.Run("new session", func(t *testing.T) {
		srv, sess, ctrl, target, log := newFixture(t)
		previousEpoch := sess.runtimeEpoch
		result, err := srv.newSession(protocol.SessionNewParams{SessionMutation: protocol.SessionMutation{
			ExpectedHostEpoch: srv.hostEpoch, Target: target, ExpectedRuntimeEpoch: previousEpoch,
		}})
		if err != nil {
			t.Fatalf("new session returned an error after controller commit: %v", err)
		}
		if ctrl.newCalls != 1 || result.RuntimeEpoch == previousEpoch || sess.runtimeEpoch != result.RuntimeEpoch || !result.SnapshotRequired {
			t.Fatalf("new session result = %+v calls=%d sessionEpoch=%q", result, ctrl.newCalls, sess.runtimeEpoch)
		}
		if _, err := srv.sessionForQuery(srv.hostEpoch, target, result.RuntimeEpoch); err != nil {
			t.Fatalf("returned epoch is not usable: %v", err)
		}
		if !strings.Contains(log.String(), "persist committed new session") {
			t.Fatalf("registry failure was not logged: %q", log.String())
		}
	})

	t.Run("clear session", func(t *testing.T) {
		srv, sess, ctrl, target, log := newFixture(t)
		previousEpoch := sess.runtimeEpoch
		result, err := srv.clearSession(protocol.SessionClearParams{SessionMutation: protocol.SessionMutation{
			ExpectedHostEpoch: srv.hostEpoch, Target: target, ExpectedRuntimeEpoch: previousEpoch,
		}})
		if err != nil {
			t.Fatalf("clear session returned an error after controller commit: %v", err)
		}
		if ctrl.clearCalls != 1 || result.RuntimeEpoch == previousEpoch || sess.runtimeEpoch != result.RuntimeEpoch || !result.SnapshotRequired {
			t.Fatalf("clear session result = %+v calls=%d sessionEpoch=%q", result, ctrl.clearCalls, sess.runtimeEpoch)
		}
		if _, err := srv.sessionForQuery(srv.hostEpoch, target, result.RuntimeEpoch); err != nil {
			t.Fatalf("returned epoch is not usable: %v", err)
		}
		if !strings.Contains(log.String(), "persist committed cleared session") {
			t.Fatalf("registry failure was not logged: %q", log.String())
		}
	})
}

func TestSessionRotationLeaseFailureRetiresSession(t *testing.T) {
	for _, operation := range []string{"new", "clear"} {
		t.Run(operation, func(t *testing.T) {
			sessionDir := t.TempDir()
			srv := New(Options{
				Workspace:    t.TempDir(),
				SessionDir:   sessionDir,
				RegistryPath: filepath.Join(t.TempDir(), "remote-sessions.json"),
			})
			t.Cleanup(srv.snapshotAndClose)

			oldPath := filepath.Join(sessionDir, "old.jsonl")
			rotatedPath := filepath.Join(sessionDir, operation+".jsonl")
			ctrl := &rotatingFakeController{
				persistentFakeController: &persistentFakeController{
					fakeController: &fakeController{model: "local/test"},
					sessionDir:     sessionDir,
					sessionPath:    oldPath,
				},
				newPath: rotatedPath, clearPath: rotatedPath,
			}
			target := srv.installTestSession(ctrl)
			srv.registryRead = true
			srv.mu.Lock()
			sess := srv.sessions[target.SessionID]
			srv.subs["subscription_test"] = &subscription{sessionID: sess.id}
			srv.mu.Unlock()

			leases := control.NewSessionLeaseKeeper()
			if err := leases.Rebind(oldPath); err != nil {
				t.Fatalf("bind old session lease: %v", err)
			}
			sess.leases = leases
			blocker, err := agent.TryAcquireSessionLease(rotatedPath)
			if err != nil {
				t.Fatalf("hold rotated session lease: %v", err)
			}
			t.Cleanup(blocker.Release)

			previousEpoch := sess.runtimeEpoch
			switch operation {
			case "new":
				_, err = srv.newSession(protocol.SessionNewParams{SessionMutation: protocol.SessionMutation{
					ExpectedHostEpoch: srv.hostEpoch, Target: target, ExpectedRuntimeEpoch: previousEpoch,
				}})
			case "clear":
				_, err = srv.clearSession(protocol.SessionClearParams{SessionMutation: protocol.SessionMutation{
					ExpectedHostEpoch: srv.hostEpoch, Target: target, ExpectedRuntimeEpoch: previousEpoch,
				}})
			}
			if err == nil {
				t.Fatalf("%s session succeeded despite rotated-path lease conflict", operation)
			}

			srv.mu.Lock()
			_, sessionExists := srv.sessions[sess.id]
			_, subscriptionExists := srv.subs["subscription_test"]
			srv.mu.Unlock()
			if sessionExists || subscriptionExists {
				t.Fatalf("retired session remains published: session=%v subscription=%v", sessionExists, subscriptionExists)
			}
			if !ctrl.closed {
				t.Fatal("controller was not closed after lease failure")
			}
			if got := leases.HeldPath(); got != "" {
				t.Fatalf("old lease remained held after retirement: %q", got)
			}
			oldLease, err := agent.TryAcquireSessionLease(oldPath)
			if err != nil {
				t.Fatalf("old session lease was not released after retirement: %v", err)
			}
			oldLease.Release()
			if sess.runtimeEpoch != previousEpoch {
				t.Fatalf("runtime epoch changed on failed rotation: got %q want %q", sess.runtimeEpoch, previousEpoch)
			}
		})
	}
}

func TestTurnDoneClearsPendingPromptAndReplayEvents(t *testing.T) {
	tests := []struct {
		name   string
		prompt event.Event
	}{
		{
			name: "approval",
			prompt: event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{
				ID: "approval_test", Tool: "bash", Subject: "go test ./...",
			}},
		},
		{
			name: "ask",
			prompt: event.Event{Kind: event.AskRequest, Ask: event.Ask{
				ID: "ask_test", Questions: []event.AskQuestion{{ID: "question_test", Prompt: "Continue?"}},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := New(Options{Workspace: t.TempDir()})
			target := srv.installTestSession(&fakeController{model: "local/test"})
			srv.mu.Lock()
			sess := srv.sessions[target.SessionID]
			sess.currentTurn = "turn_test"
			sess.lastError = "previous failure"
			srv.mu.Unlock()
			sink := &sessionSink{server: srv, sessionID: target.SessionID}

			sink.Emit(tt.prompt)
			srv.mu.Lock()
			pendingBeforeDone := sess.pendingPrompt != nil
			srv.mu.Unlock()
			if !pendingBeforeDone {
				t.Fatal("prompt event did not create a pending prompt")
			}

			sink.Emit(event.Event{Kind: event.TurnDone, Cancelled: true})
			srv.mu.Lock()
			snapshot := srv.snapshotLocked(sess, 20)
			srv.mu.Unlock()
			if snapshot.PendingPrompt != nil || len(snapshot.Runtime.LiveEvents) != 0 || snapshot.Runtime.CurrentTurn != nil || snapshot.Runtime.LastError != nil {
				t.Fatalf("completed snapshot retained turn state: prompt=%+v live=%d turn=%+v error=%v", snapshot.PendingPrompt, len(snapshot.Runtime.LiveEvents), snapshot.Runtime.CurrentTurn, snapshot.Runtime.LastError)
			}
		})
	}
}

func TestRuntimeSessionRegistryAcceptsOpaqueTopicAndRejectsNestedPath(t *testing.T) {
	sessionDir := t.TempDir()
	srv := New(Options{Workspace: t.TempDir(), SessionDir: sessionDir})
	record := runtimeSessionRecord{
		ID: "session_valid", TopicID: "customer-topic-id", Model: "local/model",
		Path: filepath.Join(sessionDir, "session.jsonl"),
	}
	if err := srv.validateSessionRecord(record); err != nil {
		t.Fatalf("valid opaque topic record rejected: %v", err)
	}
	record.Path = filepath.Join(sessionDir, "nested", "session.jsonl")
	if err := srv.validateSessionRecord(record); err == nil {
		t.Fatal("nested transcript path accepted")
	}
}

func TestRuntimeWorkspaceGitQueriesAndSnapshotProjection(t *testing.T) {
	workspace := t.TempDir()
	runGit := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", workspace}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	runGit("init")
	runGit("config", "user.email", "remote-test@example.com")
	runGit("config", "user.name", "Remote Test")
	tracked := filepath.Join(workspace, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit("add", "tracked.txt")
	runGit("commit", "-m", "initial")
	hash := runGit("rev-parse", "HEAD")
	if err := os.WriteFile(tracked, []byte("after\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "untracked.txt"), []byte("new\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctrl := &projectionController{
		fakeController: &fakeController{model: "local/stub"},
		checkpoints:    []checkpoint.Meta{{Turn: 1, Time: time.Now(), Prompt: "edit it", Paths: []string{tracked}}},
		todos:          []evidence.TodoItem{{Content: "verify", Status: "in_progress"}},
	}
	srv := New(Options{Workspace: workspace, Version: "test"})
	target := srv.installTestSession(ctrl)
	query := protocol.RuntimeQuery{ExpectedHostEpoch: srv.hostEpoch, Target: target, ExpectedRuntimeEpoch: "runtime_test"}
	mutation := protocol.SessionMutation{RequestID: "request-test", ExpectedHostEpoch: srv.hostEpoch, Target: target, ExpectedRuntimeEpoch: "runtime_test"}

	changes, err := srv.workspaceChanges(protocol.WorkspaceChangesParams{RuntimeQuery: query})
	if err != nil {
		t.Fatal(err)
	}
	if !changes.GitAvailable || changes.GitBranch == "" || len(changes.Files) != 2 {
		t.Fatalf("changes = %+v", changes)
	}
	var trackedChange *protocol.ChangedFile
	for i := range changes.Files {
		if changes.Files[i].Path == "tracked.txt" {
			trackedChange = &changes.Files[i]
		}
	}
	if trackedChange == nil || len(trackedChange.Sources) != 2 || trackedChange.LatestPrompt != "edit it" {
		t.Fatalf("tracked change = %+v", trackedChange)
	}
	trackedDetail, err := srv.workspaceChangeDetail(protocol.WorkspaceChangeDetailParams{RuntimeQuery: query, Path: "tracked.txt"})
	if err != nil || trackedDetail.Source == nil || *trackedDetail.Source != protocol.ChangeGit || trackedDetail.Diff == nil ||
		!strings.Contains(*trackedDetail.Diff, "before") || !strings.Contains(*trackedDetail.Diff, "after") ||
		trackedDetail.Added != 1 || trackedDetail.Removed != 1 {
		t.Fatalf("tracked detail = %+v err=%v", trackedDetail, err)
	}
	untrackedDetail, err := srv.workspaceChangeDetail(protocol.WorkspaceChangeDetailParams{RuntimeQuery: query, Path: "untracked.txt"})
	if err != nil || untrackedDetail.Source == nil || *untrackedDetail.Source != protocol.ChangeGit ||
		untrackedDetail.Diff == nil || !strings.Contains(*untrackedDetail.Diff, "+new") {
		t.Fatalf("untracked detail = %+v err=%v", untrackedDetail, err)
	}
	if _, err := srv.workspaceChangeDetail(protocol.WorkspaceChangeDetailParams{RuntimeQuery: query, Path: "../outside.txt"}); err == nil {
		t.Fatal("workspace change detail accepted an escaping path")
	}

	history, err := srv.gitHistory(protocol.GitHistoryParams{RuntimeQuery: query})
	if err != nil || len(history.Commits) != 1 || history.Commits[0].Hash != hash {
		t.Fatalf("history = %+v err=%v", history, err)
	}
	if err := history.Validate(); err != nil {
		t.Fatalf("history validation: %v", err)
	}
	patch, err := srv.gitCommitDetail(protocol.GitCommitDetailParams{RuntimeQuery: query, Hash: hash, Path: "tracked.txt"})
	if err != nil || patch.Body == nil || !strings.Contains(*patch.Body, "before") {
		t.Fatalf("patch = %+v err=%v", patch, err)
	}
	files, err := srv.gitCommitDetail(protocol.GitCommitDetailParams{RuntimeQuery: query, Hash: hash})
	if err != nil || files.Files == nil || len(*files.Files) != 1 || (*files.Files)[0].Path != "tracked.txt" {
		t.Fatalf("files = %+v err=%v", files, err)
	}

	srv.mu.Lock()
	snapshot := srv.snapshotLocked(srv.sessions[target.SessionID], 20)
	srv.mu.Unlock()
	if len(snapshot.Checkpoints) != 1 || !snapshot.Checkpoints[0].CanCode || !snapshot.Checkpoints[0].CanConversation {
		t.Fatalf("checkpoints = %+v", snapshot.Checkpoints)
	}
	if len(snapshot.Todos) != 1 || snapshot.Todos[0].Status != protocol.TodoInProgress {
		t.Fatalf("todos = %+v", snapshot.Todos)
	}
	goal, err := srv.setGoal(protocol.SessionGoalSetParams{SessionMutation: mutation, Goal: "ship it"})
	if err != nil || goal.Goal != "ship it" || goal.Status != protocol.GoalRunning {
		t.Fatalf("goal = %+v err=%v", goal, err)
	}
	rewound, err := srv.rewindSession(protocol.SessionRewindParams{SessionMutation: mutation, CheckpointID: "checkpoint_1", Scope: protocol.RewindConversation})
	if err != nil || !rewound.ConversationRewritten || ctrl.rewoundTurn != 1 || ctrl.rewound != control.RewindConversation {
		t.Fatalf("rewind = %+v err=%v controller=(%d,%d)", rewound, err, ctrl.rewoundTurn, ctrl.rewound)
	}
	srv.mu.Lock()
	snapshot = srv.snapshotLocked(srv.sessions[target.SessionID], 20)
	srv.mu.Unlock()
	if snapshot.Meta.Goal == nil || *snapshot.Meta.Goal != "ship it" || snapshot.Meta.GoalStatus != protocol.GoalRunning {
		t.Fatalf("snapshot goal = %+v/%q", snapshot.Meta.Goal, snapshot.Meta.GoalStatus)
	}
}

func TestRuntimeGitEmptyCollectionsEncodeAsArrays(t *testing.T) {
	workspace := t.TempDir()
	runGit := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", workspace}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	runGit("init")
	runGit("config", "user.email", "remote-test@example.com")
	runGit("config", "user.name", "Remote Test")
	runGit("commit", "--allow-empty", "-m", "empty")
	hash := runGit("rev-parse", "HEAD")
	srv := New(Options{Workspace: workspace, Version: "test"})
	target := srv.installTestSession(&fakeController{model: "local/test"})
	query := protocol.RuntimeQuery{ExpectedHostEpoch: srv.hostEpoch, Target: target, ExpectedRuntimeEpoch: "runtime_test"}

	changes, err := srv.workspaceChanges(protocol.WorkspaceChangesParams{RuntimeQuery: query})
	if err != nil {
		t.Fatal(err)
	}
	if changes.Files == nil || len(changes.Files) != 0 {
		t.Fatalf("clean workspace files = %#v, want allocated empty slice", changes.Files)
	}
	encodedChanges, err := json.Marshal(changes)
	if err != nil || !bytes.Contains(encodedChanges, []byte(`"files":[]`)) {
		t.Fatalf("encoded clean workspace changes = %s err=%v", encodedChanges, err)
	}

	detail, err := srv.gitCommitDetail(protocol.GitCommitDetailParams{RuntimeQuery: query, Hash: hash})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Files == nil || *detail.Files == nil || len(*detail.Files) != 0 {
		t.Fatalf("empty commit files = %#v, want pointer to allocated empty slice", detail.Files)
	}
	encodedDetail, err := json.Marshal(detail)
	if err != nil || !bytes.Contains(encodedDetail, []byte(`"files":[]`)) {
		t.Fatalf("encoded empty commit detail = %s err=%v", encodedDetail, err)
	}
}

func TestWorkspaceCatalogEmptyEffortLevelsEncodeAsArray(t *testing.T) {
	srv := New(Options{Workspace: t.TempDir(), Version: "test"})
	hostSide, desktopSide := net.Pipe()
	hostWire := rpcwire.NewConn(hostSide, hostSide, rpcwire.Options{
		Name: "workspace-catalog-host", StrictJSONRPC: true,
		MaxInboundBytes: protocol.FrameBytes, MaxOutboundBytes: protocol.FrameBytes,
	})
	desktopWire := rpcwire.NewConn(desktopSide, desktopSide, rpcwire.Options{
		Name: "workspace-catalog-desktop", StrictJSONRPC: true,
		MaxInboundBytes: protocol.FrameBytes, MaxOutboundBytes: protocol.FrameBytes,
	})
	desktopBroker, err := remotebroker.Attach(desktopWire, remotebroker.Options{
		Catalog: func(context.Context, map[string]struct{}) ([]protocol.BrokerProviderDescriptor, error) {
			return []protocol.BrokerProviderDescriptor{
				{Ref: "local/basic", DisplayName: "Local", Model: "basic"},
				{
					Ref: "local/reasoning", DisplayName: "Local", Model: "reasoning",
					SupportedEfforts: []string{"medium", "high"}, DefaultEffort: "high",
				},
			}, nil
		},
		Open: func(context.Context, string, string, provider.Request) (<-chan provider.Chunk, error) {
			return nil, errors.New("unexpected provider open")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := desktopBroker.Activate(); err != nil {
		t.Fatal(err)
	}
	if err := srv.broker.Attach(hostWire, 1); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(func() {
		cancel()
		desktopBroker.Close()
		_ = hostSide.Close()
		_ = desktopSide.Close()
	})
	go desktopWire.Serve(ctx)
	go hostWire.Serve(ctx)

	result, err := srv.workspaceCatalog(protocol.WorkspaceCatalogParams{
		ExpectedHostEpoch: srv.hostEpoch,
		WorkspaceID:       srv.workspaceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Models) != 2 {
		t.Fatalf("models = %#v, want two catalog entries", result.Models)
	}
	if levels := result.Models[0].Effort.Levels; levels == nil || len(levels) != 0 {
		t.Fatalf("basic effort levels = %#v, want allocated empty slice", levels)
	}
	if levels := result.Models[1].Effort.Levels; len(levels) != 2 || levels[0] != "medium" || levels[1] != "high" {
		t.Fatalf("reasoning effort levels = %#v, want [medium high]", levels)
	}

	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"levels":[]`)) {
		t.Fatalf("encoded workspace catalog = %s, want empty effort levels array", raw)
	}
	decoded, err := protocol.DecodeResult(protocol.MethodCatalogWorkspace, raw)
	if err != nil {
		t.Fatalf("decode workspace catalog: %v", err)
	}
	decodedCatalog := decoded.(protocol.WorkspaceCatalogResult)
	if levels := decodedCatalog.Models[0].Effort.Levels; levels == nil || len(levels) != 0 {
		t.Fatalf("decoded basic effort levels = %#v, want allocated empty slice", levels)
	}
}

func TestRuntimeFilePreviewDoesNotDecodeBinary(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "blob.bin"), []byte{'a', 0, 'b'}, 0o600); err != nil {
		t.Fatal(err)
	}
	srv := New(Options{Workspace: workspace, Version: "test"})
	target := srv.installTestSession(&fakeController{model: "local/stub"})
	result, err := srv.filePreview(protocol.FilePreviewParams{
		RuntimeQuery: protocol.RuntimeQuery{ExpectedHostEpoch: srv.hostEpoch, Target: target, ExpectedRuntimeEpoch: "runtime_test"},
		Path:         "blob.bin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != protocol.FileBinary || !result.Binary || result.Body != nil || result.ReturnedBytes != 0 {
		t.Fatalf("preview = %+v", result)
	}
	if err := result.Validate(); err != nil {
		t.Fatal(err)
	}
}

func (s *Server) installTestSession(ctrl SessionController) protocol.RuntimeTarget {
	id := protocol.SessionID("session_test")
	s.mu.Lock()
	s.sessions[id] = &session{
		id: id, ctrl: ctrl, model: ctrl.ModelRef(), effort: "high",
		collaboration: protocol.CollaborationNormal, tokenMode: protocol.TokenFull,
		toolApproval: protocol.ToolApprovalAsk, topicID: "topic_test", title: "Test",
		runtimeEpoch: "runtime_test", createdAt: time.Now().UnixMilli(), updatedAt: time.Now().UnixMilli(),
	}
	s.mu.Unlock()
	return s.target(id)
}

func committedTestHandlers(s *Server, generation uint64) protocol.HandlerSet {
	gate := newConnectionGate()
	gate.resolve(true)
	return s.handlers(generation, nil, gate)
}

func TestRuntimeProbeAndRejectedInitializePreserveCommittedConnection(t *testing.T) {
	workspace := t.TempDir()
	srv := New(Options{Workspace: workspace, Version: "test", SourceRevision: strings.Repeat("a", 40)})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	open := func(name string) (*rpcwire.Conn, net.Conn) {
		hostSide, desktopSide := net.Pipe()
		go srv.serveConn(ctx, hostSide)
		wire := rpcwire.NewConn(desktopSide, desktopSide, rpcwire.Options{
			Name: name, StrictJSONRPC: true,
			MaxInboundBytes: protocol.FrameBytes, MaxOutboundBytes: protocol.FrameBytes,
			MaxQueuedNotifications: protocol.RPCQueuedNotifications,
		})
		go func() { _ = wire.Serve(ctx) }()
		return wire, desktopSide
	}
	waitForGeneration := func(want uint64) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for {
			srv.mu.Lock()
			got := srv.nextGen
			srv.mu.Unlock()
			if got >= want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("next connection generation = %d, want at least %d", got, want)
			}
			time.Sleep(time.Millisecond)
		}
	}
	assertActivePing := func(wire *rpcwire.Conn, lease protocol.LeaseInfo) {
		t.Helper()
		if _, err := wire.Request(ctx, string(protocol.MethodRemotePing), protocol.PingParams{LeaseID: lease.LeaseID}); err != nil {
			t.Fatalf("committed connection ping: %v", err)
		}
		srv.mu.Lock()
		attached, generation := srv.attached, srv.gen
		srv.mu.Unlock()
		if !attached || generation != 1 {
			t.Fatalf("committed runtime attached=%v generation=%d, want true/1", attached, generation)
		}
	}

	activeWire, activeStream := open("active-desktop")
	defer activeStream.Close()
	raw, err := activeWire.Request(ctx, string(protocol.MethodRemoteInitialize), protocol.InitializeParams{
		BuildID: srv.buildID, ClientInstanceID: "desktop-active", Workspace: workspace,
	})
	if err != nil {
		t.Fatalf("initialize active connection: %v", err)
	}
	var initialized protocol.InitializeResult
	if err := json.Unmarshal(raw, &initialized); err != nil {
		t.Fatal(err)
	}
	assertActivePing(activeWire, initialized.Lease)

	probeHost, probeDesktop := net.Pipe()
	go srv.serveConn(ctx, probeHost)
	_ = probeDesktop.Close()
	waitForGeneration(2)
	assertActivePing(activeWire, initialized.Lease)

	rejectedWire, rejectedStream := open("rejected-desktop")
	badBuild := srv.buildID
	badBuild.ProductVersion += "-mismatch"
	_, err = rejectedWire.Request(ctx, string(protocol.MethodRemoteInitialize), protocol.InitializeParams{
		BuildID: badBuild, ClientInstanceID: "desktop-rejected", Workspace: workspace,
	})
	if err == nil {
		t.Fatal("mismatched candidate initialize succeeded")
	}
	var responseErr *rpcwire.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("initialize error = %T %v", err, err)
	}
	var errorData protocol.RemoteErrorData
	if json.Unmarshal(responseErr.Data, &errorData) != nil || errorData.ReasonixCode != protocol.ErrDaemonRestartRequired {
		t.Fatalf("initialize error data = %s", responseErr.Data)
	}
	_ = rejectedStream.Close()
	waitForGeneration(3)
	assertActivePing(activeWire, initialized.Lease)
}

func TestRuntimeControllerUsesDesktopBrokerWithoutHostKey(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	const toolResultMarker = "remote-broker-tool-result"
	if err := os.WriteFile(filepath.Join(workspace, "broker-proof.txt"), []byte(toolResultMarker+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("REASONIX_HOME", filepath.Join(home, ".reasonix"))
	t.Setenv("REASONIX_SAFE_MODE", "1")
	t.Setenv("DEEPSEEK_API_KEY", "")

	revision := strings.Repeat("b", 40)
	buildID, err := protocol.NewBuildID("test", revision)
	if err != nil {
		t.Fatal(err)
	}
	srv := New(Options{Workspace: workspace, Version: "test", SourceRevision: revision})
	t.Cleanup(srv.snapshotAndClose)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	hostSide, desktopSide := net.Pipe()
	defer desktopSide.Close()
	go srv.serveConn(ctx, hostSide)

	desktopWire := rpcwire.NewConn(desktopSide, desktopSide, rpcwire.Options{
		Name: "desktop-e2e", StrictJSONRPC: true,
		MaxInboundBytes: protocol.FrameBytes, MaxOutboundBytes: protocol.FrameBytes,
	})
	stub := &brokerStubProvider{requests: make(chan provider.Request, 2)}
	desktopBroker, err := remotebroker.Attach(desktopWire, remotebroker.Options{
		Catalog: func(context.Context, map[string]struct{}) ([]protocol.BrokerProviderDescriptor, error) {
			return []protocol.BrokerProviderDescriptor{
				remotebroker.DescriptorFromProvider("local/stub", "Local stub", "stub", stub, []string{"high"}, "high", false, 128_000, nil),
			}, nil
		},
		Open: func(ctx context.Context, ref, _ string, request provider.Request) (<-chan provider.Chunk, error) {
			if ref != "local/stub" {
				return nil, fmt.Errorf("unexpected provider ref %q", ref)
			}
			return stub.Stream(ctx, request)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer desktopBroker.Close()
	events := make(chan protocol.SessionEvent, 32)
	desktopWire.HandleNotify(string(protocol.MethodSessionEvent), func(_ context.Context, raw json.RawMessage) {
		decoded, decodeErr := protocol.DecodeNotificationParams(protocol.MethodSessionEvent, raw)
		if decodeErr != nil {
			t.Errorf("session event: %v", decodeErr)
			return
		}
		events <- decoded.(protocol.SessionEvent)
	})
	go desktopWire.Serve(ctx)

	request := func(method protocol.Method, params any) any {
		raw, requestErr := desktopWire.Request(ctx, string(method), params)
		if requestErr != nil {
			t.Fatalf("%s: %v", method, requestErr)
		}
		decoded, decodeErr := protocol.DecodeResult(method, raw)
		if decodeErr != nil {
			t.Fatalf("%s result: %v", method, decodeErr)
		}
		return decoded
	}
	initialized := request(protocol.MethodRemoteInitialize, protocol.InitializeParams{BuildID: buildID, ClientInstanceID: "desktop-e2e", Workspace: workspace}).(protocol.InitializeResult)
	if err := protocol.CompareBuildID(buildID, initialized.BuildID); err != nil {
		t.Fatal(err)
	}
	if err := desktopBroker.Activate(); err != nil {
		t.Fatal(err)
	}
	workspaces := request(protocol.MethodWorkspaceList, protocol.WorkspaceListParams{ExpectedHostEpoch: initialized.HostEpoch}).(protocol.WorkspaceListResult)
	model := "local/stub"
	created := request(protocol.MethodSessionCreate, protocol.SessionCreateParams{
		HostMutation: protocol.HostMutation{RequestID: "request-create", ExpectedHostEpoch: initialized.HostEpoch},
		WorkspaceID:  workspaces.Items[0].WorkspaceID, AdditionalDirectoryRefs: []protocol.DirectoryRef{},
		Topic: protocol.TopicSelection{Kind: protocol.TopicNew}, Profile: protocol.ProfileSelection{Model: &model},
	}).(protocol.SessionCreateResult)
	subscribed := request(protocol.MethodSessionSubscribe, protocol.SessionSubscribeParams{
		ExpectedHostEpoch: initialized.HostEpoch, Target: created.Target, PageTurns: 20,
	}).(protocol.SessionSubscribeResult)
	request(protocol.MethodSessionSubmit, protocol.SessionSubmitParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "request-submit", ExpectedHostEpoch: initialized.HostEpoch,
			Target: created.Target, ExpectedRuntimeEpoch: created.RuntimeEpoch,
		},
		Input: "say hello", DisplayText: "say hello",
	})

	select {
	case providerRequest := <-stub.requests:
		if len(providerRequest.Messages) == 0 || providerRequest.Messages[len(providerRequest.Messages)-1].Content != "say hello" {
			t.Fatalf("broker provider request lost user prompt: %+v", providerRequest.Messages)
		}
	case <-ctx.Done():
		t.Fatal("Desktop provider was not called for the initial turn")
	}
	select {
	case providerRequest := <-stub.requests:
		var sawReasoningToolCall, sawHostToolResult bool
		for _, message := range providerRequest.Messages {
			if message.Role == provider.RoleAssistant &&
				strings.Contains(message.ReasoningContent, "inspect the remote workspace") &&
				len(message.ToolCalls) == 1 && message.ToolCalls[0].ID == "remote-read-1" &&
				message.ToolCalls[0].Name == "read_file" {
				sawReasoningToolCall = true
			}
			if message.Role == provider.RoleTool && message.ToolCallID == "remote-read-1" &&
				message.Name == "read_file" && strings.Contains(message.Content, toolResultMarker) {
				sawHostToolResult = true
			}
		}
		if !sawReasoningToolCall || !sawHostToolResult {
			t.Fatalf("second Broker request lost DeepSeek reasoning/tool replay or Host tool result: %+v", providerRequest.Messages)
		}
	case <-ctx.Done():
		t.Fatal("Desktop provider was not called after the Host tool result")
	}
	seenToolDispatch, seenToolResult, seenText, seenDone := false, false, false, false
	for !seenDone {
		select {
		case event := <-events:
			if event.SubscriptionID != subscribed.SubscriptionID {
				t.Fatalf("event subscription = %q", event.SubscriptionID)
			}
			seenToolDispatch = seenToolDispatch || (event.Event.Kind == "tool_dispatch" && event.Event.Tool.Name == "read_file")
			seenToolResult = seenToolResult || (event.Event.Kind == "tool_result" && event.Event.Tool.Name == "read_file" && strings.Contains(event.Event.Tool.Output, toolResultMarker))
			seenText = seenText || (event.Event.Kind == "text" && strings.Contains(event.Event.Text, "hello from desktop after the Host tool result"))
			seenDone = event.Event.Kind == "turn_done"
		case <-ctx.Done():
			t.Fatal("timed out waiting for Broker-backed turn events")
		}
	}
	if !seenToolDispatch || !seenToolResult || !seenText {
		t.Fatalf("Broker-backed tool loop events incomplete: dispatch=%v result=%v text=%v", seenToolDispatch, seenToolResult, seenText)
	}
}

func TestRuntimeBuildIDUsesSharedBuildIdentity(t *testing.T) {
	want := protocol.CurrentBuildID("shared-build")
	if got := currentBuildID(Options{Version: "shared-build"}); got != want {
		t.Fatalf("runtime build ID = %+v, want shared protocol build ID %+v", got, want)
	}
}

func TestSessionMirrorArtifactIsValidNonTruncatedReference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("{\"role\":\"user\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := New(Options{Workspace: t.TempDir(), Version: "test"})
	sess := &session{ctrl: &persistentFakeController{
		fakeController: &fakeController{model: "local/test"}, sessionPath: path,
	}}

	mirror, artifacts := srv.sessionMirrorArtifactLocked(sess)
	if len(artifacts) != 1 {
		t.Fatalf("mirror artifacts = %+v, want one reference", artifacts)
	}
	if mirror.SessionJSONL == nil || !strings.Contains(*mirror.SessionJSONL, `"role":"user"`) {
		t.Fatalf("mirror placeholder did not carry the source body before externalization: %+v", mirror)
	}
	artifact := artifacts[0]
	if artifact.Truncated || artifact.OriginalBytes != nil || artifact.TruncationReason != "" {
		t.Fatalf("complete mirror was marked as truncated: %+v", artifact)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("complete mirror reference is invalid: %v", err)
	}
	snapshot := protocol.SessionSnapshot{Mirror: mirror, Externalized: artifacts}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("snapshot rejected the schema-marked mirror pointer: %v", err)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal externalized mirror snapshot: %v", err)
	}
	if strings.Contains(string(raw), `"role":"user"`) || !strings.Contains(string(raw), `"session.jsonl":null`) {
		t.Fatalf("mirror body was not replaced by a null contentRef placeholder: %s", raw)
	}
}
