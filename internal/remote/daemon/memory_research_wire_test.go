package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/autoresearch"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/memory"
	"reasonix/internal/provider"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
	"reasonix/internal/skill"
)

type memoryResearchWireController struct {
	*daemonFakeController
	root        string
	sessionDir  string
	sessionPath string
	set         *memory.Set

	domainMu  sync.Mutex
	quick     int
	forget    int
	saveDoc   int
	saveFact  int
	saveSkill int
	record    int
}

func (c *memoryResearchWireController) WorkspaceRoot() string { return c.root }
func (c *memoryResearchWireController) SessionDir() string    { return c.sessionDir }
func (c *memoryResearchWireController) SessionPath() string   { return c.sessionPath }
func (c *memoryResearchWireController) Memory() *memory.Set   { return c.set }
func (c *memoryResearchWireController) QuickAdd(scope memory.Scope, note string) (string, error) {
	c.domainMu.Lock()
	c.quick++
	c.domainMu.Unlock()
	path := c.set.DocPath(scope)
	if err := memory.AppendDoc(path, note); err != nil {
		return "", err
	}
	c.set = memory.Load(memory.Options{CWD: c.set.CWD, UserDir: c.set.UserDir})
	return path, nil
}
func (c *memoryResearchWireController) ForgetMemory(string) error {
	c.domainMu.Lock()
	c.forget++
	c.domainMu.Unlock()
	return nil
}
func (c *memoryResearchWireController) SaveDoc(path, body string) (string, error) {
	c.domainMu.Lock()
	defer c.domainMu.Unlock()
	c.saveDoc++
	written, err := c.set.WriteDoc(path, body)
	if err != nil {
		return "", err
	}
	c.set = memory.Load(memory.Options{CWD: c.set.CWD, UserDir: c.set.UserDir})
	return written, nil
}
func (c *memoryResearchWireController) SaveMemory(value memory.Memory) (string, error) {
	c.domainMu.Lock()
	c.saveFact++
	c.domainMu.Unlock()
	return filepath.Join(c.set.Store.Dir, value.Name+".md"), nil
}
func (c *memoryResearchWireController) AllSkills() []skill.Skill { return []skill.Skill{} }
func (c *memoryResearchWireController) CreateSkill(name string, _ skill.Scope, _ string) (string, error) {
	c.domainMu.Lock()
	c.saveSkill++
	c.domainMu.Unlock()
	return filepath.Join(c.root, ".agents", "skills", name, "SKILL.md"), nil
}
func (c *memoryResearchWireController) CurrentAutoResearchTask() (*autoresearch.Summary, bool, error) {
	value := wireResearchSummary(c.root)
	return &value, true, nil
}
func (c *memoryResearchWireController) ListAutoResearchTasks() ([]autoresearch.Summary, bool, error) {
	return []autoresearch.Summary{wireResearchSummary(c.root)}, true, nil
}
func (c *memoryResearchWireController) AutoResearchTaskSummary(taskID string) (*autoresearch.Summary, bool, error) {
	if taskID != "20260717-wire-task" {
		return nil, true, errors.New("missing")
	}
	value := wireResearchSummary(c.root)
	return &value, true, nil
}
func (c *memoryResearchWireController) AutoResearchTaskFindings(taskID string) ([]autoresearch.Finding, bool, error) {
	if taskID != "20260717-wire-task" {
		return nil, true, errors.New("missing")
	}
	return []autoresearch.Finding{{
		ID: "finding-wire", Kind: autoresearch.FindingKindFile, Summary: "wire finding",
		Source: autoresearch.FindingSourceFile, Command: "git -C " + c.root + " status",
		Paths:     []string{filepath.Join(c.root, "internal", "remote"), "/etc/passwd"},
		CreatedAt: time.Unix(100, 0).UTC(),
	}}, true, nil
}
func (c *memoryResearchWireController) RecordAutoResearchTaskEvidence(taskID, criterionID string, finding autoresearch.Finding) error {
	if taskID != "20260717-wire-task" || criterionID != "criterion-wire" || finding.ID == "" {
		return errors.New("invalid evidence target")
	}
	c.domainMu.Lock()
	c.record++
	c.domainMu.Unlock()
	return nil
}

func wireResearchSummary(root string) autoresearch.Summary {
	return autoresearch.Summary{
		TaskID: "20260717-wire-task", Goal: "finish Remote wire", Status: autoresearch.StatusRunning,
		OpenCriteria: []autoresearch.CriterionSummary{{ID: "criterion-wire", Description: "wire passes", Required: true, Status: "open"}},
		TaskPath:     filepath.Join(root, ".reasonix", "autoresearch", "20260717-wire-task"),
	}
}

type memoryResearchWireFactory struct{ controller *memoryResearchWireController }

func (f memoryResearchWireFactory) CreateController(ctx context.Context, _ protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	f.controller.daemonFakeController = newDaemonFakeController(ctx, sink)
	return f.controller, nil
}

func newMemoryResearchWireServer(t *testing.T, available bool) (*Server, *memoryResearchWireController, protocol.BuildID) {
	t.Helper()
	options, _, buildID := daemonTestServerOptions(t, nil)
	root := t.TempDir()
	sessionDir := filepath.Join(root, ".reasonix", "sessions")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	var firstPath string
	for index, input := range []string{
		"Always review the GitHub PR before changing Remote code.",
		"Always inspect pull request review feedback and update the PR safely.",
	} {
		session := agent.NewSession("system")
		session.Add(provider.Message{Role: provider.RoleUser, Content: input})
		path := filepath.Join(sessionDir, fmt.Sprintf("2026-07-17T13000%d-wire.jsonl", index))
		if firstPath == "" {
			firstPath = path
		}
		if err := session.Save(path); err != nil {
			t.Fatal(err)
		}
	}
	controller := &memoryResearchWireController{root: root, sessionDir: sessionDir, sessionPath: firstPath}
	if available {
		if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("wire memory\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		controller.set = memory.Load(memory.Options{CWD: root, UserDir: t.TempDir()})
		if _, err := controller.set.Store.Save(memory.Memory{Name: "wire-fact", Description: "wire", Type: memory.TypeProject, Body: "wire body"}); err != nil {
			t.Fatal(err)
		}
	}
	options.ControllerFactory = memoryResearchWireFactory{controller: controller}
	options.Capabilities = protocol.FrozenCapabilities(available, available)
	server, err := New(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return server, controller, buildID
}

func memoryResearchWireRuntime(t *testing.T, server *Server, buildID protocol.BuildID) (*daemonPeer, protocol.RuntimeTarget, protocol.RuntimeEpoch) {
	t.Helper()
	peer := openDaemonPeer(t, server, nil, nil)
	initializePeer(t, peer, buildID, "client-memory-research", "")
	target := daemonTestTarget()
	subscription := subscribePeer(t, peer, target)
	return peer, target, subscription.Snapshot.RuntimeEpoch
}

func TestMemoryResearchRealNDJSONWireDomainAndReplay(t *testing.T) {
	server, controller, buildID := newMemoryResearchWireServer(t, true)
	peer, target, epoch := memoryResearchWireRuntime(t, server, buildID)
	query := protocol.RuntimeQuery{ExpectedHostEpoch: "host-test", Target: target, ExpectedRuntimeEpoch: epoch}

	memoryView := requestResult[protocol.MemoryGetResult](t, peer, protocol.MethodMemoryGet, protocol.MemoryGetParams{RuntimeQuery: query})
	if !memoryView.Available || len(memoryView.Facts) != 1 || len(memoryView.Documents) == 0 || memoryView.Revision == "" {
		t.Fatalf("memory/get = %+v", memoryView)
	}
	for _, item := range memoryView.Documents {
		if strings.Contains(item.DisplayPath, controller.root) || filepath.IsAbs(item.DisplayPath) {
			t.Fatalf("memory document leaked Host path: %+v", item)
		}
	}
	suggestions := requestResult[protocol.MemorySuggestionsResult](t, peer, protocol.MethodMemorySuggestions, protocol.MemorySuggestionsParams{RuntimeQuery: query})
	if len(suggestions.Memories) == 0 || len(suggestions.Skills) == 0 || suggestions.Revision == "" {
		t.Fatalf("memory/suggestions = %+v", suggestions)
	}

	remember := protocol.MemoryRememberParams{SessionMutation: mutation("wire-remember", target, epoch), Scope: "project", Note: "wire note"}
	firstRemember := requestResult[protocol.MemoryRememberResult](t, peer, protocol.MethodMemoryRemember, remember)
	secondRemember := requestResult[protocol.MemoryRememberResult](t, peer, protocol.MethodMemoryRemember, remember)
	if firstRemember != secondRemember || controller.quick != 1 {
		t.Fatalf("memory/remember replay = %+v/%+v calls=%d", firstRemember, secondRemember, controller.quick)
	}
	forgetRemembered := protocol.MemoryForgetParams{SessionMutation: mutation("wire-forget-remembered", target, epoch), MemoryID: firstRemember.MemoryID}
	if result := requestResult[protocol.MemoryForgetResult](t, peer, protocol.MethodMemoryForget, forgetRemembered); !result.Forgotten || controller.saveDoc != 1 {
		t.Fatalf("forget remembered ID = %+v documentSaves=%d", result, controller.saveDoc)
	}

	forget := protocol.MemoryForgetParams{SessionMutation: mutation("wire-forget", target, epoch), MemoryID: memoryView.Facts[0].MemoryID}
	if result := requestResult[protocol.MemoryForgetResult](t, peer, protocol.MethodMemoryForget, forget); !result.Forgotten {
		t.Fatalf("memory/forget = %+v", result)
	}
	requestResult[protocol.MemoryForgetResult](t, peer, protocol.MethodMemoryForget, forget)
	if controller.forget != 1 {
		t.Fatalf("memory/forget replay calls = %d", controller.forget)
	}

	save := protocol.MemoryDocumentSaveParams{SessionMutation: mutation("wire-document", target, epoch), DocumentID: memoryView.Documents[0].DocumentID, Body: "updated body"}
	if result := requestResult[protocol.MemoryDocumentSaveResult](t, peer, protocol.MethodMemoryDocumentSave, save); !result.Saved {
		t.Fatalf("memory/document/save = %+v", result)
	}
	if controller.saveDoc != 2 {
		t.Fatalf("document saves = %d", controller.saveDoc)
	}

	acceptMemory := protocol.MemorySuggestionAcceptParams{
		SessionMutation: mutation("wire-accept-memory", target, epoch),
		SuggestionID:    suggestions.Memories[0].SuggestionID, ExpectedRevision: suggestions.Revision,
	}
	acceptedMemory := requestResult[protocol.MemorySuggestionAcceptResult](t, peer, protocol.MethodMemorySuggestionAccept, acceptMemory)
	if acceptedMemory.MemoryID == "" || controller.saveFact != 1 {
		t.Fatalf("memory/suggestion/accept = %+v saves=%d", acceptedMemory, controller.saveFact)
	}
	if replay := requestResult[protocol.MemorySuggestionAcceptResult](t, peer, protocol.MethodMemorySuggestionAccept, acceptMemory); replay != acceptedMemory || controller.saveFact != 1 {
		t.Fatalf("memory accept replay = %+v saves=%d", replay, controller.saveFact)
	}

	// Acceptance invalidates the exact candidate cache, so refresh before a
	// distinct Skill acceptance.
	suggestions = requestResult[protocol.MemorySuggestionsResult](t, peer, protocol.MethodMemorySuggestions, protocol.MemorySuggestionsParams{RuntimeQuery: query})
	acceptSkill := protocol.SkillSuggestionAcceptParams{
		SessionMutation: mutation("wire-accept-skill", target, epoch),
		SuggestionID:    suggestions.Skills[0].SuggestionID, ExpectedRevision: suggestions.Revision,
	}
	acceptedSkill := requestResult[protocol.SkillSuggestionAcceptResult](t, peer, protocol.MethodSkillSuggestionAccept, acceptSkill)
	if acceptedSkill.SkillID == "" || controller.saveSkill != 1 {
		t.Fatalf("skill/suggestion/accept = %+v saves=%d", acceptedSkill, controller.saveSkill)
	}
	if replay := requestResult[protocol.SkillSuggestionAcceptResult](t, peer, protocol.MethodSkillSuggestionAccept, acceptSkill); replay != acceptedSkill || controller.saveSkill != 1 {
		t.Fatalf("skill accept replay = %+v saves=%d", replay, controller.saveSkill)
	}

	status := requestResult[protocol.ResearchStatusResult](t, peer, protocol.MethodResearchStatus, protocol.ResearchStatusParams{RuntimeQuery: query})
	if status.Task == nil || status.Task.TaskID == "" || status.Task.TaskID == "20260717-wire-task" || status.Task.DisplayPath != "" || len(status.Task.OpenCriteria) != 1 {
		t.Fatalf("research/status = %+v", status)
	}
	listed := requestResult[protocol.ResearchListResult](t, peer, protocol.MethodResearchList, protocol.ResearchListParams{RuntimeQuery: query})
	if len(listed.Items) != 1 || listed.Items[0].TaskID != status.Task.TaskID || listed.Items[0].DisplayPath != "" {
		t.Fatalf("research/list = %+v", listed)
	}
	findings := requestResult[protocol.ResearchFindingsResult](t, peer, protocol.MethodResearchFindings, protocol.ResearchFindingsParams{RuntimeQuery: query, TaskID: status.Task.TaskID})
	if len(findings.Items) != 1 || len(findings.Items[0].Paths) != 1 || findings.Items[0].Paths[0] != "internal/remote" || strings.Contains(findings.Items[0].Command, controller.root) {
		t.Fatalf("research/findings = %+v", findings)
	}
	record := protocol.ResearchEvidenceRecordParams{
		SessionMutation: mutation("wire-record", target, epoch), TaskID: status.Task.TaskID, CriterionID: status.Task.OpenCriteria[0].CriterionID,
		Evidence: protocol.ResearchEvidence{ID: "evidence-wire", Kind: autoresearch.FindingKindManual, Summary: "wire verified", Source: autoresearch.FindingSourceManual, Paths: []string{"internal/remote"}, Accepted: true},
	}
	if result := requestResult[protocol.ResearchEvidenceRecordResult](t, peer, protocol.MethodResearchEvidenceRecord, record); !result.Recorded {
		t.Fatalf("research/evidence/record = %+v", result)
	}
	requestResult[protocol.ResearchEvidenceRecordResult](t, peer, protocol.MethodResearchEvidenceRecord, record)
	if controller.record != 1 {
		t.Fatalf("research record replay calls = %d", controller.record)
	}
}

func TestMemoryResearchWireRejectsUntrustedIDsRevisionAndPaths(t *testing.T) {
	server, _, buildID := newMemoryResearchWireServer(t, true)
	peer, target, epoch := memoryResearchWireRuntime(t, server, buildID)
	query := protocol.RuntimeQuery{ExpectedHostEpoch: "host-test", Target: target, ExpectedRuntimeEpoch: epoch}
	suggestions := requestResult[protocol.MemorySuggestionsResult](t, peer, protocol.MethodMemorySuggestions, protocol.MemorySuggestionsParams{RuntimeQuery: query})
	status := requestResult[protocol.ResearchStatusResult](t, peer, protocol.MethodResearchStatus, protocol.ResearchStatusParams{RuntimeQuery: query})

	badSuggestion := protocol.MemorySuggestionAcceptParams{
		SessionMutation: mutation("bad-suggestion", target, epoch), SuggestionID: "../../client-candidate", ExpectedRevision: suggestions.Revision,
	}
	requireRemoteError(t, requestError(t, peer, protocol.MethodMemorySuggestionAccept, badSuggestion), protocol.ErrQueryFailed)
	stale := badSuggestion
	stale.RequestID = "stale-suggestion"
	stale.SuggestionID = suggestions.Memories[0].SuggestionID
	stale.ExpectedRevision = "stale-revision"
	requireRemoteError(t, requestError(t, peer, protocol.MethodMemorySuggestionAccept, stale), protocol.ErrStaleCursor)

	badEvidence := protocol.ResearchEvidenceRecordParams{
		SessionMutation: mutation("bad-evidence", target, epoch), TaskID: status.Task.TaskID, CriterionID: status.Task.OpenCriteria[0].CriterionID,
		Evidence: protocol.ResearchEvidence{ID: "evidence-bad", Kind: autoresearch.FindingKindManual, Summary: "bad", Source: autoresearch.FindingSourceManual, Paths: []string{"../../etc/passwd"}},
	}
	requireRemoteError(t, requestError(t, peer, protocol.MethodResearchEvidenceRecord, badEvidence), protocol.ErrQueryFailed)
	badTask := protocol.ResearchFindingsParams{RuntimeQuery: query, TaskID: "../../etc/passwd"}
	requireRemoteError(t, requestError(t, peer, protocol.MethodResearchFindings, badTask), protocol.ErrQueryFailed)
	malformedCursor := protocol.ResearchListParams{RuntimeQuery: query, Cursor: "not-a-cursor"}
	if response := requestError(t, peer, protocol.MethodResearchList, malformedCursor); response.Code != rpcwire.ErrInvalidParams {
		t.Fatalf("malformed Research cursor code = %d, want %d: %+v", response.Code, rpcwire.ErrInvalidParams, response)
	}
}

func TestMemoryResearchWireCapabilityUnavailable(t *testing.T) {
	server, _, buildID := newMemoryResearchWireServer(t, false)
	peer, target, epoch := memoryResearchWireRuntime(t, server, buildID)
	query := protocol.RuntimeQuery{ExpectedHostEpoch: "host-test", Target: target, ExpectedRuntimeEpoch: epoch}
	requireRemoteError(t, requestError(t, peer, protocol.MethodMemoryGet, protocol.MemoryGetParams{RuntimeQuery: query}), protocol.ErrCapabilityUnavailable)
	requireRemoteError(t, requestError(t, peer, protocol.MethodResearchStatus, protocol.ResearchStatusParams{RuntimeQuery: query}), protocol.ErrCapabilityUnavailable)
	remember := protocol.MemoryRememberParams{SessionMutation: mutation("unavailable-memory", target, epoch), Scope: "project", Note: "note"}
	requireRemoteError(t, requestError(t, peer, protocol.MethodMemoryRemember, remember), protocol.ErrCapabilityUnavailable)
}

func TestMemoryResearchCatalogChangedIsScopedAndExactlyOnce(t *testing.T) {
	server, _, buildID := newMemoryResearchWireServer(t, true)
	peer, changes := catalogChangedPeer(t, server)
	initializePeer(t, peer, buildID, "client-memory-research-catalog", "")
	target := daemonTestTarget()
	subscription := subscribePeer(t, peer, target)
	epoch := subscription.Snapshot.RuntimeEpoch
	query := protocol.RuntimeQuery{ExpectedHostEpoch: "host-test", Target: target, ExpectedRuntimeEpoch: epoch}

	projectRemember := protocol.MemoryRememberParams{SessionMutation: mutation("catalog-project-memory", target, epoch), Scope: "project", Note: "project catalog note"}
	requestResult[protocol.MemoryRememberResult](t, peer, protocol.MethodMemoryRemember, projectRemember)
	projectChange := receiveCatalogChanged(t, changes)
	if projectChange.Scope != protocol.CatalogWorkspace || len(projectChange.AffectedWorkspaceIDs) != 1 ||
		projectChange.AffectedWorkspaceIDs[0] != target.WorkspaceID || len(projectChange.Kinds) != 1 || projectChange.Kinds[0] != protocol.CatalogMemory {
		t.Fatalf("project Memory catalog/changed = %+v", projectChange)
	}
	requestResult[protocol.MemoryRememberResult](t, peer, protocol.MethodMemoryRemember, projectRemember)
	requireNoCatalogChanged(t, changes)

	userRemember := protocol.MemoryRememberParams{SessionMutation: mutation("catalog-user-memory", target, epoch), Scope: "user", Note: "user catalog note"}
	requestResult[protocol.MemoryRememberResult](t, peer, protocol.MethodMemoryRemember, userRemember)
	userChange := receiveCatalogChanged(t, changes)
	if userChange.Scope != protocol.CatalogHost || len(userChange.AffectedWorkspaceIDs) != 0 ||
		len(userChange.Kinds) != 1 || userChange.Kinds[0] != protocol.CatalogMemory || userChange.Revision == projectChange.Revision {
		t.Fatalf("user Memory catalog/changed = %+v", userChange)
	}

	suggestions := requestResult[protocol.MemorySuggestionsResult](t, peer, protocol.MethodMemorySuggestions, protocol.MemorySuggestionsParams{RuntimeQuery: query})
	if len(suggestions.Skills) == 0 {
		t.Fatal("expected project Skill suggestion")
	}
	acceptSkill := protocol.SkillSuggestionAcceptParams{
		SessionMutation: mutation("catalog-skill", target, epoch), SuggestionID: suggestions.Skills[0].SuggestionID,
		ExpectedRevision: suggestions.Revision,
	}
	requestResult[protocol.SkillSuggestionAcceptResult](t, peer, protocol.MethodSkillSuggestionAccept, acceptSkill)
	skillChange := receiveCatalogChanged(t, changes)
	if skillChange.Scope != protocol.CatalogWorkspace || len(skillChange.AffectedWorkspaceIDs) != 1 ||
		len(skillChange.Kinds) != 1 || skillChange.Kinds[0] != protocol.CatalogSessionCatalog || skillChange.Revision == userChange.Revision {
		t.Fatalf("Skill catalog/changed = %+v", skillChange)
	}

	status := requestResult[protocol.ResearchStatusResult](t, peer, protocol.MethodResearchStatus, protocol.ResearchStatusParams{RuntimeQuery: query})
	record := protocol.ResearchEvidenceRecordParams{
		SessionMutation: mutation("catalog-research", target, epoch), TaskID: status.Task.TaskID,
		CriterionID: status.Task.OpenCriteria[0].CriterionID,
		Evidence: protocol.ResearchEvidence{ID: "catalog-evidence", Kind: autoresearch.FindingKindManual, Summary: "verified",
			Source: autoresearch.FindingSourceManual, Paths: []string{"internal/remote"}, Accepted: true},
	}
	requestResult[protocol.ResearchEvidenceRecordResult](t, peer, protocol.MethodResearchEvidenceRecord, record)
	researchChange := receiveCatalogChanged(t, changes)
	if researchChange.Scope != protocol.CatalogWorkspace || len(researchChange.AffectedWorkspaceIDs) != 1 ||
		len(researchChange.Kinds) != 1 || researchChange.Kinds[0] != protocol.CatalogResearch || researchChange.Revision == skillChange.Revision {
		t.Fatalf("Research catalog/changed = %+v", researchChange)
	}
	requestResult[protocol.ResearchEvidenceRecordResult](t, peer, protocol.MethodResearchEvidenceRecord, record)
	requireNoCatalogChanged(t, changes)
	rejected := record
	rejected.RequestID = "catalog-research-rejected"
	rejected.Evidence.Paths = []string{"../../etc/passwd"}
	requireRemoteError(t, requestError(t, peer, protocol.MethodResearchEvidenceRecord, rejected), protocol.ErrQueryFailed)
	requireNoCatalogChanged(t, changes)
}

func TestMemoryResearchHandlerSetCoversFrozenDomain(t *testing.T) {
	set := memoryResearchHandlerSet(&transport{})
	want := []protocol.Method{
		protocol.MethodMemoryGet, protocol.MethodMemorySuggestions, protocol.MethodMemoryRemember,
		protocol.MethodMemoryForget, protocol.MethodMemoryDocumentSave, protocol.MethodMemorySuggestionAccept,
		protocol.MethodSkillSuggestionAccept, protocol.MethodResearchStatus, protocol.MethodResearchList,
		protocol.MethodResearchFindings, protocol.MethodResearchEvidenceRecord,
	}
	if len(set) != len(want) {
		t.Fatalf("handler set size = %d, want %d", len(set), len(want))
	}
	for _, method := range want {
		if set[method] == nil {
			t.Fatalf("missing handler %s", method)
		}
	}
}
