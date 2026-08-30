package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/filelock"
)

func TestHeartbeatRunCompletionObservesDeletionBeforeNextTick(t *testing.T) {
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
	// Simulate a run that finishes before the scheduler's next external-edit
	// adoption pass. The completion merge itself must observe the deletion.
	engine.mergeRunUpdatesLocked(map[string]HeartbeatTask{"deleted": {ID: "deleted", LastRunAt: 123}})
	deleted := engine.cfgDeleted
	taskCount := len(engine.tasks)
	engine.mu.Unlock()

	if !deleted || taskCount != 0 {
		t.Fatalf("run completion retained deleted config: tasks=%d deleted=%v", taskCount, deleted)
	}
	if _, err := os.Stat(engine.configPath()); !os.IsNotExist(err) {
		t.Fatalf("run completion recreated deleted heartbeat config, stat err=%v", err)
	}
}

func TestHeartbeatTaskLeaseIsCrossEngine(t *testing.T) {
	isolateDesktopUserDirs(t)
	first := newHeartbeatEngine(nil)
	second := newHeartbeatEngine(nil)
	release, err := first.tryAcquireTaskLease("same-task")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.tryAcquireTaskLease("same-task"); !errors.Is(err, filelock.ErrHeld) {
		t.Fatalf("second task lease err=%v, want filelock.ErrHeld", err)
	}
	release()
	retry, err := second.tryAcquireTaskLease("same-task")
	if err != nil {
		t.Fatalf("task lease after release: %v", err)
	}
	retry()
}

func TestHeartbeatTaskLeaseCoversRunStatePersistence(t *testing.T) {
	isolateDesktopUserDirs(t)
	ctrl := &heartbeatSignalingCtrlStub{submittedSignal: make(chan struct{})}
	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"heartbeat-tab": {
			ID:          "heartbeat-tab",
			Scope:       "global",
			TopicID:     "topic",
			TopicTitle:  "Heartbeat",
			Ready:       true,
			Ctrl:        ctrl,
			disabledMCP: map[string]ServerView{},
		},
	}
	app.tabOrder = []string{"heartbeat-tab"}
	first := newHeartbeatEngine(app)
	second := newHeartbeatEngine(nil)
	task := HeartbeatTask{ID: "same-task", Title: "same", Prompt: "ping", Interval: "1h", Enabled: true, TopicID: "topic"}
	if err := first.saveTasks([]HeartbeatTask{task}); err != nil {
		t.Fatal(err)
	}
	first.ReloadConfig()

	configRelease, err := filelock.Acquire(context.Background(), first.configPath()+".lock")
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan HeartbeatTask, 1)
	go func() { result <- first.executeTask(task) }()
	<-ctrl.submittedSignal

	if release, err := second.tryAcquireTaskLease(task.ID); !errors.Is(err, filelock.ErrHeld) {
		if err == nil {
			release()
		}
		t.Fatalf("lease became available before run-state persistence: %v", err)
	}
	configRelease()
	completed := <-result
	if completed.LastRunAt == 0 {
		t.Fatal("successful execution did not update LastRunAt")
	}
	if release, err := second.tryAcquireTaskLease(task.ID); err != nil {
		t.Fatalf("lease was not released after persistence: %v", err)
	} else {
		release()
	}
	onDisk := first.loadTasks()
	if len(onDisk) != 1 || onDisk[0].LastRunAt != completed.LastRunAt {
		t.Fatalf("lease released before durable completion: disk=%+v completed=%+v", onDisk, completed)
	}
}

func TestHeartbeatScheduledTaskRevalidatesAfterLeaseHandoff(t *testing.T) {
	isolateDesktopUserDirs(t)
	first := newHeartbeatEngine(nil)
	second := newHeartbeatEngine(nil)
	now := time.Date(2026, 6, 18, 9, 2, 10, 0, time.UTC)
	stale := HeartbeatTask{ID: "same-task", Title: "same", Interval: "* * * * *", Enabled: true}
	if err := first.saveTasks([]HeartbeatTask{stale}); err != nil {
		t.Fatal(err)
	}
	first.ReloadConfig()
	second.ReloadConfig()

	completed := stale
	completed.LastRunAt = now.Add(-time.Second).UnixMilli()
	first.mu.Lock()
	first.mergeRunUpdatesLocked(map[string]HeartbeatTask{stale.ID: completed})
	first.mu.Unlock()

	got := second.executeScheduledTask(stale, now)
	if got.LastRunAt != completed.LastRunAt {
		t.Fatalf("stale owner did not adopt persisted occurrence: LastRunAt=%d, want %d", got.LastRunAt, completed.LastRunAt)
	}
}

func TestMergeHeartbeatRunUpdatesKeepsRunHistory(t *testing.T) {
	tasks := []HeartbeatTask{{ID: "t1", Title: "task", RunHistory: []HeartbeatRun{{At: 100, TopicID: "a"}}}}
	updates := map[string]HeartbeatTask{
		"t1": {ID: "t1", Title: "task", LastRunAt: 200, RunHistory: []HeartbeatRun{{At: 100, TopicID: "a"}, {At: 200, TopicID: "b"}}},
	}
	mergeHeartbeatRunUpdates(tasks, updates)
	got := tasks[0].RunHistory
	if len(got) != 2 {
		t.Fatalf("run history len=%d, want 2 (deduped union)", len(got))
	}
	if got[0].At != 100 || got[1].At != 200 {
		t.Fatalf("run history order=%v, want [100 200]", got)
	}
	if tasks[0].LastRunAt != 200 {
		t.Fatalf("LastRunAt=%d, want 200", tasks[0].LastRunAt)
	}
}

func TestMergeHeartbeatRunUpdatesCapsHistory(t *testing.T) {
	tasks := []HeartbeatTask{{ID: "t1"}}
	updates := map[string]HeartbeatTask{"t1": {ID: "t1"}}
	history := make([]HeartbeatRun, 0, maxRunHistory+5)
	for i := range maxRunHistory + 5 {
		history = append(history, HeartbeatRun{At: int64(i)})
	}
	updates["t1"] = HeartbeatTask{ID: "t1", RunHistory: history}
	mergeHeartbeatRunUpdates(tasks, updates)
	if got := len(tasks[0].RunHistory); got != maxRunHistory {
		t.Fatalf("run history len=%d, want %d", got, maxRunHistory)
	}
	if tasks[0].RunHistory[0].At != int64(5) {
		t.Fatalf("oldest kept run At=%d, want 5", tasks[0].RunHistory[0].At)
	}
}

// TestHeartbeatReplaceTasksPreservesRunHistory: 前端整表保存（ReplaceTasks）时，
// 前端快照可能不含引擎刚写入的 runHistory（竞态/旧 state），不得清空磁盘已有的历史。
// 回归：此前 ReplaceTasks 直接整表覆写，会把引擎已持久化的 runHistory 全部冲掉。
func TestHeartbeatReplaceTasksPreservesRunHistory(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	// 引擎已执行两次：磁盘上有 2 条 runHistory
	if err := engine.ReplaceTasks([]HeartbeatTask{{
		ID:         "t1",
		Title:      "task",
		RunHistory: []HeartbeatRun{{At: 100, TopicID: "a"}, {At: 200, TopicID: "b"}},
	}}); err != nil {
		t.Fatalf("seed ReplaceTasks: %v", err)
	}

	// 前端旧快照：只改了 enabled，runHistory 字段缺失（竞态下 load 到旧数据）
	if err := engine.ReplaceTasks([]HeartbeatTask{{
		ID:      "t1",
		Title:   "task",
		Enabled: true,
	}}); err != nil {
		t.Fatalf("stale ReplaceTasks: %v", err)
	}

	got := engine.ListTasks()
	if len(got) != 1 {
		t.Fatalf("tasks len=%d, want 1", len(got))
	}
	if len(got[0].RunHistory) != 2 {
		t.Fatalf("run history len=%d, want 2 (stale frontend save must not clear engine-written history): %+v", len(got[0].RunHistory), got[0].RunHistory)
	}
}

// TestHeartbeatReplaceConfigPreservesRunHistory: ReplaceConfig（revision/ETag 校验的
// 前端保存）同样不得用旧快照清掉引擎已写入的 runHistory。
func TestHeartbeatReplaceConfigPreservesRunHistory(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	seed := []HeartbeatTask{{ID: "t1", Title: "task", RunHistory: []HeartbeatRun{{At: 100, TopicID: "a"}}}}
	view, err := engine.ReplaceConfig(HeartbeatConfigUpdate{Revision: 0, Tasks: seed})
	if err != nil {
		t.Fatalf("seed ReplaceConfig: %v", err)
	}
	// 引擎随后写入一条新执行（模拟后台执行落盘）
	if err := engine.ReplaceTasks([]HeartbeatTask{{ID: "t1", Title: "task", RunHistory: []HeartbeatRun{{At: 100, TopicID: "a"}, {At: 200, TopicID: "b"}}}}); err != nil {
		t.Fatalf("engine run write: %v", err)
	}
	// 前端旧快照（revision 过期场景改用全新 engine 读取磁盘模拟 stale load）：
	// 直接验证磁盘保护——重新加载磁盘后前端提交不含 runHistory 的旧快照
	reloaded := &HeartbeatEngine{}
	snap, err := reloaded.readConfigSnapshot()
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	_ = view
	stale := []HeartbeatTask{{ID: "t1", Title: "task", Enabled: true}}
	protected := mergeHeartbeatDiskRunHistory(stale, snap.cfg.Tasks)
	if len(protected[0].RunHistory) != 2 {
		t.Fatalf("protected run history len=%d, want 2: %+v", len(protected[0].RunHistory), protected[0].RunHistory)
	}
}

func TestMergeHeartbeatDiskRunHistoryPreservesAllEngineRunState(t *testing.T) {
	submitted := []HeartbeatTask{{
		ID:         "t1",
		Title:      "edited title",
		TopicID:    "stale-topic",
		LastRunAt:  100,
		RunHistory: []HeartbeatRun{{At: 100, TopicID: "stale-topic"}},
	}}
	disk := []HeartbeatTask{{
		ID:         "t1",
		Title:      "old title",
		TopicID:    "fresh-topic",
		LastRunAt:  200,
		RunHistory: []HeartbeatRun{{At: 200, TopicID: "fresh-topic"}},
	}}

	got := mergeHeartbeatDiskRunHistory(submitted, disk)
	if got[0].Title != "edited title" {
		t.Fatalf("user-owned title=%q, want edited title", got[0].Title)
	}
	if got[0].TopicID != "fresh-topic" || got[0].LastRunAt != 200 {
		t.Fatalf("engine run state rolled back: topic=%q lastRunAt=%d", got[0].TopicID, got[0].LastRunAt)
	}
	if len(got[0].RunHistory) != 2 {
		t.Fatalf("run history len=%d, want union of both snapshots", len(got[0].RunHistory))
	}
}

// TestHeartbeatMergeRunUpdatesPersistsRunHistory: 模拟 TriggerNow 的完整写盘链路——
// executeTask 返回含 runHistory 的 t → mergeRunUpdatesLocked → 磁盘。验证 runHistory 真实落盘。
func TestHeartbeatMergeRunUpdatesPersistsRunHistory(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	// 先建立基线任务（磁盘 + 内存一致）
	seed := []HeartbeatTask{{ID: "t1", Title: "task", Enabled: true}}
	if err := engine.ReplaceTasks(seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// 模拟 executeTask 返回值：lastRunAt 更新 + runHistory 追加 1 条
	updates := map[string]HeartbeatTask{
		"t1": {
			ID:         "t1",
			Title:      "task",
			Enabled:    true,
			LastRunAt:  200,
			TopicID:    "topic-b",
			RunHistory: []HeartbeatRun{{At: 200, TopicID: "topic-b"}},
		},
	}
	engine.mergeRunUpdatesLocked(updates)

	// 从磁盘重新读，确认 runHistory 落盘
	reloaded := &HeartbeatEngine{}
	snap, err := reloaded.readConfigSnapshot()
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if len(snap.cfg.Tasks) != 1 {
		t.Fatalf("tasks len=%d", len(snap.cfg.Tasks))
	}
	got := snap.cfg.Tasks[0]
	if got.LastRunAt != 200 {
		t.Fatalf("LastRunAt=%d, want 200", got.LastRunAt)
	}
	if len(got.RunHistory) != 1 {
		t.Fatalf("run history on disk len=%d, want 1: %+v", len(got.RunHistory), got.RunHistory)
	}
}

// TestCronDueDomDowOrSemantics: 标准 cron 中 day-of-month 与 day-of-week 双受限时
// 为 OR 语义（任一匹配即触发），非 AND。回归：此前实现要求两者同时匹配。
func TestCronDueDomDowOrSemantics(t *testing.T) {
	// "0 9 1 * 1": fires on 1st of month OR Monday
	// 2026-08-03 is a Monday, not the 1st → should fire
	mon := time.Date(2026, 8, 3, 9, 0, 0, 0, time.Local)
	if !cronDue("0 9 1 * 1", mon) {
		t.Fatalf("Monday 09:00 should match (dow OR dom)")
	}
	// 2026-08-01 is a Saturday, not Monday → should fire (dom=1)
	sat := time.Date(2026, 8, 1, 9, 0, 0, 0, time.Local)
	if !cronDue("0 9 1 * 1", sat) {
		t.Fatalf("1st of month should match (dow OR dom)")
	}
	// 2026-08-05 is Wednesday, not 1st/Monday → should NOT fire
	wed := time.Date(2026, 8, 5, 9, 0, 0, 0, time.Local)
	if cronDue("0 9 1 * 1", wed) {
		t.Fatalf("Wednesday should not match")
	}
	// "0 9 * * 1": only dow restricted → Monday only
	tue := time.Date(2026, 8, 4, 9, 0, 0, 0, time.Local)
	if cronDue("0 9 * * 1", tue) {
		t.Fatalf("Tuesday should not match dow=1-only")
	}
	// "0 9 1 * *": only dom restricted → 1st only
	if cronDue("0 9 1 * *", mon) {
		t.Fatalf("Monday (not 1st) should not match dom-only")
	}
}

func TestCronStepAnchorsAndSingleValueSteps(t *testing.T) {
	loc := time.UTC
	if !cronDue("0 0 * */2 *", time.Date(2026, time.January, 1, 0, 0, 0, 0, loc)) {
		t.Fatal("*/2 month step must anchor at January, the field minimum")
	}
	if cronDue("0 0 * */2 *", time.Date(2026, time.February, 1, 0, 0, 0, 0, loc)) {
		t.Fatal("*/2 month step must skip February")
	}
	if !cronDue("0 0 */2 * *", time.Date(2026, time.January, 1, 0, 0, 0, 0, loc)) {
		t.Fatal("*/2 day-of-month step must anchor at day 1")
	}
	if !isCronExpr("1/2 * * * *") {
		t.Fatal("single-value step should be accepted consistently")
	}
	if !cronDue("1/2 * * * *", time.Date(2026, time.January, 1, 0, 3, 0, 0, loc)) {
		t.Fatal("1/2 minute step should match 1, 3, 5, ...")
	}
	if cronDue("1/2 * * * *", time.Date(2026, time.January, 1, 0, 2, 0, 0, loc)) {
		t.Fatal("1/2 minute step must not match minute 2")
	}
}

func TestWeeksBetweenUsesCivilDatesAcrossDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		a    time.Time
		b    time.Time
	}{
		{"spring forward", time.Date(2026, time.March, 2, 0, 0, 0, 0, loc), time.Date(2026, time.March, 16, 0, 0, 0, 0, loc)},
		{"fall back", time.Date(2026, time.October, 26, 0, 0, 0, 0, loc), time.Date(2026, time.November, 9, 0, 0, 0, 0, loc)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := weeksBetween(weekStart(tc.a), weekStart(tc.b)); got != 2 {
				t.Fatalf("weeksBetween=%d, want 2", got)
			}
		})
	}
}

// TestHeartbeatConfigSchemaVersionWritten: 新版本保存的配置必须带 schemaVersion=2，
// 供未来版本识别格式；旧配置（无字段）读取兼容且升级保存后带版本号。
func TestHeartbeatConfigSchemaVersionWritten(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	if err := engine.saveTasks([]HeartbeatTask{{ID: "t1", Title: "task", Interval: "1h", Enabled: false}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(engine.configPath())
	if err != nil {
		t.Fatal(err)
	}
	var cfg heartbeatConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.SchemaVersion != heartbeatSchemaVersion {
		t.Fatalf("schemaVersion = %d, want %d", cfg.SchemaVersion, heartbeatSchemaVersion)
	}
	// 旧格式（无 schemaVersion）仍可读：模拟 v1 配置
	legacy := `{"tasks":[{"id":"legacy","title":"L","interval":"1h","enabled":false}]}`
	if err := os.WriteFile(engine.configPath(), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	engine.ReloadTasks()
	if err := engine.ReplaceTasks(engine.ListTasks()); err != nil {
		t.Fatalf("legacy upgrade save: %v", err)
	}
	data, _ = os.ReadFile(engine.configPath())
	_ = json.Unmarshal(data, &cfg)
	if cfg.SchemaVersion != heartbeatSchemaVersion {
		t.Fatalf("legacy upgrade schemaVersion = %d, want %d", cfg.SchemaVersion, heartbeatSchemaVersion)
	}
}

// TestHeartbeatConfigForwardProtection: 未来版本（更高 schemaVersion）写入的配置，
// 当前二进制整表保存必须拒绝，不能静默降级覆盖 runHistory 等未来字段。
func TestHeartbeatConfigForwardProtection(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	future := `{"schemaVersion":99,"tasks":[{"id":"f","title":"future","interval":"1h","enabled":false,"runHistory":[{"at":100,"topicId":"x"}]}]}`
	if err := os.MkdirAll(filepath.Dir(engine.configPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine.configPath(), []byte(future), 0o644); err != nil {
		t.Fatal(err)
	}
	engine.ReloadTasks() // 读取成功（未知高版本不阻塞读取）
	err := engine.ReplaceTasks([]HeartbeatTask{{ID: "f", Title: "edited", Interval: "2h", Enabled: true}})
	if err == nil {
		t.Fatal("ReplaceTasks on future-schema config must be refused")
	}
	// 磁盘内容未被覆盖
	data, _ := os.ReadFile(engine.configPath())
	if !bytes.Contains(data, []byte(`"schemaVersion":99`)) {
		t.Fatalf("future config was overwritten: %s", data)
	}
}

func TestCronDueDowSevenSundayAlias(t *testing.T) {
	// "0 9 * * 7": 7 is the standard Sunday alias in the dow field — it must
	// fire on Sundays (time.Weekday() == 0), not silently never match.
	sunday := time.Date(2026, 8, 9, 9, 0, 0, 0, time.Local) // 2026-08-09 is a Sunday
	if !cronDue("0 9 * * 7", sunday) {
		t.Fatalf("Sunday 09:00 should match dow=7 (Sunday alias)")
	}
	// "0 9 * * 0,7": both Sunday spellings together
	if !cronDue("0 9 * * 0,7", sunday) {
		t.Fatalf("Sunday 09:00 should match dow=0,7")
	}
	// A non-Sunday must not match dow=7
	monday := time.Date(2026, 8, 10, 9, 0, 0, 0, time.Local)
	if cronDue("0 9 * * 7", monday) {
		t.Fatalf("Monday should not match dow=7")
	}
	// "0 9 * * 6-7": dow range ending in 7 covers Sunday (6=Sat, 7=Sun)
	if !cronDue("0 9 * * 6-7", sunday) {
		t.Fatalf("Sunday should match dow range 6-7")
	}
}

func TestIsCronExprFieldBounds(t *testing.T) {
	// dom/month are 1-based: 0 can never match and must be rejected so the
	// UI refuses the expression instead of silently scheduling a task that
	// never fires (e.g. "0 0 0 * *" typed as "midnight every day").
	rejected := []string{
		"0 0 0 * *",    // dom 0
		"0 0 1 0 *",    // month 0
		"0 0 32 * *",   // dom 32
		"0 0 1 13 *",   // month 13
		"0 0 * 0-13 *", // month range with 0
		"*/0 * * * *",  // zero step never fires (minute % 0)
		"0 0 5-1 * *",  // descending range never matches
		"0 60 * * *",   // hour 60
		"0 0 1 * 8",    // dow 8 out of range
	}
	for _, expr := range rejected {
		if isCronExpr(expr) {
			t.Fatalf("isCronExpr(%q) should be false (out-of-bounds field)", expr)
		}
	}
	accepted := []string{
		"0 9 * * 7",   // dow 7 is a valid Sunday alias
		"0 9 1 1 0-7", // dow range 0-7 valid
		"*/15 * * * *",
		"0 9 1-31 * *",
		"5-10/2 * * * *", // stepping range
	}
	for _, expr := range accepted {
		if !isCronExpr(expr) {
			t.Fatalf("isCronExpr(%q) should be true", expr)
		}
	}
}

// TestHeartbeatConfigForwardProtectionOnRead: 读侧也要拒绝更高 schema 的配置——
// 不能加载并按旧逻辑执行（调度/权限语义可能已变化）。此前只在写入侧拒绝。
func TestHeartbeatConfigForwardProtectionOnRead(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	future := `{"schemaVersion":99,"tasks":[{"id":"f","title":"future","interval":"1h","enabled":false}]}`
	if err := os.MkdirAll(filepath.Dir(engine.configPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine.configPath(), []byte(future), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.readConfigSnapshot(); err == nil {
		t.Fatal("readConfigSnapshot should reject a future schemaVersion")
	}
	if tasks := engine.loadTasks(); tasks != nil {
		t.Fatal("loadTasks should refuse to load a future schemaVersion")
	}
}
