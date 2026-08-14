package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/agent"
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

// TestIssue8372ConcurrentSessionIdentityAndRestartStress exercises the P3
// acceptance shape directly: 20 sessions are created concurrently in one
// workspace for 100 rounds, for both same-model and different-model inputs.
// Every path must be globally unique, acquire/release its lease, and reacquire
// after a simulated close/restart without leaving an in-process busy marker.
func TestIssue8372ConcurrentSessionIdentityAndRestartStress(t *testing.T) {
	const (
		rounds  = 100
		workers = 20
	)
	tests := []struct {
		name  string
		model func(round, worker int) string
	}{
		{
			name: "same model",
			model: func(int, int) string {
				return "provider/shared-model"
			},
		},
		{
			name: "different models",
			model: func(round, worker int) string {
				return fmt.Sprintf("provider-%02d/model-%03d-%02d", worker%5, round, worker)
			},
		},
	}
	type result struct {
		path string
		err  error
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			seen := make(map[string]string, rounds*workers)
			for round := range rounds {
				start := make(chan struct{})
				results := make(chan result, workers)
				var wg sync.WaitGroup
				for worker := range workers {
					wg.Add(1)
					go func(worker int) {
						defer wg.Done()
						<-start
						path, err := createEmptySessionFile(dir, tt.model(round, worker))
						if err != nil {
							results <- result{err: fmt.Errorf("create session: %w", err)}
							return
						}
						lease, err := agent.TryAcquireSessionLease(path)
						if err != nil {
							results <- result{path: path, err: fmt.Errorf("initial lease: %w", err)}
							return
						}
						lease.Release()
						if agent.SessionLeaseHeldByCurrentRuntime(path) {
							results <- result{path: path, err: fmt.Errorf("lease remained busy after close")}
							return
						}
						restarted, err := agent.TryAcquireSessionLease(path)
						if err != nil {
							results <- result{path: path, err: fmt.Errorf("restart lease: %w", err)}
							return
						}
						restarted.Release()
						results <- result{path: path}
					}(worker)
				}
				close(start)
				wg.Wait()
				close(results)

				roundKeys := make(map[string]string, workers)
				for result := range results {
					if result.err != nil {
						t.Fatalf("round %d path %q: %v", round, result.path, result.err)
					}
					key := sessionRuntimeKey(result.path)
					if previous, exists := roundKeys[key]; exists {
						t.Fatalf("round %d duplicate runtime key %q for %q and %q", round, key, previous, result.path)
					}
					roundKeys[key] = result.path
					if previous, exists := seen[key]; exists {
						t.Fatalf("runtime key reused across rounds %q for %q and %q", key, previous, result.path)
					}
					seen[key] = result.path
				}
				if len(roundKeys) != workers {
					t.Fatalf("round %d unique runtime keys = %d, want %d", round, len(roundKeys), workers)
				}
			}
			if len(seen) != rounds*workers {
				t.Fatalf("unique runtime keys = %d, want %d", len(seen), rounds*workers)
			}
		})
	}
}
