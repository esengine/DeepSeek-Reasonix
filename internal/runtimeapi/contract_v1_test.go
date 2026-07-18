package runtimeapi

import (
	"context"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// v1ConformanceFake is intentionally boring: its only purpose is to make
// signature drift in the focused V1 interfaces a compile failure. The Phase 5
// core is embedded so adding V1 does not force existing RuntimeAPI fakes to
// implement unrelated domains.
type v1ConformanceFake struct{ RuntimeAPI }

func (*v1ConformanceFake) HostCapabilities(context.Context) (Capabilities, error) {
	return Capabilities{}, nil
}
func (*v1ConformanceFake) HostConfigSummary(context.Context) (HostConfigSummary, error) {
	return HostConfigSummary{}, nil
}
func (*v1ConformanceFake) ListWorkspaces(context.Context, ListWorkspacesInput) (WorkspaceListPage, error) {
	return WorkspaceListPage{}, nil
}
func (*v1ConformanceFake) CloseWorkspace(context.Context, CloseWorkspaceInput) (CloseWorkspaceResult, error) {
	return CloseWorkspaceResult{}, nil
}
func (*v1ConformanceFake) WorkspaceCatalog(context.Context, WorkspaceCatalogInput) (WorkspaceCatalog, error) {
	return WorkspaceCatalog{}, nil
}
func (*v1ConformanceFake) SessionCatalog(context.Context, SessionCatalogInput) (SessionCatalog, error) {
	return SessionCatalog{}, nil
}
func (*v1ConformanceFake) CatalogEvents() <-chan CatalogInvalidation { return nil }
func (*v1ConformanceFake) ListTopics(context.Context, ListTopicsInput) (TopicPage, error) {
	return TopicPage{}, nil
}
func (*v1ConformanceFake) CreateTopic(context.Context, CreateTopicInput) (CreatedTopic, error) {
	return CreatedTopic{}, nil
}
func (*v1ConformanceFake) RenameTopic(context.Context, RenameTopicInput) (RenameTopicResult, error) {
	return RenameTopicResult{}, nil
}
func (*v1ConformanceFake) DeleteTopic(context.Context, DeleteTopicInput) (DeleteTopicResult, error) {
	return DeleteTopicResult{}, nil
}
func (*v1ConformanceFake) TrashTopic(context.Context, TrashTopicInput) (TrashTopicResult, error) {
	return TrashTopicResult{}, nil
}
func (*v1ConformanceFake) ListSessions(context.Context, ListSessionsInput) (SessionListPage, error) {
	return SessionListPage{}, nil
}
func (*v1ConformanceFake) CloseSession(context.Context, CloseSessionInput) (CloseSessionResult, error) {
	return CloseSessionResult{}, nil
}
func (*v1ConformanceFake) RenameSession(context.Context, RenameSessionInput) (RenameSessionResult, error) {
	return RenameSessionResult{}, nil
}
func (*v1ConformanceFake) ListTrashedSessions(context.Context, ListTrashedSessionsInput) (TrashPage, error) {
	return TrashPage{}, nil
}
func (*v1ConformanceFake) TrashSession(context.Context, TrashSessionInput) (TrashSessionResult, error) {
	return TrashSessionResult{}, nil
}
func (*v1ConformanceFake) RestoreSession(context.Context, RestoreSessionInput) (RestoreSessionResult, error) {
	return RestoreSessionResult{}, nil
}
func (*v1ConformanceFake) PurgeSession(context.Context, PurgeSessionInput) (PurgeSessionResult, error) {
	return PurgeSessionResult{}, nil
}
func (*v1ConformanceFake) UnsubscribeSession(context.Context, UnsubscribeSessionInput) error {
	return nil
}
func (*v1ConformanceFake) SessionHistory(context.Context, HistoryInput) (HistoryPage, error) {
	return HistoryPage{}, nil
}
func (*v1ConformanceFake) SessionContent(context.Context, ContentInput) (ContentChunk, error) {
	return ContentChunk{}, nil
}
func (*v1ConformanceFake) ComposerSlashArgs(context.Context, SlashArgsInput) (SlashArgsResult, error) {
	return SlashArgsResult{}, nil
}
func (*v1ConformanceFake) ComposerHistory(context.Context, PromptHistoryInput) (PromptHistoryPage, error) {
	return PromptHistoryPage{}, nil
}
func (*v1ConformanceFake) NewSession(context.Context, SessionActionInput) (NewSessionResult, error) {
	return NewSessionResult{}, nil
}
func (*v1ConformanceFake) ClearSession(context.Context, SessionActionInput) (ClearSessionResult, error) {
	return ClearSessionResult{}, nil
}
func (*v1ConformanceFake) ForkSession(context.Context, ForkSessionInput) (ForkSessionResult, error) {
	return ForkSessionResult{}, nil
}
func (*v1ConformanceFake) RewindSession(context.Context, RewindSessionInput) (RewindSessionResult, error) {
	return RewindSessionResult{}, nil
}
func (*v1ConformanceFake) CompactSession(context.Context, CompactSessionInput) (OperationStartedResult, error) {
	return OperationStartedResult{}, nil
}
func (*v1ConformanceFake) SummarizeSession(context.Context, SummarizeSessionInput) (OperationStartedResult, error) {
	return OperationStartedResult{}, nil
}
func (*v1ConformanceFake) CancelOperation(context.Context, CancelOperationInput) (CancelOperationResult, error) {
	return CancelOperationResult{}, nil
}
func (*v1ConformanceFake) SetProfile(context.Context, SetProfileInput) (SetProfileResult, error) {
	return SetProfileResult{}, nil
}
func (*v1ConformanceFake) SetGoal(context.Context, SetGoalInput) (SetGoalResult, error) {
	return SetGoalResult{}, nil
}
func (*v1ConformanceFake) ResumeGoal(context.Context, ResumeGoalInput) (ResumeGoalResult, error) {
	return ResumeGoalResult{}, nil
}
func (*v1ConformanceFake) ClearGoal(context.Context, ClearGoalInput) (ClearGoalResult, error) {
	return ClearGoalResult{}, nil
}
func (*v1ConformanceFake) RunShell(context.Context, RunShellInput) (OperationStartedResult, error) {
	return OperationStartedResult{}, nil
}
func (*v1ConformanceFake) SessionContext(context.Context, SessionContextInput) (ContextView, error) {
	return ContextView{}, nil
}
func (*v1ConformanceFake) SessionBalance(context.Context, SessionBalanceInput) (BalanceView, error) {
	return BalanceView{}, nil
}
func (*v1ConformanceFake) ListJobs(context.Context, ListJobsInput) (JobPage, error) {
	return JobPage{}, nil
}
func (*v1ConformanceFake) CancelJob(context.Context, CancelJobInput) (CancelJobResult, error) {
	return CancelJobResult{}, nil
}
func (*v1ConformanceFake) Memory(context.Context, MemoryInput) (MemoryView, error) {
	return MemoryView{}, nil
}
func (*v1ConformanceFake) MemorySuggestions(context.Context, MemoryInput) (MemorySuggestionsView, error) {
	return MemorySuggestionsView{}, nil
}
func (*v1ConformanceFake) RememberMemory(context.Context, RememberMemoryInput) (RememberMemoryResult, error) {
	return RememberMemoryResult{}, nil
}
func (*v1ConformanceFake) ForgetMemory(context.Context, ForgetMemoryInput) (ForgetMemoryResult, error) {
	return ForgetMemoryResult{}, nil
}
func (*v1ConformanceFake) SaveMemoryDocument(context.Context, SaveMemoryDocumentInput) (SaveMemoryDocumentResult, error) {
	return SaveMemoryDocumentResult{}, nil
}
func (*v1ConformanceFake) AcceptMemorySuggestion(context.Context, AcceptMemorySuggestionInput) (AcceptMemorySuggestionResult, error) {
	return AcceptMemorySuggestionResult{}, nil
}
func (*v1ConformanceFake) AcceptSkillSuggestion(context.Context, AcceptSkillSuggestionInput) (AcceptSkillSuggestionResult, error) {
	return AcceptSkillSuggestionResult{}, nil
}
func (*v1ConformanceFake) ResearchStatus(context.Context, ResearchInput) (ResearchStatusView, error) {
	return ResearchStatusView{}, nil
}
func (*v1ConformanceFake) ListResearch(context.Context, ListResearchInput) (ResearchPage, error) {
	return ResearchPage{}, nil
}
func (*v1ConformanceFake) ResearchFindings(context.Context, ResearchFindingsInput) (ResearchFindingsPage, error) {
	return ResearchFindingsPage{}, nil
}
func (*v1ConformanceFake) RecordResearchEvidence(context.Context, RecordResearchEvidenceInput) (RecordResearchEvidenceResult, error) {
	return RecordResearchEvidenceResult{}, nil
}
func (*v1ConformanceFake) ListFiles(context.Context, FileListInput) (FileListResult, error) {
	return FileListResult{}, nil
}
func (*v1ConformanceFake) SearchFiles(context.Context, FileSearchInput) (FileSearchResult, error) {
	return FileSearchResult{}, nil
}
func (*v1ConformanceFake) PreviewFile(context.Context, FilePreviewInput) (FilePreview, error) {
	return FilePreview{}, nil
}
func (*v1ConformanceFake) WorkspaceChanges(context.Context, WorkspaceChangesInput) (WorkspaceChangesPage, error) {
	return WorkspaceChangesPage{}, nil
}
func (*v1ConformanceFake) GitHistory(context.Context, GitHistoryInput) (GitHistoryResult, error) {
	return GitHistoryResult{}, nil
}
func (*v1ConformanceFake) GitCommitDetail(context.Context, GitCommitDetailInput) (GitCommitDetail, error) {
	return GitCommitDetail{}, nil
}

var _ V1RuntimeAPI = (*v1ConformanceFake)(nil)

func TestV1RuntimeAPIHasFrozenDomainSurface(t *testing.T) {
	want := []string{
		"AcceptMemorySuggestion", "AcceptSkillSuggestion", "AnswerPrompt", "ApprovePrompt",
		"AttachAndSubscribe", "BrowseWorkspace", "CancelJob", "CancelOperation", "CancelTurn", "CatalogEvents",
		"ClearGoal", "ClearSession", "CloseSession", "CloseWorkspace", "CompactSession", "ComposerHistory",
		"ComposerSlashArgs", "ComposerSubmit", "Connection", "CreateSession", "CreateTopic",
		"DeleteTopic", "Events", "ForgetMemory", "ForkSession", "GitCommitDetail", "GitHistory",
		"HostCapabilities", "HostConfigSummary", "ListFiles", "ListJobs", "ListResearch",
		"ListSessions", "ListTopics", "ListTrashedSessions", "ListWorkspaces", "Memory",
		"MemorySuggestions", "NewSession", "OpenWorkspace", "PreviewFile", "PurgeSession",
		"RecordResearchEvidence", "RememberMemory", "RenameSession", "RenameTopic", "ResearchFindings",
		"ResearchStatus", "RestoreSession", "ResumeGoal", "RewindSession", "RunShell",
		"SaveMemoryDocument", "SearchFiles", "SessionBalance", "SessionCatalog", "SessionContent",
		"SessionContext", "SessionHistory", "SetGoal", "SetProfile", "SteerTurn", "SummarizeSession",
		"TrashSession", "TrashTopic", "UnsubscribeSession", "WorkspaceCatalog", "WorkspaceChanges",
	}
	typ := reflect.TypeOf((*V1RuntimeAPI)(nil)).Elem()
	got := make([]string, typ.NumMethod())
	for i := range typ.NumMethod() {
		got[i] = typ.Method(i).Name
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("V1RuntimeAPI methods drifted\n got: %v\nwant: %v", got, want)
	}
}

func TestRuntimeAPIIdentityTypesRemainOpaqueAndDistinct(t *testing.T) {
	ids := []reflect.Type{
		reflect.TypeOf(WorkspaceID("")), reflect.TypeOf(SessionID("")), reflect.TypeOf(TopicID("")),
		reflect.TypeOf(DirectoryRef("")), reflect.TypeOf(Cursor("")), reflect.TypeOf(TurnID("")),
		reflect.TypeOf(OperationID("")), reflect.TypeOf(PromptID("")), reflect.TypeOf(QuestionID("")),
		reflect.TypeOf(CheckpointID("")), reflect.TypeOf(JobID("")), reflect.TypeOf(CatalogRevision("")),
		reflect.TypeOf(ModelRef("")), reflect.TypeOf(MemoryID("")), reflect.TypeOf(DocumentID("")),
		reflect.TypeOf(SuggestionID("")), reflect.TypeOf(SkillID("")), reflect.TypeOf(ResearchTaskID("")),
		reflect.TypeOf(CriterionID("")), reflect.TypeOf(ContentRef("")),
	}
	seen := make(map[reflect.Type]bool, len(ids))
	for _, id := range ids {
		if id.Kind() != reflect.String || id.Name() == "" {
			t.Fatalf("identity %v is not a defined opaque string", id)
		}
		if seen[id] {
			t.Fatalf("duplicate identity type %v", id)
		}
		seen[id] = true
	}
	assertFieldType(t, reflect.TypeOf(SessionRef{}), "WorkspaceID", reflect.TypeOf(WorkspaceID("")))
	assertFieldType(t, reflect.TypeOf(SessionRef{}), "SessionID", reflect.TypeOf(SessionID("")))
	assertFieldType(t, reflect.TypeOf(ForkSessionInput{}), "CheckpointID", reflect.TypeOf(CheckpointID("")))
	assertFieldType(t, reflect.TypeOf(CancelOperationInput{}), "OperationID", reflect.TypeOf(OperationID("")))
	assertFieldType(t, reflect.TypeOf(ContentInput{}), "ContentRef", reflect.TypeOf(ContentRef("")))
}

func TestV1RuntimeAPIDTOsExcludeTransportAndDesktopFields(t *testing.T) {
	forbidden := map[string]bool{
		"HostEpoch": true, "RuntimeEpoch": true, "RequestID": true, "RequestId": true,
		"SubscriptionID": true, "SubscriptionId": true, "SnapshotID": true, "SnapshotId": true,
		"Seq": true, "Sequence": true, "TabID": true, "TabId": true, "SessionPath": true,
		"Transport": true, "JSONRPC": true, "JSONRpc": true, "Wails": true,
	}
	typ := reflect.TypeOf((*V1RuntimeAPI)(nil)).Elem()
	seen := make(map[reflect.Type]bool)
	for i := range typ.NumMethod() {
		method := typ.Method(i)
		for n := 0; n < method.Type.NumIn(); n++ {
			inspectNeutralType(t, method.Type.In(n), forbidden, seen)
		}
		for n := 0; n < method.Type.NumOut(); n++ {
			inspectNeutralType(t, method.Type.Out(n), forbidden, seen)
		}
	}
}

func TestRuntimeAPIPackageDoesNotImportACPOrRemoteProtocol(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(filename)
	packages, err := parser.ParseDir(token.NewFileSet(), dir, func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range packages {
		for _, file := range pkg.Files {
			for _, spec := range file.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(path, "/remote/protocol") || strings.Contains(strings.ToLower(path), "acp") {
					t.Errorf("target-neutral runtimeapi imports forbidden package %q", path)
				}
			}
		}
	}
}

func TestFrozenRuntimeServiceLimits(t *testing.T) {
	if HistoryMinTurns != 1 || HistoryMaxTurns != 200 || DefaultAttachHistoryTurns != 60 {
		t.Fatalf("history limits = %d..%d default %d", HistoryMinTurns, HistoryMaxTurns, DefaultAttachHistoryTurns)
	}
	if PageDefaultItems != 200 || PageMaxItems != 1000 || SearchDefaultItems != 20 || SearchMaxItems != 100 || SearchMaxVisitedItems != 10000 {
		t.Fatal("shared file pagination/search limits drifted")
	}
	if PreviewBytes != 256<<10 || GitHistoryCommits != 100 || GitPatchBytes != 1<<20 {
		t.Fatal("shared file/Git byte or history limits drifted")
	}
	for _, turns := range []int{HistoryMinTurns, HistoryMaxTurns} {
		if err := ValidateHistoryTurns(turns); err != nil {
			t.Fatalf("ValidateHistoryTurns(%d): %v", turns, err)
		}
	}
	for _, turns := range []int{0, HistoryMaxTurns + 1} {
		if err := ValidateHistoryTurns(turns); err == nil {
			t.Fatalf("ValidateHistoryTurns(%d) succeeded", turns)
		}
	}
	for _, limit := range []int{0, 1, PageMaxItems} {
		if err := ValidatePageLimit(limit); err != nil {
			t.Fatalf("ValidatePageLimit(%d): %v", limit, err)
		}
	}
	for _, limit := range []int{-1, PageMaxItems + 1} {
		if err := ValidatePageLimit(limit); err == nil {
			t.Fatalf("ValidatePageLimit(%d) succeeded", limit)
		}
	}
}

func inspectNeutralType(t *testing.T, typ reflect.Type, forbidden map[string]bool, seen map[reflect.Type]bool) {
	t.Helper()
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array || typ.Kind() == reflect.Chan {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || typ.PkgPath() != reflect.TypeOf(SessionRef{}).PkgPath() || seen[typ] {
		return
	}
	seen[typ] = true
	for i := range typ.NumField() {
		field := typ.Field(i)
		if forbidden[field.Name] {
			t.Errorf("target-neutral DTO %s exposes forbidden field %s", typ.Name(), field.Name)
		}
		inspectNeutralType(t, field.Type, forbidden, seen)
	}
}

func assertFieldType(t *testing.T, typ reflect.Type, name string, want reflect.Type) {
	t.Helper()
	field, ok := typ.FieldByName(name)
	if !ok {
		t.Fatalf("%s is missing field %s", typ.Name(), name)
	}
	if field.Type != want {
		t.Fatalf("%s.%s type = %v, want %v", typ.Name(), name, field.Type, want)
	}
}
