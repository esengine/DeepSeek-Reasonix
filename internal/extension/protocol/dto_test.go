package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// methodFixtures holds one fully populated representative value for every
// registered params and result DTO. Round-tripping through the strict
// direction decoders proves the JSON shape is lossless.
var methodFixtures = map[Method]struct {
	params any
	result any
}{
	MethodExtensionInitialize: {
		params: InitializeParams{
			ProtocolVersion: "1",
			ProtocolID:      ProtocolID,
			Manifest: ManifestExpectation{
				Intercepts:   []string{"tool.before"},
				Replaces:     []string{"tool:bash"},
				Providers:    []string{"acme"},
				UIActions:    []string{"acme.refresh"},
				Capabilities: []string{"content_refs"},
			},
			Session:      SessionContext{SessionID: "s-1", WorkspaceRoot: "/repo", Generation: 3},
			Capabilities: HostCapabilities{ContentRefs: true, UIHost: UIHostDesktop, ProtocolVersion: "1"},
		},
		result: InitializeResult{
			ProtocolVersion: "1", Name: "acme", Version: "1.2.3",
			Subscriptions: []string{"tool.before"},
			Replaces:      []string{"tool:bash"},
			Providers: []ProviderDescriptor{{
				Ref: "acme", DisplayName: "Acme", Model: "acme-1", ContextWindow: 128000,
				PricingCurrency: "$", CacheHitPerMillion: 0.1, InputPerMillion: 1, OutputPerMillion: 2,
				Vision: true, Tools: true, Reasoning: true,
				Efforts: []string{"low", "high"}, DefaultEffort: "low",
				ToolCallReasoning: true, ReasoningRoundTrip: true, WarnOnMissingToolCallReasoning: true,
			}},
			UIActions:          []UIActionDecl{{ActionID: "acme.refresh", Label: "Refresh"}},
			StateSchemaVersion: 2,
		},
	},
	MethodExtensionInitialized: {params: InitializedParams{}},
	MethodExtensionShutdown: {
		params: ShutdownParams{TimeoutMillis: 5000},
		result: ShutdownResult{Accepted: true},
	},
	MethodExtensionIntercept: {
		params: InterceptParams{
			Event: EventToolBefore, Seq: 7,
			Payload:       json.RawMessage(`{"tool":"bash"}`),
			TimeoutMillis: 250,
		},
		result: InterceptResult{
			Decision:    DecisionReplace,
			Reason:      "rewritten",
			Replacement: json.RawMessage(`{"tool":"read"}`),
		},
	},
	MethodExtensionEvent: {
		params: EventParams{Event: EventSessionStart, Payload: json.RawMessage(`{"sessionId":"s-1"}`)},
	},
	MethodExtensionResourcesChanged: {
		params: ResourcesChangedParams{Paths: []string{"skills/a", "commands/b"}},
	},
	MethodExtensionProviderCatalog: {
		params: ProviderCatalogParams{},
		result: ProviderCatalogResult{Providers: []ProviderDescriptor{{Ref: "acme"}}},
	},
	MethodExtensionProviderStreamOpen: {
		params: StreamOpenParams{
			StreamID: "st-1", ProviderRef: "acme", Model: "acme-1", Effort: "high", SeqBase: 1,
			Request: ProviderRequest{
				Messages: []ProviderMessage{{
					Role: ProviderRoleAssistant, Content: "hi",
					Images:             []string{"data:image/png;base64,AA=="},
					ReasoningContent:   "thinking",
					ReasoningSignature: "sig",
					ToolCalls:          []ProviderToolCall{{ID: "c1", Name: "bash", Arguments: "{}", ThoughtSignature: "ts"}},
					ToolCallID:         "c1",
					Name:               "bash",
				}},
				Tools:       []ProviderToolSchema{{Name: "bash", Description: "run", Parameters: json.RawMessage(`{"type":"object"}`)}},
				Temperature: floatPtr(0.5),
				MaxTokens:   1024,
			},
		},
		result: StreamOpenResult{Accepted: true},
	},
	MethodExtensionProviderStreamCancel: {
		params: StreamCancelParams{StreamID: "st-1"},
		result: StreamCancelResult{Cancelled: true},
	},
	MethodExtensionProviderStreamChunk: {
		params: StreamChunkParams{
			StreamID: "st-1", Seq: 2,
			Chunk: ProviderChunk{
				Type: ChunkUsage, ArgChars: 0,
				Usage: &ProviderUsage{
					PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3,
					CacheHitTokens: 4, CacheMissTokens: 5, ReasoningTokens: 6,
					FinishReason: "stop",
				},
			},
		},
	},
	MethodExtensionProviderStreamEnd: {
		params: StreamEndParams{StreamID: "st-1", LastSeq: 9, Error: "", Interrupted: true},
	},
	MethodExtensionUIAction: {
		params: UIActionParams{
			ActionID: "acme.refresh", SessionID: "s-1", Generation: 3,
			Args: map[string]string{"k": "v"},
		},
		result: UIActionResult{Accepted: true, Message: "queued"},
	},
	MethodExtensionUISubmit: {
		params: UISubmitParams{
			SurfaceID: "form-1", SessionID: "s-1", Generation: 3,
			Values: map[string]any{"name": "x", "count": float64(2), "ok": true},
		},
		result: UISubmitResult{Accepted: true},
	},
	MethodHostUIPublish: {
		params: UIPublishParams{
			SurfaceID: "card-1", SessionID: "s-1", Generation: 3,
			Kind:    UISurfaceCard,
			Payload: json.RawMessage(`{"title":"t"}`),
		},
		result: UIPublishResult{Accepted: true},
	},
	MethodHostUIRequest: {
		params: UIRequestParams{
			SurfaceID: "ask-1", SessionID: "s-1", Generation: 3,
			Kind:    UIRequestSelect,
			Payload: json.RawMessage(`{"fields":[]}`),
		},
		result: UIRequestResult{Cancelled: false, Values: map[string]any{"choice": "a"}},
	},
	MethodHostContentRead: {
		params: ContentReadParams{ContentRef: "cref-1", Offset: 0},
		result: ContentReadResult{
			ContentRef: "cref-1", Offset: 0, DataBase64: "aGk=",
			NextOffset: int64Ptr(2), TotalBytes: 2,
			SHA256:   strings.Repeat("a", 64),
			Encoding: ContentUTF8,
		},
	},
}

func floatPtr(v float64) *float64 { return &v }
func int64Ptr(v int64) *int64     { return &v }

func TestMethodDTORoundTripsAreLossless(t *testing.T) {
	for _, spec := range Registry() {
		fixture, ok := methodFixtures[spec.Name]
		if !ok {
			t.Fatalf("no fixture for %s", spec.Name)
		}
		if reflect.TypeOf(fixture.params) != spec.ParamsType {
			t.Fatalf("%s fixture params type = %v, want %v", spec.Name, reflect.TypeOf(fixture.params), spec.ParamsType)
		}
		t.Run(string(spec.Name)+"/params", func(t *testing.T) {
			roundTripThroughDecoder(t, spec, fixture.params, true)
		})
		if spec.Notification() {
			continue
		}
		if reflect.TypeOf(fixture.result) != spec.ResultType {
			t.Fatalf("%s fixture result type = %v, want %v", spec.Name, reflect.TypeOf(fixture.result), spec.ResultType)
		}
		t.Run(string(spec.Name)+"/result", func(t *testing.T) {
			roundTripThroughDecoder(t, spec, fixture.result, false)
		})
	}
}

func roundTripThroughDecoder(t *testing.T, spec MethodSpec, value any, params bool) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded any
	switch spec.Direction {
	case DirectionHostToExtensionRequest:
		if params {
			decoded, err = DecodeHostRequestParams(spec.Name, raw)
		} else {
			decoded, err = DecodeHostRequestResult(spec.Name, raw)
		}
	case DirectionExtensionToHostRequest:
		if params {
			decoded, err = DecodeExtensionRequestParams(spec.Name, raw)
		} else {
			decoded, err = DecodeExtensionRequestResult(spec.Name, raw)
		}
	case DirectionHostToExtensionNotification:
		decoded, err = DecodeHostNotificationParams(spec.Name, raw)
	case DirectionExtensionToHostNotification:
		decoded, err = DecodeExtensionNotificationParams(spec.Name, raw)
	}
	if err != nil {
		t.Fatalf("strict decode of own fixture failed: %v\njson: %s", err, raw)
	}
	if !reflect.DeepEqual(decoded, value) {
		t.Fatalf("round trip not lossless:\n got: %#v\nwant: %#v\njson: %s", decoded, value, raw)
	}
}

// TestPayloadDTORoundTrips covers the structured UI payload documents, which
// are not method DTOs but ride inside UIPublishParams/UIRequestParams.
func TestPayloadDTORoundTrips(t *testing.T) {
	payloads := []any{
		UIStatusPayload{Label: "l", Detail: "d", Severity: UISeverityWarn, Progress: floatPtr(0.5)},
		UICardPayload{
			Title: "t", Markdown: "**m**", Text: "x",
			Fields:   []UIKeyValue{{Key: "k", Value: "v"}},
			Progress: floatPtr(1),
			Actions:  []UIActionRef{{ActionID: "a", Label: "go"}},
		},
		UIFormPayload{
			Title: "t", Message: "m",
			Fields: []UIFormField{{
				Key: "f", Label: "l", Kind: UIFieldMultiselect,
				Options: []string{"a", "b"}, Default: "a", Required: true,
			}},
		},
		UINotificationPayload{Title: "t", Body: "b", Severity: UISeverityError},
	}
	for _, payload := range payloads {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal %T: %v", payload, err)
		}
		decoded, err := decodeAndValidate(raw, reflect.TypeOf(payload))
		if err != nil {
			t.Fatalf("strict decode %T: %v\njson: %s", payload, err, raw)
		}
		if !reflect.DeepEqual(decoded, payload) {
			t.Fatalf("round trip not lossless for %T:\n got: %#v\nwant: %#v", payload, decoded, payload)
		}
	}
}

func TestStrictDecodersRejectBadShapes(t *testing.T) {
	tests := []struct {
		name   string
		decode func() (any, error)
	}{
		{"unknown field", func() (any, error) {
			return DecodeHostRequestParams(MethodExtensionShutdown, []byte(`{"timeoutMillis":1,"bogus":1}`))
		}},
		{"missing required", func() (any, error) {
			return DecodeHostRequestParams(MethodExtensionShutdown, []byte(`{}`))
		}},
		{"null for non-nullable", func() (any, error) {
			return DecodeHostRequestParams(MethodExtensionShutdown, []byte(`{"timeoutMillis":null}`))
		}},
		{"bad enum", func() (any, error) {
			return DecodeHostRequestResult(MethodExtensionIntercept, []byte(`{"decision":"bogus"}`))
		}},
		{"empty required enum", func() (any, error) {
			return DecodeHostRequestResult(MethodExtensionIntercept, []byte(`{"decision":""}`))
		}},
		{"min violation seq", func() (any, error) {
			return DecodeExtensionNotificationParams(MethodExtensionProviderStreamChunk,
				[]byte(`{"streamId":"s","seq":0,"chunk":{"type":"done"}}`))
		}},
		{"min violation offset", func() (any, error) {
			return DecodeExtensionRequestParams(MethodHostContentRead, []byte(`{"contentRef":"c","offset":-1}`))
		}},
		{"nonempty violation", func() (any, error) {
			return DecodeExtensionRequestParams(MethodHostContentRead, []byte(`{"contentRef":" ","offset":0}`))
		}},
		{"sha256 violation", func() (any, error) {
			return DecodeExtensionRequestResult(MethodHostContentRead, []byte(
				`{"contentRef":"c","offset":0,"dataBase64":"","totalBytes":0,"sha256":"zz","encoding":"utf8"}`))
		}},
		{"error chunk without error", func() (any, error) {
			return DecodeExtensionNotificationParams(MethodExtensionProviderStreamChunk,
				[]byte(`{"streamId":"s","seq":1,"chunk":{"type":"error"}}`))
		}},
		{"usage chunk without usage", func() (any, error) {
			return DecodeExtensionNotificationParams(MethodExtensionProviderStreamChunk,
				[]byte(`{"streamId":"s","seq":1,"chunk":{"type":"usage"}}`))
		}},
		{"nil request arrays", func() (any, error) {
			return DecodeHostRequestParams(MethodExtensionProviderStreamOpen,
				[]byte(`{"streamId":"s","providerRef":"p","request":{"maxTokens":0},"seqBase":0}`))
		}},
		{"tool parameters not object", func() (any, error) {
			return DecodeHostRequestParams(MethodExtensionProviderStreamOpen,
				[]byte(`{"streamId":"s","providerRef":"p","request":{"messages":[],"tools":[{"name":"t","parameters":[1]}],"maxTokens":0},"seqBase":0}`))
		}},
		{"trailing json", func() (any, error) {
			return DecodeHostRequestParams(MethodExtensionShutdown, []byte(`{"timeoutMillis":1} {}`))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.decode(); err == nil {
				t.Fatal("strict decoder accepted an invalid payload")
			}
		})
	}
}

func TestExternalizableFieldsAcceptNullPlaceholder(t *testing.T) {
	// A null payload is the content-ref placeholder shape; only
	// externalizable-tagged fields may carry it.
	if _, err := DecodeHostNotificationParams(MethodExtensionEvent, []byte(`{"event":"session.start","payload":null}`)); err != nil {
		t.Fatalf("externalizable payload null rejected: %v", err)
	}
	if _, err := DecodeExtensionRequestParams(MethodHostUIPublish,
		[]byte(`{"surfaceId":"s","sessionId":"s","generation":0,"kind":"card","payload":null}`)); err == nil {
		t.Fatal("non-externalizable payload accepted null")
	}
	pointers := ExternalizablePointers(reflect.TypeFor[InterceptParams]())
	if !reflect.DeepEqual(pointers, []string{"/payload"}) {
		t.Fatalf("ExternalizablePointers(InterceptParams) = %v", pointers)
	}
	pointers = ExternalizablePointers(reflect.TypeFor[ProviderRequest]())
	if !reflect.DeepEqual(pointers, []string{"/messages/*/content"}) {
		t.Fatalf("ExternalizablePointers(ProviderRequest) = %v", pointers)
	}
}
