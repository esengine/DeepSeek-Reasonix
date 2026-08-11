package autoresearch

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestLoadTaskReadsHostOwnedLayout(t *testing.T) {
	root := t.TempDir()
	taskID := "20260630-100000-ui-lag"
	taskRoot := writeArchiveFixture(t, root, taskID, "Find the root cause of UI lag", []SuccessCriterion{
		{ID: "objective_evidence", Description: "Direct evidence", Required: true},
	})
	store := NewStore(root)
	task, err := store.LoadTask(taskID)
	if err != nil {
		t.Fatalf("LoadTask: %v", err)
	}
	if task.ID != taskID {
		t.Fatalf("task id = %q", task.ID)
	}
	if task.Root != taskRoot {
		t.Fatalf("task root = %q, want %q", task.Root, taskRoot)
	}
	if task.Spec.Goal != "Find the root cause of UI lag" {
		t.Fatalf("goal = %q", task.Spec.Goal)
	}
	report, err := store.ValidateTask(taskID)
	if err != nil {
		t.Fatalf("ValidateTask: %v", err)
	}
	if !report.Valid {
		t.Fatalf("validation errors: %+v", report.Errors)
	}
}

func TestLoadTaskRejectsSymlinkAndUnsafeIDs(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".reasonix", "autoresearch"), 0o755); err != nil {
		t.Fatalf("create autoresearch root: %v", err)
	}
	taskID := "symlink-task"
	if err := os.Symlink(outside, filepath.Join(root, ".reasonix", "autoresearch", taskID)); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	store := NewStore(root)
	if _, err := store.LoadTask(taskID); err == nil {
		t.Fatal("LoadTask accepted symlink task")
	}
	if _, err := store.LoadTask("../escape"); err == nil {
		t.Fatal("LoadTask accepted path traversal id")
	}
	if _, err := store.LoadTask("has/slash"); err == nil {
		t.Fatal("LoadTask accepted slash id")
	}
}

func TestLoadTaskRejectsSymlinkedArchiveRoot(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	outside := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(outside); err == nil {
		outside = resolved
	}
	const taskID = "outside-task"
	writeArchiveFixture(t, outside, taskID, "outside workspace goal", nil)
	if err := os.MkdirAll(filepath.Join(root, ".reasonix"), 0o755); err != nil {
		t.Fatal(err)
	}
	outsideRoot := filepath.Join(outside, ".reasonix", "autoresearch")
	if err := os.Symlink(outsideRoot, filepath.Join(root, ".reasonix", "autoresearch")); err != nil {
		t.Fatal(err)
	}

	store := NewStore(root)
	if _, err := store.LoadTask(taskID); err == nil {
		t.Fatal("LoadTask accepted a symlinked archive root outside the workspace")
	}
	if _, err := store.ListSummaries(); err == nil {
		t.Fatal("ListSummaries accepted a symlinked archive root outside the workspace")
	}
}

func TestArchiveReaderRejectsSymlinkedTaskContent(t *testing.T) {
	t.Run("state directory", func(t *testing.T) {
		root := t.TempDir()
		writeArchiveFixture(t, root, "source-task", "source goal", nil)
		victimRoot := writeArchiveFixture(t, root, "victim-task", "victim goal", nil)
		if err := os.RemoveAll(filepath.Join(victimRoot, "state")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join("..", "source-task", "state"), filepath.Join(victimRoot, "state")); err != nil {
			t.Fatal(err)
		}
		if _, err := NewStore(root).LoadTask("victim-task"); err == nil {
			t.Fatal("LoadTask followed a state-directory symlink into another task")
		}
	})

	t.Run("task spec file", func(t *testing.T) {
		root := t.TempDir()
		taskRoot := writeArchiveFixture(t, root, "file-link-task", "linked goal", nil)
		specPath := filepath.Join(taskRoot, "state", "task_spec.json")
		if err := os.Rename(specPath, filepath.Join(taskRoot, "state", "task_spec.real.json")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("task_spec.real.json", specPath); err != nil {
			t.Fatal(err)
		}
		if _, err := NewStore(root).LoadTask("file-link-task"); err == nil {
			t.Fatal("LoadTask followed a task_spec symlink")
		}
	})

	t.Run("validation file", func(t *testing.T) {
		root := t.TempDir()
		taskRoot := writeArchiveFixture(t, root, "progress-link-task", "linked progress", nil)
		progressPath := filepath.Join(taskRoot, "state", "progress.json")
		if err := os.Rename(progressPath, filepath.Join(taskRoot, "state", "progress.real.json")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("progress.real.json", progressPath); err != nil {
			t.Fatal(err)
		}
		report, err := NewStore(root).ValidateTask("progress-link-task")
		if err != nil {
			t.Fatalf("ValidateTask: %v", err)
		}
		if report.Valid {
			t.Fatal("ValidateTask accepted a symlinked progress file")
		}
	})
}

func TestLoadTaskRejectsUnreadableArchiveFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can bypass archive file permissions")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits; chmod cannot make a file unreadable")
	}
	root := t.TempDir()
	taskRoot := writeArchiveFixture(t, root, "permission-task", "permission goal", nil)
	specPath := filepath.Join(taskRoot, "state", "task_spec.json")
	if err := os.Chmod(specPath, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(specPath, 0o644) })
	if _, err := NewStore(root).LoadTask("permission-task"); err == nil {
		t.Fatal("LoadTask accepted an unreadable task_spec.json")
	}
}

func TestFindingsPreserveVerificationAndUnknownKinds(t *testing.T) {
	root := t.TempDir()
	taskID := "findings-kinds"
	taskRoot := writeArchiveFixture(t, root, taskID, "Read evidence kinds", []SuccessCriterion{
		{ID: "verified", Description: "Verified", Required: true, EvidenceIDs: []string{"f-verify"}},
	})
	appendFindingLine(t, taskRoot, Finding{
		ID:        "f-verify",
		Kind:      "verification",
		Summary:   "targeted test passed",
		Source:    FindingSourceCommand,
		Command:   "go test ./internal/autoresearch",
		Accepted:  true,
		CreatedAt: time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC),
	})
	appendFindingLine(t, taskRoot, Finding{
		ID:        "f-future",
		Kind:      "future_kind",
		Summary:   "preserve unknown evidence",
		Source:    FindingSourceManual,
		Accepted:  true,
		CreatedAt: time.Date(2026, 6, 30, 10, 1, 0, 0, time.UTC),
	})
	store := NewStore(root)
	findings, err := store.Findings(taskID, 0)
	if err != nil {
		t.Fatalf("Findings: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %+v", findings)
	}
	if findings[0].Kind != "future_kind" || findings[1].Kind != "verification" {
		t.Fatalf("kinds = %+v, want newest-first future then verification", findings)
	}
	if err := validateFinding(findings[0]); err != nil {
		t.Fatalf("validateFinding unknown kind: %v", err)
	}
	if err := validateFinding(Finding{ID: "", Kind: "anything", Summary: "x", CreatedAt: time.Now()}); err == nil {
		t.Fatal("validateFinding accepted empty id")
	}
	summary, err := store.Summary(taskID)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if len(summary.OpenCriteria) != 0 {
		t.Fatalf("summary = %+v, want verification evidence to satisfy the legacy criterion", summary)
	}
}

func TestValidateFindingDoesNotEnumerateKind(t *testing.T) {
	now := time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC)
	if err := validateFinding(Finding{ID: "f1", Kind: "totally-unknown", Summary: "ok", CreatedAt: now}); err != nil {
		t.Fatalf("unknown kind rejected: %v", err)
	}
	if err := validateFinding(Finding{ID: "f1", Kind: "verification", Summary: "ok", CreatedAt: now}); err != nil {
		t.Fatalf("verification rejected: %v", err)
	}
	if err := validateFinding(Finding{ID: "f1", Kind: "", Summary: "ok", CreatedAt: now}); err != nil {
		t.Fatalf("empty kind should still pass base validation: %v", err)
	}
}

func TestSummaryReportsMissingCriteria(t *testing.T) {
	root := t.TempDir()
	taskID := "missing-criteria"
	writeArchiveFixture(t, root, taskID, "Block incomplete completion", []SuccessCriterion{
		{ID: "objective_evidence", Description: "Direct evidence", Required: true},
		{ID: "verification", Description: "Verification", Required: true},
	})
	store := NewStore(root)
	summary, err := store.Summary(taskID)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if len(summary.OpenCriteria) != 2 {
		t.Fatalf("summary = %+v, want missing both criteria", summary)
	}
}

func TestResumeFromGoalTextLoadsExplicitTaskPath(t *testing.T) {
	root := t.TempDir()
	taskID := "20260630-resume-path"
	writeArchiveFixture(t, root, taskID, "Resume explicit path", nil)
	store := NewStore(root)
	resumed, ok, err := store.ResumeFromGoalText("继续 .reasonix/autoresearch/" + taskID + "/ 这个任务")
	if err != nil || !ok {
		t.Fatalf("ResumeFromGoalText: ok=%v err=%v", ok, err)
	}
	if resumed.ID != taskID || resumed.Spec.Goal != "Resume explicit path" {
		t.Fatalf("resumed = %+v", resumed)
	}
	if _, ok, err := store.ResumeFromGoalText("ordinary goal text"); err != nil || ok {
		t.Fatalf("ordinary text matched archive: ok=%v err=%v", ok, err)
	}
	if _, ok, err := store.ResumeFromGoalText("resume .reasonix/autoresearch/missing-task/"); !ok || err == nil {
		t.Fatalf("missing task should fail closed: ok=%v err=%v", ok, err)
	}
	for _, input := range []string{
		"resume .reasonix/autoresearch/../escape",
		"resume .reasonix/autoresearch/" + taskID + "/../../escape",
		"resume .reasonix/autoresearch/" + taskID + "/extra",
		"resume .reasonix/autoresearch/" + taskID + `\extra`,
		"resume .reasonix/autoresearch/",
	} {
		if _, ok, err := store.ResumeFromGoalText(input); !ok || err == nil {
			t.Errorf("unsafe explicit path %q did not fail closed: ok=%v err=%v", input, ok, err)
		}
	}
}

func TestListSummariesAndSummaryAreReadOnly(t *testing.T) {
	root := t.TempDir()
	firstID := "20260630-first"
	secondID := "20260630-second"
	firstRoot := writeArchiveFixture(t, root, firstID, "First research task", nil)
	writeArchiveFixture(t, root, secondID, "Second research task", nil)
	writeProgress(t, firstRoot, Progress{
		Status:           StatusRunning,
		Iteration:        3,
		CurrentDirection: "inspect logs",
		StaleCount:       2,
		PivotCount:       1,
		UpdatedAt:        time.Date(2026, 6, 30, 11, 0, 0, 0, time.UTC),
	})
	appendHeartbeatLine(t, firstRoot, Heartbeat{
		Status:    HeartbeatTurnDone,
		Iteration: 3,
		CreatedAt: time.Date(2026, 6, 30, 11, 0, 0, 0, time.UTC),
	})
	before := hashTree(t, filepath.Join(root, ".reasonix", "autoresearch"))
	beforeModTimes := modTimes(t, filepath.Join(root, ".reasonix", "autoresearch"))
	store := NewStore(root)
	list, err := store.ListSummaries()
	if err != nil {
		t.Fatalf("ListSummaries: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list = %+v", list)
	}
	summary, err := store.Summary(firstID)
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.Iteration != 3 || !summary.PivotRequired || summary.NextRequiredAction == "" {
		t.Fatalf("summary = %+v", summary)
	}
	after := hashTree(t, filepath.Join(root, ".reasonix", "autoresearch"))
	if len(before) != len(after) {
		t.Fatalf("archive file count changed: before=%d after=%d", len(before), len(after))
	}
	for path, content := range before {
		if after[path] != content {
			t.Fatalf("archive mutated at %s", path)
		}
	}
	afterModTimes := modTimes(t, filepath.Join(root, ".reasonix", "autoresearch"))
	for path, modTime := range beforeModTimes {
		if !afterModTimes[path].Equal(modTime) {
			t.Fatalf("archive modification time changed at %s", path)
		}
	}
}

func TestValidateTaskRejectsCorruptJSON(t *testing.T) {
	root := t.TempDir()
	taskID := "corrupt-json"
	taskRoot := writeArchiveFixture(t, root, taskID, "Validate schema errors", nil)
	if err := os.WriteFile(filepath.Join(taskRoot, "state", "progress.json"), []byte("{not-json"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewStore(root)
	report, err := store.ValidateTask(taskID)
	if err != nil {
		t.Fatalf("ValidateTask: %v", err)
	}
	if report.Valid {
		t.Fatal("corrupt progress reported valid")
	}
}

func TestValidateTaskRejectsCorruptArchiveLogsButAcceptsUnknownFindingKinds(t *testing.T) {
	t.Run("corrupt finding JSON", func(t *testing.T) {
		root := t.TempDir()
		taskID := "corrupt-finding-json"
		taskRoot := writeArchiveFixture(t, root, taskID, "Validate finding JSON", nil)
		if err := os.WriteFile(filepath.Join(taskRoot, "state", "findings.jsonl"), []byte("{not-json\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		report, err := NewStore(root).ValidateTask(taskID)
		if err != nil {
			t.Fatal(err)
		}
		if report.Valid {
			t.Fatal("corrupt finding JSON reported valid")
		}
	})

	t.Run("unknown finding kind", func(t *testing.T) {
		root := t.TempDir()
		taskID := "unknown-finding-kind"
		taskRoot := writeArchiveFixture(t, root, taskID, "Accept future finding kind", nil)
		appendFindingLine(t, taskRoot, Finding{
			ID: "future", Kind: "future-kind", Summary: "preserve me", Accepted: true,
			CreatedAt: time.Date(2026, 6, 30, 10, 0, 0, 0, time.UTC),
		})
		report, err := NewStore(root).ValidateTask(taskID)
		if err != nil {
			t.Fatal(err)
		}
		if !report.Valid {
			t.Fatalf("unknown finding kind rejected: %+v", report.Errors)
		}
	})
}

func TestHeartbeatsTailRead(t *testing.T) {
	root := t.TempDir()
	taskID := "heartbeats"
	taskRoot := writeArchiveFixture(t, root, taskID, "Record heartbeats", nil)
	for i := 1; i <= 5; i++ {
		appendHeartbeatLine(t, taskRoot, Heartbeat{
			Status:    HeartbeatTurnDone,
			Iteration: i,
			CreatedAt: time.Date(2026, 6, 30, 10, i, 0, 0, time.UTC),
		})
	}
	store := NewStore(root)
	heartbeats, err := store.Heartbeats(taskID, 2)
	if err != nil {
		t.Fatalf("Heartbeats: %v", err)
	}
	if len(heartbeats) != 2 || heartbeats[0].Iteration != 4 || heartbeats[1].Iteration != 5 {
		t.Fatalf("heartbeats = %+v", heartbeats)
	}
}
