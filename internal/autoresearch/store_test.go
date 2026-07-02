package autoresearch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCreateTaskCreatesHostOwnedLayoutAndInitialState(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	store := NewStore(root)

	task, err := store.CreateTask("Find the root cause of UI lag", CreateOptions{
		Now: func() time.Time {
			return time.Date(2026, 6, 29, 15, 30, 0, 0, time.UTC)
		},
		Scope:    []string{"desktop/frontend", "desktop"},
		NonGoals: []string{"publish a release"},
		AllowedOperations: AllowedOperations{
			Write:   true,
			Network: false,
			Publish: false,
		},
		SuccessCriteria: []SuccessCriterion{
			{ID: "root_cause", Description: "A reproducible root cause is identified", Required: true},
		},
	})
	if err != nil {
		t.Fatalf("CreateTask returned error: %v", err)
	}

	if task.ID != "20260629-153000-find-the-root-cause-of-ui-lag" {
		t.Fatalf("task id = %q", task.ID)
	}
	wantRoot := filepath.Join(root, ".reasonix", "autoresearch", task.ID)
	if task.Root != wantRoot {
		t.Fatalf("task root = %q, want %q", task.Root, wantRoot)
	}
	for _, rel := range []string{
		"state/task_spec.json",
		"state/progress.json",
		"state/directions_tried.json",
		"state/findings.jsonl",
		"state/iteration_log.jsonl",
		"logs/heartbeat.jsonl",
	} {
		if _, err := os.Stat(filepath.Join(wantRoot, rel)); err != nil {
			t.Fatalf("expected %s to exist: %v", rel, err)
		}
	}

	var spec TaskSpec
	readJSON(t, filepath.Join(wantRoot, "state/task_spec.json"), &spec)
	if spec.TaskID != task.ID || spec.Goal != "Find the root cause of UI lag" {
		t.Fatalf("spec identity = (%q, %q), want (%q, goal)", spec.TaskID, spec.Goal, task.ID)
	}
	if len(spec.SuccessCriteria) != 1 || spec.SuccessCriteria[0].ID != "root_cause" || !spec.SuccessCriteria[0].Required {
		t.Fatalf("success criteria not persisted correctly: %+v", spec.SuccessCriteria)
	}
	if !spec.AllowedOperations.Write || spec.AllowedOperations.Network || spec.AllowedOperations.Publish {
		t.Fatalf("allowed operations = %+v", spec.AllowedOperations)
	}

	var progress Progress
	readJSON(t, filepath.Join(wantRoot, "state/progress.json"), &progress)
	if progress.Status != StatusRunning || progress.Iteration != 0 || progress.StaleCount != 0 || progress.PivotCount != 0 {
		t.Fatalf("initial progress = %+v", progress)
	}
	if progress.UpdatedAt.IsZero() {
		t.Fatalf("initial progress updated_at was zero")
	}

	report, err := store.ValidateTask(task.ID)
	if err != nil {
		t.Fatalf("ValidateTask returned error: %v", err)
	}
	if !report.Valid || len(report.Errors) != 0 {
		t.Fatalf("validation report = %+v, want valid", report)
	}
}

func TestCreateTaskAvoidsIDCollisions(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	now := func() time.Time { return time.Date(2026, 6, 29, 15, 30, 0, 0, time.UTC) }

	first, err := store.CreateTask("Investigate cache churn", CreateOptions{Now: now})
	if err != nil {
		t.Fatalf("first CreateTask: %v", err)
	}
	second, err := store.CreateTask("Investigate cache churn", CreateOptions{Now: now})
	if err != nil {
		t.Fatalf("second CreateTask: %v", err)
	}

	if first.ID != "20260629-153000-investigate-cache-churn" {
		t.Fatalf("first id = %q", first.ID)
	}
	if second.ID != "20260629-153000-investigate-cache-churn-2" {
		t.Fatalf("second id = %q", second.ID)
	}
}

func TestLoadTaskRejectsUnsafeOrMissingID(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)

	for _, id := range []string{"", "../escape", "bad/id", ".hidden"} {
		if _, err := store.LoadTask(id); err == nil {
			t.Fatalf("LoadTask(%q) succeeded, want error", id)
		}
	}
	if _, err := store.LoadTask("20260629-153000-missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("LoadTask missing error = %v, want not found", err)
	}
}

func TestLoadTaskRejectsSymlinkTaskDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating directory symlinks requires elevated privileges on many Windows hosts")
	}
	root := t.TempDir()
	store := NewStore(root)
	taskID := "20260630-120000-symlink-task"
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "state"), 0o755); err != nil {
		t.Fatalf("create outside state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "state", "task_spec.json"), []byte(`{"task_id":"20260630-120000-symlink-task","goal":"escape","success_criteria":[]}`), 0o644); err != nil {
		t.Fatalf("write outside spec: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".reasonix", "autoresearch"), 0o755); err != nil {
		t.Fatalf("create autoresearch root: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".reasonix", "autoresearch", taskID)); err != nil {
		t.Fatalf("create symlink task: %v", err)
	}

	if _, err := store.LoadTask(taskID); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("LoadTask symlink error = %v, want symlink rejection", err)
	}
}

func TestValidateTaskReportsSchemaErrors(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	task, err := store.CreateTask("Validate schema errors", CreateOptions{})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	specPath := filepath.Join(task.Root, "state/task_spec.json")
	var spec TaskSpec
	readJSON(t, specPath, &spec)
	spec.Goal = ""
	writeJSON(t, specPath, spec)

	report, err := store.ValidateTask(task.ID)
	if err != nil {
		t.Fatalf("ValidateTask returned error: %v", err)
	}
	if report.Valid {
		t.Fatalf("ValidateTask reported valid for missing goal")
	}
	if !containsValidationError(report.Errors, "task_spec.json", "goal") {
		t.Fatalf("validation errors = %+v, want task_spec.json goal error", report.Errors)
	}
}

func TestAppendFindingRecordsAcceptedEvidenceForReadiness(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	task, err := store.CreateTask("Verify accepted findings", CreateOptions{
		SuccessCriteria: []SuccessCriterion{
			{ID: "verified", Description: "Verification evidence exists", Required: true, EvidenceIDs: []string{"f1"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	err = store.AppendFinding(task.ID, Finding{
		ID:        "f1",
		Kind:      FindingKindTest,
		Summary:   "go test ./internal/autoresearch passed",
		Source:    FindingSourceCommand,
		Command:   "go test ./internal/autoresearch",
		Accepted:  true,
		CreatedAt: time.Date(2026, 6, 29, 16, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AppendFinding: %v", err)
	}

	readiness, err := store.Readiness(task.ID)
	if err != nil {
		t.Fatalf("Readiness: %v", err)
	}
	if !readiness.Ready || len(readiness.MissingCriteria) != 0 {
		t.Fatalf("readiness = %+v, want ready", readiness)
	}

	findings, err := store.Findings(task.ID, 10)
	if err != nil {
		t.Fatalf("Findings: %v", err)
	}
	if len(findings) != 1 || findings[0].ID != "f1" || !findings[0].Accepted {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestRecordEvidenceLinksFindingToCriterionAndSatisfiesReadiness(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	task, err := store.CreateTask("Record structured evidence", CreateOptions{
		SuccessCriteria: []SuccessCriterion{
			{ID: "verified", Description: "Verification evidence exists", Required: true},
		},
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	err = store.RecordEvidence(task.ID, "verified", Finding{
		ID:        "f1",
		Kind:      FindingKindTest,
		Summary:   "targeted test passed",
		Source:    FindingSourceCommand,
		Command:   "go test ./internal/autoresearch",
		Accepted:  true,
		CreatedAt: time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordEvidence: %v", err)
	}

	var spec TaskSpec
	readJSON(t, filepath.Join(task.Root, "state/task_spec.json"), &spec)
	if len(spec.SuccessCriteria) != 1 || len(spec.SuccessCriteria[0].EvidenceIDs) != 1 || spec.SuccessCriteria[0].EvidenceIDs[0] != "f1" {
		t.Fatalf("criterion evidence ids = %+v, want f1", spec.SuccessCriteria)
	}
	readiness, err := store.Readiness(task.ID)
	if err != nil {
		t.Fatalf("Readiness: %v", err)
	}
	if !readiness.Ready {
		t.Fatalf("readiness = %+v, want ready after linked accepted evidence", readiness)
	}
}

func TestRecordEvidenceRejectsUnknownCriterion(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	task, err := store.CreateTask("Reject unknown criterion", CreateOptions{
		SuccessCriteria: []SuccessCriterion{
			{ID: "known", Description: "Known criterion", Required: true},
		},
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	err = store.RecordEvidence(task.ID, "missing", Finding{
		ID:        "f1",
		Kind:      FindingKindTest,
		Summary:   "targeted test passed",
		Source:    FindingSourceCommand,
		Accepted:  true,
		CreatedAt: time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC),
	})
	if err == nil || !strings.Contains(err.Error(), "criterion") {
		t.Fatalf("RecordEvidence unknown criterion error = %v", err)
	}
}

func TestAppendFindingRejectsInvalidEntry(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	task, err := store.CreateTask("Reject invalid findings", CreateOptions{})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	err = store.AppendFinding(task.ID, Finding{ID: "", Kind: FindingKindTest, Summary: "missing id", Accepted: true, CreatedAt: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("AppendFinding invalid error = %v, want id error", err)
	}
}

func TestRecordDirectionIncrementsStaleAndRequiresPivot(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	task, err := store.CreateTask("Detect repeated directions", CreateOptions{})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	progress, err := store.RecordDirection(task.ID, Direction{
		Summary:             "Profile markdown rendering",
		AcceptedEvidenceIDs: []string{"f1"},
		Now:                 time.Date(2026, 6, 29, 16, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordDirection first: %v", err)
	}
	if progress.Iteration != 1 || progress.StaleCount != 0 || progress.CurrentDirection != "Profile markdown rendering" {
		t.Fatalf("first progress = %+v", progress)
	}

	progress, err = store.RecordDirection(task.ID, Direction{
		Summary: "Profile markdown rendering",
		Now:     time.Date(2026, 6, 29, 16, 1, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordDirection second: %v", err)
	}
	if progress.Iteration != 2 || progress.StaleCount != 1 {
		t.Fatalf("second progress = %+v, want iteration 2 stale 1", progress)
	}

	progress, err = store.RecordDirection(task.ID, Direction{
		Summary: "Profile markdown rendering",
		Now:     time.Date(2026, 6, 29, 16, 2, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordDirection third: %v", err)
	}
	if progress.StaleCount != 2 || progress.PivotCount != 1 {
		t.Fatalf("third progress = %+v, want stale 2 pivot 1", progress)
	}

	summary, err := store.Summary(task.ID)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if !summary.PivotRequired || summary.NextRequiredAction == "" {
		t.Fatalf("summary = %+v, want pivot required with next action", summary)
	}
}

func TestConcurrentDirectionWritesAreSerializedPerTask(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	task, err := store.CreateTask("Serialize concurrent directions", CreateOptions{})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	const writers = 20
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.RecordDirection(task.ID, Direction{
				Summary: "direction",
				Now:     time.Date(2026, 6, 30, 8, 0, i, 0, time.UTC),
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("RecordDirection concurrent error: %v", err)
		}
	}

	summary, err := store.Summary(task.ID)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.Iteration != writers || summary.StaleCount != writers {
		t.Fatalf("summary = %+v, want %d serialized iterations", summary, writers)
	}
}

func TestReadinessBlocksMissingEvidenceAndBlockedStatus(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	task, err := store.CreateTask("Block incomplete completion", CreateOptions{
		SuccessCriteria: []SuccessCriterion{
			{ID: "root_cause", Description: "Root cause identified", Required: true},
		},
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	readiness, err := store.Readiness(task.ID)
	if err != nil {
		t.Fatalf("Readiness missing: %v", err)
	}
	if readiness.Ready || !containsString(readiness.MissingCriteria, "root_cause") {
		t.Fatalf("readiness missing = %+v, want root_cause missing", readiness)
	}

	_, err = store.UpdateProgress(task.ID, ProgressPatch{Status: ptrString(StatusBlocked), BlockedReason: ptrString("needs user input")})
	if err != nil {
		t.Fatalf("UpdateProgress: %v", err)
	}
	readiness, err = store.Readiness(task.ID)
	if err != nil {
		t.Fatalf("Readiness blocked: %v", err)
	}
	if readiness.Ready || !strings.Contains(readiness.BlockedReason, "needs user input") {
		t.Fatalf("blocked readiness = %+v", readiness)
	}
}

func TestResumeFromGoalTextLoadsExplicitTaskPath(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	task, err := store.CreateTask("Resume explicit path", CreateOptions{})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	resumed, ok, err := store.ResumeFromGoalText("继续 .reasonix/autoresearch/" + task.ID + "/ 这个任务")
	if err != nil {
		t.Fatalf("ResumeFromGoalText: %v", err)
	}
	if !ok || resumed.ID != task.ID {
		t.Fatalf("resumed = %+v ok=%v, want %s", resumed, ok, task.ID)
	}
}

func TestListSummariesReturnsWorkspaceTasks(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	first, err := store.CreateTask("First research task", CreateOptions{
		Now: func() time.Time { return time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("CreateTask first: %v", err)
	}
	second, err := store.CreateTask("Second research task", CreateOptions{
		Now: func() time.Time { return time.Date(2026, 6, 29, 11, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("CreateTask second: %v", err)
	}

	summaries, err := store.ListSummaries()
	if err != nil {
		t.Fatalf("ListSummaries: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("summaries = %+v, want two tasks", summaries)
	}
	if summaries[0].TaskID != second.ID || summaries[1].TaskID != first.ID {
		t.Fatalf("summaries order = %+v, want newest task id first", summaries)
	}
	if summaries[0].Goal != "Second research task" || summaries[1].Goal != "First research task" {
		t.Fatalf("summaries goals = %+v", summaries)
	}
}

func TestAppendHeartbeatRecordsDurableTurnStatus(t *testing.T) {
	root := t.TempDir()
	store := NewStore(root)
	task, err := store.CreateTask("Record heartbeats", CreateOptions{})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	err = store.AppendHeartbeat(task.ID, Heartbeat{
		Status:    HeartbeatStartingTurn,
		Iteration: 1,
		Message:   "starting",
		CreatedAt: time.Date(2026, 6, 29, 17, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AppendHeartbeat: %v", err)
	}

	heartbeats, err := store.Heartbeats(task.ID, 10)
	if err != nil {
		t.Fatalf("Heartbeats: %v", err)
	}
	if len(heartbeats) != 1 || heartbeats[0].Status != HeartbeatStartingTurn || heartbeats[0].Iteration != 1 {
		t.Fatalf("heartbeats = %+v", heartbeats)
	}
	summary, err := store.Summary(task.ID)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if !summary.LastHeartbeatAt.Equal(heartbeats[0].CreatedAt) {
		t.Fatalf("summary last heartbeat = %s, want %s", summary.LastHeartbeatAt, heartbeats[0].CreatedAt)
	}
}

func readJSON(t *testing.T, path string, out any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("Unmarshal(%s): %v", path, err)
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func containsValidationError(errors []ValidationError, file, field string) bool {
	for _, err := range errors {
		if err.File == file && err.Field == field {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func ptrString(s string) *string {
	return &s
}
