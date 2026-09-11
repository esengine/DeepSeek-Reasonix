package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/fileutil"
)

func TestHeartbeatRunHistoryLegacySidecarReadable(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	if err := os.MkdirAll(filepath.Dir(engine.configPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	config := []byte(`{"schemaVersion":2,"revision":7,"tasks":[{"id":"t1","title":"task"}]}`)
	legacySidecar := []byte(`{"runs":{"t1":[{"at":100,"topicId":"legacy"}]}}`)
	if err := os.WriteFile(engine.configPath(), config, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(engine.runHistoryPath(), legacySidecar, 0o644); err != nil {
		t.Fatal(err)
	}
	got := engine.ReloadTasks()
	if len(got) != 1 || len(got[0].RunHistory) != 1 || got[0].RunHistory[0].TopicID != "legacy" {
		t.Fatalf("legacy sidecar was not loaded: %+v", got)
	}
}

func TestHeartbeatRunHistorySidecarSurvivesOlderFullTableSave(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	if err := engine.ReplaceTasks([]HeartbeatTask{{
		ID:         "t1",
		Title:      "task",
		RunHistory: []HeartbeatRun{{At: 100, TopicID: "a"}, {At: 200, TopicID: "b"}},
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	legacySave := `{"schemaVersion":1,"tasks":[{"id":"t1","title":"task"}]}`
	if err := os.WriteFile(engine.configPath(), []byte(legacySave), 0o644); err != nil {
		t.Fatal(err)
	}
	reloaded := &HeartbeatEngine{}
	snap, err := reloaded.readConfigSnapshot()
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if len(snap.cfg.Tasks) != 1 || len(snap.cfg.Tasks[0].RunHistory) != 2 {
		t.Fatalf("sidecar did not survive older full-table save: %+v", snap.cfg.Tasks)
	}
}

func TestHeartbeatRunHistorySidecarTrimmedOnSave(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	if err := engine.ReplaceTasks([]HeartbeatTask{{
		ID:         "t1",
		Title:      "task",
		RunHistory: []HeartbeatRun{{At: 100, TopicID: "a"}},
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	data, err := os.ReadFile(engine.configPath())
	if err != nil {
		t.Fatal(err)
	}
	var cfg heartbeatConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tasks) != 1 || cfg.Tasks[0].RunHistory != nil {
		t.Fatalf("main config retained sidecar-owned history: %+v", cfg.Tasks)
	}
	raw, err := os.ReadFile(engine.runHistoryPath())
	if err != nil {
		t.Fatal(err)
	}
	var previousReader struct {
		Runs map[string][]HeartbeatRun `json:"runs"`
	}
	if err := json.Unmarshal(raw, &previousReader); err != nil || len(previousReader.Runs["t1"]) != 1 {
		t.Fatalf("previous sidecar reader lost top-level runs: runs=%+v err=%v", previousReader.Runs, err)
	}
}

func TestHeartbeatRunHistorySidecarClearsAfterLastTaskDeletion(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	if err := engine.ReplaceTasks([]HeartbeatTask{{
		ID:         "reused",
		Title:      "old task",
		RunHistory: []HeartbeatRun{{At: 100, TopicID: "old-topic"}},
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := engine.ReplaceTasks([]HeartbeatTask{}); err != nil {
		t.Fatalf("delete last task: %v", err)
	}
	raw, err := os.ReadFile(engine.runHistoryPath())
	if err != nil {
		t.Fatalf("read empty sidecar: %v", err)
	}
	var sidecar heartbeatRunHistorySidecar
	if err := json.Unmarshal(raw, &sidecar); err != nil {
		t.Fatalf("decode empty sidecar: %v", err)
	}
	if len(sidecar.Runs) != 0 {
		t.Fatalf("sidecar runs=%v, want empty", sidecar.Runs)
	}
	if err := engine.ReplaceTasks([]HeartbeatTask{{ID: "reused", Title: "new task"}}); err != nil {
		t.Fatalf("reuse task ID: %v", err)
	}
	got := engine.ListTasks()
	if len(got) != 1 || len(got[0].RunHistory) != 0 {
		t.Fatalf("deleted task history resurrected after ID reuse: %+v", got)
	}
}

func TestHeartbeatRunHistorySidecarSurvivesCrashBeforeConfigCommit(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	if err := engine.ReplaceTasks([]HeartbeatTask{{
		ID:         "reused",
		Title:      "old task",
		RunHistory: []HeartbeatRun{{At: 100, TopicID: "old-topic"}},
	}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mainBefore, err := os.ReadFile(engine.configPath())
	if err != nil {
		t.Fatal(err)
	}

	previousCrashPoint := fileutil.CrashPoint
	defer func() { fileutil.CrashPoint = previousCrashPoint }()
	const crashMarker = "simulated process crash before config commit"
	crashed := false
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered != crashMarker {
					panic(recovered)
				}
				crashed = true
			}
		}()
		fileutil.CrashPoint = func(op, path string) {
			if op == "atomic-write" && path == engine.configPath() {
				panic(crashMarker)
			}
		}
		_ = engine.ReplaceTasks([]HeartbeatTask{})
	}()
	fileutil.CrashPoint = previousCrashPoint
	if !crashed {
		t.Fatal("delete did not reach the injected crash point")
	}
	mainAfter, err := os.ReadFile(engine.configPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(mainAfter, mainBefore) {
		t.Fatal("main config changed before its commit point")
	}

	rawSidecar, err := os.ReadFile(engine.runHistoryPath())
	if err != nil {
		t.Fatal(err)
	}
	var staged heartbeatRunHistorySidecar
	if err := json.Unmarshal(rawSidecar, &staged); err != nil {
		t.Fatal(err)
	}
	if staged.Revision != 2 || len(staged.Runs) != 0 || staged.Previous == nil || staged.Previous.Revision != 1 {
		t.Fatalf("staged sidecar = %+v, want empty revision 2 with revision 1 fallback", staged)
	}

	restarted := &HeartbeatEngine{}
	restored := restarted.ReloadTasks()
	if len(restored) != 1 || restored[0].ID != "reused" || len(restored[0].RunHistory) != 1 || restored[0].RunHistory[0].TopicID != "old-topic" {
		t.Fatalf("restart loaded inconsistent config/sidecar pair: %+v", restored)
	}
	if err := restarted.ReplaceTasks([]HeartbeatTask{}); err != nil {
		t.Fatalf("retry delete after restart: %v", err)
	}
	if err := restarted.ReplaceTasks([]HeartbeatTask{{ID: "reused", Title: "new task"}}); err != nil {
		t.Fatalf("reuse task ID: %v", err)
	}
	got := restarted.ReloadTasks()
	if len(got) != 1 || len(got[0].RunHistory) != 0 {
		t.Fatalf("deleted task history resurrected after committed delete and ID reuse: %+v", got)
	}
}

func TestHeartbeatRunHistorySidecarForwardProtection(t *testing.T) {
	isolateDesktopUserDirs(t)
	engine := &HeartbeatEngine{}
	if err := engine.ReplaceTasks([]HeartbeatTask{{ID: "t1", Title: "task"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	future := []byte(`{"schemaVersion":99,"revision":1,"runs":{"t1":[{"at":100,"topicId":"future"}]}}`)
	if err := os.WriteFile(engine.runHistoryPath(), future, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.readConfigSnapshot(); err == nil {
		t.Fatal("read should reject a future run-history sidecar schemaVersion")
	}
	// A running older process may still have the task in memory. Manual runs
	// must revalidate the persisted schemas before touching the app runtime.
	engine.TriggerNow("t1")
	restarted := newHeartbeatEngine(nil)
	restarted.Start()
	t.Cleanup(restarted.Stop)
	if tasks := restarted.ListTasks(); len(tasks) != 0 {
		t.Fatalf("startup loaded tasks beside a future sidecar schema: %+v", tasks)
	}
	if err := engine.ReplaceTasks([]HeartbeatTask{{ID: "t1", Title: "edited"}}); err == nil {
		t.Fatal("write should reject a future run-history sidecar schemaVersion")
	}
	got, err := os.ReadFile(engine.runHistoryPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, future) {
		t.Fatalf("future run-history sidecar was overwritten: %s", got)
	}
}
