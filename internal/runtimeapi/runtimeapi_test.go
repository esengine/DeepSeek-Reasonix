package runtimeapi

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"reasonix/internal/eventwire"
)

func TestHostConfigSummaryRequiresExplicitAvailability(t *testing.T) {
	if err := (HostConfigSummary{Available: true}).RequireAvailable(); err != nil {
		t.Fatalf("available summary rejected: %v", err)
	}

	err := (HostConfigSummary{UnavailableReason: "Host did not publish a safe summary"}).RequireAvailable()
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unavailable summary error = %v, want ErrUnavailable", err)
	}
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) || unavailable.Capability != CapabilityHostConfig {
		t.Fatalf("unavailable summary error = %#v, want HostConfig capability", err)
	}
	if unavailable.Detail != "Host did not publish a safe summary" {
		t.Fatalf("unavailable detail = %q", unavailable.Detail)
	}
}

func TestCapabilitiesSupportsOnlyAdvertisedMethods(t *testing.T) {
	capabilities := Capabilities{WorkspaceBrowse: true, ComposerSubmit: true}
	for _, capability := range []Capability{CapabilityWorkspaceBrowse, CapabilityComposerSubmit} {
		if !capabilities.Supports(capability) {
			t.Fatalf("Supports(%q) = false", capability)
		}
	}
	for _, capability := range []Capability{
		CapabilityHostConfig,
		CapabilitySessionCreate,
		CapabilitySessionAttach,
		CapabilityTurnSteer,
		CapabilityTurnCancel,
		CapabilityPromptApprove,
		CapabilityPromptAnswer,
		Capability("wire/method"),
	} {
		if capabilities.Supports(capability) {
			t.Fatalf("Supports(%q) = true", capability)
		}
	}
}

func TestSessionSnapshotRetainsRecoveryAndWorkbenchState(t *testing.T) {
	detail := "detail"
	content := "assistant output"
	reasoning := "reasoning"
	lastError := "transport failed"
	goal := "finish Remote V1"
	todo := "wire Desktop"
	prompt := "question"
	toolArgs := `{\"path\":\"README.md\"}`
	toolSummary := "read README"
	archive := "history archive"
	toolResultError := "permission denied"
	offset := int64(10)
	limit := int64(20)

	want := SessionSnapshot{
		Session:    SessionRef{WorkspaceID: "workspace-opaque", SessionID: "session-opaque"},
		TopicID:    "topic-opaque",
		Title:      "Remote session",
		Profile:    ResolvedProfile{Model: "model", Effort: "high", CollaborationMode: "agent", TokenMode: "max", ToolApprovalMode: "ask"},
		Goal:       &goal,
		GoalStatus: GoalRunning,
		Capabilities: Capabilities{
			HostConfig: true, WorkspaceBrowse: true, SessionCreate: true, SessionAttach: true,
			ComposerSubmit: true, TurnSteer: true, TurnCancel: true, PromptApprove: true, PromptAnswer: true,
			Features: Features{CoreSession: true, PrimaryFileQueries: true, UserShell: true, Memory: true, Research: true},
			Limits:   Limits{FrameBytes: 1024, HistoryMaxTurns: 50, PreviewBytes: 4096},
		},
		Runtime: RuntimeState{
			Running:          true,
			CurrentTurn:      &TurnState{ID: "turn-opaque", CancelRequested: true},
			CurrentOperation: &OperationState{ID: "operation-opaque", Kind: "compact", CancelRequested: false},
			CancelRequested:  true,
			LastOutcome:      OutcomeInterrupted,
			LastError:        &lastError,
			Interruption:     &RuntimeInterruption{PreviousTurnInterrupted: true, Reason: "daemon restarted"},
			LiveEvents:       []eventwire.Event{{Kind: "notice", Text: "recover me", Detail: detail}},
		},
		History: HistoryPage{
			Messages: []HistoryMessage{{
				Role: "assistant", Content: &content, Detail: &detail, Code: "code", SubmitText: &content,
				CheckpointID: "checkpoint-opaque", CreatedAtMillis: 123, Reasoning: &reasoning,
				WorkDurationMillis: 456,
				MemoryCitations:    []eventwire.MemoryCitation{{ID: "memory-id", Source: "MEMORY.md", LineStart: 1, LineEnd: 2, Note: "note", Kind: "memory"}},
				Level:              "warn",
				ToolCalls: []HistoryToolCall{{
					ID: "tool-call", Name: "read", Arguments: &toolArgs, Subject: "README.md", Summary: &toolSummary,
					Diff: &detail, Added: 3, Removed: 2, ArgumentsArchived: true,
				}},
				ToolCallID: "tool-call", ToolName: "read", ToolResultArchived: true, ToolResultError: &toolResultError,
				Pending: true, Trigger: "auto", Messages: 4, Summary: &toolSummary, Archive: &archive,
			}},
			StartTurn: 2, EndTurn: 4, TotalTurns: 9, HasOlder: true, Next: "cursor-opaque",
		},
		PendingPrompt: &PendingPrompt{Kind: PromptAsk, Ask: &AskPrompt{
			ID: "prompt-opaque",
			Questions: []AskQuestion{{
				ID: "question-opaque", Header: "Choose", Prompt: &prompt,
				Options: []AskOption{{Label: "Continue", Description: &detail}}, Multi: true,
			}},
		}},
		Todos: []TodoItem{{Content: &todo, Status: TodoInProgress, ActiveForm: "wiring Desktop", Level: 1}},
		Context: ContextView{
			UsedTokens: 1, WindowTokens: 2, PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7,
			ReasoningTokens: 5, CacheHitTokens: 6, CacheMissTokens: 7, SessionCacheHitTokens: 8,
			SessionCacheMissTokens: 9, SessionCompletionTokens: 10, RequestCount: 11, ElapsedMillis: 12,
			SessionCost: 1.25, SessionCurrency: "$",
			Sources:   []UsageSource{{Source: "provider", PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7, ReasoningTokens: 5, CacheHitTokens: 6, CacheMissTokens: 7, RequestCount: 1, SessionCost: 1.25, SessionCurrency: "$"}},
			ReadFiles: []ReadFileRecord{{Path: "README.md", Turn: 3, TimeMs: 99, Offset: &offset, Limit: &limit, Truncated: true}},
		},
		Jobs: []Job{{ID: "job-opaque", Kind: JobBash, Label: "go test", Status: JobRunning, StartedAtMillis: 999}},
		Checkpoints: []Checkpoint{{
			ID: "checkpoint-opaque", DisplayTurn: 4, Prompt: &prompt, Files: []string{"a.go", "b.go"},
			FileCount: 2, FilesTruncated: true, CreatedAtMillis: 1000, CanCode: true, CanConversation: true,
		}},
	}

	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got SessionSnapshot
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot JSON round trip lost state\n got: %#v\nwant: %#v", got, want)
	}
}

func TestTargetNeutralDTOsDoNotExposeTransportOrDesktopIdentity(t *testing.T) {
	forbidden := map[string]struct{}{
		"TabID": {}, "TabId": {}, "SessionPath": {},
		"Wails": {}, "JSONRPC": {}, "JSONRpc": {},
		"HostEpoch": {}, "RuntimeEpoch": {}, "Epoch": {}, "Seq": {}, "Sequence": {},
		"RequestID": {}, "RequestId": {}, "SubscriptionID": {}, "SubscriptionId": {},
		"Transport": {},
	}

	roots := []reflect.Type{
		reflect.TypeOf(ConnectionView{}),
		reflect.TypeOf(BrowseWorkspaceInput{}),
		reflect.TypeOf(WorkspacePage{}),
		reflect.TypeOf(OpenWorkspaceInput{}),
		reflect.TypeOf(OpenWorkspaceResult{}),
		reflect.TypeOf(CreateSessionInput{}),
		reflect.TypeOf(CreatedSession{}),
		reflect.TypeOf(AttachAndSubscribeInput{}),
		reflect.TypeOf(SessionSnapshot{}),
		reflect.TypeOf(ComposerSubmitInput{}),
		reflect.TypeOf(ComposerSubmitResult{}),
		reflect.TypeOf(SteerInput{}),
		reflect.TypeOf(CancelTurnInput{}),
		reflect.TypeOf(ApproveInput{}),
		reflect.TypeOf(AnswerInput{}),
		reflect.TypeOf(Event{}),
	}
	seen := make(map[reflect.Type]bool)
	var inspect func(reflect.Type)
	inspect = func(typ reflect.Type) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || typ.PkgPath() != reflect.TypeOf(SessionRef{}).PkgPath() || seen[typ] {
			return
		}
		seen[typ] = true
		for index := 0; index < typ.NumField(); index++ {
			field := typ.Field(index)
			if _, blocked := forbidden[field.Name]; blocked {
				t.Errorf("target-neutral DTO %s exposes forbidden field %s", typ.Name(), field.Name)
			}
			inspect(field.Type)
		}
	}
	for _, root := range roots {
		inspect(root)
	}
}

func TestSessionSnapshotContainsRequiredAtomicAttachState(t *testing.T) {
	required := []string{
		"Session", "TopicID", "Title", "Profile", "Goal", "GoalStatus", "Capabilities",
		"Runtime", "History", "PendingPrompt", "Todos", "Context", "Jobs", "Checkpoints",
	}
	assertStructFields(t, reflect.TypeOf(SessionSnapshot{}), required)
	assertStructFields(t, reflect.TypeOf(HistoryMessage{}), []string{
		"Role", "Content", "Detail", "Code", "SubmitText", "CheckpointID", "CreatedAtMillis",
		"Reasoning", "WorkDurationMillis", "MemoryCitations", "Level", "ToolCalls", "ToolCallID",
		"ToolName", "ToolResultArchived", "ToolResultError", "Pending", "Trigger", "Messages",
		"Summary", "Archive",
	})
	assertStructFields(t, reflect.TypeOf(HistoryToolCall{}), []string{
		"ID", "Name", "Arguments", "Subject", "Summary", "Diff", "Added", "Removed", "ArgumentsArchived",
	})
}

func assertStructFields(t *testing.T, typ reflect.Type, required []string) {
	t.Helper()
	for _, name := range required {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("%s is missing required field %s", typ.Name(), name)
		}
	}
}
