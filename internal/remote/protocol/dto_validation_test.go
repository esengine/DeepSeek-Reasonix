package protocol

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/eventwire"
)

func TestSessionHistoryInitialPageOmitsEmptyCursor(t *testing.T) {
	params := SessionHistoryParams{
		RuntimeQuery: RuntimeQuery{
			ExpectedHostEpoch:    "host-1",
			Target:               RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-1"},
			ExpectedRuntimeEpoch: "runtime-1",
		},
		SnapshotID: "snapshot-1",
		PageTurns:  20,
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"cursor"`) {
		t.Fatalf("initial history request serialized an empty cursor: %s", raw)
	}
	if _, err := DecodeRequestParams(MethodSessionHistory, raw); err != nil {
		t.Fatalf("initial history request without cursor was rejected: %v", err)
	}
}

func TestSessionContentResultByteInvariants(t *testing.T) {
	body := []byte("hello")
	next := int64(len(body))
	valid := SessionContentResult{
		ContentRef: "content-1", Offset: 0, DataBase64: base64.StdEncoding.EncodeToString(body),
		NextOffset: &next, TotalBytes: 10, SHA256: strings.Repeat("a", 64), Encoding: ContentUTF8,
	}
	if err := validateDecoded(valid); err != nil {
		t.Fatalf("valid content chunk rejected: %v", err)
	}
	bad := valid
	bad.NextOffset = nil
	if err := validateDecoded(bad); err == nil {
		t.Fatal("non-final chunk without nextOffset accepted")
	}
	bad = valid
	bad.DataBase64 = "not-base64"
	if err := validateDecoded(bad); err == nil {
		t.Fatal("invalid Base64 accepted")
	}
	large := make([]byte, ContentRefChunkBytes+1)
	bad = valid
	bad.DataBase64 = base64.StdEncoding.EncodeToString(large)
	bad.TotalBytes = int64(len(large))
	end := int64(len(large))
	bad.NextOffset = &end
	if err := validateDecoded(bad); err == nil {
		t.Fatal("oversized content chunk accepted")
	}
}

func TestExternalizedFieldTruncationContract(t *testing.T) {
	valid := ExternalizedField{
		JSONPointer: "/history/messages/0/content", ContentRef: "content-1", TotalBytes: 100,
		SHA256: strings.Repeat("b", 64),
	}
	if err := validateDecoded(valid); err != nil {
		t.Fatalf("valid externalized field rejected: %v", err)
	}
	original := int64(200)
	valid.Truncated = true
	valid.OriginalBytes = &original
	valid.TruncationReason = "object_limit"
	if err := validateDecoded(valid); err != nil {
		t.Fatalf("valid truncated field rejected: %v", err)
	}
	valid.OriginalBytes = nil
	if err := validateDecoded(valid); err == nil {
		t.Fatal("truncated field without originalBytes accepted")
	}
}

func TestHistoryPageAndPageCursorInvariants(t *testing.T) {
	valid := HistoryPage{
		SnapshotID: "snapshot-1", StartTurn: 3, EndTurn: 5, TotalTurns: 5,
		ActualTurns: 2, HasOlder: true, NextCursor: "cursor-1", Externalized: []ExternalizedField{},
	}
	if err := validateDecoded(valid); err != nil {
		t.Fatalf("valid history page rejected: %v", err)
	}
	bad := valid
	bad.ActualTurns = 1
	if err := validateDecoded(bad); err == nil {
		t.Fatal("inconsistent actualTurns accepted")
	}
	bad = valid
	bad.NextCursor = ""
	if err := validateDecoded(bad); err == nil {
		t.Fatal("hasOlder without cursor accepted")
	}
	if err := validateDecoded(WorkspaceListResult{Items: []WorkspaceSummary{}, HasMore: false, NextCursor: "unexpected"}); err == nil {
		t.Fatal("nextCursor without hasMore accepted")
	}
}

func TestNotificationConditionalIdentities(t *testing.T) {
	target := RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-1"}
	event := SessionEvent{
		SubscriptionID: "subscription-1", HostEpoch: "host-1", Target: target,
		RuntimeEpoch: "runtime-1", Seq: 0, Event: eventwire.Event{Kind: "text"}, Externalized: []ExternalizedField{},
	}
	if err := validateDecoded(event); err == nil {
		t.Fatal("seq zero event accepted")
	}
	resync := SessionResyncRequired{
		SubscriptionID: "subscription-1", HostEpoch: "host-1", Target: target,
		RuntimeEpoch: "runtime-1", LastSeq: 3, Reason: ResyncRuntimeReplaced,
	}
	if err := validateDecoded(resync); err == nil {
		t.Fatal("runtime replacement without replacement epoch accepted")
	}
	resync.ReplacementRuntimeEpoch = "runtime-2"
	if err := validateDecoded(resync); err != nil {
		t.Fatalf("valid runtime replacement rejected: %v", err)
	}
	change := CatalogChanged{HostEpoch: "host-1", Revision: "rev-1", Scope: CatalogWorkspace, Kinds: []CatalogKind{CatalogSessions}}
	if err := validateDecoded(change); err == nil {
		t.Fatal("workspace catalog notification without affected workspace accepted")
	}
	change.AffectedWorkspaceIDs = []WorkspaceID{"workspace-1"}
	if err := validateDecoded(change); err != nil {
		t.Fatalf("valid workspace catalog notification rejected: %v", err)
	}
}

func TestFilePreviewBodyPresenceDiscriminator(t *testing.T) {
	empty := ""
	text := FilePreviewResult{Name: "empty.txt", Path: "empty.txt", Kind: FileText, Body: &empty}
	if err := validateDecoded(text); err != nil {
		t.Fatalf("empty text preview rejected: %v", err)
	}
	text.Body = nil
	if err := validateDecoded(text); err == nil {
		t.Fatal("text preview without body accepted")
	}
	binary := FilePreviewResult{Name: "blob.bin", Path: "blob.bin", Kind: FileBinary, Binary: true}
	if err := validateDecoded(binary); err != nil {
		t.Fatalf("binary metadata preview rejected: %v", err)
	}
	binary.Body = &empty
	if err := validateDecoded(binary); err == nil {
		t.Fatal("binary preview body accepted")
	}
}

func TestSessionSubmitStructuredVariantsAreMutuallyExclusive(t *testing.T) {
	base := SessionSubmitParams{
		SessionMutation: SessionMutation{
			RequestID: "request-1", ExpectedHostEpoch: "host-1",
			Target:               RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-1"},
			ExpectedRuntimeEpoch: "runtime-1",
		},
		Input: "prompt", DisplayText: "prompt",
	}
	if err := validateDecoded(base); err != nil {
		t.Fatalf("ordinary submit rejected: %v", err)
	}
	bad := base
	bad.DeliveryRecovery = true
	bad.EditedOriginal = "old prompt"
	if err := validateDecoded(bad); err == nil {
		t.Fatal("delivery recovery plus edited original accepted")
	}
	bad = base
	bad.EditedOriginal = "old prompt"
	bad.Invocations = []Invocation{{Name: "review", Kind: InvocationSkill}}
	if err := validateDecoded(bad); err == nil {
		t.Fatal("edited original plus invocations accepted")
	}
}

func TestSessionSubmitResultSnapshotContractAllowsInPlaceRewindRefresh(t *testing.T) {
	target := RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-1"}
	valid := []SessionSubmitResult{
		{Kind: SubmitTurn, TurnID: "turn-1", Target: target, RuntimeEpoch: "runtime-1"},
		{Kind: SubmitOperation, OperationID: "operation-1", Operation: OperationShell, Target: target, RuntimeEpoch: "runtime-1"},
		{Kind: SubmitCompleted, Effect: EffectNone, Target: target, RuntimeEpoch: "runtime-1"},
		{Kind: SubmitCompleted, Effect: EffectStateChanged, Target: target, RuntimeEpoch: "runtime-1"},
		{Kind: SubmitCompleted, Effect: EffectStateChanged, Target: target, RuntimeEpoch: "runtime-1", SnapshotRequired: true},
		{Kind: SubmitCompleted, Effect: EffectRuntimeReplaced, Target: target, RuntimeEpoch: "runtime-2", SnapshotRequired: true},
		{Kind: SubmitCompleted, Effect: EffectSessionReplaced, Target: RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-2"}, RuntimeEpoch: "runtime-2", SnapshotRequired: true},
	}
	for index, result := range valid {
		if err := validateDecoded(result); err != nil {
			t.Fatalf("valid result %d rejected: %+v: %v", index, result, err)
		}
	}

	invalid := []SessionSubmitResult{
		{Kind: SubmitTurn, TurnID: "turn-1", Target: target, RuntimeEpoch: "runtime-1", SnapshotRequired: true},
		{Kind: SubmitOperation, OperationID: "operation-1", Operation: OperationShell, Target: target, RuntimeEpoch: "runtime-1", SnapshotRequired: true},
		{Kind: SubmitCompleted, Effect: EffectNone, Target: target, RuntimeEpoch: "runtime-1", SnapshotRequired: true},
		{Kind: SubmitCompleted, Effect: EffectRuntimeReplaced, Target: target, RuntimeEpoch: "runtime-2"},
		{Kind: SubmitCompleted, Effect: EffectSessionReplaced, Target: RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-2"}, RuntimeEpoch: "runtime-2"},
	}
	for index, result := range invalid {
		if err := validateDecoded(result); err == nil {
			t.Fatalf("invalid result %d accepted: %+v", index, result)
		}
	}
}

func TestFrozenCapabilitiesAndLeaseTiming(t *testing.T) {
	if err := validateDecoded(FrozenCapabilities(false, true)); err != nil {
		t.Fatalf("frozen capabilities rejected: %v", err)
	}
	bad := FrozenCapabilities(false, true)
	bad.Features.PTY = true
	if err := validateDecoded(bad); err == nil {
		t.Fatal("PTY capability accepted in V1")
	}
	if err := validateDecoded(LeaseInfo{LeaseID: "lease-1", TTLMillis: 1, PingIntervalMs: 1}); err == nil {
		t.Fatal("non-frozen lease timing accepted")
	}
	if err := validateDecoded(PingResult{HostEpoch: "host-1", LeaseTTL: 1}); err == nil {
		t.Fatal("non-frozen ping TTL accepted")
	}
}

func TestWorkspaceChangeDetailResultInvariants(t *testing.T) {
	source := ChangeGit
	patch := "@@ -1 +1 @@\n-old\n+new"
	valid := WorkspaceChangeDetailResult{Diff: &patch, Source: &source, Added: 1, Removed: 1}
	if err := validateDecoded(valid); err != nil {
		t.Fatalf("valid workspace change detail rejected: %v", err)
	}
	if err := validateDecoded(WorkspaceChangeDetailResult{Diff: &patch}); err == nil {
		t.Fatal("workspace change detail without source was accepted")
	}
	if err := validateDecoded(WorkspaceChangeDetailResult{Diff: &patch, Source: &source, Truncated: true}); err == nil {
		t.Fatal("truncated workspace change detail with a patch was accepted")
	}
	if err := validateDecoded(WorkspaceChangeDetailResult{Source: &source, Truncated: true}); err != nil {
		t.Fatalf("valid truncated workspace change detail rejected: %v", err)
	}
}

func TestCapabilitiesValidateEveryFixedFeatureAndLimit(t *testing.T) {
	for _, memory := range []bool{false, true} {
		for _, research := range []bool{false, true} {
			if err := FrozenCapabilities(memory, research).Validate(); err != nil {
				t.Fatalf("dynamic memory=%v research=%v rejected: %v", memory, research, err)
			}
		}
	}

	requiredTrue := []func(*Features){
		func(f *Features) { f.CoreSession = false },
		func(f *Features) { f.PrimaryFileQueries = false },
		func(f *Features) { f.UserShell = false },
		func(f *Features) { f.JobCancel = false },
	}
	for i, mutate := range requiredTrue {
		capabilities := FrozenCapabilities(false, false)
		mutate(&capabilities.Features)
		if err := capabilities.Validate(); err == nil {
			t.Fatalf("required-true feature mutation %d accepted", i)
		}
	}
	requiredFalse := []func(*Features){
		func(f *Features) { f.MediaPreview = true },
		func(f *Features) { f.Attachments = true },
		func(f *Features) { f.ClipboardImages = true },
		func(f *Features) { f.SFTP = true },
		func(f *Features) { f.LocalPathOperations = true },
		func(f *Features) { f.GitWrite = true },
		func(f *Features) { f.PTY = true },
		func(f *Features) { f.DeliveryWorktree = true },
	}
	for i, mutate := range requiredFalse {
		capabilities := FrozenCapabilities(false, false)
		mutate(&capabilities.Features)
		if err := capabilities.Validate(); err == nil {
			t.Fatalf("required-false feature mutation %d accepted", i)
		}
	}

	limitType := reflect.TypeOf(ProtocolLimits{})
	for i := 0; i < limitType.NumField(); i++ {
		capabilities := FrozenCapabilities(false, false)
		limits := reflect.ValueOf(&capabilities.Limits).Elem()
		field := limits.Field(i)
		field.SetInt(field.Int() + 1)
		if err := capabilities.Validate(); err == nil {
			t.Fatalf("mutated frozen limit %s accepted", limitType.Field(i).Name)
		}
	}
}
