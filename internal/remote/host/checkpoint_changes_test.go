package host

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"reasonix/internal/checkpoint"
	"reasonix/internal/control"
)

func TestCheckpointChangesUsesActorOwnedFlattenedProjection(t *testing.T) {
	manager, factory := newTestRuntimeManager(t, context.Background(), 16, 64)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	controller := factory.controller(0)
	controller.mu.Lock()
	controller.checkpointState = control.CheckpointSnapshot{
		Metas: []checkpoint.Meta{
			{Turn: 2, Time: time.UnixMilli(1_700_000_000_000), Prompt: "first", Paths: []string{"a.go", "b.go"}},
			{Turn: 5, Prompt: "second", Paths: []string{"docs/readme.md"}},
		},
		TurnsByMessageIndex:   map[int]int{},
		ConversationAvailable: map[int]bool{},
	}
	controller.mu.Unlock()

	changes, err := runtime.CheckpointChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []CheckpointChange{
		{Path: "a.go", Turn: 2, Prompt: "first", TimeMillis: 1_700_000_000_000},
		{Path: "b.go", Turn: 2, Prompt: "first", TimeMillis: 1_700_000_000_000},
		{Path: "docs/readme.md", Turn: 5, Prompt: "second", TimeMillis: 0},
	}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("CheckpointChanges = %+v, want %+v", changes, want)
	}

	controller.mu.Lock()
	controller.checkpointState.Metas[0].Paths[0] = "mutated-after-read"
	controller.mu.Unlock()
	if changes[0].Path != "a.go" {
		t.Fatalf("returned change aliased Controller metadata: %+v", changes[0])
	}
}

func TestPrimaryRelativeCheckpointPathNeverExposesOrEscapesRoot(t *testing.T) {
	root := t.TempDir()
	inside := root + string(filepath.Separator) + "nested" + string(filepath.Separator) + "file.go"
	got, err := primaryRelativeCheckpointPath(root, inside)
	if err != nil || got != "nested/file.go" {
		t.Fatalf("inside absolute path = %q, %v", got, err)
	}
	got, err = primaryRelativeCheckpointPath(root, "nested"+string(filepath.Separator)+"other.go")
	if err != nil || got != "nested/other.go" {
		t.Fatalf("relative path = %q, %v", got, err)
	}
	outside := filepath.Join(filepath.Dir(root), "outside.go")
	if _, err := primaryRelativeCheckpointPath(root, outside); err == nil {
		t.Fatal("outside absolute checkpoint path was accepted")
	}
	if _, err := primaryRelativeCheckpointPath(root, filepath.Join("..", "escape.go")); err == nil {
		t.Fatal("relative checkpoint escape was accepted")
	}
}
