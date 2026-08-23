package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
	fileencoding "reasonix/internal/fileutil/encoding"
)

func TestHeartbeatConfigPathUsesReasonixUserStateDir(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	want := filepath.Join(config.MemoryUserDir(), "heartbeat-tasks.json")

	if got := engine.configPath(); got != want {
		t.Fatalf("configPath = %q, want %q", got, want)
	}
}

func TestHeartbeatConfigRevisionKeepsLegacyFilesReadable(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	legacy := `{"tasks":[{"id":"legacy","title":"Legacy","interval":"1h","enabled":false}]}`
	if err := os.MkdirAll(filepath.Dir(engine.configPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine.configPath(), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	tasks := engine.ReloadTasks()
	if len(tasks) != 1 || tasks[0].ID != "legacy" {
		t.Fatalf("legacy tasks = %+v, want one readable task", tasks)
	}
	if engine.cfgRevision != 0 {
		t.Fatalf("legacy revision = %d, want zero", engine.cfgRevision)
	}
	if err := engine.ReplaceTasks(tasks); err != nil {
		t.Fatalf("upgrade save: %v", err)
	}
	data, err := os.ReadFile(engine.configPath())
	if err != nil {
		t.Fatal(err)
	}
	var cfg heartbeatConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Revision != 1 || len(cfg.Tasks) != 1 {
		t.Fatalf("upgraded config = %+v, want revision 1 with legacy task", cfg)
	}
	var previousReader struct {
		Tasks []HeartbeatTask `json:"tasks"`
	}
	if err := json.Unmarshal(data, &previousReader); err != nil || len(previousReader.Tasks) != 1 {
		t.Fatalf("previous reader could not ignore revision: tasks=%+v err=%v", previousReader.Tasks, err)
	}
}

func TestHeartbeatReplaceTasksRejectsStaleRevision(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	initial := []HeartbeatTask{{ID: "same", Title: "initial", Interval: "1h", Enabled: false}}
	if err := engine.saveTasks(initial); err != nil {
		t.Fatal(err)
	}
	engine.ReloadTasks()
	external := []HeartbeatTask{{ID: "same", Title: "edited externally", Interval: "2h", Enabled: false}}
	if err := engine.saveTasks(external); err != nil {
		t.Fatal(err)
	}
	err := engine.ReplaceTasks([]HeartbeatTask{{ID: "same", Title: "stale UI edit", Interval: "3h", Enabled: false}})
	if !errors.Is(err, ErrHeartbeatConfigConflict) {
		t.Fatalf("ReplaceTasks error = %v, want config conflict", err)
	}
	onDisk := engine.loadTasks()
	if len(onDisk) != 1 || onDisk[0].Title != "edited externally" || onDisk[0].Interval != "2h" {
		t.Fatalf("stale replacement changed disk config: %+v", onDisk)
	}
	if got := engine.ListTasks()[0].Title; got != "initial" {
		t.Fatalf("stale replacement changed in-memory tasks: %q", got)
	}
}

func TestHeartbeatReplaceConfigRejectsSameRevisionExternalEditByETag(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	initial := []HeartbeatTask{{ID: "same", Title: "initial", Interval: "1h", Enabled: false}}
	if err := engine.saveTasks(initial); err != nil {
		t.Fatal(err)
	}
	loaded := engine.ReloadConfig()
	external := heartbeatConfig{Revision: loaded.Revision, Tasks: []HeartbeatTask{{ID: "same", Title: "edited externally", Interval: "2h", Enabled: false}}}
	data, err := json.MarshalIndent(external, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine.configPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = engine.ReplaceConfig(HeartbeatConfigUpdate{
		Revision: loaded.Revision,
		ETag:     loaded.ETag,
		Tasks:    []HeartbeatTask{{ID: "same", Title: "stale UI edit", Interval: "3h", Enabled: false}},
	})
	if !errors.Is(err, ErrHeartbeatConfigConflict) {
		t.Fatalf("ReplaceConfig error = %v, want config conflict", err)
	}
	onDisk := engine.loadTasks()
	if len(onDisk) != 1 || onDisk[0].Title != "edited externally" {
		t.Fatalf("same-revision external edit was overwritten: %+v", onDisk)
	}
}

func TestHeartbeatLoadTasksDecodesGB18030Config(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	body := `{"tasks":[{"id":"daily","title":"每日检查","prompt":"总结中文状态","interval":"1h","enabled":true}]}`
	if err := os.MkdirAll(filepath.Dir(engine.configPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine.configPath(), fileencoding.Encode(body, fileencoding.GB18030), 0o644); err != nil {
		t.Fatal(err)
	}

	tasks := engine.loadTasks()
	if len(tasks) != 1 || tasks[0].Title != "每日检查" || tasks[0].Prompt != "总结中文状态" {
		t.Fatalf("loadTasks = %+v, want decoded Chinese task", tasks)
	}
}

func TestHeartbeatTaskDueAtWaitsForDailySchedule(t *testing.T) {
	loc := time.FixedZone("test", 8*60*60)
	created := time.Date(2026, 6, 18, 8, 30, 0, 0, loc)
	task := HeartbeatTask{
		ID:        "daily",
		Interval:  "24h|daily@09:00",
		Enabled:   true,
		CreatedAt: created.UnixMilli(),
	}

	if heartbeatTaskDueAt(task, time.Date(2026, 6, 18, 8, 59, 0, 0, loc)) {
		t.Fatal("daily task should wait for the configured clock time")
	}
	if !heartbeatTaskDueAt(task, time.Date(2026, 6, 18, 9, 0, 0, 0, loc)) {
		t.Fatal("daily task should be due at the configured clock time")
	}

	task.LastRunAt = time.Date(2026, 6, 18, 9, 0, 0, 0, loc).UnixMilli()
	if heartbeatTaskDueAt(task, time.Date(2026, 6, 18, 10, 0, 0, 0, loc)) {
		t.Fatal("daily task should not run twice for the same scheduled occurrence")
	}
	if !heartbeatTaskDueAt(task, time.Date(2026, 6, 19, 9, 0, 0, 0, loc)) {
		t.Fatal("daily task should be due at the next scheduled occurrence")
	}
}

func TestHeartbeatTaskDueAtCronExpression(t *testing.T) {
	loc := time.FixedZone("test", 8*60*60)
	task := HeartbeatTask{
		ID:        "cron",
		Interval:  "0 9 * * 1-5", // weekdays at 09:00
		Enabled:   true,
		CreatedAt: time.Date(2026, 6, 15, 0, 0, 0, 0, loc).UnixMilli(), // Monday
	}

	// Not due outside the cron window (Monday 08:59).
	if heartbeatTaskDueAt(task, time.Date(2026, 6, 15, 8, 59, 0, 0, loc)) {
		t.Fatal("cron task should wait for the configured time")
	}
	// Due exactly at Monday 09:00.
	if !heartbeatTaskDueAt(task, time.Date(2026, 6, 15, 9, 0, 0, 0, loc)) {
		t.Fatal("cron task should be due at the configured time")
	}
	// Not due again within the same minute after running.
	task.LastRunAt = time.Date(2026, 6, 15, 9, 0, 0, 0, loc).UnixMilli()
	if heartbeatTaskDueAt(task, time.Date(2026, 6, 15, 9, 0, 30, 0, loc)) {
		t.Fatal("cron task should not fire twice for the same occurrence")
	}
	// Due again on the next weekday.
	if !heartbeatTaskDueAt(task, time.Date(2026, 6, 16, 9, 0, 0, 0, loc)) {
		t.Fatal("cron task should be due at the next weekday occurrence")
	}
	// Weekend (Saturday) is not part of 1-5.
	if heartbeatTaskDueAt(task, time.Date(2026, 6, 20, 9, 0, 0, 0, loc)) {
		t.Fatal("cron task should skip weekends")
	}
}

func TestHeartbeatTaskDueAtCronEvery15Minutes(t *testing.T) {
	loc := time.FixedZone("test", 8*60*60)
	task := HeartbeatTask{
		ID:        "cron-15",
		Interval:  "*/15 * * * *",
		Enabled:   true,
		CreatedAt: time.Date(2026, 6, 18, 0, 0, 0, 0, loc).UnixMilli(),
	}

	for _, tt := range []struct {
		at   time.Time
		want bool
	}{
		{time.Date(2026, 6, 18, 10, 7, 0, 0, loc), false},
		{time.Date(2026, 6, 18, 10, 15, 0, 0, loc), true},
		{time.Date(2026, 6, 18, 10, 30, 0, 0, loc), true},
		{time.Date(2026, 6, 18, 10, 31, 0, 0, loc), false},
	} {
		if got := heartbeatTaskDueAt(task, tt.at); got != tt.want {
			t.Fatalf("cron */15 due at %v = %v, want %v", tt.at, got, tt.want)
		}
	}
}

func TestHeartbeatTaskDueAtCronDedupesByOccurrenceMinute(t *testing.T) {
	loc := time.UTC
	lastRun := time.Date(2026, 6, 18, 9, 1, 41, 0, loc)
	task := HeartbeatTask{Interval: "* * * * *", LastRunAt: lastRun.UnixMilli()}

	if heartbeatTaskDueAt(task, time.Date(2026, 6, 18, 9, 1, 59, 0, loc)) {
		t.Fatal("cron task must not run twice in one occurrence minute")
	}
	if !heartbeatTaskDueAt(task, time.Date(2026, 6, 18, 9, 2, 10, 0, loc)) {
		t.Fatal("every-minute cron task must run in the next occurrence minute")
	}
}

func TestHeartbeatTaskDueAtHonorsWeeklySelection(t *testing.T) {
	loc := time.UTC
	task := HeartbeatTask{
		ID:        "weekly",
		Interval:  "168h|weekly:fri@09:00",
		Enabled:   true,
		CreatedAt: time.Date(2026, 6, 15, 8, 0, 0, 0, loc).UnixMilli(),
	}

	if heartbeatTaskDueAt(task, time.Date(2026, 6, 18, 12, 0, 0, 0, loc)) {
		t.Fatal("weekly task should not run before the selected weekday")
	}
	if !heartbeatTaskDueAt(task, time.Date(2026, 6, 19, 9, 0, 0, 0, loc)) {
		t.Fatal("weekly task should run on the selected weekday and time")
	}
}

type heartbeatStatusStub struct {
	status control.RuntimeStatus
}

func (s heartbeatStatusStub) RuntimeStatus() control.RuntimeStatus {
	return s.status
}

type heartbeatExecuteTaskCtrlStub struct {
	stubSessionAPI
	status       control.RuntimeStatus
	submitted    []string
	approvalMode string
}

type heartbeatSignalingCtrlStub struct {
	heartbeatExecuteTaskCtrlStub
	submittedSignal chan struct{}
}

func (s *heartbeatSignalingCtrlStub) SubmitUserTurn(input, display string) {
	s.heartbeatExecuteTaskCtrlStub.SubmitUserTurn(input, display)
	close(s.submittedSignal)
}

func (s *heartbeatExecuteTaskCtrlStub) RuntimeStatus() control.RuntimeStatus {
	return s.status
}

func (s *heartbeatExecuteTaskCtrlStub) SubmitUserTurn(input, display string) {
	s.submitted = append(s.submitted, input)
	s.status.Running = true
}

func (s *heartbeatExecuteTaskCtrlStub) SetToolApprovalMode(mode string) {
	s.approvalMode = mode
}

func (s *heartbeatExecuteTaskCtrlStub) PlanMode() bool {
	return false
}

func (s *heartbeatExecuteTaskCtrlStub) AutoApproveTools() bool {
	return false
}

func (s *heartbeatExecuteTaskCtrlStub) Goal() string {
	return ""
}

func (s *heartbeatExecuteTaskCtrlStub) GoalStatus() string {
	return control.GoalStatusStopped
}

func (s *heartbeatExecuteTaskCtrlStub) ToolApprovalMode() string {
	return s.approvalMode
}

func (s *heartbeatExecuteTaskCtrlStub) SetSessionPath(string) {}

func (s *heartbeatExecuteTaskCtrlStub) SessionPath() string {
	return ""
}

func (s *heartbeatExecuteTaskCtrlStub) SessionDir() string {
	return ""
}

func (s *heartbeatExecuteTaskCtrlStub) Close() {}

func TestHeartbeatControllerBusyIncludesPendingPrompt(t *testing.T) {
	if heartbeatControllerBusy(heartbeatStatusStub{status: control.RuntimeStatus{Running: false, PendingPrompt: false}}) {
		t.Fatal("idle controller should be available for heartbeat execution")
	}
	if !heartbeatControllerBusy(heartbeatStatusStub{status: control.RuntimeStatus{Running: true}}) {
		t.Fatal("running controller should be busy")
	}
	if !heartbeatControllerBusy(heartbeatStatusStub{status: control.RuntimeStatus{PendingPrompt: true}}) {
		t.Fatal("pending prompt should keep controller busy")
	}
}

func TestHeartbeatTaskExecutionReservationSerializesTriggers(t *testing.T) {
	engine := &HeartbeatEngine{}
	if !engine.claimTask("same") {
		t.Fatal("first task claim should succeed")
	}
	second := make(chan bool, 1)
	go func() { second <- engine.claimTask("same") }()
	if <-second {
		t.Fatal("overlapping task trigger should be rejected")
	}
	engine.releaseTask("same")
	if !engine.claimTask("same") {
		t.Fatal("task should be claimable after the owner releases it")
	}
	engine.releaseTask("same")
}

func TestHeartbeatExecuteTaskPersistsFreshConversationTopicID(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	app.runtimeEvents.emit = func(context.Context, string, ...any) {}
	engine := &HeartbeatEngine{
		app:           app,
		pendingTopics: map[string]heartbeatPendingTopic{},
	}
	seed := HeartbeatTask{
		ID:                     "fresh",
		Title:                  "Fresh",
		Prompt:                 "ping",
		NewConversationEachRun: true,
		ApprovalMode:           "auto",
	}
	if err := engine.saveTasks([]HeartbeatTask{seed}); err != nil {
		t.Fatal(err)
	}
	engine.ReloadConfig()
	ctrl := &heartbeatExecuteTaskCtrlStub{}
	injected := make(chan struct{})

	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-injected:
				return
			case <-ticker.C:
				var cancel context.CancelFunc
				var tabToInject *WorkspaceTab
				app.mu.Lock()
				for _, tab := range app.tabs {
					if tab == nil {
						continue
					}
					tab.removed = true
					cancel = tab.buildCancel
					tabToInject = tab
					break
				}
				app.mu.Unlock()
				if tabToInject == nil {
					continue
				}
				if cancel != nil {
					cancel()
				}
				app.mu.Lock()
				if tabToInject.Ctrl == nil {
					tabToInject.Ctrl = ctrl
					tabToInject.Ready = true
					tabToInject.StartupErr = ""
					app.advanceSessionRuntimeEpochLocked(tabToInject)
					app.mu.Unlock()
					close(injected)
					return
				}
				app.mu.Unlock()
			}
		}
	}()

	got := engine.executeTask(seed)

	if got.TopicID == "" {
		t.Fatal("fresh conversation task should return the newly created topic ID")
	}
	if got.LastRunAt == 0 {
		t.Fatal("fresh conversation task should update LastRunAt after submit")
	}
	if len(ctrl.submitted) != 1 || ctrl.submitted[0] != "ping" {
		t.Fatalf("submitted prompts = %v, want [ping]", ctrl.submitted)
	}
	if ctrl.approvalMode != "auto" {
		t.Fatalf("approval mode = %q, want auto", ctrl.approvalMode)
	}
	pending := engine.pendingTopics["fresh"]
	if pending.TopicID != got.TopicID || !pending.Submitted {
		t.Fatalf("pending topic = %+v, want submitted %q", pending, got.TopicID)
	}
}

func TestHeartbeatExecuteTaskSkipsPendingPrompt(t *testing.T) {
	isolateDesktopUserDirs(t)
	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	app.runtimeEvents.emit = func(context.Context, string, ...any) {}
	engine := &HeartbeatEngine{
		app:           app,
		pendingTopics: map[string]heartbeatPendingTopic{},
	}
	seed := HeartbeatTask{
		ID:                     "fresh",
		Title:                  "Fresh",
		Prompt:                 "ping",
		NewConversationEachRun: true,
		ApprovalMode:           "auto",
	}
	if err := engine.saveTasks([]HeartbeatTask{seed}); err != nil {
		t.Fatal(err)
	}
	engine.ReloadConfig()
	ctrl := &heartbeatExecuteTaskCtrlStub{status: control.RuntimeStatus{PendingPrompt: true}}
	injected := make(chan struct{})

	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-injected:
				return
			case <-ticker.C:
				var cancel context.CancelFunc
				var tabToInject *WorkspaceTab
				app.mu.Lock()
				for _, tab := range app.tabs {
					if tab == nil {
						continue
					}
					tab.removed = true
					cancel = tab.buildCancel
					tabToInject = tab
					break
				}
				app.mu.Unlock()
				if tabToInject == nil {
					continue
				}
				if cancel != nil {
					cancel()
				}
				app.mu.Lock()
				if tabToInject.Ctrl == nil {
					tabToInject.Ctrl = ctrl
					tabToInject.Ready = true
					tabToInject.StartupErr = ""
					app.advanceSessionRuntimeEpochLocked(tabToInject)
					app.mu.Unlock()
					close(injected)
					return
				}
				app.mu.Unlock()
			}
		}
	}()

	got := engine.executeTask(seed)

	if got.LastRunAt != 0 {
		t.Fatalf("pending prompt should not mark heartbeat run complete, LastRunAt=%d", got.LastRunAt)
	}
	if len(ctrl.submitted) != 0 {
		t.Fatalf("submitted prompts = %v, want none while prompt is pending", ctrl.submitted)
	}
	if ctrl.approvalMode != "" {
		t.Fatalf("approval mode = %q, want unchanged while prompt is pending", ctrl.approvalMode)
	}
}

func TestHeartbeatTaskDueAtHonorsIntervalTimeWindow(t *testing.T) {
	loc := time.UTC
	lastRun := time.Date(2026, 6, 18, 16, 0, 0, 0, loc)
	task := HeartbeatTask{
		ID:              "window",
		Interval:        "30m",
		Enabled:         true,
		LastRunAt:       lastRun.UnixMilli(),
		TimeWindowStart: "09:00",
		TimeWindowEnd:   "17:00",
	}

	if !heartbeatTaskDueAt(task, time.Date(2026, 6, 18, 16, 30, 0, 0, loc)) {
		t.Fatal("interval task should run in the configured time window once due")
	}
	if heartbeatTaskDueAt(task, time.Date(2026, 6, 18, 17, 20, 0, 0, loc)) {
		t.Fatal("interval task should wait while outside the configured time window")
	}
	if !heartbeatTaskDueAt(task, time.Date(2026, 6, 19, 9, 0, 0, 0, loc)) {
		t.Fatal("interval task should run when the next time window opens")
	}

	neverRun := HeartbeatTask{
		ID:              "never-run-window",
		Interval:        "30m",
		Enabled:         true,
		TimeWindowStart: "09:00",
		TimeWindowEnd:   "17:00",
	}
	if heartbeatTaskDueAt(neverRun, time.Date(2026, 6, 18, 20, 0, 0, 0, loc)) {
		t.Fatal("never-run interval task should wait while outside the configured time window")
	}
	if !heartbeatTaskDueAt(neverRun, time.Date(2026, 6, 19, 9, 0, 0, 0, loc)) {
		t.Fatal("never-run interval task should run when the configured time window opens")
	}
}

func TestHeartbeatMergeRunUpdatesPreservesConcurrentEditsAndDeletes(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{
		tasks: []HeartbeatTask{
			{ID: "run", Title: "edited", Prompt: "new", Interval: "2h", Enabled: false, CreatedAt: 10},
			{ID: "keep", Title: "keep", Interval: "1h", Enabled: true},
		},
	}

	engine.mergeRunUpdatesLocked(map[string]HeartbeatTask{
		"run": {
			ID:        "run",
			Title:     "old",
			Prompt:    "old",
			Interval:  "1h",
			Enabled:   true,
			TopicID:   "topic-run",
			LastRunAt: 200,
			CreatedAt: 100,
		},
		"deleted": {
			ID:        "deleted",
			TopicID:   "topic-deleted",
			LastRunAt: 200,
		},
	})

	if len(engine.tasks) != 2 {
		t.Fatalf("tasks len = %d, want 2", len(engine.tasks))
	}
	got := engine.tasks[0]
	if got.Title != "edited" || got.Prompt != "new" || got.Interval != "2h" || got.Enabled {
		t.Fatalf("concurrent task edits were overwritten: %+v", got)
	}
	if got.TopicID != "topic-run" || got.LastRunAt != 200 || got.CreatedAt != 10 {
		t.Fatalf("run fields were not patched correctly: %+v", got)
	}
	for _, task := range engine.tasks {
		if task.ID == "deleted" {
			t.Fatalf("deleted task was resurrected: %+v", engine.tasks)
		}
	}
}

func TestHeartbeatMergeRunUpdatesNeverRegressesNewerRunState(t *testing.T) {
	tasks := []HeartbeatTask{{
		ID:        "run",
		TopicID:   "topic-new",
		LastRunAt: 300,
	}}
	mergeHeartbeatRunUpdates(tasks, map[string]HeartbeatTask{
		"run": {ID: "run", TopicID: "topic-old", LastRunAt: 200},
	})
	if tasks[0].TopicID != "topic-new" || tasks[0].LastRunAt != 300 {
		t.Fatalf("stale run state regressed the owner result: %+v", tasks[0])
	}

	mergeHeartbeatRunUpdates(tasks, map[string]HeartbeatTask{
		"run": {ID: "run", TopicID: "topic-latest", LastRunAt: 400},
	})
	if tasks[0].TopicID != "topic-latest" || tasks[0].LastRunAt != 400 {
		t.Fatalf("newer run state was not adopted: %+v", tasks[0])
	}
}

func TestHeartbeatReplaceTasksPrunesFreshConversationPendingTopics(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{
		pendingTopics: map[string]heartbeatPendingTopic{
			"fresh":   {TopicID: "topic-fresh", Submitted: true},
			"legacy":  {TopicID: "topic-legacy", Submitted: true},
			"deleted": {TopicID: "topic-deleted", Submitted: true},
		},
	}

	err := engine.ReplaceTasks([]HeartbeatTask{
		{ID: "fresh", NewConversationEachRun: true},
		{ID: "legacy", NewConversationEachRun: false},
	})
	if err != nil {
		t.Fatalf("ReplaceTasks: %v", err)
	}

	if len(engine.pendingTopics) != 1 {
		t.Fatalf("pendingTopics len = %d, want 1: %+v", len(engine.pendingTopics), engine.pendingTopics)
	}
	if got := engine.pendingTopics["fresh"]; got.TopicID != "topic-fresh" || !got.Submitted {
		t.Fatalf("fresh pending topic = %+v, want submitted topic-fresh", got)
	}
	if _, ok := engine.pendingTopics["legacy"]; ok {
		t.Fatalf("legacy task should not keep a fresh-conversation pending topic")
	}
	if _, ok := engine.pendingTopics["deleted"]; ok {
		t.Fatalf("deleted task should not keep a pending topic")
	}
}

func TestHeartbeatInactiveOpenDoesNotChangeActiveTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := t.TempDir()
	app := &App{
		tabs: map[string]*WorkspaceTab{
			"heartbeat": {
				ID:            "heartbeat",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicID:       "topic-heartbeat",
				TopicTitle:    "Heartbeat",
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
			"active": {
				ID:            "active",
				Scope:         "project",
				WorkspaceRoot: projectRoot,
				TopicID:       "topic-active",
				TopicTitle:    "Active",
				Ready:         true,
				disabledMCP:   map[string]ServerView{},
			},
		},
		tabOrder:    []string{"heartbeat", "active"},
		activeTabID: "active",
	}

	meta, err := app.openProjectTabInactive(projectRoot, "topic-heartbeat")
	if err != nil {
		t.Fatalf("openProjectTabInactive: %v", err)
	}
	if got := app.activeTabID; got != "active" {
		t.Fatalf("active tab = %q, want active", got)
	}
	if meta.ID != "heartbeat" || meta.Active {
		t.Fatalf("inactive open meta = %+v, want heartbeat and inactive", meta)
	}
}

func TestHeartbeatMergeRunUpdatesAdoptsExternalFileEdits(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{
		tasks: []HeartbeatTask{
			{ID: "a", Title: "stale title", Prompt: "stale", Interval: "1h", Enabled: true},
		},
	}
	// An external editor (the documented human/AI flow) rewrote the file after
	// the engine's in-memory snapshot: task a was edited and task b was added.
	external := []HeartbeatTask{
		{ID: "a", Title: "edited externally", Prompt: "new prompt", Interval: "2h", Enabled: true},
		{ID: "b", Title: "added externally", Prompt: "hello", Interval: "1h", Enabled: false},
	}
	if err := engine.saveTasks(external); err != nil {
		t.Fatalf("seed external file: %v", err)
	}

	engine.mergeRunUpdatesLocked(map[string]HeartbeatTask{
		"a": {ID: "a", TopicID: "topic-a", LastRunAt: 4242},
	})

	if len(engine.tasks) != 2 {
		t.Fatalf("tasks len = %d, want 2 (external addition adopted): %+v", len(engine.tasks), engine.tasks)
	}
	got := engine.tasks[0]
	if got.Title != "edited externally" || got.Prompt != "new prompt" || got.Interval != "2h" {
		t.Fatalf("external edit was rolled back by the run-state save: %+v", got)
	}
	if got.TopicID != "topic-a" || got.LastRunAt != 4242 {
		t.Fatalf("run state was not merged onto the disk copy: %+v", got)
	}
	// The full-list save must have preserved the externally added task on disk.
	onDisk := engine.loadTasks()
	if len(onDisk) != 2 || onDisk[1].ID != "b" || onDisk[1].Title != "added externally" {
		t.Fatalf("externally added task was lost on save: %+v", onDisk)
	}
}

func TestHeartbeatTickAdoptsExternalFileEdits(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := newHeartbeatEngine(nil)
	if err := engine.saveTasks([]HeartbeatTask{{ID: "a", Title: "A", Interval: "1h", Enabled: false}}); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	engine.mu.Lock()
	engine.tasks = engine.loadTasks()
	engine.mu.Unlock()

	// External edit lands after the engine last touched the file. Force the
	// mtime forward so coarse filesystem timestamps cannot make this flaky.
	if err := engine.saveTasks([]HeartbeatTask{
		{ID: "a", Title: "A", Interval: "1h", Enabled: false},
		{ID: "b", Title: "added externally", Interval: "1h", Enabled: false},
	}); err != nil {
		t.Fatalf("external edit: %v", err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(engine.configPath(), future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	engine.tick() // disabled tasks only: adoption runs, nothing executes

	tasks := engine.ListTasks()
	if len(tasks) != 2 || tasks[1].ID != "b" {
		t.Fatalf("tick did not adopt the external edit: %+v", tasks)
	}
}

func TestHeartbeatExternalDeletionDoesNotResurrectTasks(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := newHeartbeatEngine(nil)
	if err := engine.saveTasks([]HeartbeatTask{{ID: "deleted", Title: "old", Interval: "1h", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := engine.readConfigSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	engine.mu.Lock()
	engine.recordConfigSnapshotLocked(snapshot)
	engine.tasks = append([]HeartbeatTask(nil), snapshot.cfg.Tasks...)
	if err := os.Remove(engine.configPath()); err != nil {
		engine.mu.Unlock()
		t.Fatal(err)
	}
	engine.adoptExternalEditsLocked()
	if len(engine.tasks) != 0 || !engine.cfgDeleted {
		engine.mu.Unlock()
		t.Fatalf("deleted config left stale tasks: tasks=%+v deleted=%v", engine.tasks, engine.cfgDeleted)
	}
	engine.mergeRunUpdatesLocked(map[string]HeartbeatTask{"deleted": {ID: "deleted", LastRunAt: 123}})
	engine.mu.Unlock()
	if _, err := os.Stat(engine.configPath()); !os.IsNotExist(err) {
		t.Fatalf("deleted heartbeat config was recreated, stat err=%v", err)
	}
}
