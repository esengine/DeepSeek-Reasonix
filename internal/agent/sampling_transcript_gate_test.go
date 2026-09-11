package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestSamplingTranscriptGateBlocksInvalidArgumentsBeforeProvider(t *testing.T) {
	for _, args := range []string{`{"path":`, `[]`, `null`, `"string"`} {
		t.Run(args, func(t *testing.T) {
			mp := &mockProvider{name: "p", chunks: []provider.Chunk{{Type: provider.ChunkDone}}}
			a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{}, event.Discard)
			req := provider.Request{Messages: []provider.Message{
				{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "a", Name: "read_file", Arguments: args}}},
				{Role: provider.RoleTool, ToolCallID: "a", Name: "read_file", Content: "result"},
			}}
			_, err := a.streamProviderRequest(context.Background(), req)
			if err == nil {
				t.Fatal("invalid tool arguments reached provider")
			}
			if len(mp.requests) != 0 {
				t.Fatalf("invalid request sent %d times", len(mp.requests))
			}
		})
	}
}

func TestSamplingTranscriptGateRunsAfterProviderInterceptor(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, payload json.RawMessage) (protocol.InterceptResult, error) {
		if ev != protocol.EventProviderRequest {
			return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
		}
		var in dispatch.ProviderRequestPayload
		if err := json.Unmarshal(payload, &in); err != nil {
			return protocol.InterceptResult{}, err
		}
		// A schema-valid replacement can still contain a protocol-invalid tool
		// transcript. The final gate must inspect this replacement.
		var extra []protocol.ProviderMessage
		if err := json.Unmarshal([]byte(`[{"role":"assistant","tool_calls":[{"id":"a","name":"read_file","arguments":"[]"}]},{"role":"tool","tool_call_id":"a","name":"read_file","content":"result"}]`), &extra); err != nil {
			return protocol.InterceptResult{}, err
		}
		in.Request.Messages = append(in.Request.Messages, extra...)
		return replaceWith(t, in), nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointProviderRequest)
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{{Type: provider.ChunkDone}}}
	a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{Extensions: d}, event.Discard)
	if _, err := a.buildSamplingRequest(context.Background(), CompactionTriggerPressure); err == nil {
		t.Fatal("final interceptor replacement bypassed transcript gate")
	}
	if len(mp.requests) != 0 {
		t.Fatal("request preparation contacted provider")
	}
}

func TestSamplingTranscriptGatePreservesHealthyRequestBytes(t *testing.T) {
	mp := &mockProvider{name: "p", chunks: []provider.Chunk{{Type: provider.ChunkDone}}}
	a := New(mp, tool.NewRegistry(), NewSession("sys"), Options{}, event.Discard)
	req := provider.Request{MaxTokens: 1234, Temperature: provider.OptionalTemperature(0.25), Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "stable cache prefix"},
		{Role: provider.RoleUser, Content: "read both"},
		{Role: provider.RoleAssistant, ReasoningContent: "read files", ToolCalls: []provider.ToolCall{{ID: "a", Name: "read_file", Arguments: ` { "path": "one" } `}, {ID: "b", Name: "read_file", Arguments: `{}`}}},
		{Role: provider.RoleTool, ToolCallID: "b", Name: "read_file", Content: "two"},
		{Role: provider.RoleTool, ToolCallID: "a", Name: "read_file", Content: "one"},
	}}
	before, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := a.streamProviderRequest(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	if len(mp.requests) != 1 {
		t.Fatalf("provider requests=%d", len(mp.requests))
	}
	got, err := json.Marshal(mp.requests[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, got) {
		t.Fatalf("healthy request changed\nbefore=%s\nafter=%s", before, got)
	}
	after, _ := json.Marshal(req)
	if !bytes.Equal(before, after) {
		t.Fatal("transcript gate mutated caller request")
	}
}
