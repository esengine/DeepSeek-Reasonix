package extension

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
)

func TestToolCallRejectsMissingArguments(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params string
	}{
		{"served tool", `{"name":"lookup","timeoutMillis":0}`},
		{"unknown tool", `{"name":"absent","timeoutMillis":0}`},
		{"missing name", `{"arguments":{},"timeoutMillis":0}`},
		{"negative timeout", `{"name":"lookup","arguments":{},"timeoutMillis":-1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			host, _ := startFakeHost(t, basicHandler(), Options{Tools: map[string]ToolFunc{
				"lookup": func(context.Context, json.RawMessage) (string, error) {
					calls.Add(1)
					return "ran", nil
				},
			}})
			host.handshake(t)
			resp := host.request(MethodExtensionToolCall, json.RawMessage(tc.params))
			if resp.Err == nil || resp.Err.Code != CodeInvalidParams {
				t.Fatalf("response = %+v, want invalid_params", resp)
			}
			if n := calls.Load(); n != 0 {
				t.Fatalf("handler ran %d times for invalid params", n)
			}
		})
	}
}

func TestToolCallPreservesExplicitArguments(t *testing.T) {
	for _, args := range []string{"null", "{}", "[]", `"text"`, "7", "true"} {
		t.Run(args, func(t *testing.T) {
			var calls atomic.Int32
			host, _ := startFakeHost(t, basicHandler(), Options{Tools: map[string]ToolFunc{
				"lookup": func(_ context.Context, raw json.RawMessage) (string, error) {
					calls.Add(1)
					return string(raw), nil
				},
			}})
			host.handshake(t)
			resp := host.request(MethodExtensionToolCall, ToolCallParams{Name: "lookup", Arguments: json.RawMessage(args)})
			var result ToolCallResult
			if resp.Err != nil || json.Unmarshal(resp.Result, &result) != nil || result.IsError || result.Content != args {
				t.Fatalf("response = %+v, result = %+v, want %s", resp, result, args)
			}
			if n := calls.Load(); n != 1 {
				t.Fatalf("handler ran %d times, want 1", n)
			}
		})
	}
}
