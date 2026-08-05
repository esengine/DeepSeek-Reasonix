package recovery

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestTaskScopePersistenceIsBackwardCompatible(t *testing.T) {
	const oldJSON = `{"tasks":{"root":{"phase":"diagnosing","failure":{"tool":"write_file","task_id":"root"},"consecutive_fails":3}}}`
	var old Snapshot
	if err := json.Unmarshal([]byte(oldJSON), &old); err != nil {
		t.Fatalf("decode old snapshot: %v", err)
	}
	if got := old.Tasks["root"].Failure.TaskScopeID; got != "" {
		t.Fatalf("old snapshot task scope = %q, want zero value", got)
	}

	newJSON, err := json.Marshal(Snapshot{Tasks: map[string]*TaskState{
		"root": {
			Phase: PhaseDiagnosing,
			Failure: &FailureEvent{
				Tool: "write_file", TaskID: "root", TaskScopeID: "goal:ship",
			},
		},
	}})
	if err != nil {
		t.Fatalf("encode new snapshot: %v", err)
	}
	var legacy struct {
		Tasks map[string]struct {
			Phase   Phase `json:"phase"`
			Failure struct {
				Tool   string `json:"tool"`
				TaskID string `json:"task_id,omitempty"`
			} `json:"failure"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(newJSON, &legacy); err != nil {
		t.Fatalf("legacy reader rejected new snapshot: %v", err)
	}
	if got := legacy.Tasks["root"].Failure.Tool; got != "write_file" {
		t.Fatalf("legacy reader lost known fields: %q", got)
	}
}

func TestFailureClassPersistenceIsBackwardCompatible(t *testing.T) {
	const oldJSON = `{"tasks":{"root":{"phase":"diagnosing","last_failure":{"tool":"bash","err_summary":"command timed out"}}}}`
	var old Snapshot
	if err := json.Unmarshal([]byte(oldJSON), &old); err != nil {
		t.Fatalf("decode old snapshot: %v", err)
	}
	if got := old.Tasks["root"].LastFailure.Class; got != "" {
		t.Fatalf("old snapshot failure class = %q, want zero value", got)
	}

	newJSON, err := json.Marshal(Snapshot{Tasks: map[string]*TaskState{
		"root": {
			Phase: PhaseDiagnosing,
			LastFailure: &FailureEvent{
				Class: FailureClassTransient, Tool: "bash", ErrSummary: "command timed out",
			},
		},
	}})
	if err != nil {
		t.Fatalf("encode new snapshot: %v", err)
	}
	var legacy struct {
		Tasks map[string]struct {
			LastFailure struct {
				Tool       string `json:"tool"`
				ErrSummary string `json:"err_summary,omitempty"`
			} `json:"last_failure"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(newJSON, &legacy); err != nil {
		t.Fatalf("legacy reader rejected failure class: %v", err)
	}
	if got := legacy.Tasks["root"].LastFailure.Tool; got != "bash" {
		t.Fatalf("legacy reader lost known failure fields: %q", got)
	}
}

func TestSaveSnapshotIsAtomicAndOwnerOnly(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	const writers = 24
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			snap := Snapshot{Tasks: map[string]*TaskState{
				"root": {Phase: PhaseDiagnosing, Failure: &FailureEvent{ErrSummary: fmt.Sprintf("failure-%d", i)}},
			}}
			if err := SaveSnapshot(sessionPath, snap); err != nil {
				t.Errorf("SaveSnapshot(%d): %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	snap, err := LoadSnapshot(sessionPath)
	if err != nil {
		t.Fatalf("LoadSnapshot after concurrent writes: %v", err)
	}
	if st := snap.Tasks["root"]; st == nil || st.Failure == nil || st.Failure.ErrSummary == "" {
		t.Fatalf("loaded snapshot = %+v", snap)
	}
	info, err := os.Stat(PathFor(sessionPath))
	if err != nil {
		t.Fatal(err)
	}
	// Windows reports synthetic permission bits for NTFS files; the requested
	// mode is enforced by the inherited ACL rather than FileMode.Perm.
	if got := info.Mode().Perm(); runtime.GOOS != "windows" && got != 0o600 {
		t.Fatalf("recovery state permissions = %o, want 600", got)
	}
}
