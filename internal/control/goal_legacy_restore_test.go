package control

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
)

func writeLegacyGoalArchive(t *testing.T, root, taskID, goal string) string {
	t.Helper()
	taskRoot := filepath.Join(root, ".reasonix", "autoresearch", taskID)
	if err := os.MkdirAll(filepath.Join(taskRoot, "state"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(taskRoot, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"state/task_spec.json":        `{"task_id":"` + taskID + `","goal":"` + goal + `","allowed_operations":{"write":true},"success_criteria":[]}`,
		"state/progress.json":         `{"status":"running","updated_at":"2026-06-30T10:00:00Z"}`,
		"state/directions_tried.json": "[]\n",
		"state/findings.jsonl":        "",
		"state/iteration_log.jsonl":   "",
		"logs/heartbeat.jsonl":        "",
	} {
		if err := os.WriteFile(filepath.Join(taskRoot, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return taskRoot
}

func TestUnknownPersistedBudgetClassFallsBackToGoalClassification(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	raw, err := json.Marshal(goalState{Goal: "fix the crash in settings", Status: GoalStatusRunning, BudgetClass: "future-budget-class", TurnsLimit: 99})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goalStatePath(path), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	g := &goalMachine{}
	g.setStatePath(goalStatePath(path))
	_, _, migrated, _ := g.restoreFromState(path)
	if !migrated || g.budgetClass != budgetClassWrite || g.turnsLimit != 99 {
		t.Fatalf("unknown budget restore = migrated:%v class:%q turns:%d", migrated, g.budgetClass, g.turnsLimit)
	}
}

func TestGoalSidecarWriterFencesLegacyAutoResearchForEveryBudget(t *testing.T) {
	tests := []struct {
		name  string
		goal  string
		class string
	}{
		{name: "simple", goal: "summarize the current status", class: budgetClassSimple},
		{name: "write", goal: "fix the settings crash", class: budgetClassWrite},
		{name: "research", goal: "investigate the latency regression thoroughly", class: budgetClassResearch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			sessionPath := filepath.Join(dir, "session.jsonl")
			g := &goalMachine{statePath: goalStatePath(sessionPath)}
			path, raw, ok := g.set(tt.goal, tt.class, nil)
			if !ok {
				t.Fatal("set did not produce sidecar data")
			}
			var state goalState
			if err := json.Unmarshal(raw, &state); err != nil {
				t.Fatal(err)
			}
			if state.ResearchMode != GoalResearchOff || state.AutoResearchTaskID != "" {
				t.Fatalf("legacy reader fence missing: %+v", state)
			}
			if state.BudgetClass != tt.class || state.TurnsLimit != 0 {
				t.Fatalf("budget state = %+v, want %s with no turn quota", state, tt.class)
			}
			// Frozen previous readers treated any non-Off mode or retained task id
			// as an AutoResearch activation signal.
			var legacyReader struct {
				ResearchMode       GoalResearchMode `json:"researchMode"`
				AutoResearchTaskID string           `json:"autoResearchTaskID"`
			}
			if err := json.Unmarshal(raw, &legacyReader); err != nil {
				t.Fatal(err)
			}
			if legacyReader.ResearchMode != GoalResearchOff || strings.TrimSpace(legacyReader.AutoResearchTaskID) != "" {
				t.Fatal("frozen previous reader would reactivate AutoResearch")
			}
			if err := g.writeStateErr(path, raw); err != nil {
				t.Fatal(err)
			}
			reloaded := &goalMachine{}
			reloaded.restoreFromState(sessionPath)
			if reloaded.budgetClass != tt.class || reloaded.turnsLimit != 0 {
				t.Fatalf("reloaded budget = %q/%d, want %q with no turn quota", reloaded.budgetClass, reloaded.turnsLimit, tt.class)
			}
		})
	}
}

func TestEmptyGoalSidecarStillFencesLegacyAutoResearch(t *testing.T) {
	g := &goalMachine{statePath: filepath.Join(t.TempDir(), "goal.json")}
	_, raw, ok := g.set("", "", nil)
	if !ok {
		t.Fatal("empty Goal did not produce stopped sidecar state")
	}
	var state goalState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if state.ResearchMode != GoalResearchOff || state.AutoResearchTaskID != "" || state.BudgetClass != "" {
		t.Fatalf("empty Goal downgrade fence = %+v", state)
	}
}

func TestGoalSetIdempotencyUsesEffectiveBudgetClass(t *testing.T) {
	g := &goalMachine{statePath: filepath.Join(t.TempDir(), "goal.json")}
	if _, _, ok := g.set("same goal", budgetClassSimple, nil); !ok {
		t.Fatal("initial set did not persist")
	}
	if _, _, ok := g.set("same goal", budgetClassSimple, nil); ok {
		t.Fatal("same Goal and budget class was not idempotent")
	}
	if _, _, ok := g.set("same goal", budgetClassResearch, nil); !ok {
		t.Fatal("budget class change was incorrectly treated as idempotent")
	}
	if g.budgetClass != budgetClassResearch || g.turnsLimit != 0 {
		t.Fatalf("budget upgrade = class:%q turns:%d, want the class kept and no turn quota", g.budgetClass, g.turnsLimit)
	}
}

func TestLegacySidecarArchiveFailureBlocksWithRetryableTaskID(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	sessionPath := filepath.Join(root, "sessions", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const (
		taskID  = "retry-legacy-archive"
		scopeID = "legacy-goal-scope"
	)
	wantTodo := evidence.TodoItem{Content: "preserve legacy verification", Status: "in_progress"}
	wantCheckpoint := evidence.DeliveryCheckpoint{ScopeID: scopeID, CriteriaEstablished: true, WorkObserved: true}
	legacy := goalState{
		Status: GoalStatusRunning, ResearchMode: GoalResearchOn, AutoResearchTaskID: taskID,
		ScopeID: scopeID, DeliveryCheckpoint: wantCheckpoint, Todos: []evidence.TodoItem{wantTodo},
		BudgetClass: budgetClassResearch, TurnsUsed: 3, TurnsLimit: 40, TokensUsed: 1234,
		NoProgressTurns: 2, NoProgressLimit: defaultNoProgressLimit, BudgetExtensions: 1,
		LastContinuationReason: "continue verification",
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goalStatePath(sessionPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{WorkspaceRoot: root, SessionDir: root, Executor: exec})
	c.Resume(sess, sessionPath)
	defer c.Close()
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("failed legacy restore status = %q, want blocked", got)
	}
	failedRaw, err := os.ReadFile(goalStatePath(sessionPath))
	if err != nil {
		t.Fatal(err)
	}
	var failed goalState
	if err := json.Unmarshal(failedRaw, &failed); err != nil {
		t.Fatal(err)
	}
	if failed.Status != GoalStatusBlocked || failed.ResearchMode != GoalResearchOn || failed.AutoResearchTaskID != taskID || failed.StopCause != stopCauseLegacyArchive || failed.Block == "" {
		t.Fatalf("failed restore state = %+v, want retryable blocked legacy migration", failed)
	}
	if failed.ScopeID != scopeID || failed.DeliveryCheckpoint != wantCheckpoint || len(failed.Todos) != 1 || failed.Todos[0] != wantTodo {
		t.Fatalf("failed restore lost goal state: %+v", failed)
	}
	if failed.BudgetClass != budgetClassResearch || failed.TurnsUsed != 3 || failed.TurnsLimit != 40 || failed.TokensUsed != 1234 || failed.NoProgressTurns != 2 || failed.BudgetExtensions != 1 {
		t.Fatalf("failed restore lost runtime state: %+v", failed)
	}
	if got := exec.CanonicalTodoState(); len(got) != 1 || got[0] != wantTodo {
		t.Fatalf("failed restore todos = %+v, want %+v", got, wantTodo)
	}
	if runtime := c.GoalRuntime(); runtime.TurnsUsed != 3 || runtime.TurnsLimit != 40 || runtime.TokensUsed != 1234 || runtime.NoProgressTurns != 2 {
		t.Fatalf("failed restore lost in-memory runtime state: %+v", runtime)
	}
	taskRoot := writeLegacyGoalArchive(t, root, taskID, "recover after archive repair")
	archiveBefore, err := os.ReadFile(filepath.Join(taskRoot, "state", "task_spec.json"))
	if err != nil {
		t.Fatal(err)
	}

	if !c.ResumeGoal() {
		t.Fatal("repaired archive did not resume through the in-memory legacy token")
	}
	if got := c.Goal(); got != "recover after archive repair" {
		t.Fatalf("retried Goal() = %q", got)
	}
	if got := c.GoalStatus(); got != GoalStatusRunning {
		t.Fatalf("retried status = %q, want running", got)
	}
	runtime := c.GoalRuntime()
	// Consumption counters survive the migration; the retired turn quota does not.
	if runtime.TurnsUsed != 3 || runtime.TurnsLimit != 0 || runtime.TokensUsed != 1234 || runtime.NoProgressTurns != 2 || runtime.BudgetExtensions != 1 {
		t.Fatalf("retried runtime = %+v, want preserved legacy consumption without a turn quota", runtime)
	}
	if got := exec.CanonicalTodoState(); len(got) != 1 || got[0] != wantTodo {
		t.Fatalf("retried todos = %+v, want %+v", got, wantTodo)
	}
	if got := c.goals.deliveryState(); got != wantCheckpoint {
		t.Fatalf("retried delivery checkpoint = %+v, want %+v", got, wantCheckpoint)
	}
	retriedRaw, err := os.ReadFile(goalStatePath(sessionPath))
	if err != nil {
		t.Fatal(err)
	}
	var retried goalState
	if err := json.Unmarshal(retriedRaw, &retried); err != nil {
		t.Fatal(err)
	}
	if retried.AutoResearchTaskID != "" || retried.StopCause != "" || retried.Block != "" {
		t.Fatalf("successful retry retained migration-only fields: %+v", retried)
	}
	archiveAfter, err := os.ReadFile(filepath.Join(taskRoot, "state", "task_spec.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(archiveAfter) != string(archiveBefore) {
		t.Fatal("legacy archive changed during retry")
	}
}

func TestLegacySidecarPendingTaskRetriesAfterRestart(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "sessions", "restart.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const taskID = "restartable-legacy"
	raw, err := json.Marshal(goalState{
		Status: GoalStatusRunning, ResearchMode: GoalResearchOn,
		AutoResearchTaskID: taskID, BudgetClass: budgetClassResearch, TurnsUsed: 4, TurnsLimit: 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goalStatePath(sessionPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	firstSession := agent.NewSession("sys")
	first := New(Options{WorkspaceRoot: root, SessionDir: root, Executor: agent.New(nil, nil, firstSession, agent.Options{}, event.Discard)})
	first.Resume(firstSession, sessionPath)
	if first.GoalStatus() != GoalStatusBlocked {
		t.Fatalf("first restore status = %q, want blocked", first.GoalStatus())
	}
	first.Close()

	writeLegacyGoalArchive(t, root, taskID, "recover after process restart")
	secondSession := agent.NewSession("sys")
	second := New(Options{WorkspaceRoot: root, SessionDir: root, Executor: agent.New(nil, nil, secondSession, agent.Options{}, event.Discard)})
	defer second.Close()
	second.Resume(secondSession, sessionPath)
	if second.GoalStatus() != GoalStatusRunning || second.Goal() != "recover after process restart" {
		t.Fatalf("restart migration = goal:%q status:%q", second.Goal(), second.GoalStatus())
	}
	persisted, err := os.ReadFile(goalStatePath(sessionPath))
	if err != nil {
		t.Fatal(err)
	}
	var state goalState
	if err := json.Unmarshal(persisted, &state); err != nil {
		t.Fatal(err)
	}
	if state.AutoResearchTaskID != "" || state.ResearchMode != GoalResearchOff || state.BudgetClass != budgetClassResearch {
		t.Fatalf("restart migration left compatibility fields: %+v", state)
	}
}

func TestLegacySidecarInvalidArchivesRemainRetryableAndReadOnly(t *testing.T) {
	tests := []struct {
		name   string
		file   string
		mutate func(taskID string) string
	}{
		{name: "corrupt json", file: "state/progress.json", mutate: func(string) string { return "{not-json" }},
		{name: "invalid schema", file: "state/task_spec.json", mutate: func(string) string {
			return `{"task_id":"different-task","goal":"schema mismatch","allowed_operations":{"write":true},"success_criteria":[]}`
		}},
		{name: "empty goal", file: "state/task_spec.json", mutate: func(taskID string) string {
			return `{"task_id":"` + taskID + `","goal":"","allowed_operations":{"write":true},"success_criteria":[]}`
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			taskID := "invalid-" + strings.ReplaceAll(tt.name, " ", "-")
			taskRoot := writeLegacyGoalArchive(t, root, taskID, "recover only from a valid archive")
			target := filepath.Join(taskRoot, tt.file)
			if err := os.WriteFile(target, []byte(tt.mutate(taskID)), 0o644); err != nil {
				t.Fatal(err)
			}
			archiveBefore, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			sessionPath := filepath.Join(root, "sessions", "s.jsonl")
			if err := os.MkdirAll(filepath.Dir(sessionPath), 0o755); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(goalState{
				Status: GoalStatusRunning, ResearchMode: GoalResearchOn,
				AutoResearchTaskID: taskID, BudgetClass: budgetClassResearch, TurnsLimit: 40,
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(goalStatePath(sessionPath), raw, 0o644); err != nil {
				t.Fatal(err)
			}

			sess := agent.NewSession("sys")
			exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
			c := New(Options{WorkspaceRoot: root, SessionDir: root, Executor: exec})
			c.Resume(sess, sessionPath)
			defer c.Close()
			if c.GoalStatus() != GoalStatusBlocked || c.ResumeGoal() {
				t.Fatalf("invalid archive status=%q resumed unexpectedly", c.GoalStatus())
			}
			persistedRaw, err := os.ReadFile(goalStatePath(sessionPath))
			if err != nil {
				t.Fatal(err)
			}
			var persisted goalState
			if err := json.Unmarshal(persistedRaw, &persisted); err != nil {
				t.Fatal(err)
			}
			if persisted.AutoResearchTaskID != taskID || persisted.ResearchMode != GoalResearchOn || persisted.StopCause != stopCauseLegacyArchive {
				t.Fatalf("retry state = %+v", persisted)
			}
			archiveAfter, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			if string(archiveAfter) != string(archiveBefore) {
				t.Fatal("invalid legacy archive changed during failed restore")
			}
		})
	}
}

func TestLegacySidecarArchiveCanRetryInSameController(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "sessions", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const taskID = "same-controller-retry"
	legacy := goalState{
		Status: GoalStatusRunning, AutoResearchTaskID: taskID, ResearchMode: GoalResearchOn,
		TurnsUsed: 5, TurnsLimit: 20,
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goalStatePath(sessionPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{WorkspaceRoot: root, SessionDir: root, Executor: exec})
	c.Resume(sess, sessionPath)
	defer c.Close()
	if c.GoalStatus() != GoalStatusBlocked || c.ResumeGoal() {
		t.Fatal("missing archive did not remain blocked")
	}

	writeLegacyGoalArchive(t, root, taskID, "recover objective in the same controller")
	if !c.ResumeGoal() {
		t.Fatal("repaired sidecar archive did not resume in the same controller")
	}
	if got := c.Goal(); got != "recover objective in the same controller" {
		t.Fatalf("Goal() = %q, want recovered archive objective", got)
	}
	if runtime := c.GoalRuntime(); runtime.TurnsUsed != 5 || runtime.TurnsLimit != 0 {
		t.Fatalf("runtime = %+v, want preserved use without a turn quota", runtime)
	}
	persisted, err := os.ReadFile(goalStatePath(sessionPath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(persisted), "autoResearchTaskID") {
		t.Fatalf("successful retry retained legacy task id: %s", persisted)
	}
}

func TestLegacyArchiveMigrationWriteFailureRemainsBlockedAndRetryable(t *testing.T) {
	root := t.TempDir()
	const taskID = "write-retry"
	writeLegacyGoalArchive(t, root, taskID, "recover after sidecar write repair")

	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{WorkspaceRoot: root, SessionDir: root, Executor: exec})
	defer c.Close()

	blockedParent := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(blockedParent, []byte("block mkdir"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.goals.setStatePath(filepath.Join(blockedParent, "goal.json"))
	rawGoal := "resume .reasonix/autoresearch/" + taskID + "/"
	_, _, _ = c.goals.setLegacyArchiveBlocked(rawGoal, budgetClassResearch, "retry migration", nil)
	_, epoch, ok := c.goals.legacyArchiveBlockedState()
	if !ok {
		t.Fatal("legacy archive block state unavailable")
	}
	c.replaceLegacyRestore(legacyGoalRestore{taskID: taskID, epoch: epoch, explicit: true})

	if c.ResumeGoal() {
		t.Fatal("migration reported success after its sidecar write failed")
	}
	goal, retryEpoch, blocked := c.goals.legacyArchiveBlockedState()
	legacy, hasLegacy := c.legacyRestoreSnapshot()
	if !blocked || !hasLegacy || legacy.taskID != taskID || retryEpoch != legacy.epoch || goal != "recover after sidecar write repair" {
		t.Fatalf("failed write lost retry state: goal=%q legacy=%+v blocked=%v", goal, legacy, blocked)
	}
	if c.GoalStatus() != GoalStatusBlocked {
		t.Fatalf("status = %q, want fail-closed blocked", c.GoalStatus())
	}

	statePath := filepath.Join(root, "sessions", "goal.json")
	c.goals.setStatePath(statePath)
	if !c.ResumeGoal() {
		t.Fatal("migration did not retry after sidecar persistence was repaired")
	}
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted goalState
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Status != GoalStatusRunning || persisted.AutoResearchTaskID != "" || persisted.ResearchMode != GoalResearchOff {
		t.Fatalf("retried migration state = %+v", persisted)
	}
}

func TestStaleLegacyArchiveRetryCannotReplaceNewGoal(t *testing.T) {
	var g goalMachine
	g.setLegacyArchiveBlocked("resume .reasonix/autoresearch/old/", budgetClassResearch, "missing", nil)
	_, epoch, ok := g.legacyArchiveBlockedState()
	if !ok {
		t.Fatal("legacy archive block state unavailable")
	}
	g.set("new goal", budgetClassWrite, nil)
	if _, resumed := g.resumeLegacyArchive(epoch, "stale archive goal"); resumed {
		t.Fatal("stale archive retry replaced a newer Goal")
	}
	if got := g.goalText(); got != "new goal" {
		t.Fatalf("Goal() = %q, want concurrent replacement", got)
	}
}

func TestStaleInitialLegacyFailureCannotBlockNewGoal(t *testing.T) {
	var g goalMachine
	g.set("legacy goal", budgetClassResearch, nil)
	epoch := g.continuationToken()
	g.set("new goal", budgetClassWrite, nil)

	if _, blocked := g.blockLegacyRestore(epoch, "archive disappeared"); blocked {
		t.Fatal("stale archive failure blocked a newer Goal")
	}
	if got := g.goalText(); got != "new goal" || g.statusForDisplay() != GoalStatusRunning {
		t.Fatalf("Goal = %q status=%q, want newer running Goal", got, g.statusForDisplay())
	}
}

func TestStaleLegacyMigrationCannotRewriteNewGoalSidecar(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "goal.json")
	g := &goalMachine{statePath: statePath}
	g.set("legacy goal", budgetClassResearch, nil)
	legacyEpoch := g.continuationToken()
	path, data, ok := g.set("new goal", budgetClassWrite, nil)
	if !ok {
		t.Fatal("new Goal did not build sidecar state")
	}
	if err := g.writeStateErr(path, data); err != nil {
		t.Fatal(err)
	}
	if applied, err := g.writeStateAtEpoch(legacyEpoch, nil); err != nil || applied {
		t.Fatalf("stale migration write = applied:%v err:%v", applied, err)
	}
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state goalState
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatal(err)
	}
	if state.Goal != "new goal" {
		t.Fatalf("sidecar Goal = %q, want new goal", state.Goal)
	}
}

func TestLegacySidecarWithGoalMigratesWithoutArchive(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "sessions", "s.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := goalState{
		Goal: "preserve the original goal", Status: GoalStatusRunning,
		AutoResearchTaskID: "missing-archive", ResearchMode: GoalResearchOn,
		TurnsUsed: 2, TurnsLimit: 40,
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goalStatePath(sessionPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{WorkspaceRoot: root, SessionDir: root, Executor: exec})
	c.Resume(sess, sessionPath)
	defer c.Close()
	if got := c.Goal(); got != legacy.Goal {
		t.Fatalf("Goal() = %q, want %q", got, legacy.Goal)
	}
	if got := c.GoalStatus(); got != GoalStatusRunning {
		t.Fatalf("status = %q, want running", got)
	}
	if runtime := c.GoalRuntime(); runtime.TurnsUsed != 2 || runtime.TurnsLimit != 40 {
		t.Fatalf("runtime = %+v, want preserved research budget", runtime)
	}
	persistedRaw, err := os.ReadFile(goalStatePath(sessionPath))
	if err != nil {
		t.Fatal(err)
	}
	var persisted goalState
	if err := json.Unmarshal(persistedRaw, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.AutoResearchTaskID != "" || persisted.ResearchMode != GoalResearchOff || persisted.BudgetClass != budgetClassResearch {
		t.Fatalf("migrated sidecar = %+v, want Goal-only research state", persisted)
	}
}

func TestExplicitLegacyGoalRetryNeverRunsArchivePathAsGoal(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "sessions", "s.jsonl")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{WorkspaceRoot: root, SessionDir: root, Executor: exec})
	c.Resume(sess, sessionPath)
	defer c.Close()

	const taskID = "repair-explicit-archive"
	rawGoal := "resume .reasonix/autoresearch/" + taskID + "/"
	c.SetGoal(rawGoal)
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("initial status = %q, want blocked", got)
	}
	persistedRaw, err := os.ReadFile(goalStatePath(sessionPath))
	if err != nil {
		t.Fatal(err)
	}
	var blocked goalState
	if err := json.Unmarshal(persistedRaw, &blocked); err != nil {
		t.Fatal(err)
	}
	if blocked.Status != GoalStatusBlocked || blocked.StopCause != stopCauseLegacyArchive || blocked.AutoResearchTaskID != taskID || blocked.ResearchMode != GoalResearchOn {
		t.Fatalf("blocked sidecar = %+v", blocked)
	}
	if c.ResumeGoal() {
		t.Fatal("resume succeeded while archive was still missing")
	}
	if got := c.Goal(); got != rawGoal || c.GoalStatus() != GoalStatusBlocked {
		t.Fatalf("failed retry changed Goal: goal=%q status=%q", got, c.GoalStatus())
	}

	writeLegacyGoalArchive(t, root, taskID, "recover the original objective")
	if !c.ResumeGoal() {
		t.Fatal("resume did not recover the repaired archive")
	}
	if got := c.Goal(); got != "recover the original objective" {
		t.Fatalf("Goal() = %q, want archive objective", got)
	}
	if c.GoalStatus() != GoalStatusRunning || c.GoalRuntime().TurnsLimit != 0 {
		t.Fatalf("recovered runtime = status:%q %+v", c.GoalStatus(), c.GoalRuntime())
	}
}

func TestMalformedLegacyArchivePathCannotResumeAsGoalText(t *testing.T) {
	c := New(Options{WorkspaceRoot: t.TempDir()})
	defer c.Close()

	c.SetGoal("resume .reasonix/autoresearch/../escape")
	if c.GoalStatus() != GoalStatusBlocked {
		t.Fatalf("status = %q, want blocked", c.GoalStatus())
	}
	if c.ResumeGoal() {
		t.Fatal("malformed archive path resumed as an ordinary Goal")
	}
	if c.GoalStatus() != GoalStatusBlocked {
		t.Fatalf("status after resume = %q, want blocked", c.GoalStatus())
	}
}

func TestMalformedExplicitLegacyGoalStaysBlockedAfterRestart(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "sessions", "s.jsonl")
	rawGoal := "resume .reasonix/autoresearch/bad-task/../../escape"

	exec1 := agent.New(nil, nil, agent.NewSession("sys"), agent.Options{}, event.Discard)
	c1 := New(Options{WorkspaceRoot: root, SessionDir: root, Executor: exec1})
	c1.Resume(agent.NewSession("sys"), sessionPath)
	c1.SetGoal(rawGoal)
	if got := c1.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("initial status = %q, want blocked", got)
	}
	c1.Close()

	c2 := New(Options{WorkspaceRoot: root, SessionDir: root})
	c2.Resume(agent.NewSession("sys"), sessionPath)
	defer c2.Close()
	if got := c2.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("restart status = %q, want blocked", got)
	}
	if c2.ResumeGoal() {
		t.Fatal("malformed explicit archive resumed after restart")
	}
	if got := c2.Goal(); got != rawGoal {
		t.Fatalf("restart retry changed Goal = %q, want %q", got, rawGoal)
	}
}

func TestMissingLegacyGoalCommandDoesNotStartProviderTurn(t *testing.T) {
	runner := &gatedTurnRunner{started: make(chan struct{}), release: make(chan struct{})}
	c := New(Options{WorkspaceRoot: t.TempDir(), Runner: runner})
	t.Cleanup(c.Close)

	if !c.applyGoalCommand("/goal resume .reasonix/autoresearch/missing-task/", "") {
		t.Fatal("legacy Goal command was not parsed")
	}
	if c.Running() {
		t.Fatal("missing legacy archive started a provider turn")
	}
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("GoalStatus() = %q, want blocked", got)
	}
}

func TestUnreadableExplicitLegacyArchiveBlocks(t *testing.T) {
	root := t.TempDir()
	const taskID = "unreadable-explicit-archive"
	taskRoot := writeLegacyGoalArchive(t, root, taskID, "never run an unreadable archive")
	specPath := filepath.Join(taskRoot, "state", "task_spec.json")
	if err := os.Remove(specPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(specPath, 0o755); err != nil {
		t.Fatal(err)
	}
	c := New(Options{WorkspaceRoot: root})
	t.Cleanup(c.Close)

	c.SetGoal("resume .reasonix/autoresearch/" + taskID + "/")
	if got := c.GoalStatus(); got != GoalStatusBlocked {
		t.Fatalf("GoalStatus() = %q, want blocked", got)
	}
	if got := c.Goal(); got != "resume .reasonix/autoresearch/"+taskID+"/" {
		t.Fatalf("Goal() = %q, archive goal must not be trusted", got)
	}
}
