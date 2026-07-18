package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/autoresearch"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/memory"
	"reasonix/internal/provider"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/skill"
)

type memoryResearchHostController struct {
	*fakeSessionController

	root        string
	sessionDir  string
	sessionPath string
	set         *memory.Set

	domainMu      sync.Mutex
	quickAdds     int
	forgotten     []string
	savedDocs     int
	savedMemories []memory.Memory
	createdSkills int
	research      []autoresearch.Summary
	findings      map[string][]autoresearch.Finding
	currentTask   string
	recorded      []recordedResearchEvidence
}

type recordedResearchEvidence struct {
	taskID      string
	criterionID string
	finding     autoresearch.Finding
}

func (c *memoryResearchHostController) WorkspaceRoot() string { return c.root }
func (c *memoryResearchHostController) SessionDir() string    { return c.sessionDir }
func (c *memoryResearchHostController) SessionPath() string   { return c.sessionPath }
func (c *memoryResearchHostController) Memory() *memory.Set   { return c.set }
func (c *memoryResearchHostController) QuickAdd(scope memory.Scope, note string) (string, error) {
	c.domainMu.Lock()
	c.quickAdds++
	c.domainMu.Unlock()
	path := c.set.DocPath(scope)
	if err := memory.AppendDoc(path, note); err != nil {
		return "", err
	}
	c.set = memory.Load(memory.Options{CWD: c.set.CWD, UserDir: c.set.UserDir})
	return path, nil
}
func (c *memoryResearchHostController) ForgetMemory(name string) error {
	c.domainMu.Lock()
	defer c.domainMu.Unlock()
	c.forgotten = append(c.forgotten, name)
	return nil
}
func (c *memoryResearchHostController) SaveDoc(path, body string) (string, error) {
	c.domainMu.Lock()
	defer c.domainMu.Unlock()
	c.savedDocs++
	written, err := c.set.WriteDoc(path, body)
	if err != nil {
		return "", err
	}
	c.set = memory.Load(memory.Options{CWD: c.set.CWD, UserDir: c.set.UserDir})
	return written, nil
}
func (c *memoryResearchHostController) SaveMemory(value memory.Memory) (string, error) {
	c.domainMu.Lock()
	defer c.domainMu.Unlock()
	c.savedMemories = append(c.savedMemories, value)
	return filepath.Join(c.set.Store.Dir, value.Name+".md"), nil
}
func (c *memoryResearchHostController) AllSkills() []skill.Skill { return []skill.Skill{} }
func (c *memoryResearchHostController) CreateSkill(name string, scope skill.Scope, content string) (string, error) {
	c.domainMu.Lock()
	c.createdSkills++
	c.domainMu.Unlock()
	return filepath.Join(c.root, ".agents", "skills", name, "SKILL.md"), nil
}
func (c *memoryResearchHostController) CurrentAutoResearchTask() (*autoresearch.Summary, bool, error) {
	c.domainMu.Lock()
	defer c.domainMu.Unlock()
	if c.findings == nil {
		return nil, false, nil
	}
	for index := range c.research {
		if c.research[index].TaskID == c.currentTask {
			copyValue := c.research[index]
			return &copyValue, true, nil
		}
	}
	return nil, true, nil
}
func (c *memoryResearchHostController) ListAutoResearchTasks() ([]autoresearch.Summary, bool, error) {
	c.domainMu.Lock()
	defer c.domainMu.Unlock()
	if c.findings == nil {
		return nil, false, nil
	}
	return append([]autoresearch.Summary(nil), c.research...), true, nil
}
func (c *memoryResearchHostController) AutoResearchTaskSummary(taskID string) (*autoresearch.Summary, bool, error) {
	for index := range c.research {
		if c.research[index].TaskID == taskID {
			copyValue := c.research[index]
			return &copyValue, true, nil
		}
	}
	return nil, true, errors.New("not found")
}
func (c *memoryResearchHostController) AutoResearchTaskFindings(taskID string) ([]autoresearch.Finding, bool, error) {
	c.domainMu.Lock()
	defer c.domainMu.Unlock()
	if c.findings == nil {
		return nil, false, nil
	}
	items, ok := c.findings[taskID]
	if !ok {
		return nil, true, errors.New("not found")
	}
	return append([]autoresearch.Finding(nil), items...), true, nil
}
func (c *memoryResearchHostController) RecordAutoResearchTaskEvidence(taskID, criterionID string, finding autoresearch.Finding) error {
	c.domainMu.Lock()
	defer c.domainMu.Unlock()
	if _, ok := c.findings[taskID]; !ok {
		return errors.New("not found")
	}
	c.recorded = append(c.recorded, recordedResearchEvidence{taskID: taskID, criterionID: criterionID, finding: finding})
	return nil
}

type memoryResearchHostFactory struct {
	controller *memoryResearchHostController
}

func (f memoryResearchHostFactory) CreateController(ctx context.Context, _ protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	f.controller.fakeSessionController = newFakeSessionController(ctx, sink)
	return f.controller, nil
}

func newMemoryResearchHostRuntime(t *testing.T, memoryAvailable, researchAvailable bool) (*RuntimeManager, *SessionRuntime, *memoryResearchHostController, *idempotency.Registry) {
	t.Helper()
	root := t.TempDir()
	sessionDir := filepath.Join(root, ".reasonix", "sessions")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessionDir, "2026-07-17T120000-test.jsonl")
	for index, input := range []string{
		"Always verify the frozen Remote architecture and review each PR.",
		"Always inspect the GitHub pull request and review feedback before updating the PR.",
	} {
		history := agent.NewSession("system")
		history.Add(provider.Message{Role: provider.RoleUser, Content: input})
		historyPath := filepath.Join(sessionDir, fmt.Sprintf("2026-07-17T12000%d-test.jsonl", index))
		if index == 0 {
			path = historyPath
		}
		if err := history.Save(historyPath); err != nil {
			t.Fatal(err)
		}
	}
	controller := &memoryResearchHostController{root: root, sessionDir: sessionDir, sessionPath: path}
	if memoryAvailable {
		if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("remote memory\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		controller.set = memory.Load(memory.Options{CWD: root, UserDir: t.TempDir()})
		if _, err := controller.set.Store.Save(memory.Memory{Name: "host-fact", Description: "host fact", Type: memory.TypeProject, Body: "body"}); err != nil {
			t.Fatal(err)
		}
	}
	if researchAvailable {
		controller.currentTask = "20260717-task"
		controller.research = []autoresearch.Summary{{
			TaskID: controller.currentTask, Goal: "finish Remote", Status: autoresearch.StatusRunning,
			OpenCriteria: []autoresearch.CriterionSummary{{ID: "criterion-1", Description: "tests pass", Required: true, Status: "open"}},
		}}
		controller.findings = map[string][]autoresearch.Finding{controller.currentTask: {{
			ID: "finding-1", Kind: autoresearch.FindingKindFile, Summary: "wire ready", Source: autoresearch.FindingSourceFile,
			Paths: []string{filepath.Join(root, "internal", "remote")}, CreatedAt: time.Unix(100, 0).UTC(),
		}}}
	}
	manager, err := NewRuntimeManager(context.Background(), "host-test", memoryResearchHostFactory{controller: controller}, RuntimeManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	runtime, err := manager.GetOrCreate(testTarget())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := idempotency.New("host-test", idempotency.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return manager, runtime, controller, registry
}

func runtimeQueryFor(runtime *SessionRuntime) protocol.RuntimeQuery {
	return protocol.RuntimeQuery{ExpectedHostEpoch: "host-test", Target: runtime.Target(), ExpectedRuntimeEpoch: runtime.Epoch()}
}

func TestMemoryHostActorQueriesMutationsAndReplay(t *testing.T) {
	_, runtime, controller, registry := newMemoryResearchHostRuntime(t, true, true)
	query := runtimeQueryFor(runtime)
	view, err := runtime.MemoryQuery(context.Background(), protocol.MemoryGetParams{RuntimeQuery: query}, true, nil)
	if err != nil || len(view.Facts) != 1 || view.Facts[0].MemoryID == "" {
		t.Fatalf("MemoryQuery = %+v, %v", view, err)
	}
	suggestions, err := runtime.MemorySuggestionsQuery(context.Background(), protocol.MemorySuggestionsParams{RuntimeQuery: query}, true, nil)
	if err != nil || len(suggestions.Memories) == 0 {
		t.Fatalf("MemorySuggestionsQuery = %+v, %v", suggestions, err)
	}

	remember := protocol.MemoryRememberParams{SessionMutation: mutationEnvelope(runtime, "remember-1"), Scope: "project", Note: "one note"}
	first, err := runtime.RememberMemoryMutation(context.Background(), registry, remember, true, nil)
	if err != nil || first.MemoryID == "" {
		t.Fatalf("RememberMemoryMutation = %+v, %v", first, err)
	}
	second, err := runtime.RememberMemoryMutation(context.Background(), registry, remember, true, nil)
	if err != nil || second != first || controller.quickAdds != 1 {
		t.Fatalf("remember replay = %+v calls=%d err=%v", second, controller.quickAdds, err)
	}
	conflict := remember
	conflict.Note = "different"
	if _, err := runtime.RememberMemoryMutation(context.Background(), registry, conflict, true, nil); err == nil {
		t.Fatal("requestId conflict unexpectedly succeeded")
	}

	forget := protocol.MemoryForgetParams{SessionMutation: mutationEnvelope(runtime, "forget-1"), MemoryID: protocol.MemoryID(view.Facts[0].MemoryID)}
	if result, err := runtime.ForgetMemoryMutation(context.Background(), registry, forget, true, nil); err != nil || !result.Forgotten {
		t.Fatalf("ForgetMemoryMutation = %+v, %v", result, err)
	}
	if _, err := runtime.ForgetMemoryMutation(context.Background(), registry, forget, true, nil); err != nil || len(controller.forgotten) != 1 {
		t.Fatalf("forget replay calls=%v err=%v", controller.forgotten, err)
	}

	if len(view.Documents) == 0 {
		t.Fatal("expected writable document target")
	}
	save := protocol.MemoryDocumentSaveParams{SessionMutation: mutationEnvelope(runtime, "save-doc-1"), DocumentID: protocol.DocumentID(view.Documents[0].DocumentID), Body: "updated"}
	if result, err := runtime.SaveMemoryDocumentMutation(context.Background(), registry, save, true, nil); err != nil || !result.Saved || controller.savedDocs != 1 {
		t.Fatalf("SaveMemoryDocumentMutation = %+v saves=%d err=%v", result, controller.savedDocs, err)
	}

	accept := protocol.MemorySuggestionAcceptParams{
		SessionMutation: mutationEnvelope(runtime, "accept-memory-1"),
		SuggestionID:    protocol.SuggestionID(suggestions.Memories[0].SuggestionID), ExpectedRevision: protocol.CatalogRevision(suggestions.Revision),
	}
	accepted, err := runtime.AcceptMemorySuggestionMutation(context.Background(), registry, accept, true, nil)
	if err != nil || accepted.MemoryID == "" || len(controller.savedMemories) != 1 {
		t.Fatalf("AcceptMemorySuggestionMutation = %+v saved=%d err=%v", accepted, len(controller.savedMemories), err)
	}
	replayed, err := runtime.AcceptMemorySuggestionMutation(context.Background(), registry, accept, true, nil)
	if err != nil || replayed != accepted || len(controller.savedMemories) != 1 {
		t.Fatalf("accept replay = %+v saved=%d err=%v", replayed, len(controller.savedMemories), err)
	}
	if len(suggestions.Skills) == 0 {
		t.Fatal("expected the shared repeated-workflow Skill algorithm to produce a candidate")
	}
	// The memory acceptance consumed the exact cache. Refresh before accepting a
	// Skill, just as the UI does after catalog invalidation.
	suggestions, err = runtime.MemorySuggestionsQuery(context.Background(), protocol.MemorySuggestionsParams{RuntimeQuery: query}, true, nil)
	if err != nil || len(suggestions.Skills) == 0 {
		t.Fatalf("refresh suggestions = %+v, %v", suggestions, err)
	}
	acceptSkill := protocol.SkillSuggestionAcceptParams{
		SessionMutation:  mutationEnvelope(runtime, "accept-skill-1"),
		SuggestionID:     protocol.SuggestionID(suggestions.Skills[0].SuggestionID),
		ExpectedRevision: protocol.CatalogRevision(suggestions.Revision),
	}
	skillResult, err := runtime.AcceptSkillSuggestionMutation(context.Background(), registry, acceptSkill, true, nil)
	if err != nil || skillResult.SkillID == "" || controller.createdSkills != 1 {
		t.Fatalf("AcceptSkillSuggestionMutation = %+v created=%d err=%v", skillResult, controller.createdSkills, err)
	}
	if replay, err := runtime.AcceptSkillSuggestionMutation(context.Background(), registry, acceptSkill, true, nil); err != nil || replay != skillResult || controller.createdSkills != 1 {
		t.Fatalf("skill replay = %+v created=%d err=%v", replay, controller.createdSkills, err)
	}
}

func TestResearchHostActorExplicitTaskPagingAndEvidenceReplay(t *testing.T) {
	_, runtime, controller, registry := newMemoryResearchHostRuntime(t, true, true)
	query := runtimeQueryFor(runtime)
	status, err := runtime.ResearchStatusQuery(context.Background(), protocol.ResearchStatusParams{RuntimeQuery: query}, true, nil)
	if err != nil || status.Task == nil || status.Task.TaskID == "" || status.Task.TaskID == "20260717-task" || len(status.Task.OpenCriteria) != 1 {
		t.Fatalf("ResearchStatusQuery = %+v, %v", status, err)
	}
	list, err := runtime.ResearchListQuery(context.Background(), protocol.ResearchListParams{RuntimeQuery: query}, true, nil)
	if err != nil || len(list.Items) != 1 || list.Items[0].TaskID != status.Task.TaskID || list.Items[0].DisplayPath != "" {
		t.Fatalf("ResearchListQuery = %+v, %v", list, err)
	}
	findings, err := runtime.ResearchFindingsQuery(context.Background(), protocol.ResearchFindingsParams{RuntimeQuery: query, TaskID: protocol.ResearchTaskID(status.Task.TaskID)}, true, nil)
	if err != nil || len(findings.Items) != 1 || len(findings.Items[0].Paths) != 1 || findings.Items[0].Paths[0] != "internal/remote" {
		t.Fatalf("ResearchFindingsQuery = %+v, %v", findings, err)
	}
	if _, err := runtime.ResearchFindingsQuery(context.Background(), protocol.ResearchFindingsParams{RuntimeQuery: query, TaskID: "../../escape"}, true, nil); err == nil {
		t.Fatal("adversarial task id reached Controller")
	}

	record := protocol.ResearchEvidenceRecordParams{
		SessionMutation: mutationEnvelope(runtime, "research-record-1"), TaskID: protocol.ResearchTaskID(status.Task.TaskID), CriterionID: protocol.CriterionID(status.Task.OpenCriteria[0].CriterionID),
		Evidence: protocol.ResearchEvidence{ID: "evidence-1", Kind: autoresearch.FindingKindManual, Summary: "verified", Source: autoresearch.FindingSourceManual, Paths: []string{"internal/remote"}, Accepted: true},
	}
	result, err := runtime.RecordResearchEvidenceMutation(context.Background(), registry, record, true, nil)
	if err != nil || !result.Recorded || len(controller.recorded) != 1 {
		t.Fatalf("RecordResearchEvidenceMutation = %+v recorded=%d err=%v", result, len(controller.recorded), err)
	}
	replay, err := runtime.RecordResearchEvidenceMutation(context.Background(), registry, record, true, nil)
	if err != nil || replay != result || len(controller.recorded) != 1 {
		t.Fatalf("record replay = %+v recorded=%d err=%v", replay, len(controller.recorded), err)
	}
	record.RequestID = "research-record-escape"
	record.Evidence.Paths = []string{"../../etc/passwd"}
	if _, err := runtime.RecordResearchEvidenceMutation(context.Background(), registry, record, true, nil); err == nil || len(controller.recorded) != 1 {
		t.Fatalf("unsafe evidence was persisted: recorded=%d err=%v", len(controller.recorded), err)
	}
}

func TestMemoryResearchCapabilityUnavailableIsStructuredAndCached(t *testing.T) {
	_, runtime, controller, registry := newMemoryResearchHostRuntime(t, false, false)
	query := runtimeQueryFor(runtime)
	if _, err := runtime.MemoryQuery(context.Background(), protocol.MemoryGetParams{RuntimeQuery: query}, true, nil); err == nil {
		t.Fatal("unavailable memory query succeeded")
	} else {
		requireRemoteCode(t, err, protocol.ErrCapabilityUnavailable)
	}
	remember := protocol.MemoryRememberParams{SessionMutation: mutationEnvelope(runtime, "remember-unavailable"), Scope: "project", Note: "note"}
	if _, err := runtime.RememberMemoryMutation(context.Background(), registry, remember, true, nil); err == nil {
		t.Fatal("unavailable memory mutation succeeded")
	} else {
		requireRemoteCode(t, err, protocol.ErrCapabilityUnavailable)
	}
	controller.set = memory.Load(memory.Options{CWD: controller.root, UserDir: t.TempDir()})
	if _, err := runtime.RememberMemoryMutation(context.Background(), registry, remember, true, nil); err == nil {
		t.Fatal("cached deterministic rejection changed after capability became available")
	} else {
		requireRemoteCode(t, err, protocol.ErrCapabilityUnavailable)
	}
	if controller.quickAdds != 0 {
		t.Fatalf("cached rejection executed Controller %d times", controller.quickAdds)
	}
	if _, err := runtime.ResearchStatusQuery(context.Background(), protocol.ResearchStatusParams{RuntimeQuery: query}, true, nil); err == nil {
		t.Fatal("unavailable research query succeeded")
	} else {
		requireRemoteCode(t, err, protocol.ErrCapabilityUnavailable)
	}
}

func TestMemoryResearchMutationLeaseGuardRunsBeforeRegistration(t *testing.T) {
	_, runtime, controller, registry := newMemoryResearchHostRuntime(t, true, true)
	params := protocol.MemoryRememberParams{SessionMutation: mutationEnvelope(runtime, "guarded-memory"), Scope: "project", Note: "note"}
	want := errors.New("lease changed")
	if _, err := runtime.RememberMemoryMutation(context.Background(), registry, params, true, func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("guard error = %v", err)
	}
	if _, err := runtime.RememberMemoryMutation(context.Background(), registry, params, true, nil); err != nil || controller.quickAdds != 1 {
		t.Fatalf("request was registered before guard: calls=%d err=%v", controller.quickAdds, err)
	}
}

func TestMemoryResearchActorRejectsStaleEpochBeforeController(t *testing.T) {
	_, runtime, controller, registry := newMemoryResearchHostRuntime(t, true, true)
	params := protocol.MemoryRememberParams{SessionMutation: mutationEnvelope(runtime, "stale-memory"), Scope: "project", Note: "note"}
	params.ExpectedRuntimeEpoch = protocol.RuntimeEpoch(fmt.Sprintf("%s-stale", runtime.Epoch()))
	if _, err := runtime.RememberMemoryMutation(context.Background(), registry, params, true, nil); err == nil {
		t.Fatal("stale runtime mutation succeeded")
	} else {
		requireRemoteCode(t, err, protocol.ErrStaleRuntimeEpoch)
	}
	if controller.quickAdds != 0 {
		t.Fatalf("stale mutation executed Controller %d times", controller.quickAdds)
	}
}
