package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/desktop/internal/draftstate"
	"reasonix/desktop/internal/workspacestate"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
)

func TestDraftAdmissionErrorPreservesWrappedCodedError(t *testing.T) {
	coded := &inboxCodedError{code: "image_attachment_unreadable", cause: errors.New("missing image")}
	wrapped := fmt.Errorf("validate draft: %w", coded)

	got := draftAdmissionError(wrapped)
	var found *inboxCodedError
	if !errors.As(got, &found) || found != coded {
		t.Fatalf("draftAdmissionError() = %v, want wrapped coded error", got)
	}
	if strings.Contains(got.Error(), "draft submission not admitted") {
		t.Fatalf("draftAdmissionError() added generic prefix: %v", got)
	}
}

func beginDraftTestOperation(t *testing.T, a *App, phase string) (draftstate.Draft, draftstate.Operation) {
	t.Helper()
	draft, _, err := a.draftStore().Open(context.Background(), "workspace", "project", t.TempDir(), "draft-"+phase, `{}`)
	if err != nil {
		t.Fatal(err)
	}
	op, _, err := a.draftStore().BeginOperation(context.Background(), draftstate.Operation{
		ID: "operation-" + phase, DraftID: draft.ID, WorkspaceID: draft.WorkspaceID,
		DraftRevision: draft.Revision, SessionID: "session-" + phase, TopicID: "topic-" + phase,
		SubmissionID: "submission-" + phase, Fingerprint: "fingerprint-" + phase, RequestJSON: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if phase != "reserved" {
		op, err = a.draftStore().SetOperationPhase(context.Background(), op.ID, phase, "")
		if err != nil {
			t.Fatal(err)
		}
	}
	return draft, op
}

func newDraftTestApp(t *testing.T) *App {
	t.Helper()
	a := NewApp()
	a.desktopDrafts = draftstate.New(filepath.Join(t.TempDir(), "drafts.sqlite"))
	t.Cleanup(func() { _ = a.desktopDrafts.Close() })
	return a
}

func TestDraftExternalFolderWithSpacesUsesStableRuntimeToken(t *testing.T) {
	path := filepath.Join(string(filepath.Separator), "Users", "example", "Folder With Spaces")
	input := "inspect @" + path + "/ and keep this visible"
	got := rewriteDraftExternalFolderRef(input, path, "__reasonix_external_folder/abc/Folder-With-Spaces")
	want := "inspect @__reasonix_external_folder/abc/Folder-With-Spaces/ and keep this visible"
	if got != want {
		t.Fatalf("rewritten input = %q, want %q", got, want)
	}
}

func TestDraftHostIdentitiesDoNotEnterProviderSubmission(t *testing.T) {
	request := SessionDraftSubmissionRequest{
		DraftID: "draft-secret", Display: "display", Input: "input", Goal: "goal",
		ToolApprovalMode: "ask", Invocations: []InvocationRequest{{Name: "skill", Kind: "skill"}},
	}
	providerRequest := draftControlSubmissionRequest("submission-secret", request)
	body, err := json.Marshal(providerRequest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) == "" || containsAny(string(body), "draft-secret", "submission-secret") {
		t.Fatalf("host identity leaked into provider request: %s", body)
	}
}

func TestDraftModelResolutionIsStrict(t *testing.T) {
	cfg := &config.Config{Providers: []config.ProviderEntry{{Name: "fixture", Model: "model-a"}}}
	if _, err := resolveDraftCreateModelStrict(cfg, "removed/model"); !errors.Is(err, boot.ErrUnknownModel) {
		t.Fatalf("strict resolution error = %v, want boot.ErrUnknownModel", err)
	}
	resolved, err := resolveDraftCreateModelStrict(cfg, "model-a")
	if err != nil || resolved != "fixture/model-a" {
		t.Fatalf("alias resolution = %q, %v", resolved, err)
	}
	pluginRef := "plugin/example/model-a"
	resolved, err = resolveDraftCreateModelStrict(cfg, pluginRef)
	if err != nil || resolved != pluginRef {
		t.Fatalf("plugin resolution = %q, %v", resolved, err)
	}
}

func TestModelsForDraftUsesItsWorkspaceConfiguration(t *testing.T) {
	isolateDesktopUserDirs(t)
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(`
default_model = "local/model-a"

[desktop]
provider_access = ["local"]

[[providers]]
name = "local"
kind = "openai"
base_url = "http://127.0.0.1:23333/v1"
models = ["model-a", "model-b"]
default = "model-a"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newDraftTestApp(t)
	rootA, rootB := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(rootA, "reasonix.toml"), []byte(`default_model = "local/model-a"`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootB, "reasonix.toml"), []byte(`default_model = "local/model-b"`), 0o644); err != nil {
		t.Fatal(err)
	}
	draftA, err := a.OpenSessionDraftForTarget("project", rootA)
	if err != nil {
		t.Fatal(err)
	}
	draftB, err := a.OpenSessionDraftForTarget("project", rootB)
	if err != nil {
		t.Fatal(err)
	}
	current := func(models []ModelInfo) string {
		for _, model := range models {
			if model.Current {
				return model.Ref
			}
		}
		return ""
	}
	if got := current(a.ModelsForDraft(draftA.ID)); got != "local/model-a" {
		t.Fatalf("workspace A current model = %q, want local/model-a", got)
	}
	if got := current(a.ModelsForDraft(draftB.ID)); got != "local/model-b" {
		t.Fatalf("workspace B current model = %q, want local/model-b", got)
	}
}

func TestDraftRetryAppliesFrozenSettingsToReservedSession(t *testing.T) {
	a := newDraftTestApp(t)
	oldEffort := "low"
	tab := &WorkspaceTab{
		ID: "tab", SessionID: "session", PendingCreateOperationID: "old-operation",
		model: "old/model", effort: &oldEffort, mode: "normal", toolApprovalMode: "ask",
		disabledMCP: map[string]ServerView{}, mcpOrder: []string{"old"},
	}
	a.tabs[tab.ID] = tab
	a.tabOrder = []string{tab.ID}
	settings := SessionDraftSettings{
		Model: "new/model", Effort: "high", QualityFloor: "high", CollaborationMode: "plan",
		ToolApprovalMode: control.ToolApprovalDangerFullAccess,
		DisabledMCP:      map[string]ServerView{"disabled": {Name: "disabled"}}, MCPOrder: []string{"disabled"},
	}
	if err := a.applyDraftOperationSettings(draftstate.Operation{ID: "new-operation", SessionID: tab.SessionID}, settings); err != nil {
		t.Fatal(err)
	}
	if tab.PendingCreateOperationID != "new-operation" || tab.model != "new/model" || tab.effort == nil || *tab.effort != "high" {
		t.Fatalf("retry identity/model/effort = %q / %q / %+v", tab.PendingCreateOperationID, tab.model, tab.effort)
	}
	if tab.qualityFloor != "high" || !tabModeHasPlan(tab.mode) || normalizeToolApprovalMode(tab.toolApprovalMode) != control.ToolApprovalDangerFullAccess {
		t.Fatalf("retry profile = quality %q mode %q approval %q", tab.qualityFloor, tab.mode, tab.toolApprovalMode)
	}
	if _, ok := tab.disabledMCP["disabled"]; !ok || len(tab.mcpOrder) != 1 || tab.mcpOrder[0] != "disabled" {
		t.Fatalf("retry MCP profile = disabled %+v order %+v", tab.disabledMCP, tab.mcpOrder)
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func TestOpenSessionDraftDoesNotCreateRuntimeArtifacts(t *testing.T) {
	a := newDraftTestApp(t)
	root := t.TempDir()
	var id string
	for range 20 {
		draft, err := a.OpenSessionDraftForTarget("project", root)
		if err != nil {
			t.Fatalf("OpenSessionDraftForTarget() error = %v", err)
		}
		if id == "" {
			id = draft.ID
		}
		if draft.ID != id {
			t.Fatalf("draft ID = %q, want reused %q", draft.ID, id)
		}
	}
	a.mu.RLock()
	visible, detached := len(a.tabs), len(a.detachedSessions)
	a.mu.RUnlock()
	if visible != 0 || detached != 0 {
		t.Fatalf("runtime tabs = visible %d detached %d, want zero", visible, detached)
	}
	if entries, err := os.ReadDir(desktopSessionDir(root)); err == nil && len(entries) != 0 {
		t.Fatalf("draft open created session files: %+v", entries)
	}
}

func TestDraftsStayIsolatedAcrossWorkspaces(t *testing.T) {
	a := newDraftTestApp(t)
	rootA, rootB := t.TempDir(), t.TempDir()
	draftA, err := a.OpenSessionDraftForTarget("project", rootA)
	if err != nil {
		t.Fatal(err)
	}
	draftB, err := a.OpenSessionDraftForTarget("project", rootB)
	if err != nil {
		t.Fatal(err)
	}
	if draftA.ID == draftB.ID {
		t.Fatal("different workspaces reused one DraftID")
	}
	if _, err := a.SaveSessionDraft(SessionDraftSaveRequest{DraftID: draftA.ID, Revision: draftA.Revision, ContentJSON: `{"text":"A"}`, Settings: draftA.Settings}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SaveSessionDraft(SessionDraftSaveRequest{DraftID: draftB.ID, Revision: draftB.Revision, ContentJSON: `{"text":"B"}`, Settings: draftB.Settings}); err != nil {
		t.Fatal(err)
	}
	reopenedA, err := a.OpenSessionDraftForTarget("project", rootA)
	if err != nil {
		t.Fatal(err)
	}
	reopenedB, err := a.OpenSessionDraftForTarget("project", rootB)
	if err != nil {
		t.Fatal(err)
	}
	if reopenedA.ContentJSON != `{"text":"A"}` || reopenedB.ContentJSON != `{"text":"B"}` {
		t.Fatalf("restored content = %s / %s", reopenedA.ContentJSON, reopenedB.ContentJSON)
	}
}

func TestLateSaveReportsDiscardedWithoutRevivingDraft(t *testing.T) {
	a := newDraftTestApp(t)
	draft, err := a.OpenSessionDraftForTarget("project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.DiscardSessionDraft(draft.ID, draft.Revision); err != nil {
		t.Fatal(err)
	}
	result, err := a.SaveSessionDraft(SessionDraftSaveRequest{
		DraftID: draft.ID, Revision: draft.Revision, ContentJSON: `{"text":"late"}`, Settings: draft.Settings,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "discarded" || result.Draft.Status != "discarded" {
		t.Fatalf("late save result = %+v, want discarded", result)
	}
	restored, err := a.draftStore().Get(t.Context(), draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Status != "discarded" || strings.Contains(restored.ContentJSON, "late") {
		t.Fatalf("late save revived or overwrote draft: %+v", restored)
	}
}

func TestMissingDraftAttachmentFailsBeforeSessionReservation(t *testing.T) {
	a := newDraftTestApp(t)
	root := t.TempDir()
	draft, err := a.OpenSessionDraftForTarget("project", root)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := a.SaveSessionDraft(SessionDraftSaveRequest{
		DraftID: draft.ID, Revision: draft.Revision,
		ContentJSON: `{"text":"inspect","attachments":[{"path":".reasonix/attachments/missing.txt"}]}`,
		Settings:    draft.Settings,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.BeginDraftSubmission(SessionDraftSubmissionRequest{DraftID: draft.ID, Revision: saved.Draft.Revision, Display: "inspect", Input: "inspect @.reasonix/attachments/missing.txt"}); err == nil {
		t.Fatal("missing attachment should reject submission")
	}
	if operations, err := a.draftStore().PendingOperations(a.bootContext()); err != nil || len(operations) != 0 {
		t.Fatalf("operations after validation failure = %+v, err %v", operations, err)
	}
	if tabs := a.ListTabs(); len(tabs) != 0 {
		t.Fatalf("tabs after validation failure = %+v", tabs)
	}
}

func TestMissingDraftImageReturnsStableErrorWithoutHostPath(t *testing.T) {
	a := newDraftTestApp(t)
	root := t.TempDir()
	draft, err := a.OpenSessionDraftForTarget("project", root)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := a.SaveSessionDraft(SessionDraftSaveRequest{
		DraftID: draft.ID, Revision: draft.Revision,
		ContentJSON: `{"text":"inspect","attachments":[{"path":".reasonix/attachments/missing.png"}]}`,
		Settings:    draft.Settings,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.BeginDraftSubmission(SessionDraftSubmissionRequest{
		DraftID: draft.ID, Revision: saved.Draft.Revision,
		Display: "inspect", Input: "inspect @.reasonix/attachments/missing.png",
	})
	if err == nil || err.Error() != "reasonix_error:image_attachment_unreadable" {
		t.Fatalf("BeginDraftSubmission() error = %v, want stable image failure", err)
	}
	if strings.Contains(err.Error(), root) {
		t.Fatalf("bridge error exposed workspace root: %v", err)
	}
	if operations, loadErr := a.draftStore().PendingOperations(a.bootContext()); loadErr != nil || len(operations) != 0 {
		t.Fatalf("operations after image validation failure = %+v, err %v", operations, loadErr)
	}
	if tabs := a.ListTabs(); len(tabs) != 0 {
		t.Fatalf("tabs after image validation failure = %+v", tabs)
	}
}

func TestInvalidDraftModelFailsBeforeSessionReservation(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := newDraftTestApp(t)
	root := t.TempDir()
	draft, err := a.OpenSessionDraftForTarget("project", root)
	if err != nil {
		t.Fatal(err)
	}
	settings := draft.Settings
	settings.Model = "removed-provider/removed-model"
	saved, err := a.SaveSessionDraft(SessionDraftSaveRequest{
		DraftID: draft.ID, Revision: draft.Revision, ContentJSON: `{"text":"inspect"}`, Settings: settings,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.BeginDraftSubmission(SessionDraftSubmissionRequest{
		DraftID: draft.ID, Revision: saved.Draft.Revision, Display: "inspect", Input: "inspect",
	})
	if !errors.Is(err, boot.ErrUnknownModel) {
		t.Fatalf("BeginDraftSubmission() error = %v, want boot.ErrUnknownModel", err)
	}
	if operations, listErr := a.draftStore().PendingOperations(a.bootContext()); listErr != nil || len(operations) != 0 {
		t.Fatalf("operations after invalid model = %+v, err %v", operations, listErr)
	}
	if tabs := a.ListTabs(); len(tabs) != 0 {
		t.Fatalf("tabs after invalid model = %+v", tabs)
	}
}

func TestDraftManagementCommandCannotCreateSession(t *testing.T) {
	a := newDraftTestApp(t)
	draft, err := a.OpenSessionDraftForTarget("project", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"/new", "/compact", "/model provider/model", "/theme dark", "/mcp"} {
		if _, err := a.BeginDraftSubmission(SessionDraftSubmissionRequest{
			DraftID: draft.ID, Revision: draft.Revision, Display: input, Input: input,
		}); err == nil {
			t.Fatalf("%s created a draft submission", input)
		}
	}
	if operations, err := a.draftStore().PendingOperations(a.bootContext()); err != nil || len(operations) != 0 {
		t.Fatalf("management commands reserved operations = %+v, err %v", operations, err)
	}
	if tabs := a.ListTabs(); len(tabs) != 0 {
		t.Fatalf("management commands created tabs = %+v", tabs)
	}
}

func TestDraftContextProjectsConfiguredMCPWithoutController(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := newDraftTestApp(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(`
[[plugins]]
name = "fixture"
command = "fixture-mcp"
args = ["serve"]
`), 0o644); err != nil {
		t.Fatal(err)
	}
	draft, err := a.OpenSessionDraftForTarget("project", root)
	if err != nil {
		t.Fatal(err)
	}
	context, err := a.GetDraftContext(draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(context.Servers) != 1 || context.Servers[0].Name != "fixture" || context.Servers[0].Command != "fixture-mcp" {
		t.Fatalf("draft MCP projection = %+v", context.Servers)
	}
	if !context.Servers[0].Enabled || context.Servers[0].RuntimeState != "idle" {
		t.Fatalf("draft MCP availability = %+v", context.Servers[0])
	}
	if tabs := a.ListTabs(); len(tabs) != 0 {
		t.Fatalf("capability projection created runtime tabs: %+v", tabs)
	}
}

func TestTargetedAttachmentsDoNotFollowActiveWorkspace(t *testing.T) {
	a := newDraftTestApp(t)
	rootA, rootB := t.TempDir(), t.TempDir()
	draftA, err := a.OpenSessionDraftForTarget("project", rootA)
	if err != nil {
		t.Fatal(err)
	}
	draftB, err := a.OpenSessionDraftForTarget("project", rootB)
	if err != nil {
		t.Fatal(err)
	}
	payloadA := "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("attachment-a"))
	payloadB := "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("attachment-b"))
	targetA := ComposerTarget{Kind: "draft", DraftID: draftA.ID}
	targetB := ComposerTarget{Kind: "draft", DraftID: draftB.ID}
	var pathA, pathB string
	var errA, errB error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); pathA, errA = a.SavePastedFileForComposerTarget(targetA, "a.txt", payloadA) }()
	go func() { defer wg.Done(); pathB, errB = a.SavePastedFileForComposerTarget(targetB, "b.txt", payloadB) }()
	wg.Wait()
	if errA != nil || errB != nil {
		t.Fatalf("targeted saves = %v / %v", errA, errB)
	}
	gotA, err := os.ReadFile(filepath.Join(rootA, filepath.FromSlash(pathA)))
	if err != nil {
		t.Fatal(err)
	}
	gotB, err := os.ReadFile(filepath.Join(rootB, filepath.FromSlash(pathB)))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotA) != "attachment-a" || string(gotB) != "attachment-b" {
		t.Fatalf("attachment contents = %q / %q", gotA, gotB)
	}
	if _, err := os.Stat(filepath.Join(rootB, filepath.FromSlash(pathA))); err == nil && pathA != pathB {
		t.Fatalf("A attachment %q leaked into workspace B", pathA)
	}
}

func TestReconcileDraftSubmissionOperationsNeverReplaysInterruptedDispatch(t *testing.T) {
	for _, phase := range []string{"reserved", "starting", "dispatching", "dispatching_shell"} {
		t.Run(phase, func(t *testing.T) {
			a := newDraftTestApp(t)
			_, op := beginDraftTestOperation(t, a, phase)
			a.reconcileDraftSubmissionOperations()
			got, err := a.draftStore().Operation(context.Background(), op.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := "resume_required"
			if phase == "dispatching" || phase == "dispatching_shell" {
				want = "dispatch_unknown"
			}
			if got.Phase != want {
				t.Fatalf("phase after restart = %q, want %q", got.Phase, want)
			}
			if tabs := a.ListTabs(); len(tabs) != 0 {
				t.Fatalf("startup reconciliation created runtime tabs: %+v", tabs)
			}
		})
	}
}

func TestReconcileAcceptedDraftCompletesConversion(t *testing.T) {
	a := newDraftTestApp(t)
	draft, op := beginDraftTestOperation(t, a, "accepted")
	a.reconcileDraftSubmissionOperations()
	got, err := a.draftStore().Get(context.Background(), draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "converted" {
		t.Fatalf("draft status = %q, want converted", got.Status)
	}
	accepted, err := a.draftStore().Operation(context.Background(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Phase != "accepted" || accepted.SessionID != op.SessionID || accepted.TopicID != op.TopicID {
		t.Fatalf("accepted operation identity changed: %+v", accepted)
	}
}

func TestPersistedDraftCreateOperationKeepsIdentity(t *testing.T) {
	tab := &WorkspaceTab{ID: "tab", SessionID: "session", PendingCreateOperationID: "operation"}
	entry := persistedDesktopTabEntry(tab)
	if entry.CreateOperationID != "operation" {
		t.Fatalf("persisted create operation = %q", entry.CreateOperationID)
	}
}

func TestDraftRetryReplacesOnlyTerminalWorkspaceReservation(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := newDraftTestApp(t)
	root := t.TempDir()
	draft, err := a.OpenSessionDraftForTarget("project", root)
	if err != nil {
		t.Fatal(err)
	}
	old, _, err := a.draftStore().BeginOperation(t.Context(), draftstate.Operation{
		ID: "old", DraftID: draft.ID, WorkspaceID: draft.WorkspaceID, DraftRevision: draft.Revision,
		SessionID: "session", TopicID: "topic", SubmissionID: "submission-old", Fingerprint: "old", RequestJSON: `{}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.workspaceRegistry().BeginCreate(t.Context(), workspacestate.PendingCreate{OperationID: old.ID, WorkspaceID: draft.WorkspaceID, SessionID: old.SessionID}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.draftStore().SetOperationPhase(t.Context(), old.ID, "terminal_failed", "failed before bind"); err != nil {
		t.Fatal(err)
	}
	next, created, err := a.draftStore().BeginOperation(t.Context(), draftstate.Operation{
		ID: "new", DraftID: draft.ID, WorkspaceID: draft.WorkspaceID, DraftRevision: draft.Revision,
		SessionID: "different", TopicID: "different", SubmissionID: "submission-new", Fingerprint: "new", RequestJSON: `{}`,
	})
	if err != nil || !created {
		t.Fatalf("retry operation = %+v, created %v, err %v", next, created, err)
	}
	if next.SessionID != old.SessionID {
		t.Fatalf("retry session = %q, want %q", next.SessionID, old.SessionID)
	}
	if err := a.beginDraftWorkspaceCreate(next, draft.WorkspaceID); err != nil {
		t.Fatalf("begin retry create: %v", err)
	}
	state, err := a.workspaceRegistry().Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if pending := state.PendingCreates[next.SessionID]; pending.OperationID != next.ID {
		t.Fatalf("pending create = %+v, want retry operation", pending)
	}
}

func TestDraftSubmissionRefusesArchivedReservedSession(t *testing.T) {
	isolateDesktopUserDirs(t)
	a := newDraftTestApp(t)
	root := t.TempDir()
	draft, err := a.OpenSessionDraftForTarget("project", root)
	if err != nil {
		t.Fatal(err)
	}
	op := draftstate.Operation{ID: "operation", DraftID: draft.ID, WorkspaceID: draft.WorkspaceID, SessionID: "session"}
	if err := a.workspaceRegistry().AttachSession(t.Context(), "", draft.WorkspaceID, op.SessionID, ""); err != nil {
		t.Fatal(err)
	}
	if err := a.workspaceRegistry().ArchiveSession(t.Context(), op.SessionID); err != nil {
		t.Fatal(err)
	}
	if err := a.beginDraftWorkspaceCreate(op, draft.WorkspaceID); err == nil || !strings.Contains(err.Error(), "archived") {
		t.Fatalf("archived create error = %v", err)
	}
}
