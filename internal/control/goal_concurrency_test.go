package control

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
)

// TestGoalStateWritesAreConcurrencySafe hammers goal-state persistence from many
// goroutines (each GoalStrict builds the JSON under c.mu, then writes off-lock via
// goalWriteMu) while c.mu-guarded reads run concurrently. Under -race this proves
// the new build-under-lock / write-off-lock split has no data race, and that
// goalWriteMu keeps the on-disk file from being torn by interleaved writes.
func TestGoalStateWritesAreConcurrencySafe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test"})
	c.SetGoalWithResearchMode("concurrent goal", GoalResearchOn)

	stop := make(chan struct{})
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = c.Running()       // takes c.mu
				_ = c.RuntimeStatus() // takes c.mu
				_ = c.Goal()          // takes c.mu
				_ = c.GoalStatus()    // takes c.mu
			}
		}
	}()

	var writers sync.WaitGroup
	for w := range 8 {
		writers.Add(1)
		go func(w int) {
			defer writers.Done()
			for i := range 10 {
				c.GoalStrict(i%2 == 0) // build under c.mu, write off-lock
			}
		}(w)
	}
	writers.Wait()
	close(stop)
	readers.Wait()

	// goalWriteMu must have kept the file intact: still valid JSON, goal preserved.
	data, err := os.ReadFile(goalStatePath(path))
	if err != nil {
		t.Fatalf("read goal state: %v", err)
	}
	var state goalState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatalf("goal state file torn by concurrent writes: %v\n%s", err, data)
	}
	if state.Goal != "concurrent goal" || state.Status != GoalStatusRunning {
		t.Fatalf("goal state = %+v, want the active goal preserved", state)
	}
}
