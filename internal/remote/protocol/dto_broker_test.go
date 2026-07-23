package protocol

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestBrokerProviderRequestRoundTripIsLossless(t *testing.T) {
	temperature := 0.25
	want := provider.Request{
		Messages: []provider.Message{{
			Role: provider.RoleAssistant, Content: "answer", ReasoningContent: "thought",
			ReasoningSignature: "signed", ToolCalls: []provider.ToolCall{{ID: "call-1", Name: "read", Arguments: `{"path":"x"}`}},
		}},
		Tools:       []provider.ToolSchema{{Name: "read", Description: "read a file", Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)}},
		Temperature: &temperature, MaxTokens: 4096,
	}
	wired := BrokerProviderRequestFromProvider(want)
	raw, err := json.Marshal(BrokerStreamOpenParams{StreamID: "s1", ProviderRef: "local/model", Request: wired})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeBrokerRequestParams(MethodBrokerStreamOpen, raw)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.(BrokerStreamOpenParams).Request.ProviderRequest()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("provider request changed\n got: %#v\nwant: %#v", got, want)
	}
}

func TestBrokerProviderDescriptorRoundTripPreservesRuntimeMetadata(t *testing.T) {
	want := BrokerProviderDescriptor{
		Ref: "local/model", DisplayName: "Local", Model: "model",
		ContextWindow: 1_000_000, PricingCurrency: "$",
		CacheHitPerMillion: 0.1, InputPerMillion: 1.25, OutputPerMillion: 4.5,
		SupportsVision: true, SupportedEfforts: []string{"low", "high"}, DefaultEffort: "high",
	}
	raw, err := json.Marshal(BrokerCatalogResult{Providers: []BrokerProviderDescriptor{want}})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeBrokerResult(MethodBrokerCatalog, raw)
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.(BrokerCatalogResult).Providers
	if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
		t.Fatalf("descriptor metadata changed\n got: %#v\nwant: %#v", got, want)
	}
}

func TestBrokerRequestStrictlyRejectsUnknownAndNonObjectToolSchema(t *testing.T) {
	unknown := json.RawMessage(`{"streamId":"s","providerRef":"p","request":{"messages":[{"role":"user","unknown":true}],"tools":[],"maxTokens":0}}`)
	if _, err := DecodeBrokerRequestParams(MethodBrokerStreamOpen, unknown); err == nil {
		t.Fatal("accepted unknown nested provider field")
	}
	nonObject := json.RawMessage(`{"streamId":"s","providerRef":"p","request":{"messages":[],"tools":[{"name":"x","description":"x","parameters":[]}],"maxTokens":0}}`)
	if _, err := DecodeBrokerRequestParams(MethodBrokerStreamOpen, nonObject); err == nil {
		t.Fatal("accepted non-object tool parameters")
	}
}

func TestBrokerProviderChunkRedactsProviderErrors(t *testing.T) {
	const secret = "sk-live-secret-canary"
	wired := BrokerProviderChunkFromProvider(provider.Chunk{Type: provider.ChunkError, Err: errors.New("Authorization: Bearer " + secret)})
	raw, err := json.Marshal(wired)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) || wired.Error == nil || wired.Error.Code != BrokerProviderFailed {
		t.Fatalf("unsafe provider error chunk: %s", raw)
	}
	converted := wired.ProviderChunk()
	if converted.Type != provider.ChunkError || converted.Err == nil || strings.Contains(converted.Err.Error(), secret) {
		t.Fatalf("converted chunk lost safe error semantics: %#v", converted)
	}
}

func TestBrokerSchemaUsesTypedRequestChunkAndJSONValue(t *testing.T) {
	document, err := BuildSchemaDocument()
	if err != nil {
		t.Fatal(err)
	}
	open := findSchemaMethod(t, document, MethodBrokerStreamOpen)
	request := findProperty(t, open.Params, "request").Schema
	if request.Type != "object" {
		t.Fatalf("request schema type = %q, want object", request.Type)
	}
	tools := findProperty(t, request, "tools").Schema
	parameters := findProperty(t, *tools.Items, "parameters").Schema
	if parameters.Type != "json" {
		t.Fatalf("tool parameters schema = %#v, want json", parameters)
	}
	chunkMethod := findSchemaMethod(t, document, MethodBrokerStreamChunk)
	chunk := findProperty(t, chunkMethod.Params, "chunk").Schema
	if chunk.Type != "object" || len(findProperty(t, chunk, "type").Schema.Enum) != 8 {
		t.Fatalf("chunk schema is not a typed enum object: %#v", chunk)
	}
}
