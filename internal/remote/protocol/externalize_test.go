package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/eventwire"
)

func externalizedDescriptor(pointer string) ExternalizedField {
	return ExternalizedField{
		JSONPointer: pointer, ContentRef: "content-1", TotalBytes: 128,
		SHA256: strings.Repeat("a", 64),
	}
}

func externalizableTestEvent(event eventwire.Event, fields ...ExternalizedField) SessionEvent {
	return SessionEvent{
		SubscriptionID: "subscription-1", HostEpoch: "host-1",
		Target:       RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-1"},
		RuntimeEpoch: "runtime-1", Seq: 1, Event: event, Externalized: fields,
	}
}

func TestSessionEventMarshalEmitsExplicitNullAndRehydratesBeforeTypedDecode(t *testing.T) {
	wire := externalizableTestEvent(
		eventwire.Event{Kind: "text", Text: strings.Repeat("x", 100)},
		externalizedDescriptor("/event/text"),
	)
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	root := decodeObjectForTest(t, raw)
	event := root["event"].(map[string]any)
	value, present := event["text"]
	if !present || value != nil {
		t.Fatalf("externalized event text = %#v, present=%v; want explicit null", value, present)
	}
	if _, err := DecodeRehydratedJSON[SessionEvent](raw, nil); err == nil {
		t.Fatal("typed decode accepted an unreplaced externalized null")
	}
	decoded, err := DecodeRehydratedJSON[SessionEvent](raw, []RehydratedExternalizedField{{JSONPointer: "/event/text", Value: "restored"}})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Event.Text != "restored" {
		t.Fatalf("rehydrated text = %q", decoded.Event.Text)
	}
	if _, err := RehydrateExternalizedJSON[SessionEvent](raw, []RehydratedExternalizedField{{JSONPointer: "/event/code", Value: "bad"}}); err == nil {
		t.Fatal("non-externalizable event field accepted")
	}
}

func TestExternalizedPointersRejectMissingNonNullAndDuplicates(t *testing.T) {
	missingFinal := externalizableTestEvent(
		eventwire.Event{Kind: "text"},
		externalizedDescriptor("/event/text"),
	)
	if _, err := json.Marshal(missingFinal); err == nil {
		t.Fatal("descriptor whose final field is omitted was accepted")
	}

	missing := externalizableTestEvent(
		eventwire.Event{Kind: "tool_result"},
		externalizedDescriptor("/event/tool/output"),
	)
	if _, err := json.Marshal(missing); err == nil {
		t.Fatal("descriptor whose parent payload is missing was accepted")
	}

	inline := externalizableTestEvent(eventwire.Event{Kind: "text", Text: "inline"})
	raw, err := json.Marshal(inline)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RehydrateExternalizedJSON[SessionEvent](raw, []RehydratedExternalizedField{{JSONPointer: "/event/text", Value: "replacement"}}); err == nil {
		t.Fatal("rehydration replaced a non-null inline string")
	}
	if _, err := RehydrateExternalizedJSON[SessionEvent](raw, []RehydratedExternalizedField{{JSONPointer: "/event/tool/output", Value: "replacement"}}); err == nil {
		t.Fatal("rehydration accepted a missing pointer parent")
	}

	duplicateDescriptor := externalizedDescriptor("/event/text")
	duplicate := externalizableTestEvent(eventwire.Event{Kind: "text", Text: "large"}, duplicateDescriptor, duplicateDescriptor)
	if _, err := json.Marshal(duplicate); err == nil {
		t.Fatal("duplicate descriptor pointers were accepted")
	}

	externalized := externalizableTestEvent(eventwire.Event{Kind: "text", Text: "large"}, duplicateDescriptor)
	raw, err = json.Marshal(externalized)
	if err != nil {
		t.Fatal(err)
	}
	fields := []RehydratedExternalizedField{{JSONPointer: "/event/text", Value: "one"}, {JSONPointer: "/event/text", Value: "two"}}
	if _, err := RehydrateExternalizedJSON[SessionEvent](raw, fields); err == nil {
		t.Fatal("duplicate rehydration pointers were accepted")
	}
}

func TestEventExternalizationPreservesOmittedAndRequiredFieldSemantics(t *testing.T) {
	omitted := externalizableTestEvent(eventwire.Event{Kind: "text"})
	raw, err := json.Marshal(omitted)
	if err != nil {
		t.Fatal(err)
	}
	event := decodeObjectForTest(t, raw)["event"].(map[string]any)
	if _, exists := event["text"]; exists {
		t.Fatal("ordinary empty optional text stopped being omitted")
	}

	ask := externalizableTestEvent(eventwire.Event{
		Kind: "ask_request",
		Ask: &eventwire.Ask{ID: "ask-1", Questions: []eventwire.AskQuestion{{
			ID: "question-1", Prompt: strings.Repeat("p", 100),
			Options: []eventwire.AskOption{{Label: "yes"}},
		}}},
	}, externalizedDescriptor("/event/ask/questions/0/prompt"))
	raw, err = json.Marshal(ask)
	if err != nil {
		t.Fatal(err)
	}
	event = decodeObjectForTest(t, raw)["event"].(map[string]any)
	question := event["ask"].(map[string]any)["questions"].([]any)[0].(map[string]any)
	if prompt, exists := question["prompt"]; !exists || prompt != nil {
		t.Fatalf("required externalized Ask prompt = %#v, present=%v", prompt, exists)
	}
	option := question["options"].([]any)[0].(map[string]any)
	if _, exists := option["description"]; exists {
		t.Fatal("optional empty Ask option description stopped being omitted")
	}
}

func TestApprovalSubjectExternalizesInEventAndSnapshot(t *testing.T) {
	eventEnvelope := externalizableTestEvent(eventwire.Event{
		Kind:     "approval_request",
		Approval: &eventwire.Approval{ID: "approval-1", Tool: "shell", Subject: strings.Repeat("command ", 20)},
	}, externalizedDescriptor("/event/approval/subject"))
	raw, err := json.Marshal(eventEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	approval := decodeObjectForTest(t, raw)["event"].(map[string]any)["approval"].(map[string]any)
	if subject, exists := approval["subject"]; !exists || subject != nil {
		t.Fatalf("event approval subject = %#v, present=%v", subject, exists)
	}
	if _, err := DecodeRehydratedJSON[SessionEvent](raw, []RehydratedExternalizedField{{JSONPointer: "/event/approval/subject", Value: "restored command"}}); err != nil {
		t.Fatal(err)
	}

	goal, reason := "goal", "reason"
	snapshot := SessionSnapshot{
		Meta:    SessionMetaSnapshot{Goal: &goal},
		Runtime: SessionRuntimeState{LiveEvents: []eventwire.Event{}},
		PendingPrompt: &PendingPrompt{Kind: PromptApproval, Approval: &ApprovalPrompt{
			PromptID: "prompt-1", Tool: "shell", Subject: strings.Repeat("command ", 20), Reason: &reason,
			AllowedDecisions: []PromptDecision{DecisionAllowOnce},
		}},
		Externalized: []ExternalizedField{externalizedDescriptor("/pendingPrompt/approval/subject")},
	}
	raw, err = json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	root := decodeObjectForTest(t, raw)
	approval = root["pendingPrompt"].(map[string]any)["approval"].(map[string]any)
	if subject, exists := approval["subject"]; !exists || subject != nil {
		t.Fatalf("snapshot approval subject = %#v, present=%v", subject, exists)
	}
	rehydrated, err := RehydrateExternalizedJSON[SessionSnapshot](raw, []RehydratedExternalizedField{{JSONPointer: "/pendingPrompt/approval/subject", Value: "restored command"}})
	if err != nil {
		t.Fatal(err)
	}
	approval = decodeObjectForTest(t, rehydrated)["pendingPrompt"].(map[string]any)["approval"].(map[string]any)
	if approval["subject"] != "restored command" {
		t.Fatalf("snapshot approval subject not rehydrated: %#v", approval["subject"])
	}
}

func TestSnapshotLiveEventExternalizationUsesOwningPointer(t *testing.T) {
	goal := "goal"
	snapshot := SessionSnapshot{
		Meta:         SessionMetaSnapshot{Goal: &goal},
		Runtime:      SessionRuntimeState{LiveEvents: []eventwire.Event{{Kind: "text", Text: "large"}}},
		Externalized: []ExternalizedField{externalizedDescriptor("/runtime/liveEvents/0/text")},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	root := decodeObjectForTest(t, raw)
	live := root["runtime"].(map[string]any)["liveEvents"].([]any)[0].(map[string]any)
	if text, exists := live["text"]; !exists || text != nil {
		t.Fatalf("snapshot live-event text = %#v, present=%v", text, exists)
	}
	rehydrated, err := RehydrateExternalizedJSON[SessionSnapshot](raw, []RehydratedExternalizedField{{JSONPointer: "/runtime/liveEvents/0/text", Value: "restored"}})
	if err != nil {
		t.Fatal(err)
	}
	live = decodeObjectForTest(t, rehydrated)["runtime"].(map[string]any)["liveEvents"].([]any)[0].(map[string]any)
	if live["text"] != "restored" {
		t.Fatalf("snapshot live event was not rehydrated: %#v", live["text"])
	}
}

func TestSnapshotRehydrationPreservesLegitimateNilOptionalStrings(t *testing.T) {
	snapshotID := SnapshotID("snapshot-nil-optionals")
	snapshot := SessionSnapshot{
		SnapshotID: snapshotID, HostEpoch: "host-1",
		Target: RuntimeTarget{WorkspaceID: "workspace-1", SessionID: "session-1"}, RuntimeEpoch: "runtime-1",
		Meta: SessionMetaSnapshot{
			TopicID: "topic-1", Title: "Session", Goal: nil,
			ResolvedProfile: ResolvedProfile{
				Model: "test/model", Effort: "medium", CollaborationMode: CollaborationNormal,
				TokenMode: TokenFull, ToolApprovalMode: ToolApprovalAsk,
			},
			Capabilities: FrozenCapabilities(false, false),
		},
		Runtime: SessionRuntimeState{
			LastError: nil, LiveEvents: []eventwire.Event{},
		},
		History: HistoryPage{
			SnapshotID: snapshotID, Messages: []HistoryMessage{}, Externalized: []ExternalizedField{},
		},
		PendingPrompt: nil, Todos: []TodoItem{},
		Context: ContextView{Sources: []UsageSourceView{}, ReadFiles: []ReadFileRecord{}},
		Jobs:    []JobView{}, Checkpoints: []CheckpointView{},
		Externalized: []ExternalizedField{},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRehydratedJSON[SessionSnapshot](raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Meta.Goal != nil || decoded.Runtime.LastError != nil {
		t.Fatalf("optional nil fields changed: goal=%v lastError=%v", decoded.Meta.Goal, decoded.Runtime.LastError)
	}
}

func TestHistoryPageExternalizationAndTypedRehydration(t *testing.T) {
	content := "large history body"
	page := HistoryPage{
		SnapshotID: "snapshot-1", Messages: []HistoryMessage{{Role: "assistant", Content: &content}},
		StartTurn: 0, EndTurn: 1, TotalTurns: 1, ActualTurns: 1,
		Externalized: []ExternalizedField{externalizedDescriptor("/messages/0/content")},
	}
	raw, err := json.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	message := decodeObjectForTest(t, raw)["messages"].([]any)[0].(map[string]any)
	if contentValue, exists := message["content"]; !exists || contentValue != nil {
		t.Fatalf("history content = %#v, present=%v", contentValue, exists)
	}
	decoded, err := DecodeRehydratedJSON[HistoryPage](raw, []RehydratedExternalizedField{{JSONPointer: "/messages/0/content", Value: "restored history"}})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Messages[0].Content == nil || *decoded.Messages[0].Content != "restored history" {
		t.Fatalf("history content was not rehydrated: %#v", decoded.Messages[0].Content)
	}
}

func TestEventwireExternalizableFieldsDriveRemoteSchema(t *testing.T) {
	want := []string{
		"/approval/reason", "/approval/subject",
		"/ask/questions/*/options/*/description", "/ask/questions/*/prompt",
		"/compaction/archive", "/compaction/summary", "/detail", "/err",
		"/guardian/rationale", "/reasoning", "/text",
		"/tool/args", "/tool/diff", "/tool/err", "/tool/output",
	}
	got := externalizableJSONPointerPatterns(reflect.TypeOf(eventwire.Event{}))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event externalizable fields drifted\n got: %v\nwant: %v", got, want)
	}
	document, err := BuildSchemaDocument()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(document.Event.ExternalizableJSONPointers, got) {
		t.Fatal("event schema pointer list is not derived from eventwire tags")
	}
	for _, pointer := range want {
		property := schemaPropertyAtPattern(t, document.Event.Payload, pointer)
		if !property.Externalizable || !property.Schema.Nullable || property.Schema.Type != "string" {
			t.Fatalf("schema field %s = %+v", pointer, property)
		}
	}
}

type eventwireWithFutureExternalizable struct {
	eventwire.Event
	Future string `json:"future,omitempty" externalizable:"true"`
}

func TestNewNeutralEventwireFieldAutomaticallyEntersSchema(t *testing.T) {
	schema, err := buildEventSchema(reflect.TypeOf(eventwireWithFutureExternalizable{}))
	if err != nil {
		t.Fatal(err)
	}
	if !contains(schema.ExternalizableJSONPointers, "/future") {
		t.Fatalf("future neutral event field did not enter pointer patterns: %v", schema.ExternalizableJSONPointers)
	}
	property := schemaPropertyAtPattern(t, schema.Payload, "/future")
	if !property.Externalizable || !property.Schema.Nullable {
		t.Fatalf("future neutral event field lost schema overlay: %+v", property)
	}
}

func schemaPropertyAtPattern(t *testing.T, schema SchemaType, pointer string) SchemaProperty {
	t.Helper()
	segments := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	var property SchemaProperty
	for _, segment := range segments {
		if segment == "*" {
			if schema.Items == nil {
				t.Fatalf("%s does not cross an array", pointer)
			}
			schema = *schema.Items
			continue
		}
		found := false
		for _, candidate := range schema.Properties {
			if candidate.Name == segment {
				property = candidate
				schema = candidate.Schema
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s is absent from schema at %s", pointer, segment)
		}
	}
	return property
}

func decodeObjectForTest(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	return root
}
