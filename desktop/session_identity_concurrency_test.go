package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCreateEmptySessionFileConcurrentPathsAreUnique(t *testing.T) {
	const workers = 64
	tests := []struct {
		name  string
		model func(int) string
	}{
		{
			name: "same model",
			model: func(int) string {
				return "same-model"
			},
		},
		{
			name: "different models",
			model: func(i int) string {
				return fmt.Sprintf("provider-%02d/model:%02d", i%8, i)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			start := make(chan struct{})
			type result struct {
				path string
				err  error
			}
			results := make(chan result, workers)
			var wg sync.WaitGroup
			for i := range workers {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					path, err := createEmptySessionFile(dir, tt.model(i))
					results <- result{path: path, err: err}
				}(i)
			}
			close(start)
			wg.Wait()
			close(results)

			seen := make(map[string]string, workers)
			for result := range results {
				if result.err != nil {
					t.Fatalf("createEmptySessionFile: %v", result.err)
				}
				key := sessionRuntimeKey(result.path)
				if previous, exists := seen[key]; exists {
					t.Fatalf("duplicate session runtime key %q for %q and %q", key, previous, result.path)
				}
				seen[key] = result.path
				if filepath.Clean(filepath.Dir(result.path)) != filepath.Clean(dir) {
					t.Fatalf("session path %q escaped dir %q", result.path, dir)
				}
				if info, err := os.Stat(result.path); err != nil || info.IsDir() {
					t.Fatalf("session file %q stat = %#v, %v", result.path, info, err)
				}
			}
			if len(seen) != workers {
				t.Fatalf("unique session paths = %d, want %d", len(seen), workers)
			}
		})
	}
}

func TestTopicSessionResolutionKeepsDistinctTopicsIsolated(t *testing.T) {
	isolateDesktopUserDirs(t)
	projectRoot := robustTempDir(t)
	app := NewApp()

	topicA, err := app.CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatalf("CreateTopic A: %v", err)
	}
	topicB, err := app.CreateTopic("project", projectRoot, "")
	if err != nil {
		t.Fatalf("CreateTopic B: %v", err)
	}
	if topicA.ID == topicB.ID {
		t.Fatalf("topics share id %q", topicA.ID)
	}

	dir := desktopSessionDir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	pathA := writeTopicSession(t, dir, "topic-a.jsonl", topicA.ID, "Topic A", projectRoot)
	pathB := writeTopicSession(t, dir, "topic-b.jsonl", topicB.ID, "Topic B", projectRoot)

	resolvedA, _ := app.findTopicSessionForTarget("project", projectRoot, topicA.ID)
	resolvedB, _ := app.findTopicSessionForTarget("project", projectRoot, topicB.ID)
	if !sameDesktopPath(resolvedA, pathA) {
		t.Fatalf("topic A resolved to %q, want %q", resolvedA, pathA)
	}
	if !sameDesktopPath(resolvedB, pathB) {
		t.Fatalf("topic B resolved to %q, want %q", resolvedB, pathB)
	}
	if sessionRuntimeKey(resolvedA) == sessionRuntimeKey(resolvedB) {
		t.Fatalf("distinct topics resolved to one session runtime key %q", sessionRuntimeKey(resolvedA))
	}
}
