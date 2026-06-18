package main

import (
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/config"
)

func TestHeartbeatConfigPathUsesReasonixUserStateDir(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	want := filepath.Join(config.MemoryUserDir(), "heartbeat-tasks.json")

	if got := engine.configPath(); got != want {
		t.Fatalf("configPath = %q, want %q", got, want)
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
