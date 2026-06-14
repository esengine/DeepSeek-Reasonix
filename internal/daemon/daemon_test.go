package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type daemonScriptedProvider struct {
	turns [][]provider.Chunk
	calls int
}

func (p *daemonScriptedProvider) Name() string { return "daemon-test" }

func (p *daemonScriptedProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	ch := make(chan provider.Chunk, 8)
	turn := []provider.Chunk{{Type: provider.ChunkText, Text: "done\n\n[goal:complete]"}}
	if len(p.turns) > 0 {
		turn = p.turns[0]
		p.turns = p.turns[1:]
	}
	go func() {
		defer close(ch)
		for _, c := range turn {
			select {
			case <-ctx.Done():
				return
			case ch <- c:
			}
		}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return ch, nil
}

type daemonBlockingProvider struct {
	started chan struct{}
	release chan struct{}
	calls   int
}

func (p *daemonBlockingProvider) Name() string { return "daemon-blocking-test" }

func (p *daemonBlockingProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.calls++
	if p.calls == 1 {
		close(p.started)
	}
	ch := make(chan provider.Chunk, 4)
	go func() {
		defer close(ch)
		select {
		case <-ctx.Done():
			return
		case <-p.release:
		}
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done\n\n[goal:complete]"}
		ch <- provider.Chunk{Type: provider.ChunkDone}
	}()
	return ch, nil
}

func TestDaemonStartAndStatus(t *testing.T) {
	dir := t.TempDir()

	d := New(Options{
		Addr:       "127.0.0.1:0",
		SessionDir: dir,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Test lock acquisition.
	if err := d.acquireLock(); err != nil {
		t.Fatalf("acquireLock: %v", err)
	}
	defer d.releaseLock()

	// Verify lockfile exists.
	if _, err := os.Stat(d.lockFile()); err != nil {
		t.Fatalf("lockfile not created: %v", err)
	}

	// Second acquire should fail.
	d2 := New(Options{SessionDir: dir})
	if err := d2.acquireLock(); err == nil {
		t.Fatal("expected second lock acquire to fail")
		d2.releaseLock()
	}

	_ = ctx
}

func TestDaemonStatusIncludesFileWatcherStats(t *testing.T) {
	d := New(Options{SessionDir: t.TempDir()})
	d.fileWatcher = NewFileWatcher(d, nil)
	d.fileWatcher.mu.Lock()
	d.fileWatcher.stats = FileWatcherStats{
		Mode:             "hybrid",
		NativeAvailable:  true,
		NativeWatchRoots: 2,
		LastPollDirs:     7,
		IgnoredChanges:   3,
	}
	d.fileWatcher.mu.Unlock()

	req := httptest.NewRequest("GET", "/status", nil)
	rr := httptest.NewRecorder()
	d.handleStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp StatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if resp.FileWatcher == nil || resp.FileWatcher.Mode != "hybrid" || !resp.FileWatcher.NativeAvailable ||
		resp.FileWatcher.NativeWatchRoots != 2 || resp.FileWatcher.LastPollDirs != 7 || resp.FileWatcher.IgnoredChanges != 3 {
		t.Fatalf("file watcher stats missing from status: %+v", resp.FileWatcher)
	}
}

func TestDaemonScanSessions(t *testing.T) {
	dir := t.TempDir()

	sess1 := filepath.Join(dir, "session1.jsonl")
	os.WriteFile(sess1, []byte(`{"role":"user"}`), 0o644)
	agent.SaveRuntimeMeta(sess1, agent.RuntimeMeta{
		SessionID: "session1",
		Goal:      agent.RuntimeGoalMeta{Text: "goal 1", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	})

	sess2 := filepath.Join(dir, "session2.jsonl")
	os.WriteFile(sess2, []byte(`{"role":"user"}`), 0o644)
	agent.SaveRuntimeMeta(sess2, agent.RuntimeMeta{
		SessionID: "session2",
		Goal:      agent.RuntimeGoalMeta{Text: "goal 2", Status: "blocked"},
		Run:       agent.RuntimeRunMeta{Status: "running"},
	})

	d := New(Options{SessionDir: dir})
	d.scanSessions()

	d.mu.RLock()
	defer d.mu.RUnlock()
	if len(d.registry) != 2 {
		t.Fatalf("registry has %d entries, want 2", len(d.registry))
	}
	if d.registry["session1"] == nil {
		t.Error("session1 not found in registry")
	}
	if d.registry["session2"] == nil {
		t.Error("session2 not found in registry")
	}
}

func TestDaemonRecoverInterrupted(t *testing.T) {
	dir := t.TempDir()

	sess := filepath.Join(dir, "crashed.jsonl")
	os.WriteFile(sess, []byte(`{"role":"user"}`), 0o644)
	agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "crashed",
		Goal:      agent.RuntimeGoalMeta{Text: "ship it", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "running"},
	})

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	d.recoverInterrupted()

	d.mu.RLock()
	entry := d.registry["crashed"]
	d.mu.RUnlock()

	if entry == nil {
		t.Fatal("entry not found")
	}
	if entry.Runtime.Run.Status != "interrupted" {
		t.Errorf("Run.Status = %q, want 'interrupted'", entry.Runtime.Run.Status)
	}

	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "interrupted" {
		t.Errorf("persisted Run.Status = %q, want 'interrupted'", loaded.Run.Status)
	}
	select {
	case intent := <-d.intentCh:
		t.Fatalf("recoverInterrupted should not auto-resume or enqueue intent: %+v", intent)
	default:
	}
}

func TestDaemonRecoverWaitingApprovalAsInterrupted(t *testing.T) {
	dir := t.TempDir()

	sess := filepath.Join(dir, "waiting.jsonl")
	os.WriteFile(sess, []byte(`{"role":"user"}`), 0o644)
	agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "waiting",
		Goal:      agent.RuntimeGoalMeta{Text: "ship it", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "waiting_approval"},
		Wait:      agent.RuntimeWaitMeta{Kind: "approval", ApprovalID: "7", Tool: "bash"},
	})

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	d.recoverInterrupted()

	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "interrupted" {
		t.Fatalf("Run.Status = %q, want interrupted", loaded.Run.Status)
	}
	if loaded.Wait.ApprovalID != "7" {
		t.Fatalf("wait metadata should be preserved, got %+v", loaded.Wait)
	}
	events, ok, err := agent.LoadRuntimeTimeline(sess, 1)
	if err != nil || !ok || len(events) != 1 {
		t.Fatalf("LoadRuntimeTimeline: events=%+v err=%v ok=%v", events, err, ok)
	}
	if events[0].Type != "run_interrupted" || events[0].WaitID != "7" {
		t.Fatalf("unexpected recovery timeline: %+v", events[0])
	}
}

func TestDaemonRecoverPreservesDaemonOwnedWaits(t *testing.T) {
	dir := t.TempDir()

	eventSess := filepath.Join(dir, "waiting-event.jsonl")
	os.WriteFile(eventSess, []byte(`{"role":"user"}`), 0o644)
	agent.SaveRuntimeMeta(eventSess, agent.RuntimeMeta{
		SessionID: "waiting-event",
		Goal:      agent.RuntimeGoalMeta{Text: "ship it", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "waiting_event"},
		Wait:      agent.RuntimeWaitMeta{Kind: "event", EventSource: "github.workflow_run"},
	})

	timeSess := filepath.Join(dir, "waiting-time.jsonl")
	os.WriteFile(timeSess, []byte(`{"role":"user"}`), 0o644)
	agent.SaveRuntimeMeta(timeSess, agent.RuntimeMeta{
		SessionID: "waiting-time",
		Goal:      agent.RuntimeGoalMeta{Text: "ship it", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "waiting_time"},
		Wait:      agent.RuntimeWaitMeta{Kind: "time", Until: time.Now().Add(time.Hour).UTC()},
	})

	fileSess := filepath.Join(dir, "waiting-file.jsonl")
	os.WriteFile(fileSess, []byte(`{"role":"user"}`), 0o644)
	agent.SaveRuntimeMeta(fileSess, agent.RuntimeMeta{
		SessionID: "waiting-file",
		Goal:      agent.RuntimeGoalMeta{Text: "ship it", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "waiting_file"},
		Wait:      agent.RuntimeWaitMeta{Kind: "file", FilePaths: []string{"src/a.go"}},
	})

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	d.recoverInterrupted()

	eventMeta, ok, err := agent.LoadRuntimeMeta(eventSess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta event: err=%v ok=%v", err, ok)
	}
	if eventMeta.Run.Status != "waiting_event" || eventMeta.Wait.Kind != "event" {
		t.Fatalf("event wait should be preserved: run=%+v wait=%+v", eventMeta.Run, eventMeta.Wait)
	}
	timeMeta, ok, err := agent.LoadRuntimeMeta(timeSess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta time: err=%v ok=%v", err, ok)
	}
	if timeMeta.Run.Status != "waiting_time" || timeMeta.Wait.Kind != "time" {
		t.Fatalf("time wait should be preserved: run=%+v wait=%+v", timeMeta.Run, timeMeta.Wait)
	}
	fileMeta, ok, err := agent.LoadRuntimeMeta(fileSess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta file: err=%v ok=%v", err, ok)
	}
	if fileMeta.Run.Status != "waiting_file" || fileMeta.Wait.Kind != "file" {
		t.Fatalf("file wait should be preserved: run=%+v wait=%+v", fileMeta.Run, fileMeta.Wait)
	}
}

func TestDaemonHTTPHandlers(t *testing.T) {
	dir := t.TempDir()

	sess := filepath.Join(dir, "api-test.jsonl")
	os.WriteFile(sess, []byte(`{"role":"user"}`), 0o644)
	agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "api-test",
		Goal:      agent.RuntimeGoalMeta{Text: "test goal", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	})

	d := New(Options{
		Addr:       "127.0.0.1:0",
		SessionDir: dir,
	})
	d.scanSessions()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", d.handleStatus)
	mux.HandleFunc("GET /sessions", d.handleSessions)
	mux.HandleFunc("POST /continue-goal", d.handleContinueGoal)
	mux.HandleFunc("POST /stop", d.handleStop)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()

	addr := ln.Addr().String()
	client := &http.Client{Timeout: 2 * time.Second}

	// GET /status
	resp, err := client.Get("http://" + addr + "/status")
	if err != nil {
		t.Fatalf("GET /status: %v", err)
	}
	var status StatusResponse
	json.NewDecoder(resp.Body).Decode(&status)
	resp.Body.Close()
	if status.Status != "running" {
		t.Errorf("status = %q, want running", status.Status)
	}
	if status.Sessions != 1 {
		t.Errorf("sessions = %d, want 1", status.Sessions)
	}

	// GET /sessions
	resp, err = client.Get("http://" + addr + "/sessions")
	if err != nil {
		t.Fatalf("GET /sessions: %v", err)
	}
	var sessions SessionsResponse
	json.NewDecoder(resp.Body).Decode(&sessions)
	resp.Body.Close()
	if len(sessions.Sessions) != 1 {
		t.Fatalf("sessions count = %d, want 1", len(sessions.Sessions))
	}
	if sessions.Sessions[0].GoalText != "test goal" {
		t.Errorf("goal text = %q", sessions.Sessions[0].GoalText)
	}

	// POST /stop
	resp, err = client.Post("http://"+addr+"/stop", "application/json",
		strings.NewReader(`{"session_id":"api-test"}`))
	if err != nil {
		t.Fatalf("POST /stop: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("stop status = %d", resp.StatusCode)
	}

	// Verify the session was stopped.
	d.mu.RLock()
	entry := d.registry["api-test"]
	d.mu.RUnlock()
	if entry.Runtime.Run.Status != "stopped" {
		t.Errorf("after stop: Run.Status = %q", entry.Runtime.Run.Status)
	}
}

func TestDaemonSessionsOverviewAndFilters(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll root: %v", err)
	}
	next := time.Date(2026, 6, 14, 9, 30, 0, 0, time.UTC)

	projectSess := filepath.Join(dir, "project-waiting.jsonl")
	if err := os.WriteFile(projectSess, []byte(`{"role":"user","content":"project"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile project: %v", err)
	}
	if err := agent.SaveRuntimeMeta(projectSess, agent.RuntimeMeta{
		SessionID:     "project-waiting",
		WorkspaceRoot: root,
		Goal:          agent.RuntimeGoalMeta{Text: "finish project triage", Status: control.GoalStatusRunning},
		Run:           agent.RuntimeRunMeta{Status: agent.RunStatusWaitingEvent},
		Wait:          agent.RuntimeWaitMeta{Kind: "event", Reason: "ci", EventID: "event-1"},
		Scheduler:     agent.RuntimeSchedMeta{Enabled: true, NextWakeupAt: next},
		FileWatch:     agent.RuntimeWatchMeta{Enabled: true, Paths: []string{"CHANGELOG.md"}},
		Budget: agent.RuntimeBudgetMeta{
			DailyWakeupLimit:    3,
			DailyWakeups:        1,
			MaxGoalAutoTurns:    4,
			DailyModelCallLimit: 5,
			DailyModelCalls:     2,
			DailyModelCostLimit: 1.5,
			DailyModelCost:      0.25,
			ModelCostCurrency:   "$",
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta project: %v", err)
	}
	if err := agent.SaveBranchMeta(projectSess, agent.BranchMeta{
		Scope:         "project",
		WorkspaceRoot: root,
		TopicID:       "topic-project",
		TopicTitle:    "Project topic",
	}); err != nil {
		t.Fatalf("SaveBranchMeta project: %v", err)
	}

	globalSess := filepath.Join(dir, "global-id.jsonl")
	if err := os.WriteFile(globalSess, []byte(`{"role":"user","content":"global"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile global: %v", err)
	}
	if err := agent.SaveRuntimeMeta(globalSess, agent.RuntimeMeta{
		SessionID: "global-id",
		Goal:      agent.RuntimeGoalMeta{Text: "global recap", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: agent.RunStatusIdle},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta global: %v", err)
	}
	if err := agent.SaveBranchMeta(globalSess, agent.BranchMeta{Scope: "global"}); err != nil {
		t.Fatalf("SaveBranchMeta global: %v", err)
	}

	blockedSess := filepath.Join(dir, "blocked-id.jsonl")
	if err := os.WriteFile(blockedSess, []byte(`{"role":"user","content":"blocked"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile blocked: %v", err)
	}
	if err := agent.SaveRuntimeMeta(blockedSess, agent.RuntimeMeta{
		SessionID: "blocked-id",
		Goal:      agent.RuntimeGoalMeta{Text: "blocked goal", Status: control.GoalStatusBlocked},
		Run:       agent.RuntimeRunMeta{Status: agent.RunStatusBlocked},
		Budget:    agent.RuntimeBudgetMeta{LastBlockedReason: "daily wakeup budget exhausted"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta blocked: %v", err)
	}
	if err := agent.SaveBranchMeta(blockedSess, agent.BranchMeta{Scope: "global"}); err != nil {
		t.Fatalf("SaveBranchMeta blocked: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()

	req := httptest.NewRequest("GET", "/sessions?scope=project&status=waiting", nil)
	rr := httptest.NewRecorder()
	d.handleSessions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("sessions status = %d body=%s", rr.Code, rr.Body.String())
	}
	var projectResp SessionsResponse
	if err := json.NewDecoder(rr.Body).Decode(&projectResp); err != nil {
		t.Fatalf("decode project response: %v", err)
	}
	if len(projectResp.Sessions) != 1 {
		t.Fatalf("project waiting sessions = %+v, want 1", projectResp.Sessions)
	}
	view := projectResp.Sessions[0]
	if view.ID != "project-waiting" || view.Scope != "project" || view.WorkspaceRoot != root || view.TopicTitle != "Project topic" {
		t.Fatalf("unexpected project view: %+v", view)
	}
	if view.WaitKind != "event" || view.WaitID != "event-1" || view.NextWakeupAt == nil || !view.NextWakeupAt.Equal(next) {
		t.Fatalf("project wait/next fields missing: %+v", view)
	}
	if !view.Scheduled || !view.Watched || view.DailyWakeupLimit != 3 || view.DailyWakeups != 1 ||
		view.MaxGoalAutoTurns != 4 || view.DailyModelCallLimit != 5 || view.DailyModelCalls != 2 ||
		view.DailyModelCostLimit != 1.5 || view.DailyModelCost != 0.25 || view.ModelCostCurrency != "$" {
		t.Fatalf("project overview fields missing: %+v", view)
	}

	req = httptest.NewRequest("GET", "/sessions?scope=global", nil)
	rr = httptest.NewRecorder()
	d.handleSessions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("global sessions status = %d body=%s", rr.Code, rr.Body.String())
	}
	var globalResp SessionsResponse
	if err := json.NewDecoder(rr.Body).Decode(&globalResp); err != nil {
		t.Fatalf("decode global response: %v", err)
	}
	if len(globalResp.Sessions) != 2 {
		t.Fatalf("global sessions = %+v, want 2", globalResp.Sessions)
	}

	req = httptest.NewRequest("GET", "/sessions?status=blocked", nil)
	rr = httptest.NewRecorder()
	d.handleSessions(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("blocked sessions status = %d body=%s", rr.Code, rr.Body.String())
	}
	var blockedResp SessionsResponse
	if err := json.NewDecoder(rr.Body).Decode(&blockedResp); err != nil {
		t.Fatalf("decode blocked response: %v", err)
	}
	if len(blockedResp.Sessions) != 1 || blockedResp.Sessions[0].ID != "blocked-id" || blockedResp.Sessions[0].BudgetBlockedReason == "" {
		t.Fatalf("blocked sessions = %+v, want blocked-id with reason", blockedResp.Sessions)
	}
}

func TestDaemonTimelineHandler(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "timeline-api.jsonl")
	os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644)
	agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "timeline-api",
		Goal:      agent.RuntimeGoalMeta{Text: "timeline goal", Status: "running"},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	})
	if err := agent.AppendRuntimeTimeline(sess, agent.RuntimeTimelineEvent{Type: "intent_queued", Source: "test"}); err != nil {
		t.Fatalf("AppendRuntimeTimeline: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	req := httptest.NewRequest("GET", "/timeline?session_id=timeline-api&limit=10", nil)
	rr := httptest.NewRecorder()
	d.handleTimeline(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("timeline status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp TimelineResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	if resp.SessionID != "timeline-api" || len(resp.Events) != 1 || resp.Events[0].Type != "intent_queued" {
		t.Fatalf("unexpected timeline response: %+v", resp)
	}
}

func TestDaemonScheduleHandlerPersistsTimezone(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "schedule-api.jsonl")
	if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "schedule-api",
		Goal:      agent.RuntimeGoalMeta{Text: "daily check", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	d.scheduler = NewScheduler(d, nil)
	req := httptest.NewRequest("POST", "/schedule", strings.NewReader(`{"session_id":"schedule-api","daily_at":"09:00","timezone":"Asia/Shanghai","enabled":true}`))
	rr := httptest.NewRecorder()
	d.handleSchedule(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("schedule status = %d body=%s", rr.Code, rr.Body.String())
	}

	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Scheduler.DailyAt != "09:00" || loaded.Scheduler.Timezone != "Asia/Shanghai" || !loaded.Scheduler.Enabled {
		t.Fatalf("schedule config not persisted: %+v", loaded.Scheduler)
	}
	if loaded.Scheduler.NextWakeupAt.IsZero() {
		t.Fatalf("NextWakeupAt not computed: %+v", loaded.Scheduler)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["timezone"] != "Asia/Shanghai" {
		t.Fatalf("response timezone = %v, want Asia/Shanghai", resp["timezone"])
	}
}

func TestDaemonScheduleHandlerAppliesProjectAndGlobalScopes(t *testing.T) {
	dir := t.TempDir()
	rootA := filepath.Join(dir, "workspace-a")
	rootB := filepath.Join(dir, "workspace-b")
	if err := os.MkdirAll(rootA, 0o755); err != nil {
		t.Fatalf("MkdirAll rootA: %v", err)
	}
	if err := os.MkdirAll(rootB, 0o755); err != nil {
		t.Fatalf("MkdirAll rootB: %v", err)
	}

	globalSess := filepath.Join(dir, "global-api.jsonl")
	projectASess := filepath.Join(dir, "project-a.jsonl")
	projectBSess := filepath.Join(dir, "project-b.jsonl")
	for _, sess := range []string{globalSess, projectASess, projectBSess} {
		if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
			t.Fatalf("WriteFile %s: %v", sess, err)
		}
	}
	if err := agent.SaveRuntimeMeta(globalSess, agent.RuntimeMeta{
		SessionID: "global-api",
		Goal:      agent.RuntimeGoalMeta{Text: "global goal", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta global: %v", err)
	}
	if err := agent.SaveBranchMeta(globalSess, agent.BranchMeta{Scope: "global"}); err != nil {
		t.Fatalf("SaveBranchMeta global: %v", err)
	}
	if err := agent.SaveRuntimeMeta(projectASess, agent.RuntimeMeta{
		SessionID:     "project-a",
		WorkspaceRoot: rootA,
		Goal:          agent.RuntimeGoalMeta{Text: "project goal", Status: control.GoalStatusRunning},
		Run:           agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta projectA: %v", err)
	}
	if err := agent.SaveBranchMeta(projectASess, agent.BranchMeta{Scope: "project", WorkspaceRoot: rootA}); err != nil {
		t.Fatalf("SaveBranchMeta projectA: %v", err)
	}
	if err := agent.SaveRuntimeMeta(projectBSess, agent.RuntimeMeta{
		SessionID:     "project-b",
		WorkspaceRoot: rootB,
		Goal:          agent.RuntimeGoalMeta{Text: "project goal", Status: control.GoalStatusRunning},
		Run:           agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta projectB: %v", err)
	}
	if err := agent.SaveBranchMeta(projectBSess, agent.BranchMeta{Scope: "project", WorkspaceRoot: rootB}); err != nil {
		t.Fatalf("SaveBranchMeta projectB: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	d.scheduler = NewScheduler(d, nil)

	req := httptest.NewRequest("POST", "/schedule", strings.NewReader(`{"scope":"project","workspace_root":`+strconv.Quote(rootA)+`,"daily_at":"08:30","timezone":"Asia/Shanghai","enabled":true}`))
	rr := httptest.NewRecorder()
	d.handleSchedule(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("project schedule status = %d body=%s", rr.Code, rr.Body.String())
	}
	var projectResp struct {
		Updated    int      `json:"updated"`
		SessionIDs []string `json:"session_ids"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&projectResp); err != nil {
		t.Fatalf("decode project response: %v", err)
	}
	if projectResp.Updated != 1 || len(projectResp.SessionIDs) != 1 || projectResp.SessionIDs[0] != "project-a" {
		t.Fatalf("unexpected project schedule response: %+v", projectResp)
	}

	projectA, ok, err := agent.LoadRuntimeMeta(projectASess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta projectA: err=%v ok=%v", err, ok)
	}
	projectB, ok, err := agent.LoadRuntimeMeta(projectBSess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta projectB: err=%v ok=%v", err, ok)
	}
	global, ok, err := agent.LoadRuntimeMeta(globalSess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta global: err=%v ok=%v", err, ok)
	}
	if projectA.Scheduler.DailyAt != "08:30" || projectA.Scheduler.Timezone != "Asia/Shanghai" || !projectA.Scheduler.Enabled {
		t.Fatalf("projectA schedule not applied: %+v", projectA.Scheduler)
	}
	if projectB.Scheduler.Enabled || projectB.Scheduler.DailyAt != "" {
		t.Fatalf("projectB schedule should be untouched: %+v", projectB.Scheduler)
	}
	if global.Scheduler.Enabled || global.Scheduler.DailyAt != "" {
		t.Fatalf("global schedule should be untouched by project scope: %+v", global.Scheduler)
	}

	req = httptest.NewRequest("POST", "/schedule", strings.NewReader(`{"scope":"global","interval":"1h","enabled":true}`))
	rr = httptest.NewRecorder()
	d.handleSchedule(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("global schedule status = %d body=%s", rr.Code, rr.Body.String())
	}
	var globalResp struct {
		Updated    int      `json:"updated"`
		SessionIDs []string `json:"session_ids"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&globalResp); err != nil {
		t.Fatalf("decode global response: %v", err)
	}
	if globalResp.Updated != 1 || len(globalResp.SessionIDs) != 1 || globalResp.SessionIDs[0] != "global-api" {
		t.Fatalf("unexpected global schedule response: %+v", globalResp)
	}
	global, ok, err = agent.LoadRuntimeMeta(globalSess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta global after schedule: err=%v ok=%v", err, ok)
	}
	projectA, ok, err = agent.LoadRuntimeMeta(projectASess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta projectA after global: err=%v ok=%v", err, ok)
	}
	if global.Scheduler.Interval != time.Hour || !global.Scheduler.Enabled {
		t.Fatalf("global schedule not applied: %+v", global.Scheduler)
	}
	if projectA.Scheduler.Interval != 0 || projectA.Scheduler.DailyAt != "08:30" {
		t.Fatalf("project schedule should not be overwritten by global scope: %+v", projectA.Scheduler)
	}
}

func TestDaemonScheduleHandlerRejectsInvalidTimezone(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "schedule-bad-tz.jsonl")
	if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{SessionID: "schedule-bad-tz"}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	req := httptest.NewRequest("POST", "/schedule", strings.NewReader(`{"session_id":"schedule-bad-tz","timezone":"Mars/Olympus"}`))
	rr := httptest.NewRecorder()
	d.handleSchedule(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("schedule status = %d body=%s, want bad request", rr.Code, rr.Body.String())
	}
}

func TestDaemonWatchHandlerPersistsConfig(t *testing.T) {
	dir := t.TempDir()
	watchDir := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(watchDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sess := filepath.Join(dir, "watch-api.jsonl")
	if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "watch-api",
		Goal:      agent.RuntimeGoalMeta{Text: "watch files", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	d.fileWatcher = NewFileWatcher(d, nil)

	body := `{"session_id":"watch-api","paths":["` + watchDir + `"],"ignore_patterns":["*.tmp"],"debounce":"5s","enabled":true}`
	req := httptest.NewRequest("POST", "/watch", strings.NewReader(body))
	rr := httptest.NewRecorder()
	d.handleWatch(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("watch status = %d body=%s", rr.Code, rr.Body.String())
	}

	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if !loaded.FileWatch.Enabled || len(loaded.FileWatch.Paths) != 1 || loaded.FileWatch.Paths[0] != watchDir {
		t.Fatalf("file watch config not persisted: %+v", loaded.FileWatch)
	}
	if loaded.FileWatch.Debounce != 5*time.Second {
		t.Fatalf("Debounce = %v, want 5s", loaded.FileWatch.Debounce)
	}
	d.fileWatcher.mu.Lock()
	registered := d.fileWatcher.watches["watch-api"]
	d.fileWatcher.mu.Unlock()
	if registered == nil || !registered.config.Enabled || len(registered.config.Paths) != 1 {
		t.Fatalf("file watcher not registered: %+v", registered)
	}
	events, ok, err := agent.LoadRuntimeTimeline(sess, 1)
	if err != nil || !ok || len(events) != 1 || events[0].Type != "watch_configured" {
		t.Fatalf("watch timeline not recorded: events=%+v err=%v ok=%v", events, err, ok)
	}

	req = httptest.NewRequest("POST", "/watch", strings.NewReader(`{"session_id":"watch-api","enabled":false}`))
	rr = httptest.NewRecorder()
	d.handleWatch(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("disable watch status = %d body=%s", rr.Code, rr.Body.String())
	}
	loaded, ok, err = agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta disabled watch: err=%v ok=%v", err, ok)
	}
	if loaded.FileWatch.Enabled || len(loaded.FileWatch.Paths) != 1 || loaded.FileWatch.Paths[0] != watchDir ||
		len(loaded.FileWatch.IgnorePatterns) != 1 || loaded.FileWatch.IgnorePatterns[0] != "*.tmp" ||
		loaded.FileWatch.Debounce != 5*time.Second {
		t.Fatalf("disabled watch should preserve config: %+v", loaded.FileWatch)
	}
	d.fileWatcher.mu.Lock()
	registered = d.fileWatcher.watches["watch-api"]
	d.fileWatcher.mu.Unlock()
	if registered != nil {
		t.Fatalf("disabled watch should unregister watcher: %+v", registered)
	}
}

func TestDaemonBudgetHandlerPersistsConfig(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "budget-api.jsonl")
	if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "budget-api",
		Goal:      agent.RuntimeGoalMeta{Text: "keep budget", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
		Budget: agent.RuntimeBudgetMeta{
			DailyWakeups:      3,
			DailyModelCalls:   2,
			DailyModelCost:    0.75,
			ModelCostCurrency: "$",
			LastBlockedReason: "old",
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	req := httptest.NewRequest("POST", "/budget", strings.NewReader(`{"session_id":"budget-api","daily_wakeup_limit":5,"max_goal_auto_turns":12,"daily_model_call_limit":7,"daily_model_cost_limit":1.25,"reset":true}`))
	rr := httptest.NewRecorder()
	d.handleBudget(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("budget status = %d body=%s", rr.Code, rr.Body.String())
	}

	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Budget.DailyWakeupLimit != 5 || loaded.Budget.MaxGoalAutoTurns != 12 ||
		loaded.Budget.DailyModelCallLimit != 7 || loaded.Budget.DailyModelCostLimit != 1.25 ||
		loaded.Budget.DailyWakeups != 0 || loaded.Budget.DailyModelCalls != 0 || loaded.Budget.DailyModelCost != 0 ||
		loaded.Budget.ModelCostCurrency != "" || loaded.Budget.LastBlockedReason != "" {
		t.Fatalf("budget not persisted/reset: %+v", loaded.Budget)
	}
	if loaded.Budget.WindowStartedAt.IsZero() {
		t.Fatal("budget reset should stamp WindowStartedAt")
	}
	events, ok, err := agent.LoadRuntimeTimeline(sess, 1)
	if err != nil || !ok || len(events) != 1 || events[0].Type != "budget_configured" {
		t.Fatalf("budget timeline not recorded: events=%+v err=%v ok=%v", events, err, ok)
	}
	if !strings.Contains(events[0].Message, "max_goal_auto_turns=12") ||
		!strings.Contains(events[0].Message, "daily_model_call_limit=7") ||
		!strings.Contains(events[0].Message, "daily_model_cost_limit=1.250000") {
		t.Fatalf("budget timeline missing auto-turn cap: %+v", events[0])
	}
}

func TestDaemonBudgetHandlerConfiguresProjectAggregateQuota(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "workspace")
	now := time.Now().UTC()
	writeBudgetAggregateSession(t, dir, root, "project-budget-a", 1, 0.25, now)
	writeBudgetAggregateSession(t, dir, root, "project-budget-b", 1, 0.50, now)
	writeBudgetAggregateSession(t, dir, "", "global-budget-a", 1, 0.75, now)

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	body := fmt.Sprintf(`{"scope":"project","workspace_root":%q,"daily_model_call_limit":3,"daily_model_cost_limit":2.5}`, root)
	req := httptest.NewRequest("POST", "/budget", strings.NewReader(body))
	rr := httptest.NewRecorder()
	d.handleBudget(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("project budget status = %d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest("GET", "/budgets", nil)
	rr = httptest.NewRecorder()
	d.handleBudgets(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("budgets status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp BudgetAggregatesResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode budgets: %v", err)
	}
	var project, global *BudgetAggregateView
	for i := range resp.Budgets {
		view := &resp.Budgets[i]
		if view.Scope == "project" && sameWorkspaceRoot(view.WorkspaceRoot, root) {
			project = view
		}
		if view.Scope == "global" {
			global = view
		}
	}
	if project == nil {
		t.Fatalf("project aggregate missing: %+v", resp.Budgets)
	}
	if project.SessionCount != 2 || project.DailyModelCalls != 2 || project.DailyModelCallLimit != 3 ||
		project.DailyModelCost != 0.75 || project.DailyModelCostLimit != 2.5 || project.Blocked {
		t.Fatalf("unexpected project aggregate: %+v", project)
	}
	if global == nil || global.SessionCount != 3 || global.DailyModelCalls != 3 {
		t.Fatalf("unexpected global aggregate: %+v", global)
	}
}

func writeBudgetAggregateSession(t *testing.T, dir, workspaceRoot, id string, calls int, cost float64, now time.Time) string {
	t.Helper()
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", id, err)
	}
	if err := agent.SaveRuntimeMeta(path, agent.RuntimeMeta{
		SessionID:     id,
		WorkspaceRoot: workspaceRoot,
		Goal:          agent.RuntimeGoalMeta{Text: "budget aggregate", Status: control.GoalStatusRunning},
		Run:           agent.RuntimeRunMeta{Status: agent.RunStatusIdle},
		Budget: agent.RuntimeBudgetMeta{
			DailyModelCalls:   calls,
			DailyModelCost:    cost,
			ModelCostCurrency: "$",
			WindowStartedAt:   budgetWindowStart(now),
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta %s: %v", id, err)
	}
	scope := "global"
	if workspaceRoot != "" {
		scope = "project"
	}
	if err := agent.SaveBranchMeta(path, agent.BranchMeta{Scope: scope, WorkspaceRoot: workspaceRoot}); err != nil {
		t.Fatalf("SaveBranchMeta %s: %v", id, err)
	}
	return path
}

func TestDaemonWaitEventHandlerPersistsConfig(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "wait-event-api.jsonl")
	if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "wait-event-api",
		Goal:      agent.RuntimeGoalMeta{Text: "wait for CI", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	req := httptest.NewRequest("POST", "/wait-event", strings.NewReader(`{"session_id":"wait-event-api","event_source":"github.workflow_run","event_id":"delivery-42","event_status":"completed","event_conclusion":"success","reason":"waiting for CI","subject":"PR #42"}`))
	rr := httptest.NewRecorder()
	d.handleWaitEvent(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("wait-event status = %d body=%s", rr.Code, rr.Body.String())
	}

	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "waiting_event" {
		t.Fatalf("Run.Status = %q, want waiting_event", loaded.Run.Status)
	}
	if loaded.Wait.Kind != "event" || loaded.Wait.EventSource != "github.workflow_run" || loaded.Wait.EventID != "delivery-42" {
		t.Fatalf("wait condition not persisted: %+v", loaded.Wait)
	}
	if loaded.Wait.EventStatus != "completed" || loaded.Wait.EventConclusion != "success" {
		t.Fatalf("wait event status not persisted: %+v", loaded.Wait)
	}
	if loaded.Wait.Reason != "waiting for CI" || loaded.Wait.Subject != "PR #42" || loaded.Wait.Since.IsZero() {
		t.Fatalf("wait metadata incomplete: %+v", loaded.Wait)
	}
	events, ok, err := agent.LoadRuntimeTimeline(sess, 1)
	if err != nil || !ok || len(events) != 1 || events[0].Type != "wait_started" {
		t.Fatalf("wait timeline not recorded: events=%+v err=%v ok=%v", events, err, ok)
	}

	req = httptest.NewRequest("POST", "/wait-event", strings.NewReader(`{"session_id":"wait-event-api","clear":true}`))
	rr = httptest.NewRecorder()
	d.handleWaitEvent(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("clear wait-event status = %d body=%s", rr.Code, rr.Body.String())
	}
	loaded, ok, err = agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta after clear: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "idle" || loaded.Wait.Kind != "" {
		t.Fatalf("wait condition not cleared: run=%+v wait=%+v", loaded.Run, loaded.Wait)
	}
	events, ok, err = agent.LoadRuntimeTimeline(sess, 1)
	if err != nil || !ok || len(events) != 1 || events[0].Type != "wait_cleared" {
		t.Fatalf("wait clear timeline not recorded: events=%+v err=%v ok=%v", events, err, ok)
	}
}

func TestDaemonWaitTimeHandlerPersistsConfig(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "wait-time-api.jsonl")
	if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "wait-time-api",
		Goal:      agent.RuntimeGoalMeta{Text: "wait until later", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	req := httptest.NewRequest("POST", "/wait-time", strings.NewReader(`{"session_id":"wait-time-api","after":"1h","reason":"wait for release window","subject":"release"}`))
	rr := httptest.NewRecorder()
	d.handleWaitTime(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("wait-time status = %d body=%s", rr.Code, rr.Body.String())
	}

	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "waiting_time" || loaded.Wait.Kind != "time" {
		t.Fatalf("time wait not persisted: run=%+v wait=%+v", loaded.Run, loaded.Wait)
	}
	if loaded.Wait.Until.IsZero() || loaded.Wait.Reason != "wait for release window" || loaded.Wait.Subject != "release" {
		t.Fatalf("time wait metadata incomplete: %+v", loaded.Wait)
	}
	events, ok, err := agent.LoadRuntimeTimeline(sess, 1)
	if err != nil || !ok || len(events) != 1 || events[0].Type != "wait_started" {
		t.Fatalf("time wait timeline not recorded: events=%+v err=%v ok=%v", events, err, ok)
	}

	req = httptest.NewRequest("POST", "/wait-time", strings.NewReader(`{"session_id":"wait-time-api","clear":true}`))
	rr = httptest.NewRecorder()
	d.handleWaitTime(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("clear wait-time status = %d body=%s", rr.Code, rr.Body.String())
	}
	loaded, ok, err = agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta after clear: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "idle" || loaded.Wait.Kind != "" {
		t.Fatalf("time wait not cleared: run=%+v wait=%+v", loaded.Run, loaded.Wait)
	}
}

func TestDaemonWaitFileHandlerPersistsConfig(t *testing.T) {
	dir := t.TempDir()
	watchDir := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(filepath.Join(watchDir, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sess := filepath.Join(dir, "wait-file-api.jsonl")
	if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID:     "wait-file-api",
		WorkspaceRoot: watchDir,
		Goal:          agent.RuntimeGoalMeta{Text: "wait for generated file", Status: control.GoalStatusRunning},
		Run:           agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	d.fileWatcher = NewFileWatcher(d, nil)
	req := httptest.NewRequest("POST", "/wait-file", strings.NewReader(`{"session_id":"wait-file-api","paths":["src/output.txt"],"ignore_patterns":["*.tmp"],"debounce":"5s","reason":"waiting for generator","subject":"output.txt"}`))
	rr := httptest.NewRecorder()
	d.handleWaitFile(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("wait-file status = %d body=%s", rr.Code, rr.Body.String())
	}

	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "waiting_file" || loaded.Wait.Kind != "file" || loaded.Wait.FilePaths[0] != "src/output.txt" {
		t.Fatalf("file wait not persisted: run=%+v wait=%+v", loaded.Run, loaded.Wait)
	}
	if !loaded.FileWatch.Enabled || loaded.FileWatch.Paths[0] != "src" || loaded.FileWatch.Debounce != 5*time.Second {
		t.Fatalf("file watch not configured for wait: %+v", loaded.FileWatch)
	}
	d.fileWatcher.mu.Lock()
	registered := d.fileWatcher.watches["wait-file-api"]
	d.fileWatcher.mu.Unlock()
	wantPath := filepath.Join(watchDir, "src")
	if registered == nil || len(registered.config.Paths) != 1 || registered.config.Paths[0] != wantPath {
		t.Fatalf("file watcher not registered with resolved path: %+v", registered)
	}
	events, ok, err := agent.LoadRuntimeTimeline(sess, 1)
	if err != nil || !ok || len(events) != 1 || events[0].Type != "wait_started" {
		t.Fatalf("wait-file timeline not recorded: events=%+v err=%v ok=%v", events, err, ok)
	}

	req = httptest.NewRequest("POST", "/wait-file", strings.NewReader(`{"session_id":"wait-file-api","clear":true}`))
	rr = httptest.NewRecorder()
	d.handleWaitFile(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("clear wait-file status = %d body=%s", rr.Code, rr.Body.String())
	}
	loaded, ok, err = agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta after clear: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "idle" || loaded.Wait.Kind != "" || loaded.FileWatch.Enabled {
		t.Fatalf("file wait not cleared: run=%+v wait=%+v watch=%+v", loaded.Run, loaded.Wait, loaded.FileWatch)
	}
	d.fileWatcher.mu.Lock()
	registered = d.fileWatcher.watches["wait-file-api"]
	d.fileWatcher.mu.Unlock()
	if registered != nil {
		t.Fatalf("file watcher should be unregistered after clear: %+v", registered)
	}
}

func TestDaemonRestoreFileWatches(t *testing.T) {
	dir := t.TempDir()
	watchDir := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(watchDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sess := filepath.Join(dir, "watch-restore.jsonl")
	if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID:     "watch-restore",
		WorkspaceRoot: watchDir,
		Goal:          agent.RuntimeGoalMeta{Text: "watch files", Status: control.GoalStatusRunning},
		Run:           agent.RuntimeRunMeta{Status: "idle"},
		FileWatch: agent.RuntimeWatchMeta{
			Enabled:        true,
			Paths:          []string{"src"},
			IgnorePatterns: []string{"*.tmp"},
			Debounce:       4 * time.Second,
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	d.fileWatcher = NewFileWatcher(d, nil)
	d.restoreFileWatches()

	d.fileWatcher.mu.Lock()
	registered := d.fileWatcher.watches["watch-restore"]
	d.fileWatcher.mu.Unlock()
	if registered == nil {
		t.Fatal("file watch not restored")
	}
	if registered.config.Debounce != 4*time.Second || registered.config.IgnorePatterns[0] != "*.tmp" {
		t.Fatalf("unexpected restored config: %+v", registered.config)
	}
	wantPath := filepath.Join(watchDir, "src")
	if len(registered.config.Paths) != 1 || registered.config.Paths[0] != wantPath {
		t.Fatalf("restored paths = %+v, want %q", registered.config.Paths, wantPath)
	}
}

func TestFileWatcherWakeupIncludesChangedFileSummary(t *testing.T) {
	dir := t.TempDir()
	watchDir := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(filepath.Join(watchDir, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sess := filepath.Join(dir, "file-summary.jsonl")
	if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "file-summary",
		Goal:      agent.RuntimeGoalMeta{Text: "react to files", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	fw := NewFileWatcher(d, d.logger)
	state := &watchState{
		config: FileWatchConfig{
			Paths:   []string{watchDir},
			Enabled: true,
		},
		lastSeen: map[string]time.Time{},
		changes: map[string]struct{}{
			filepath.Join(watchDir, "src", "b.go"): {},
			filepath.Join(watchDir, "src", "a.go"): {},
		},
		pending: true,
	}

	fw.fireWakeup("file-summary", state, time.Now())

	select {
	case intent := <-d.intentCh:
		if intent.Source != "file_watch" || intent.Reason != "file_change" {
			t.Fatalf("unexpected intent: %+v", intent)
		}
		if !strings.Contains(intent.Context, "File watch detected 2 changed file(s)") ||
			!strings.Contains(intent.Context, "src/a.go") ||
			!strings.Contains(intent.Context, "src/b.go") {
			t.Fatalf("missing changed file summary:\n%s", intent.Context)
		}
		if strings.Contains(intent.Context, watchDir) {
			t.Fatalf("summary should prefer paths relative to watch root:\n%s", intent.Context)
		}
	default:
		t.Fatal("file watcher did not enqueue an intent")
	}

	events, ok, err := agent.LoadRuntimeTimeline(sess, 0)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeTimeline: err=%v ok=%v", err, ok)
	}
	var found bool
	for _, event := range events {
		if event.Type == "file_change_detected" && strings.Contains(event.Message, "src/a.go") {
			if event.Step != "deterministic" {
				t.Fatalf("file change timeline should be deterministic: %+v", event)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("file change timeline event missing: %+v", events)
	}
}

func TestFileWatcherDebouncesRapidChangesIntoOneWakeup(t *testing.T) {
	dir := t.TempDir()
	watchDir := filepath.Join(dir, "workspace")
	srcDir := filepath.Join(watchDir, "src")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	aPath := filepath.Join(srcDir, "a.go")
	bPath := filepath.Join(srcDir, "b.go")
	if err := os.WriteFile(aPath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile a: %v", err)
	}
	if err := os.WriteFile(bPath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile b: %v", err)
	}
	base := time.Now().Add(-10 * time.Minute)
	if err := os.Chtimes(aPath, base, base); err != nil {
		t.Fatalf("Chtimes a: %v", err)
	}
	if err := os.Chtimes(bPath, base, base); err != nil {
		t.Fatalf("Chtimes b: %v", err)
	}

	sess := filepath.Join(dir, "file-debounce.jsonl")
	if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile session: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "file-debounce",
		Goal:      agent.RuntimeGoalMeta{Text: "react to files", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	fw := NewFileWatcher(d, d.logger)
	fw.Register("file-debounce", FileWatchConfig{Paths: []string{watchDir}, Debounce: time.Hour, Enabled: true})

	// First poll establishes the baseline without firing.
	fw.poll()
	if err := os.Chtimes(aPath, base.Add(time.Minute), base.Add(time.Minute)); err != nil {
		t.Fatalf("Chtimes a update: %v", err)
	}
	fw.poll()
	if err := os.Chtimes(bPath, base.Add(2*time.Minute), base.Add(2*time.Minute)); err != nil {
		t.Fatalf("Chtimes b update: %v", err)
	}
	fw.poll()

	fw.mu.Lock()
	state := fw.watches["file-debounce"]
	if state == nil {
		fw.mu.Unlock()
		t.Fatal("file watch missing")
	}
	if len(state.changes) != 2 {
		fw.mu.Unlock()
		t.Fatalf("pending changes = %d, want 2: %+v", len(state.changes), state.changes)
	}
	state.timer = time.Now().Add(-time.Second)
	fw.mu.Unlock()
	fw.poll()

	select {
	case intent := <-d.intentCh:
		if !strings.Contains(intent.Context, "File watch detected 2 changed file(s)") ||
			!strings.Contains(intent.Context, "src/a.go") ||
			!strings.Contains(intent.Context, "src/b.go") {
			t.Fatalf("rapid changes not merged into one summary:\n%s", intent.Context)
		}
	default:
		t.Fatal("debounced file changes did not enqueue one intent")
	}
	select {
	case intent := <-d.intentCh:
		t.Fatalf("rapid changes should produce only one intent, got second: %+v", intent)
	default:
	}
}

func TestFileWatcherNativeEventDebouncesIntoWakeup(t *testing.T) {
	dir := t.TempDir()
	watchDir := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(watchDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	changedPath := filepath.Join(watchDir, "CHANGELOG.md")
	if err := os.WriteFile(changedPath, []byte("# Changelog\n"), 0o644); err != nil {
		t.Fatalf("WriteFile changed: %v", err)
	}
	sess := filepath.Join(dir, "file-native.jsonl")
	if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile session: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "file-native",
		Goal:      agent.RuntimeGoalMeta{Text: "react to native file events", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	fw := NewFileWatcher(d, d.logger)
	fw.Register("file-native", FileWatchConfig{Paths: []string{watchDir}, Debounce: time.Hour, Enabled: true})
	now := time.Now()
	fw.handleNativeEvent(fsnotify.Event{Name: changedPath, Op: fsnotify.Write}, now)

	fw.mu.Lock()
	state := fw.watches["file-native"]
	if state == nil {
		fw.mu.Unlock()
		t.Fatal("file-native watch missing")
	}
	if !state.pending || len(state.changes) != 1 {
		fw.mu.Unlock()
		t.Fatalf("native event did not record one pending change: %+v", state)
	}
	state.timer = now.Add(-time.Second)
	fw.mu.Unlock()
	fw.flushDue(now)

	select {
	case intent := <-d.intentCh:
		if intent.Source != "file_watch" || !strings.Contains(intent.Context, "CHANGELOG.md") {
			t.Fatalf("unexpected native event intent: %+v", intent)
		}
	default:
		t.Fatal("native file event did not enqueue an intent")
	}
	if stats := fw.Stats(); stats.NativeEvents != 1 {
		t.Fatalf("NativeEvents = %d, want 1", stats.NativeEvents)
	}
}

func TestFileWatcherStatsTrackPollAndIgnoredNativeChanges(t *testing.T) {
	dir := t.TempDir()
	watchDir := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(watchDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	kept := filepath.Join(watchDir, "main.go")
	ignored := filepath.Join(watchDir, "scratch.tmp")
	if err := os.WriteFile(kept, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile kept: %v", err)
	}
	if err := os.WriteFile(ignored, []byte("tmp\n"), 0o644); err != nil {
		t.Fatalf("WriteFile ignored: %v", err)
	}

	fw := NewFileWatcher(nil, nil)
	fw.Register("file-stats", FileWatchConfig{Paths: []string{watchDir}, IgnorePatterns: []string{"*.tmp"}, Enabled: true})
	fw.poll()
	stats := fw.Stats()
	if stats.PollScans != 1 || stats.LastPollFiles != 1 || stats.LastPollDirs < 1 || stats.LastPollDurationMS < 0 {
		t.Fatalf("poll stats not recorded: %+v", stats)
	}
	fw.handleNativeEvent(fsnotify.Event{Name: ignored, Op: fsnotify.Write}, time.Now())
	stats = fw.Stats()
	if stats.NativeEvents != 1 || stats.IgnoredChanges != 1 {
		t.Fatalf("ignored native change stats not recorded: %+v", stats)
	}
}

func TestFileWatcherDoesNotWakeActiveRun(t *testing.T) {
	dir := t.TempDir()
	watchDir := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(watchDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sess := filepath.Join(dir, "file-active-run.jsonl")
	if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "file-active-run",
		Goal:      agent.RuntimeGoalMeta{Text: "react to files", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	d.activeRuns["file-active-run"] = &ActiveRun{Intent: RunIntent{SessionID: "file-active-run"}}
	fw := NewFileWatcher(d, d.logger)
	state := &watchState{
		config:   FileWatchConfig{Paths: []string{watchDir}, Enabled: true},
		lastSeen: map[string]time.Time{},
		changes: map[string]struct{}{
			filepath.Join(watchDir, "changed.md"): {},
		},
		pending: true,
	}
	fw.fireWakeup("file-active-run", state, time.Now())

	select {
	case intent := <-d.intentCh:
		t.Fatalf("file watcher should not wake active run: %+v", intent)
	default:
	}
	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "idle" || loaded.Run.ResumeCount != 0 {
		t.Fatalf("active run should leave runtime unchanged: %+v", loaded.Run)
	}
}

func TestFileWatcherWakeupClearsMatchingFileWait(t *testing.T) {
	dir := t.TempDir()
	watchDir := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(filepath.Join(watchDir, "src"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sess := filepath.Join(dir, "file-wait-match.jsonl")
	if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "file-wait-match",
		Goal:      agent.RuntimeGoalMeta{Text: "react to one file", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "waiting_file"},
		Wait: agent.RuntimeWaitMeta{
			Kind:      "file",
			FilePaths: []string{"src/a.go"},
			Subject:   "src/a.go",
		},
		FileWatch: agent.RuntimeWatchMeta{
			Enabled: true,
			Paths:   []string{filepath.Join(watchDir, "src", "a.go")},
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	fw := NewFileWatcher(d, d.logger)
	fw.Register("file-wait-match", FileWatchConfig{Paths: []string{filepath.Join(watchDir, "src", "a.go")}, Enabled: true})
	state := &watchState{
		config:   FileWatchConfig{Paths: []string{filepath.Join(watchDir, "src", "a.go")}, Enabled: true},
		lastSeen: map[string]time.Time{},
		changes: map[string]struct{}{
			filepath.Join(watchDir, "src", "a.go"): {},
		},
		pending: true,
	}

	fw.fireWakeup("file-wait-match", state, time.Now())

	select {
	case intent := <-d.intentCh:
		if intent.SessionID != "file-wait-match" || intent.Source != "file_watch" {
			t.Fatalf("unexpected intent: %+v", intent)
		}
	default:
		t.Fatal("matching file wait did not enqueue intent")
	}
	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "queued" || loaded.Wait.Kind != "" || loaded.FileWatch.Enabled {
		t.Fatalf("matching file wait should clear wait and watcher: run=%+v wait=%+v watch=%+v", loaded.Run, loaded.Wait, loaded.FileWatch)
	}
	fw.mu.Lock()
	registered := fw.watches["file-wait-match"]
	fw.mu.Unlock()
	if registered != nil {
		t.Fatalf("matching file wait should unregister watcher: %+v", registered)
	}
}

func TestFileWatcherDoesNotWakeDifferentWaitKind(t *testing.T) {
	dir := t.TempDir()
	watchDir := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(watchDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sess := filepath.Join(dir, "file-wait-event.jsonl")
	if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "file-wait-event",
		Goal:      agent.RuntimeGoalMeta{Text: "wait for CI", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "waiting_event"},
		Wait:      agent.RuntimeWaitMeta{Kind: "event", EventSource: "github.workflow_run"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	fw := NewFileWatcher(d, d.logger)
	state := &watchState{
		config:   FileWatchConfig{Paths: []string{watchDir}, Enabled: true},
		lastSeen: map[string]time.Time{},
		changes: map[string]struct{}{
			filepath.Join(watchDir, "changed.md"): {},
		},
		pending: true,
	}
	fw.fireWakeup("file-wait-event", state, time.Now())

	select {
	case intent := <-d.intentCh:
		t.Fatalf("file watcher should not wake event wait: %+v", intent)
	default:
	}
	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "waiting_event" || loaded.Wait.Kind != "event" {
		t.Fatalf("event wait should remain unchanged: run=%+v wait=%+v", loaded.Run, loaded.Wait)
	}
}

func TestFileWatcherIgnoresNonMatchingFileWait(t *testing.T) {
	dir := t.TempDir()
	watchDir := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(watchDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sess := filepath.Join(dir, "file-wait-miss.jsonl")
	if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "file-wait-miss",
		Goal:      agent.RuntimeGoalMeta{Text: "wait for one file", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "waiting_file"},
		Wait:      agent.RuntimeWaitMeta{Kind: "file", FilePaths: []string{"wanted.txt"}},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	fw := NewFileWatcher(d, d.logger)
	state := &watchState{
		config:   FileWatchConfig{Paths: []string{watchDir}, Enabled: true},
		lastSeen: map[string]time.Time{},
		changes: map[string]struct{}{
			filepath.Join(watchDir, "other.txt"): {},
		},
		pending: true,
	}
	fw.fireWakeup("file-wait-miss", state, time.Now())

	select {
	case intent := <-d.intentCh:
		t.Fatalf("non-matching file wait should not enqueue intent: %+v", intent)
	default:
	}
	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "waiting_file" || loaded.Wait.Kind != "file" {
		t.Fatalf("file wait should remain unchanged: run=%+v wait=%+v", loaded.Run, loaded.Wait)
	}
	events, ok, err := agent.LoadRuntimeTimeline(sess, 1)
	if err != nil || !ok || len(events) != 1 || events[0].Type != "wait_file_ignored" {
		t.Fatalf("file wait miss timeline not recorded: events=%+v err=%v ok=%v", events, err, ok)
	}
}

func TestFileWatcherWakeupRespectsDailyBudget(t *testing.T) {
	dir := t.TempDir()
	watchDir := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(watchDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	sess := filepath.Join(dir, "file-budget.jsonl")
	if err := os.WriteFile(sess, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	now := time.Now().UTC()
	if err := agent.SaveRuntimeMeta(sess, agent.RuntimeMeta{
		SessionID: "file-budget",
		Goal:      agent.RuntimeGoalMeta{Text: "react to files", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
		Budget: agent.RuntimeBudgetMeta{
			DailyWakeupLimit: 1,
			DailyWakeups:     1,
			WindowStartedAt:  budgetWindowStart(now),
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	fw := NewFileWatcher(d, d.logger)
	state := &watchState{
		config:   FileWatchConfig{Paths: []string{watchDir}, Enabled: true},
		lastSeen: map[string]time.Time{},
		changes: map[string]struct{}{
			filepath.Join(watchDir, "changed.md"): {},
		},
		pending: true,
	}
	fw.fireWakeup("file-budget", state, now)

	select {
	case intent := <-d.intentCh:
		t.Fatalf("budget-blocked file watch should not enqueue intent: %+v", intent)
	default:
	}
	loaded, ok, err := agent.LoadRuntimeMeta(sess)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "idle" {
		t.Fatalf("Run.Status = %q, want idle", loaded.Run.Status)
	}
	if loaded.Budget.LastBlockedReason == "" || loaded.Scheduler.LastWakeupReason != "budget_blocked:file_watch" {
		t.Fatalf("budget block not persisted: budget=%+v scheduler=%+v", loaded.Budget, loaded.Scheduler)
	}
}

func TestDaemonExecuteIntentCompletesGoal(t *testing.T) {
	dir := t.TempDir()
	sessPath := filepath.Join(dir, "worker-test.jsonl")
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "start"})
	if err := sess.Save(sessPath); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sessPath, agent.RuntimeMeta{
		SessionID: "worker-test",
		Model:     "daemon-test-model",
		Goal:      agent.RuntimeGoalMeta{Text: "finish worker", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "queued"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	prov := &daemonScriptedProvider{turns: [][]provider.Chunk{{
		{Type: provider.ChunkUsage, Usage: &provider.Usage{
			PromptTokens:     30,
			CompletionTokens: 10,
			TotalTokens:      40,
			CacheHitTokens:   20,
			CacheMissTokens:  10,
			ReasoningTokens:  3,
			FinishReason:     "stop",
		}},
		{Type: provider.ChunkText, Text: "done\n\n[goal:complete]"},
	}}}
	d := New(Options{
		SessionDir: dir,
		ControllerFactory: func(ctx context.Context, d *Daemon, entry *SessionEntry, sink event.Sink) (*control.Controller, error) {
			loaded, err := agent.LoadSession(entry.Path)
			if err != nil {
				return nil, err
			}
			ag := agent.New(prov, tool.NewRegistry(), loaded, agent.Options{
				Pricing: &provider.Pricing{CacheHit: 0.5, Input: 1, Output: 2, Currency: "usd"},
			}, sink)
			c := control.New(control.Options{
				Runner:      ag,
				Executor:    ag,
				SessionPath: entry.Path,
				SessionDir:  dir,
				Sink:        sink,
			})
			c.Resume(loaded, entry.Path)
			return c, nil
		},
	})
	d.scanSessions()
	d.executeIntent(context.Background(), RunIntent{SessionID: "worker-test", Source: "test", Reason: "test"})

	loaded, ok, err := agent.LoadRuntimeMeta(sessPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Goal.Status != control.GoalStatusComplete {
		t.Fatalf("Goal.Status = %q, want complete", loaded.Goal.Status)
	}
	if loaded.Run.Status != "idle" {
		t.Fatalf("Run.Status = %q, want idle", loaded.Run.Status)
	}
	if loaded.Budget.DailyModelCalls != 1 || loaded.Budget.DailyModelCost <= 0 || loaded.Budget.ModelCostCurrency != "$" {
		t.Fatalf("model budget usage not persisted: %+v", loaded.Budget)
	}
	events, ok, err := agent.LoadRuntimeTimeline(sessPath, 0)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeTimeline: err=%v ok=%v", err, ok)
	}
	var sawUsage, sawComplete bool
	for _, event := range events {
		switch event.Type {
		case "model_usage":
			sawUsage = true
			if event.Step != "model" || event.Model != "daemon-test-model" || event.Finish != "stop" {
				t.Fatalf("model usage decision metadata incomplete: %+v", event)
			}
			if event.Prompt != 30 || event.Completion != 10 || event.Total != 40 || event.CacheHit != 20 || event.CacheMiss != 10 || event.Reasoning != 3 {
				t.Fatalf("model usage tokens not recorded: %+v", event)
			}
			if event.Cost <= 0 || event.Currency != "$" {
				t.Fatalf("model usage cost not recorded: %+v", event)
			}
		case "goal_continuation_complete":
			sawComplete = true
			if event.Step != "model" {
				t.Fatalf("goal completion should be marked as a model decision: %+v", event)
			}
		}
	}
	if !sawUsage || !sawComplete {
		t.Fatalf("missing model observability events usage=%t complete=%t events=%+v", sawUsage, sawComplete, events)
	}
}

func TestDaemonExecuteIntentBlocksWhenModelCallBudgetExhausted(t *testing.T) {
	dir := t.TempDir()
	sessPath := filepath.Join(dir, "worker-budget-block.jsonl")
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "start"})
	if err := sess.Save(sessPath); err != nil {
		t.Fatalf("Save: %v", err)
	}
	now := time.Now().UTC()
	if err := agent.SaveRuntimeMeta(sessPath, agent.RuntimeMeta{
		SessionID: "worker-budget-block",
		Goal:      agent.RuntimeGoalMeta{Text: "finish worker", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "queued"},
		Budget: agent.RuntimeBudgetMeta{
			DailyModelCallLimit: 1,
			DailyModelCalls:     1,
			WindowStartedAt:     budgetWindowStart(now),
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	prov := &daemonScriptedProvider{}
	d := New(Options{
		SessionDir: dir,
		ControllerFactory: func(ctx context.Context, d *Daemon, entry *SessionEntry, sink event.Sink) (*control.Controller, error) {
			loaded, err := agent.LoadSession(entry.Path)
			if err != nil {
				return nil, err
			}
			ag := agent.New(prov, tool.NewRegistry(), loaded, agent.Options{}, sink)
			c := control.New(control.Options{
				Runner:      ag,
				Executor:    ag,
				SessionPath: entry.Path,
				SessionDir:  dir,
				Sink:        sink,
			})
			c.Resume(loaded, entry.Path)
			return c, nil
		},
	})
	d.scanSessions()
	d.executeIntent(context.Background(), RunIntent{SessionID: "worker-budget-block", Source: "cron", Reason: "cron"})

	if prov.calls != 0 {
		t.Fatalf("provider should not be called after model budget is exhausted, got %d call(s)", prov.calls)
	}
	loaded, ok, err := agent.LoadRuntimeMeta(sessPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "idle" || loaded.Scheduler.LastWakeupReason != "budget_blocked:model" {
		t.Fatalf("model budget block not persisted: run=%+v scheduler=%+v", loaded.Run, loaded.Scheduler)
	}
	if !strings.Contains(loaded.Budget.LastBlockedReason, "daily model call budget exhausted") {
		t.Fatalf("missing model budget block reason: %+v", loaded.Budget)
	}
	events, ok, err := agent.LoadRuntimeTimeline(sessPath, 0)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeTimeline: err=%v ok=%v", err, ok)
	}
	var blocked bool
	for _, event := range events {
		if event.Type == "model_budget_blocked" {
			blocked = true
			if event.Step != "deterministic" || event.Source != "cron" {
				t.Fatalf("unexpected model budget timeline event: %+v", event)
			}
		}
		if event.Type == "model_usage" {
			t.Fatalf("blocked intent should not record model usage: %+v", event)
		}
	}
	if !blocked {
		t.Fatalf("missing model budget block timeline event: %+v", events)
	}
}

func TestDaemonExecuteIntentBlocksWhenProjectModelCallBudgetExhausted(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "workspace")
	now := time.Now().UTC()
	writeBudgetAggregateSession(t, dir, root, "project-budget-used", 1, 0, now)
	targetPath := writeBudgetAggregateSession(t, dir, root, "project-budget-target", 0, 0, now)

	prov := &daemonScriptedProvider{}
	d := New(Options{
		SessionDir: dir,
		ControllerFactory: func(ctx context.Context, d *Daemon, entry *SessionEntry, sink event.Sink) (*control.Controller, error) {
			loaded, err := agent.LoadSession(entry.Path)
			if err != nil {
				return nil, err
			}
			ag := agent.New(prov, tool.NewRegistry(), loaded, agent.Options{}, sink)
			c := control.New(control.Options{
				Runner:      ag,
				Executor:    ag,
				SessionPath: entry.Path,
				SessionDir:  dir,
				Sink:        sink,
			})
			c.Resume(loaded, entry.Path)
			return c, nil
		},
	})
	if err := d.saveScopeBudgetConfig(ScopeBudgetConfig{Quotas: []ScopeBudgetQuota{{
		Scope:               "project",
		WorkspaceRoot:       root,
		DailyModelCallLimit: 1,
	}}}); err != nil {
		t.Fatalf("saveScopeBudgetConfig: %v", err)
	}
	d.scanSessions()
	d.executeIntent(context.Background(), RunIntent{SessionID: "project-budget-target", Source: "cron", Reason: "cron"})

	if prov.calls != 0 {
		t.Fatalf("provider should not be called after project budget is exhausted, got %d call(s)", prov.calls)
	}
	loaded, ok, err := agent.LoadRuntimeMeta(targetPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != agent.RunStatusIdle || loaded.Scheduler.LastWakeupReason != "budget_blocked:model" {
		t.Fatalf("project model budget block not persisted: run=%+v scheduler=%+v", loaded.Run, loaded.Scheduler)
	}
	if !strings.Contains(loaded.Budget.LastBlockedReason, "project") ||
		!strings.Contains(loaded.Budget.LastBlockedReason, "daily model call budget exhausted") {
		t.Fatalf("missing project budget block reason: %+v", loaded.Budget)
	}
	events, ok, err := agent.LoadRuntimeTimeline(targetPath, 0)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeTimeline: err=%v ok=%v", err, ok)
	}
	var blocked bool
	for _, event := range events {
		if event.Type == "model_budget_blocked" {
			blocked = true
			if event.Step != "deterministic" || event.Source != "cron" || !strings.Contains(event.Reason, "project") {
				t.Fatalf("unexpected project budget timeline event: %+v", event)
			}
		}
		if event.Type == "model_usage" {
			t.Fatalf("blocked intent should not record model usage: %+v", event)
		}
	}
	if !blocked {
		t.Fatalf("missing project model budget block timeline event: %+v", events)
	}
}

func TestDaemonDailyTriageE2EQueuesOnceAndBlocksOnModelBudget(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC)
	due := now.Add(-time.Minute)

	triagePath := writeDailyTriageE2ESession(t, dir, "daily-triage-e2e", due, agent.RuntimeBudgetMeta{
		DailyWakeupLimit:    2,
		DailyModelCallLimit: 4,
		WindowStartedAt:     budgetWindowStart(now),
	})
	blockedPath := writeDailyTriageE2ESession(t, dir, "daily-triage-budget-e2e", due, agent.RuntimeBudgetMeta{
		DailyWakeupLimit:    2,
		DailyModelCallLimit: 1,
		DailyModelCalls:     1,
		WindowStartedAt:     budgetWindowStart(now),
	})

	prov := &daemonScriptedProvider{turns: [][]provider.Chunk{{
		{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, FinishReason: "stop"}},
		{Type: provider.ChunkText, Text: "triage complete\n\n[goal:complete]"},
	}}}
	d := New(Options{
		SessionDir:        dir,
		MaxConcurrentRuns: 1,
		ControllerFactory: func(ctx context.Context, d *Daemon, entry *SessionEntry, sink event.Sink) (*control.Controller, error) {
			loaded, err := agent.LoadSession(entry.Path)
			if err != nil {
				return nil, err
			}
			ag := agent.New(prov, tool.NewRegistry(), loaded, agent.Options{}, sink)
			c := control.New(control.Options{
				Runner:      ag,
				Executor:    ag,
				SessionPath: entry.Path,
				SessionDir:  dir,
				Sink:        sink,
			})
			c.Resume(loaded, entry.Path)
			return c, nil
		},
	})
	d.scanSessions()
	scheduler := NewScheduler(d, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.runIntentWorker(ctx)

	scheduler.wakeupSession("daily-triage-e2e", now)
	scheduler.wakeupSession("daily-triage-e2e", now)
	waitForTimelineCount(t, triagePath, "goal_continuation_complete", 1)
	if got := countTimelineEvents(t, triagePath, "intent_queued"); got != 1 {
		t.Fatalf("daily triage should enqueue exactly once for the due window, got %d", got)
	}
	if got := countTimelineEvents(t, triagePath, "run_started"); got != 1 {
		t.Fatalf("daily triage should start exactly once for the due window, got %d", got)
	}
	if prov.calls != 1 {
		t.Fatalf("daily triage should call provider once, got %d", prov.calls)
	}
	loaded, ok, err := agent.LoadRuntimeMeta(triagePath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta triage: err=%v ok=%v", err, ok)
	}
	if loaded.Scheduler.LastWakeupEventID != "daily:daily-triage-e2e:2026-06-13" || loaded.Budget.DailyWakeups != 1 {
		t.Fatalf("daily triage wakeup metadata not persisted: scheduler=%+v budget=%+v", loaded.Scheduler, loaded.Budget)
	}

	callsBeforeBudgetBlock := prov.calls
	scheduler.wakeupSession("daily-triage-budget-e2e", now)
	waitForTimelineCount(t, blockedPath, "model_budget_blocked", 1)
	if prov.calls != callsBeforeBudgetBlock {
		t.Fatalf("budget exhausted daily triage should not call provider, before=%d after=%d", callsBeforeBudgetBlock, prov.calls)
	}
	if got := countTimelineEvents(t, blockedPath, "intent_queued"); got != 1 {
		t.Fatalf("budget exhausted daily triage should still record one queued intent, got %d", got)
	}
	if got := countTimelineEvents(t, blockedPath, "model_usage"); got != 0 {
		t.Fatalf("budget exhausted daily triage should not record model usage, got %d", got)
	}
	blocked, ok, err := agent.LoadRuntimeMeta(blockedPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta blocked: err=%v ok=%v", err, ok)
	}
	if blocked.Run.Status != agent.RunStatusIdle || blocked.Scheduler.LastWakeupReason != "budget_blocked:model" {
		t.Fatalf("budget exhausted runtime not persisted: run=%+v scheduler=%+v", blocked.Run, blocked.Scheduler)
	}
	if !strings.Contains(blocked.Budget.LastBlockedReason, "daily model call budget exhausted") {
		t.Fatalf("budget block reason missing: %+v", blocked.Budget)
	}
}

func TestDaemonCIWatcherE2EDiagnosesThenContinuesAndDedupes(t *testing.T) {
	dir := t.TempDir()
	webhookKey := "ci-watch-e2e-key"
	failurePath := writeCIWatchE2ESession(t, dir, "ci-workflow-failure-e2e", "github.workflow_run")
	workflowPath := writeCIWatchE2ESession(t, dir, "ci-workflow-success-e2e", "github.workflow_run")
	checkSuitePath := writeCIWatchE2ESession(t, dir, "ci-check-suite-e2e", "github.check_suite")
	statusPath := writeCIWatchE2ESession(t, dir, "ci-status-e2e", "github.status")

	prov := &daemonScriptedProvider{turns: [][]provider.Chunk{
		{
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 11, CompletionTokens: 5, TotalTokens: 16, FinishReason: "stop"}},
			{Type: provider.ChunkText, Text: "workflow CI passed\n\n[goal:complete]"},
		},
		{
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 9, CompletionTokens: 3, TotalTokens: 12, FinishReason: "stop"}},
			{Type: provider.ChunkText, Text: "check suite passed\n\n[goal:complete]"},
		},
		{
			{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 8, CompletionTokens: 3, TotalTokens: 11, FinishReason: "stop"}},
			{Type: provider.ChunkText, Text: "commit status passed\n\n[goal:complete]"},
		},
	}}
	d := New(Options{
		SessionDir:        dir,
		Webhook:           &WebhookConfig{Secret: webhookKey, Enabled: true},
		MaxConcurrentRuns: 1,
		ControllerFactory: func(ctx context.Context, d *Daemon, entry *SessionEntry, sink event.Sink) (*control.Controller, error) {
			loaded, err := agent.LoadSession(entry.Path)
			if err != nil {
				return nil, err
			}
			ag := agent.New(prov, tool.NewRegistry(), loaded, agent.Options{}, sink)
			c := control.New(control.Options{
				Runner:      ag,
				Executor:    ag,
				SessionPath: entry.Path,
				SessionDir:  dir,
				Sink:        sink,
			})
			c.Resume(loaded, entry.Path)
			return c, nil
		},
	})
	d.scanSessions()

	workflowFailurePayload := `{"session_id":"ci-workflow-failure-e2e","action":"completed","repository":{"full_name":"example/repo"},"workflow_run":{"status":"completed","conclusion":"failure","pull_requests":[{"number":42}],"head_branch":"feature/ci-watch"}}`
	resp := postSignedGitHubWebhook(t, d, webhookKey, "workflow_run", "delivery-workflow-failure", workflowFailurePayload)
	if resp["status"] != "pending_diagnosis" {
		t.Fatalf("workflow failure status = %v, want pending_diagnosis", resp["status"])
	}
	select {
	case intent := <-d.intentCh:
		if intent.Source != "webhook" || intent.Reason != "webhook:github.workflow_run:failure" {
			t.Fatalf("unexpected diagnosis intent: %+v", intent)
		}
		if !strings.Contains(intent.Context, "CI finished without the awaited successful conclusion") ||
			!strings.Contains(intent.Context, "conclusion=failure") {
			t.Fatalf("diagnosis context missing CI failure details:\n%s", intent.Context)
		}
	default:
		t.Fatal("workflow failure should enqueue diagnosis intent")
	}
	failureRuntime, ok, err := agent.LoadRuntimeMeta(failurePath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta workflow failure: err=%v ok=%v", err, ok)
	}
	if failureRuntime.Wait.Kind != "event" || failureRuntime.Wait.EventConclusion != "success" {
		t.Fatalf("failure diagnosis should preserve CI wait: %+v", failureRuntime.Wait)
	}
	if got := countTimelineEvents(t, failurePath, "wait_event_failure_detected"); got != 1 {
		t.Fatalf("failure diagnosis should record wait_event_failure_detected once, got %d", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.runIntentWorker(ctx)

	workflowSuccessPayload := `{"session_id":"ci-workflow-success-e2e","action":"completed","repository":{"full_name":"example/repo"},"workflow_run":{"status":"completed","conclusion":"success","pull_requests":[{"number":42}],"head_branch":"feature/ci-watch"}}`
	resp = postSignedGitHubWebhook(t, d, webhookKey, "workflow_run", "delivery-workflow-success", workflowSuccessPayload)
	if resp["status"] != "queued" {
		t.Fatalf("workflow success status = %v, want queued", resp["status"])
	}
	waitForTimelineCount(t, workflowPath, "goal_continuation_complete", 1)
	if prov.calls != 1 {
		t.Fatalf("workflow success should call provider once, got %d", prov.calls)
	}
	workflowRuntime, ok, err := agent.LoadRuntimeMeta(workflowPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta workflow success: err=%v ok=%v", err, ok)
	}
	if workflowRuntime.Wait.Kind != "" || workflowRuntime.Goal.Status != control.GoalStatusComplete {
		t.Fatalf("workflow success should clear wait and complete goal: goal=%+v wait=%+v", workflowRuntime.Goal, workflowRuntime.Wait)
	}

	resp = postSignedGitHubWebhook(t, d, webhookKey, "workflow_run", "delivery-workflow-success-replay", workflowSuccessPayload)
	if resp["status"] != "duplicate" {
		t.Fatalf("workflow success replay status = %v, want duplicate", resp["status"])
	}
	time.Sleep(50 * time.Millisecond)
	if prov.calls != 1 {
		t.Fatalf("duplicate workflow success should not execute again, got provider calls=%d", prov.calls)
	}
	if got := countTimelineEvents(t, workflowPath, "run_started"); got != 1 {
		t.Fatalf("workflow run should start exactly once for success, got %d", got)
	}

	checkSuitePayload := `{"session_id":"ci-check-suite-e2e","action":"completed","repository":{"full_name":"example/repo"},"check_suite":{"status":"completed","conclusion":"success","pull_requests":[{"number":42}],"head_branch":"feature/ci-watch"}}`
	resp = postSignedGitHubWebhook(t, d, webhookKey, "check_suite", "delivery-check-suite-success", checkSuitePayload)
	if resp["status"] != "queued" {
		t.Fatalf("check_suite success status = %v, want queued", resp["status"])
	}
	waitForTimelineCount(t, checkSuitePath, "goal_continuation_complete", 1)

	statusPayload := `{"session_id":"ci-status-e2e","state":"success","context":"ci/build","repository":{"full_name":"example/repo"},"branches":[{"name":"feature/ci-watch"}]}`
	resp = postSignedGitHubWebhook(t, d, webhookKey, "status", "delivery-status-success", statusPayload)
	if resp["status"] != "queued" {
		t.Fatalf("status success status = %v, want queued", resp["status"])
	}
	waitForTimelineCount(t, statusPath, "goal_continuation_complete", 1)
	if prov.calls != 3 {
		t.Fatalf("workflow success, check_suite, and status should call provider three times, got %d", prov.calls)
	}
}

func TestDaemonReleaseAssistantE2EDebouncesAndQueuesApproval(t *testing.T) {
	dir := t.TempDir()
	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("MkdirAll workspace: %v", err)
	}
	changelogPath := filepath.Join(workspace, "CHANGELOG.md")
	versionPath := filepath.Join(workspace, "package.json")
	base := time.Now().Add(-10 * time.Minute)
	for path, body := range map[string]string{
		changelogPath: "# Changelog\n\n## 1.2.2\n- Previous release\n",
		versionPath:   `{"version":"1.2.2"}` + "\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("WriteFile baseline %s: %v", filepath.Base(path), err)
		}
		if err := os.Chtimes(path, base, base); err != nil {
			t.Fatalf("Chtimes baseline %s: %v", filepath.Base(path), err)
		}
	}

	sessionPath := writeReleaseAssistantE2ESession(t, dir, workspace)
	writer := &daemonReleaseWriter{executed: make(chan string, 1)}
	prov := &daemonScriptedProvider{turns: [][]provider.Chunk{{
		{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 17, CompletionTokens: 5, TotalTokens: 22, FinishReason: "tool_calls"}},
		{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID:        "release-write-1",
			Name:      "publish_release",
			Arguments: `{"path":"releases/v1.2.3"}`,
		}},
	}}}
	d := New(Options{
		SessionDir:        dir,
		MaxConcurrentRuns: 1,
		ControllerFactory: func(ctx context.Context, d *Daemon, entry *SessionEntry, sink event.Sink) (*control.Controller, error) {
			loaded, err := agent.LoadSession(entry.Path)
			if err != nil {
				return nil, err
			}
			reg := tool.NewRegistry()
			reg.Add(writer)
			ag := agent.New(prov, reg, loaded, agent.Options{}, sink)
			c := control.New(control.Options{
				Runner:      ag,
				Executor:    ag,
				Policy:      permission.New("ask", nil, nil, nil),
				SessionPath: entry.Path,
				SessionDir:  dir,
				Sink:        sink,
			})
			c.EnableInteractiveApproval()
			c.Resume(loaded, entry.Path)
			return c, nil
		},
	})
	d.scanSessions()
	d.fileWatcher = NewFileWatcher(d, d.logger)

	body := `{"session_id":"release-assistant-e2e","paths":["CHANGELOG.md","package.json"],"debounce":"1s","reason":"release files changed; check changelog and version before publishing","subject":"v1.2.3"}`
	rr := httptest.NewRecorder()
	d.handleWaitFile(rr, httptest.NewRequest("POST", "/wait-file", strings.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("wait-file status = %d body=%s", rr.Code, rr.Body.String())
	}

	d.fileWatcher.poll()
	if err := os.WriteFile(changelogPath, []byte("# Changelog\n\n## 1.2.3\n- Ship release assistant\n"), 0o644); err != nil {
		t.Fatalf("WriteFile changelog update: %v", err)
	}
	if err := os.Chtimes(changelogPath, base.Add(time.Minute), base.Add(time.Minute)); err != nil {
		t.Fatalf("Chtimes changelog update: %v", err)
	}
	d.fileWatcher.poll()
	if err := os.WriteFile(versionPath, []byte(`{"version":"1.2.3"}`+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile version update: %v", err)
	}
	if err := os.Chtimes(versionPath, base.Add(2*time.Minute), base.Add(2*time.Minute)); err != nil {
		t.Fatalf("Chtimes version update: %v", err)
	}
	d.fileWatcher.poll()
	d.fileWatcher.mu.Lock()
	state := d.fileWatcher.watches["release-assistant-e2e"]
	if state == nil {
		d.fileWatcher.mu.Unlock()
		t.Fatal("release assistant file watch missing before debounce fires")
	}
	if len(state.changes) != 2 {
		d.fileWatcher.mu.Unlock()
		t.Fatalf("release assistant pending changes = %d, want 2: %+v", len(state.changes), state.changes)
	}
	state.timer = time.Now().Add(-time.Second)
	d.fileWatcher.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer func() {
		cancel()
		waitForInactiveRun(d, "release-assistant-e2e")
	}()
	go d.runIntentWorker(ctx)
	d.fileWatcher.poll()
	waitForTimelineCount(t, sessionPath, "wait_started", 1)
	waitForTimelineCount(t, sessionPath, "file_change_detected", 1)
	waitForTimelineCount(t, sessionPath, "wait_started", 2)

	if prov.calls != 1 {
		t.Fatalf("release assistant should call provider once after debounced file changes, got %d", prov.calls)
	}
	if got := countTimelineEvents(t, sessionPath, "intent_queued"); got != 1 {
		t.Fatalf("release assistant should enqueue exactly one intent, got %d", got)
	}
	if got := countTimelineEvents(t, sessionPath, "file_change_detected"); got != 1 {
		t.Fatalf("release assistant should record exactly one file_change_detected event, got %d", got)
	}
	loaded, ok, err := agent.LoadRuntimeMeta(sessionPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta release assistant: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != agent.RunStatusWaitingApproval || loaded.Wait.Kind != "approval" ||
		loaded.Wait.Tool != "publish_release" || !strings.Contains(loaded.Wait.Subject, "v1.2.3") {
		t.Fatalf("release publish action should wait for approval: run=%+v wait=%+v", loaded.Run, loaded.Wait)
	}
	d.fileWatcher.mu.Lock()
	registered := d.fileWatcher.watches["release-assistant-e2e"]
	d.fileWatcher.mu.Unlock()
	if registered != nil {
		t.Fatalf("matching wait-file should unregister one-shot watcher: %+v", registered)
	}

	req := httptest.NewRequest("GET", "/approvals", nil)
	rr = httptest.NewRecorder()
	d.handleApprovals(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("approvals status = %d body=%s", rr.Code, rr.Body.String())
	}
	var desk ApprovalDeskResponse
	if err := json.NewDecoder(rr.Body).Decode(&desk); err != nil {
		t.Fatalf("decode approvals: %v", err)
	}
	if len(desk.Items) != 1 {
		t.Fatalf("approval desk items = %d, want 1: %+v", len(desk.Items), desk.Items)
	}
	item := desk.Items[0]
	if item.SessionID != "release-assistant-e2e" || item.Kind != "approval" || item.Tool != "publish_release" ||
		!strings.Contains(item.Subject, "v1.2.3") || !item.Active || item.RunStatus != agent.RunStatusWaitingApproval ||
		item.GoalText != "prepare release v1.2.3" {
		t.Fatalf("unexpected release approval desk item: %+v", item)
	}
	select {
	case target := <-writer.executed:
		t.Fatalf("release writer executed before approval: %s", target)
	default:
	}
}

func TestDaemonIntentQueueSelectsHighestPriorityRunnable(t *testing.T) {
	d := New(Options{SessionDir: t.TempDir()})
	now := time.Now().UTC()
	pending := []RunIntent{
		{SessionID: "low", Priority: 10, CreatedAt: now},
		{SessionID: "high", Priority: 90, CreatedAt: now.Add(time.Millisecond)},
		{SessionID: "same-high", Priority: 90, CreatedAt: now.Add(2 * time.Millisecond)},
	}
	if got := d.nextRunnableIntentIndex(pending, map[string]struct{}{"same-high": {}}); got != 1 {
		t.Fatalf("nextRunnableIntentIndex = %d, want high-priority runnable index 1", got)
	}
	pending[0].Priority = 90
	if got := d.nextRunnableIntentIndex(pending[:2], nil); got != 0 {
		t.Fatalf("same priority should preserve FIFO order, got index %d", got)
	}
}

func TestDaemonIntentWorkerSerializesSameSessionQueue(t *testing.T) {
	dir := t.TempDir()
	sessPath := filepath.Join(dir, "queue-session.jsonl")
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "start"})
	if err := sess.Save(sessPath); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := agent.SaveRuntimeMeta(sessPath, agent.RuntimeMeta{
		SessionID: "queue-session",
		Goal:      agent.RuntimeGoalMeta{Text: "finish queue", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}

	prov := &daemonBlockingProvider{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	d := New(Options{
		SessionDir:        dir,
		MaxConcurrentRuns: 2,
		ControllerFactory: func(ctx context.Context, d *Daemon, entry *SessionEntry, sink event.Sink) (*control.Controller, error) {
			loaded, err := agent.LoadSession(entry.Path)
			if err != nil {
				return nil, err
			}
			ag := agent.New(prov, tool.NewRegistry(), loaded, agent.Options{}, sink)
			c := control.New(control.Options{
				Runner:      ag,
				Executor:    ag,
				SessionPath: entry.Path,
				SessionDir:  dir,
				Sink:        sink,
			})
			c.Resume(loaded, entry.Path)
			return c, nil
		},
	})
	d.scanSessions()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.runIntentWorker(ctx)

	d.enqueueIntent(RunIntent{SessionID: "queue-session", SessionPath: sessPath, Source: "test", Reason: "first", Priority: 10})
	d.enqueueIntent(RunIntent{SessionID: "queue-session", SessionPath: sessPath, Source: "test", Reason: "second", Priority: 10})

	select {
	case <-prov.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first queued intent did not start")
	}
	time.Sleep(50 * time.Millisecond)
	if got := countTimelineEvents(t, sessPath, "run_started"); got != 1 {
		t.Fatalf("same-session second intent started while first was active; run_started=%d", got)
	}

	close(prov.release)
	waitForTimelineCount(t, sessPath, "run_started", 2)
	if prov.calls != 1 {
		t.Fatalf("second intent should start after the first but not call provider after goal completion; provider calls=%d", prov.calls)
	}
}

func TestDaemonIntentWorkerRespectsPriorityAndGlobalConcurrency(t *testing.T) {
	dir := t.TempDir()
	highPath := writeDaemonQueueSession(t, dir, "queue-high")
	lowPath := writeDaemonQueueSession(t, dir, "queue-low")

	prov := &daemonBlockingProvider{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	d := New(Options{
		SessionDir:        dir,
		MaxConcurrentRuns: 1,
		ControllerFactory: func(ctx context.Context, d *Daemon, entry *SessionEntry, sink event.Sink) (*control.Controller, error) {
			loaded, err := agent.LoadSession(entry.Path)
			if err != nil {
				return nil, err
			}
			ag := agent.New(prov, tool.NewRegistry(), loaded, agent.Options{}, sink)
			c := control.New(control.Options{
				Runner:      ag,
				Executor:    ag,
				SessionPath: entry.Path,
				SessionDir:  dir,
				Sink:        sink,
			})
			c.Resume(loaded, entry.Path)
			return c, nil
		},
	})
	d.scanSessions()
	d.enqueueIntent(RunIntent{SessionID: "queue-low", SessionPath: lowPath, Source: "cron", Reason: "low", Priority: 10, CreatedAt: time.Now().UTC()})
	d.enqueueIntent(RunIntent{SessionID: "queue-high", SessionPath: highPath, Source: "api", Reason: "high", Priority: 100, CreatedAt: time.Now().UTC().Add(time.Millisecond)})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.runIntentWorker(ctx)

	select {
	case <-prov.started:
	case <-time.After(2 * time.Second):
		t.Fatal("queued high-priority intent did not start")
	}
	if got := countTimelineEvents(t, highPath, "run_started"); got != 1 {
		t.Fatalf("high-priority intent should start first, got high run_started=%d", got)
	}
	if got := countTimelineEvents(t, lowPath, "run_started"); got != 0 {
		t.Fatalf("low-priority intent should wait behind global concurrency limit, got low run_started=%d", got)
	}

	close(prov.release)
	waitForTimelineCount(t, lowPath, "run_started", 1)
}

func TestDaemonRecordWaitAndApproveClearsRuntime(t *testing.T) {
	dir := t.TempDir()
	sessPath := filepath.Join(dir, "approval-test.jsonl")
	os.WriteFile(sessPath, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644)
	if err := agent.SaveRuntimeMeta(sessPath, agent.RuntimeMeta{
		SessionID: "approval-test",
		Goal:      agent.RuntimeGoalMeta{Text: "needs approval", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "running"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}
	d := New(Options{SessionDir: dir})
	d.scanSessions()
	ctrl := control.New(control.Options{Sink: event.Discard})
	d.mu.Lock()
	d.activeRuns["approval-test"] = &ActiveRun{
		Control:   ctrl,
		Approvals: map[string]event.Approval{"42": {ID: "42", Tool: "bash", Subject: "go test ./..."}},
		Asks:      map[string]event.Ask{},
	}
	d.mu.Unlock()
	d.recordWait("approval-test", agent.RuntimeWaitMeta{
		Kind:       "approval",
		Reason:     "approval required",
		ApprovalID: "42",
		Tool:       "bash",
		Subject:    "go test ./...",
		Since:      time.Now().UTC(),
	}, event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "42", Tool: "bash", Subject: "go test ./..."}})

	loaded, ok, err := agent.LoadRuntimeMeta(sessPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "waiting_approval" || loaded.Wait.ApprovalID != "42" {
		t.Fatalf("wait state not persisted: run=%q wait=%+v", loaded.Run.Status, loaded.Wait)
	}

	req, _ := http.NewRequest("POST", "/approvals/approve", strings.NewReader(`{"session_id":"approval-test","approval_id":"42"}`))
	rr := httptest.NewRecorder()
	d.handleApprove(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("approve status = %d body=%s", rr.Code, rr.Body.String())
	}
	loaded, _, _ = agent.LoadRuntimeMeta(sessPath)
	if loaded.Run.Status != "running" || loaded.Wait.Kind != "" {
		t.Fatalf("approval should clear wait state: run=%q wait=%+v", loaded.Run.Status, loaded.Wait)
	}
}

func TestDaemonRecordAskAndAnswerClearsRuntime(t *testing.T) {
	dir := t.TempDir()
	sessPath := filepath.Join(dir, "ask-test.jsonl")
	os.WriteFile(sessPath, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644)
	if err := agent.SaveRuntimeMeta(sessPath, agent.RuntimeMeta{
		SessionID: "ask-test",
		Goal:      agent.RuntimeGoalMeta{Text: "needs answer", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "running"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta: %v", err)
	}
	d := New(Options{SessionDir: dir})
	d.scanSessions()
	ctrl := control.New(control.Options{Sink: event.Discard})
	ask := event.Ask{ID: "ask-1", Questions: []event.AskQuestion{{ID: "q1", Prompt: "Ship?"}}}
	d.mu.Lock()
	d.activeRuns["ask-test"] = &ActiveRun{
		Control:   ctrl,
		Approvals: map[string]event.Approval{},
		Asks:      map[string]event.Ask{"ask-1": ask},
	}
	d.mu.Unlock()
	d.recordWait("ask-test", agent.RuntimeWaitMeta{
		Kind:   "ask",
		Reason: "user answer required",
		AskID:  "ask-1",
		Since:  time.Now().UTC(),
	}, event.Event{Kind: event.AskRequest, Ask: ask})

	loaded, ok, err := agent.LoadRuntimeMeta(sessPath)
	if err != nil || !ok {
		t.Fatalf("LoadRuntimeMeta: err=%v ok=%v", err, ok)
	}
	if loaded.Run.Status != "waiting_ask" || loaded.Wait.AskID != "ask-1" {
		t.Fatalf("ask wait state not persisted: run=%q wait=%+v", loaded.Run.Status, loaded.Wait)
	}

	req, _ := http.NewRequest("POST", "/asks/answer", strings.NewReader(`{"session_id":"ask-test","ask_id":"ask-1","selected":"Yes"}`))
	rr := httptest.NewRecorder()
	d.handleAnswer(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("answer status = %d body=%s", rr.Code, rr.Body.String())
	}
	loaded, _, _ = agent.LoadRuntimeMeta(sessPath)
	if loaded.Run.Status != "running" || loaded.Wait.Kind != "" {
		t.Fatalf("answer should clear wait state: run=%q wait=%+v", loaded.Run.Status, loaded.Wait)
	}
}

func TestDaemonApprovalDeskListsActiveAndDormantWaits(t *testing.T) {
	dir := t.TempDir()
	activeApprovalPath := filepath.Join(dir, "active-approval.jsonl")
	activeAskPath := filepath.Join(dir, "active-ask.jsonl")
	dormantAskPath := filepath.Join(dir, "dormant-ask.jsonl")
	for _, path := range []string{activeApprovalPath, activeAskPath, dormantAskPath} {
		if err := os.WriteFile(path, []byte(`{"role":"user","content":"start"}`+"\n"), 0o644); err != nil {
			t.Fatalf("write session: %v", err)
		}
	}
	since := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)
	if err := agent.SaveRuntimeMeta(activeApprovalPath, agent.RuntimeMeta{
		SessionID: "active-approval",
		Goal:      agent.RuntimeGoalMeta{Text: "ship release", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: agent.RunStatusWaitingApproval},
		Wait: agent.RuntimeWaitMeta{
			Kind:       "approval",
			Reason:     "approval required",
			ApprovalID: "approval-1",
			Tool:       "shell",
			Subject:    "git push",
			Since:      since,
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta active approval: %v", err)
	}
	if err := agent.SaveRuntimeMeta(activeAskPath, agent.RuntimeMeta{
		SessionID: "active-ask",
		Goal:      agent.RuntimeGoalMeta{Text: "choose channel", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: agent.RunStatusWaitingAsk},
		Wait: agent.RuntimeWaitMeta{
			Kind:   "ask",
			Reason: "user answer required",
			AskID:  "ask-1",
			Since:  since.Add(time.Minute),
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta active ask: %v", err)
	}
	if err := agent.SaveRuntimeMeta(dormantAskPath, agent.RuntimeMeta{
		SessionID: "dormant-ask",
		Goal:      agent.RuntimeGoalMeta{Text: "recover prompt", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: agent.RunStatusWaitingAsk},
		Wait: agent.RuntimeWaitMeta{
			Kind:    "ask",
			Reason:  "user answer required",
			AskID:   "ask-2",
			Subject: "Which fix?",
			Since:   since.Add(2 * time.Minute),
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta dormant ask: %v", err)
	}

	d := New(Options{SessionDir: dir})
	d.scanSessions()
	d.mu.Lock()
	d.activeRuns["active-approval"] = &ActiveRun{
		Control:   control.New(control.Options{Sink: event.Discard}),
		Approvals: map[string]event.Approval{"approval-1": {ID: "approval-1", Tool: "shell", Subject: "git push"}},
		Asks:      map[string]event.Ask{},
	}
	d.activeRuns["active-ask"] = &ActiveRun{
		Control:   control.New(control.Options{Sink: event.Discard}),
		Approvals: map[string]event.Approval{},
		Asks: map[string]event.Ask{"ask-1": {
			ID: "ask-1",
			Questions: []event.AskQuestion{{
				ID:     "q1",
				Prompt: "Release now?",
				Options: []event.AskOption{
					{Label: "yes", Description: "ship now"},
					{Label: "no"},
				},
			}},
		}},
	}
	d.mu.Unlock()

	req := httptest.NewRequest("GET", "/approvals", nil)
	rr := httptest.NewRecorder()
	d.handleApprovals(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("approvals status = %d body=%s", rr.Code, rr.Body.String())
	}
	var resp ApprovalDeskResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode approvals: %v", err)
	}
	if len(resp.Items) != 3 {
		t.Fatalf("items len = %d, want 3: %+v", len(resp.Items), resp.Items)
	}
	byKey := map[string]ApprovalDeskItem{}
	for _, item := range resp.Items {
		byKey[item.SessionID+"/"+item.Kind+"/"+item.ID] = item
	}
	approval := byKey["active-approval/approval/approval-1"]
	if !approval.Active || approval.Tool != "shell" || approval.Subject != "git push" || approval.GoalText != "ship release" {
		t.Fatalf("active approval item = %+v", approval)
	}
	ask := byKey["active-ask/ask/ask-1"]
	if !ask.Active || len(ask.Questions) != 1 || ask.Questions[0].Prompt != "Release now?" ||
		len(ask.Questions[0].Options) != 2 || ask.Questions[0].Options[0].Description != "ship now" {
		t.Fatalf("active ask item = %+v", ask)
	}
	dormant := byKey["dormant-ask/ask/ask-2"]
	if dormant.Active || dormant.Subject != "Which fix?" || dormant.Reason != "user answer required" {
		t.Fatalf("dormant ask item = %+v", dormant)
	}
}

func TestDaemonStaleLock(t *testing.T) {
	dir := t.TempDir()
	d := New(Options{SessionDir: dir})

	lockPath := d.lockFile()
	os.MkdirAll(filepath.Dir(lockPath), 0o755)
	os.WriteFile(lockPath, []byte("99999999\n"), 0o644)

	if err := d.acquireLock(); err != nil {
		t.Fatalf("should reclaim stale lock: %v", err)
	}
	d.releaseLock()
}

func TestDaemonAuthMiddlewareRequiresToken(t *testing.T) {
	d := New(Options{SessionDir: t.TempDir(), Token: "secret-token"})
	handler := d.withAuth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest("GET", "/sessions", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want 401", rr.Code)
	}

	req = httptest.NewRequest("GET", "/sessions", nil)
	req.Header.Set("X-Reasonix-Daemon-Token", "secret-token")
	rr = httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("authorized status = %d, want 204", rr.Code)
	}
}

func TestRotateTokenWritesFreshToken(t *testing.T) {
	dir := t.TempDir()
	path := TokenFile(dir)
	if err := os.WriteFile(path, []byte("old-token\n"), 0o600); err != nil {
		t.Fatalf("WriteFile old token: %v", err)
	}

	token, err := RotateToken(dir)
	if err != nil {
		t.Fatalf("RotateToken: %v", err)
	}
	if len(token) != 64 {
		t.Fatalf("token length = %d, want 64", len(token))
	}
	for _, r := range token {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("token contains non-hex rune %q", r)
		}
	}
	if token == "old-token" {
		t.Fatal("token should change")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile token: %v", err)
	}
	if got := strings.TrimSpace(string(b)); got != token {
		t.Fatal("stored token does not match generated token")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat token: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("token file mode = %v, want 0600", mode)
	}
}

func countTimelineEvents(t *testing.T, sessionPath, eventType string) int {
	t.Helper()
	events, ok, err := agent.LoadRuntimeTimeline(sessionPath, 0)
	if err != nil {
		t.Fatalf("LoadRuntimeTimeline: %v", err)
	}
	if !ok {
		return 0
	}
	n := 0
	for _, event := range events {
		if event.Type == eventType {
			n++
		}
	}
	return n
}

func waitForTimelineCount(t *testing.T, sessionPath, eventType string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := countTimelineEvents(t, sessionPath, eventType); got >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d %q timeline events; got %d", want, eventType, countTimelineEvents(t, sessionPath, eventType))
}

func waitForInactiveRun(d *Daemon, sessionID string) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		d.mu.RLock()
		_, active := d.activeRuns[sessionID]
		d.mu.RUnlock()
		if !active {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func writeDaemonQueueSession(t *testing.T, dir, id string) string {
	t.Helper()
	sessionPath := filepath.Join(dir, id+".jsonl")
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "start"})
	if err := sess.Save(sessionPath); err != nil {
		t.Fatalf("Save(%s): %v", id, err)
	}
	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		SessionID: id,
		Goal:      agent.RuntimeGoalMeta{Text: "finish " + id, Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "idle"},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta(%s): %v", id, err)
	}
	return sessionPath
}

func writeDailyTriageE2ESession(t *testing.T, dir, id string, due time.Time, budget agent.RuntimeBudgetMeta) string {
	t.Helper()
	sessionPath := filepath.Join(dir, id+".jsonl")
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "start daily triage"})
	if err := sess.Save(sessionPath); err != nil {
		t.Fatalf("Save(%s): %v", id, err)
	}
	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		SessionID: id,
		Model:     "daemon-test-model",
		Goal:      agent.RuntimeGoalMeta{Text: "daily PR and issue triage", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: agent.RunStatusIdle},
		Scheduler: agent.RuntimeSchedMeta{
			Enabled:      true,
			DailyAt:      "09:00",
			Timezone:     "UTC",
			NextWakeupAt: due,
		},
		Budget: budget,
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta(%s): %v", id, err)
	}
	return sessionPath
}

func writeCIWatchE2ESession(t *testing.T, dir, id, source string) string {
	t.Helper()
	sessionPath := filepath.Join(dir, id+".jsonl")
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "wait for CI"})
	if err := sess.Save(sessionPath); err != nil {
		t.Fatalf("Save(%s): %v", id, err)
	}
	eventStatus := "completed"
	if source == "github.status" {
		eventStatus = ""
	}
	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		SessionID: id,
		Model:     "daemon-test-model",
		Goal:      agent.RuntimeGoalMeta{Text: "continue after successful CI", Status: control.GoalStatusRunning},
		Run:       agent.RuntimeRunMeta{Status: "waiting_event"},
		Wait: agent.RuntimeWaitMeta{
			Kind:            "event",
			EventSource:     source,
			EventStatus:     eventStatus,
			EventConclusion: "success",
			Reason:          "waiting for CI success",
			Subject:         "PR #42",
			Since:           time.Date(2026, 6, 13, 8, 0, 0, 0, time.UTC),
		},
		Budget: agent.RuntimeBudgetMeta{
			DailyWakeupLimit:    8,
			DailyModelCallLimit: 8,
			MaxGoalAutoTurns:    2,
			WindowStartedAt:     budgetWindowStart(time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC)),
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta(%s): %v", id, err)
	}
	return sessionPath
}

func writeReleaseAssistantE2ESession(t *testing.T, dir, workspace string) string {
	t.Helper()
	id := "release-assistant-e2e"
	sessionPath := filepath.Join(dir, id+".jsonl")
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "prepare release v1.2.3"})
	if err := sess.Save(sessionPath); err != nil {
		t.Fatalf("Save(%s): %v", id, err)
	}
	if err := agent.SaveRuntimeMeta(sessionPath, agent.RuntimeMeta{
		SessionID:     id,
		Model:         "daemon-test-model",
		WorkspaceRoot: workspace,
		Goal:          agent.RuntimeGoalMeta{Text: "prepare release v1.2.3", Status: control.GoalStatusRunning},
		Run:           agent.RuntimeRunMeta{Status: agent.RunStatusIdle},
		Budget: agent.RuntimeBudgetMeta{
			DailyWakeupLimit:    8,
			DailyModelCallLimit: 8,
			MaxGoalAutoTurns:    2,
			WindowStartedAt:     budgetWindowStart(time.Date(2026, 6, 13, 9, 0, 0, 0, time.UTC)),
		},
	}); err != nil {
		t.Fatalf("SaveRuntimeMeta(%s): %v", id, err)
	}
	return sessionPath
}

type daemonReleaseWriter struct {
	executed chan string
}

func (w *daemonReleaseWriter) Name() string { return "publish_release" }

func (w *daemonReleaseWriter) Description() string {
	return "publish a release after changelog and version updates"
}

func (w *daemonReleaseWriter) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"target":{"type":"string"}},"required":["path"]}`)
}

func (w *daemonReleaseWriter) ReadOnly() bool { return false }

func (w *daemonReleaseWriter) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Target string `json:"target"`
		Path   string `json:"path"`
	}
	_ = json.Unmarshal(args, &in)
	target := firstNonEmpty(in.Target, in.Path)
	select {
	case w.executed <- target:
	default:
	}
	return "published " + target, nil
}

func postSignedGitHubWebhook(t *testing.T, d *Daemon, webhookKey, eventName, delivery, payload string) map[string]any {
	t.Helper()
	sig := computeHMAC([]byte(payload), webhookKey)
	req := httptest.NewRequest("POST", "/webhook", strings.NewReader(payload))
	req.Header.Set("X-Webhook-Signature", sig)
	req.Header.Set("X-GitHub-Event", eventName)
	req.Header.Set("X-GitHub-Delivery", delivery)
	rr := httptest.NewRecorder()
	d.handleWebhook(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("webhook %s/%s status = %d body=%s", eventName, delivery, rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode webhook %s/%s response: %v", eventName, delivery, err)
	}
	return resp
}
