package runtimeservice

import (
	"encoding/json"
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
	"reasonix/internal/memory"
	"reasonix/internal/provider"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/skill"
)

type memoryResearchTestController struct {
	mu          sync.Mutex
	set         *memory.Set
	workspace   string
	sessionDir  string
	sessionPath string
	forgetCalls []string
	savedDocs   map[string]string
	saved       []memory.Memory
	skills      []skill.Skill
	created     []skillCreateCall
}

type skillCreateCall struct {
	name    string
	scope   skill.Scope
	content string
}

func (c *memoryResearchTestController) Memory() *memory.Set { return c.set }
func (c *memoryResearchTestController) QuickAdd(scope memory.Scope, note string) (string, error) {
	path := c.set.DocPath(scope)
	if err := memory.AppendDoc(path, note); err != nil {
		return "", err
	}
	c.set = memory.Load(memory.Options{CWD: c.set.CWD, UserDir: c.set.UserDir})
	return path, nil
}
func (c *memoryResearchTestController) ForgetMemory(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forgetCalls = append(c.forgetCalls, name)
	return nil
}
func (c *memoryResearchTestController) SaveDoc(path, body string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	written, err := c.set.WriteDoc(path, body)
	if err != nil {
		return "", err
	}
	c.set = memory.Load(memory.Options{CWD: c.set.CWD, UserDir: c.set.UserDir})
	if c.savedDocs == nil {
		c.savedDocs = make(map[string]string)
	}
	c.savedDocs[path] = body
	return written, nil
}
func (c *memoryResearchTestController) SaveMemory(value memory.Memory) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.saved = append(c.saved, value)
	return filepath.Join(c.set.Store.DirFor(value.Type), value.Name+".md"), nil
}
func (c *memoryResearchTestController) AllSkills() []skill.Skill {
	return append([]skill.Skill(nil), c.skills...)
}
func (c *memoryResearchTestController) CreateSkill(name string, scope skill.Scope, content string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.created = append(c.created, skillCreateCall{name: name, scope: scope, content: content})
	return filepath.Join(c.workspace, ".agents", "skills", name, "SKILL.md"), nil
}
func (c *memoryResearchTestController) WorkspaceRoot() string { return c.workspace }
func (c *memoryResearchTestController) SessionDir() string    { return c.sessionDir }
func (c *memoryResearchTestController) SessionPath() string   { return c.sessionPath }

func newMemoryResearchTestService(t *testing.T) (*MemoryResearchService, runtimeapi.SessionRef, string) {
	t.Helper()
	root := t.TempDir()
	session := runtimeapi.SessionRef{WorkspaceID: "workspace-1", SessionID: "session-1"}
	service, err := NewMemoryResearchService(RuntimeBinding{Session: session, Incarnation: "runtime-1"}, root)
	if err != nil {
		t.Fatalf("NewMemoryResearchService: %v", err)
	}
	return service, session, root
}

func TestMemoryProjectionUsesOpaqueIDsAndCollapsedPaths(t *testing.T) {
	service, session, root := newMemoryResearchTestService(t)
	userDir := t.TempDir()
	projectDoc := filepath.Join(root, "AGENTS.md")
	userDoc := filepath.Join(userDir, "REASONIX.md")
	if err := os.WriteFile(projectDoc, []byte("project rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userDoc, []byte("user rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	set := memory.Load(memory.Options{CWD: root, UserDir: userDir})
	if _, err := set.Store.Save(memory.Memory{Name: "secret-fact", Description: "safe description", Type: memory.TypeProject, Body: "safe body"}); err != nil {
		t.Fatal(err)
	}
	if _, err := set.Store.Save(memory.Memory{Name: "old-fact", Description: "old", Type: memory.TypeProject, Body: "old body"}); err != nil {
		t.Fatal(err)
	}
	if _, err := set.Store.Archive("old-fact"); err != nil {
		t.Fatal(err)
	}
	controller := &memoryResearchTestController{set: set, workspace: root}

	view, err := service.MemoryView(session, controller)
	if err != nil {
		t.Fatalf("MemoryView: %v", err)
	}
	if !view.Available || view.Revision == "" || len(view.Documents) != 2 || len(view.Facts) != 1 || len(view.Archives) != 1 {
		t.Fatalf("unexpected view: %+v", view)
	}
	raw, _ := json.Marshal(view)
	for _, forbidden := range []string{root, userDir, filepath.ToSlash(root), filepath.ToSlash(userDir)} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("wire view leaked Host path %q: %s", forbidden, raw)
		}
	}
	if strings.Contains(string(view.Facts[0].MemoryID), view.Facts[0].Name) || strings.Contains(string(view.Documents[0].DocumentID), filepath.Base(projectDoc)) {
		t.Fatalf("opaque IDs contain source identity: %+v", view)
	}
	for _, document := range view.Documents {
		if strings.HasPrefix(document.DisplayPath, "/") || strings.Contains(document.DisplayPath, "..") {
			t.Fatalf("unsafe document display path: %q", document.DisplayPath)
		}
	}

	if _, err := service.Forget(session, controller, runtimeapi.MemoryID("../../secret-fact")); !errors.Is(err, ErrUnknownMemoryID) {
		t.Fatalf("adversarial memory id error = %v", err)
	}
	if len(controller.forgetCalls) != 0 {
		t.Fatal("unknown opaque id reached Controller")
	}
	result, err := service.Forget(session, controller, view.Facts[0].MemoryID)
	if err != nil || !result.Forgotten || len(controller.forgetCalls) != 1 || controller.forgetCalls[0] != "secret-fact" {
		t.Fatalf("Forget = %+v, calls=%v, err=%v", result, controller.forgetCalls, err)
	}

	if _, err := service.SaveDocument(session, controller, runtimeapi.DocumentID("/etc/passwd"), "malicious"); !errors.Is(err, ErrUnknownDocumentID) {
		t.Fatalf("adversarial document id error = %v", err)
	}
	legitimate := view.Documents[0]
	if _, err := service.SaveDocument(session, controller, legitimate.DocumentID, "updated"); err != nil {
		t.Fatalf("SaveDocument: %v", err)
	}
	if len(controller.savedDocs) != 1 {
		t.Fatalf("saved docs = %v", controller.savedDocs)
	}
}

func TestMemorySuggestionsAreExactCachedCandidates(t *testing.T) {
	service, session, root := newMemoryResearchTestService(t)
	userDir := t.TempDir()
	sessionDir := filepath.Join(root, ".reasonix", "sessions")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for index, text := range []string{
		"Always verify the frozen Remote architecture before changing the protocol.",
		"Remember: never expose raw Host paths in Remote responses.",
	} {
		value := agent.NewSession("system")
		value.Add(provider.Message{Role: provider.RoleUser, Content: text})
		path := filepath.Join(sessionDir, fmt.Sprintf("2026-07-17T12000%d-test.jsonl", index))
		if err := value.Save(path); err != nil {
			t.Fatalf("save suggestion session: %v", err)
		}
	}
	set := memory.Load(memory.Options{CWD: root, UserDir: userDir})
	controller := &memoryResearchTestController{set: set, workspace: root, sessionDir: sessionDir}

	view, err := service.Suggestions(session, controller)
	if err != nil {
		t.Fatalf("Suggestions: %v", err)
	}
	if !view.Available || view.Revision == "" || len(view.Memories) == 0 {
		t.Fatalf("unexpected suggestions: %+v", view)
	}
	for _, item := range view.Memories {
		for _, evidence := range item.Evidence {
			if strings.Contains(evidence, sessionDir) || strings.Contains(evidence, ".jsonl") {
				t.Fatalf("suggestion evidence leaked Session path: %q", evidence)
			}
		}
	}

	if _, err := service.AcceptMemorySuggestion(session, controller, runtimeapi.SuggestionID("candidate-from-client"), view.Revision); !errors.Is(err, ErrUnknownSuggestionID) {
		t.Fatalf("client candidate error = %v", err)
	}
	if len(controller.saved) != 0 {
		t.Fatal("untrusted candidate reached SaveMemory")
	}
	if _, err := service.AcceptMemorySuggestion(session, controller, view.Memories[0].SuggestionID, "old-revision"); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale revision error = %v", err)
	}
	changed := agent.NewSession("system")
	changed.Add(provider.Message{Role: provider.RoleUser, Content: "Never accept a Remote suggestion after its source revision changes."})
	if err := changed.Save(filepath.Join(sessionDir, "2026-07-17T130000-changed.jsonl")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcceptMemorySuggestion(session, controller, view.Memories[0].SuggestionID, view.Revision); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("changed source revision was accepted: %v", err)
	}
	view, err = service.Suggestions(session, controller)
	if err != nil || len(view.Memories) == 0 {
		t.Fatalf("refreshed Suggestions = %+v, %v", view, err)
	}
	wantBody := ""
	if view.Memories[0].Body != nil {
		wantBody = *view.Memories[0].Body
	}
	result, err := service.AcceptMemorySuggestion(session, controller, view.Memories[0].SuggestionID, view.Revision)
	if err != nil || result.MemoryID == "" || len(controller.saved) != 1 || controller.saved[0].Body != wantBody {
		t.Fatalf("AcceptMemorySuggestion = %+v saved=%+v err=%v", result, controller.saved, err)
	}
	if _, err := service.AcceptMemorySuggestion(session, controller, view.Memories[0].SuggestionID, view.Revision); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("consumed cache replay without idempotency was not rejected: %v", err)
	}
}

func TestRememberReturnsAddressableIDThatCanBeForgotten(t *testing.T) {
	service, session, root := newMemoryResearchTestService(t)
	path := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(path, []byte("baseline\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := &memoryResearchTestController{
		set: memory.Load(memory.Options{CWD: root, UserDir: t.TempDir()}), workspace: root,
	}
	remembered, err := service.Remember(session, controller, "project", "keep the Remote protocol frozen")
	if err != nil || remembered.MemoryID == "" || remembered.InvalidationScope != runtimeapi.CatalogWorkspace {
		t.Fatalf("Remember = %+v, %v", remembered, err)
	}
	if _, err := service.MemoryView(session, controller); err != nil {
		t.Fatalf("MemoryView after Remember: %v", err)
	}
	forgotten, err := service.Forget(session, controller, remembered.MemoryID)
	if err != nil || !forgotten.Forgotten || forgotten.InvalidationScope != runtimeapi.CatalogWorkspace {
		t.Fatalf("Forget remembered ID = %+v, %v", forgotten, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || strings.TrimSpace(string(raw)) != "baseline" {
		t.Fatalf("forgotten document = %q, %v", raw, err)
	}
	if _, err := service.Forget(session, controller, remembered.MemoryID); !errors.Is(err, ErrUnknownMemoryID) {
		t.Fatalf("forgotten ID remained addressable: %v", err)
	}
}

func TestResearchPagingPathsAndCursorsAreBound(t *testing.T) {
	service, session, root := newMemoryResearchTestService(t)
	summaries := make([]autoresearch.Summary, 205)
	for index := range summaries {
		summaries[index] = autoresearch.Summary{
			TaskID: fmt.Sprintf("20260717-%03d-task", index), Goal: fmt.Sprintf("goal %d", index),
			Status: autoresearch.StatusRunning, FindingCount: index,
			TaskPath: filepath.Join(root, ".reasonix", "autoresearch", fmt.Sprintf("task-%d", index)),
		}
	}
	first, err := service.ResearchList(session, summaries, "", 0)
	if err != nil {
		t.Fatalf("ResearchList: %v", err)
	}
	if len(first.Items) != runtimeapi.PageDefaultItems || !first.HasMore || first.Next == "" {
		t.Fatalf("first page = items=%d more=%v cursor=%q", len(first.Items), first.HasMore, first.Next)
	}
	second, err := service.ResearchList(session, summaries, first.Next, 0)
	if err != nil || len(second.Items) != 5 || second.HasMore {
		t.Fatalf("second page = %+v err=%v", second, err)
	}
	raw, _ := json.Marshal(first)
	if strings.Contains(string(raw), root) || strings.Contains(string(raw), "task-") {
		t.Fatalf("research list leaked TaskPath: %s", raw)
	}
	changed := append([]autoresearch.Summary(nil), summaries...)
	changed[0].FindingCount++
	if _, err := service.ResearchList(session, changed, first.Next, 0); !errors.Is(err, ErrStaleCursor) {
		t.Fatalf("changed revision cursor error = %v", err)
	}
	if _, err := service.ResearchList(session, summaries, "not-a-cursor", 0); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("malformed cursor error = %v", err)
	}

	when := time.Date(2026, 7, 17, 13, 0, 0, 0, time.UTC)
	findings := []autoresearch.Finding{{
		ID: "finding-1", Kind: autoresearch.FindingKindFile, Summary: "verified file", Source: autoresearch.FindingSourceFile,
		Command: "git -C " + root + " status", Paths: []string{
			filepath.Join(root, "docs", "REMOTE.md"), "internal/remote", "../outside", "/etc/passwd",
			`C:\Windows\secret`, `dir\hidden`, "colon:name", "control\nname",
		},
		Accepted: true, CreatedAt: when,
	}}
	issuedTaskID := first.Items[0].TaskID
	if _, err := service.ResearchFindings(session, issuedTaskID, findings, first.Next, 1); !errors.Is(err, ErrStaleCursor) {
		t.Fatalf("cross-method cursor error = %v", err)
	}
	page, err := service.ResearchFindings(session, issuedTaskID, findings, "", 1)
	if err != nil {
		t.Fatalf("ResearchFindings: %v", err)
	}
	if got := page.Items[0].Paths; len(got) != 2 || got[0] != "docs/REMOTE.md" || got[1] != "internal/remote" {
		t.Fatalf("projected paths = %v", got)
	}
	if strings.Contains(page.Items[0].Command, root) || page.Items[0].Command != "git -C . status" {
		t.Fatalf("projected command = %q", page.Items[0].Command)
	}
	findings[0].Command = "cat /etc/passwd"
	page, err = service.ResearchFindings(session, issuedTaskID, findings, "", 1)
	if err != nil || page.Items[0].Command != "" {
		t.Fatalf("external absolute command was not redacted: %+v err=%v", page, err)
	}
	if _, err := service.ResearchFindings(session, "../escape", findings, "", 1); !errors.Is(err, ErrInvalidResearchInput) {
		t.Fatalf("adversarial task id error = %v", err)
	}
	for _, unsafeCommand := range []string{"tool --path=/etc/passwd", "HOME=/etc tool", `cat C:\Windows\secret`, "cat /etc/passwd", "tool > output"} {
		findings[0].Command = unsafeCommand
		page, err = service.ResearchFindings(session, issuedTaskID, findings, "", 1)
		if err != nil || page.Items[0].Command != "" {
			t.Fatalf("unsafe command %q was not redacted: %+v err=%v", unsafeCommand, page, err)
		}
	}
	findings[0].Source = "file:///etc/passwd"
	if _, err := service.ResearchFindings(session, issuedTaskID, findings, "", 1); !errors.Is(err, ErrQueryFailed) {
		t.Fatalf("uncontrolled finding source error = %v", err)
	}
}

func TestPrepareResearchEvidenceRejectsEscapes(t *testing.T) {
	service, session, _ := newMemoryResearchTestService(t)
	status, err := service.ResearchStatus(session, &autoresearch.Summary{
		TaskID: "20260717-task", Status: autoresearch.StatusRunning,
		OpenCriteria: []autoresearch.CriterionSummary{{ID: "criterion-1", Description: "verify", Status: "open"}},
	})
	if err != nil || status.Task == nil || len(status.Task.OpenCriteria) != 1 {
		t.Fatalf("ResearchStatus = %+v, %v", status, err)
	}
	taskID := status.Task.TaskID
	criterionID := status.Task.OpenCriteria[0].CriterionID
	if string(taskID) == "20260717-task" || string(criterionID) == "criterion-1" {
		t.Fatalf("Research store IDs were exposed: task=%q criterion=%q", taskID, criterionID)
	}
	if _, err := service.ResolveResearchTask(session, "20260717-task"); !errors.Is(err, ErrInvalidResearchInput) {
		t.Fatalf("guessed task ID resolved: %v", err)
	}
	if _, _, err := service.ResolveResearchEvidenceTarget(session, taskID, "criterion-1"); !errors.Is(err, ErrInvalidResearchInput) {
		t.Fatalf("guessed criterion ID resolved: %v", err)
	}
	input := runtimeapi.ResearchEvidence{
		ID: "evidence-1", Kind: autoresearch.FindingKindManual, Summary: "verified", Source: autoresearch.FindingSourceManual,
		Paths: []string{"internal/remote/host"}, Accepted: true,
	}
	finding, err := service.PrepareResearchEvidence(session, taskID, criterionID, input, time.Unix(10, 0))
	if err != nil || len(finding.Paths) != 1 || finding.CreatedAt.IsZero() {
		t.Fatalf("PrepareResearchEvidence = %+v, %v", finding, err)
	}
	input.Paths = []string{"../../etc/passwd"}
	if _, err := service.PrepareResearchEvidence(session, taskID, criterionID, input, time.Now()); !errors.Is(err, ErrInvalidResearchInput) {
		t.Fatalf("escape path error = %v", err)
	}
	input.Paths = nil
	input.ID = "bad/id"
	if _, err := service.PrepareResearchEvidence(session, taskID, criterionID, input, time.Now()); !errors.Is(err, ErrInvalidResearchInput) {
		t.Fatalf("adversarial evidence id error = %v", err)
	}
	input.ID = "evidence-2"
	input.Source = "file:///etc/passwd"
	if _, err := service.PrepareResearchEvidence(session, taskID, criterionID, input, time.Now()); !errors.Is(err, ErrInvalidResearchInput) {
		t.Fatalf("uncontrolled evidence source error = %v", err)
	}
}

func TestMemoryResearchHardResponseAndSourceBudgets(t *testing.T) {
	service, session, root := newMemoryResearchTestService(t)
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(strings.Repeat("x", memoryResearchJSONBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	controller := &memoryResearchTestController{set: memory.Load(memory.Options{CWD: root, UserDir: t.TempDir()}), workspace: root}
	if _, err := service.MemoryView(session, controller); !errors.Is(err, ErrQueryFailed) {
		t.Fatalf("oversized Memory response error = %v", err)
	}
	if _, err := service.ResearchStatus(session, &autoresearch.Summary{
		TaskID: "oversized-task", Status: autoresearch.StatusRunning, Goal: strings.Repeat("g", memoryResearchJSONBytes+1),
	}); !errors.Is(err, ErrQueryFailed) {
		t.Fatalf("oversized Research status error = %v", err)
	}
	summaries := make([]autoresearch.Summary, memoryResearchSourceItems+1)
	if _, err := service.ResearchList(session, summaries, "", 0); !errors.Is(err, ErrQueryFailed) {
		t.Fatalf("unbounded Research source error = %v", err)
	}
}

func TestMemoryResearchServiceConcurrentQueries(t *testing.T) {
	service, session, root := newMemoryResearchTestService(t)
	controller := &memoryResearchTestController{
		set: memory.Load(memory.Options{CWD: root, UserDir: t.TempDir()}), workspace: root,
	}
	var wait sync.WaitGroup
	for index := 0; index < 20; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := service.MemoryView(session, controller); err != nil {
				t.Errorf("MemoryView: %v", err)
			}
			if _, err := service.Suggestions(session, controller); err != nil {
				t.Errorf("Suggestions: %v", err)
			}
			if _, err := service.ResearchList(session, nil, "", 10); err != nil {
				t.Errorf("ResearchList: %v", err)
			}
		}()
	}
	wait.Wait()
}
