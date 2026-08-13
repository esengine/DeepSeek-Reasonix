package taskcatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/taskmonitor"
)

func BenchmarkWarmFirstPageHundredThousandTasks(b *testing.B) {
	catalog, err := Open(context.Background(), filepath.Join(b.TempDir(), "tasks.sqlite"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = catalog.Close(context.Background()) })
	root := b.TempDir()
	project := Project{Key: ProjectKey(root), Root: root, Label: "Benchmark"}
	if _, err := catalog.db.Exec(`INSERT INTO task_projects(project_key,project_root,project_label,state) VALUES(?,?,?,'ready')`,
		project.Key, project.Root, project.Label); err != nil {
		b.Fatal(err)
	}
	tx, err := catalog.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	now := time.Now().UTC()
	for i := range 100_000 {
		task := taskmonitor.TaskSnapshot{SchemaVersion: 1, TaskID: fmt.Sprintf("task-%06d", i), SessionID: "session",
			State: taskmonitor.TaskStateSucceeded, Version: 1, CreatedAt: now, UpdatedAt: now.Add(time.Duration(i) * time.Millisecond)}
		raw, _ := json.Marshal(task)
		if _, err := tx.Exec(`INSERT INTO task_snapshots(project_key,task_id,session_id,state,version,created_at,updated_at,snapshot_json)
			VALUES(?,?,?,?,?,?,?,?)`, project.Key, task.TaskID, task.SessionID, task.State, task.Version,
			task.CreatedAt.UnixMilli(), task.UpdatedAt.UnixMilli(), raw); err != nil {
			b.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		if _, err := catalog.ListPage(context.Background(), PageRequest{ProjectKeys: []string{project.Key}, Limit: 50}); err != nil {
			b.Fatal(err)
		}
	}
}
