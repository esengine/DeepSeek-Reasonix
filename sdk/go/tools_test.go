package extension

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// A tool the extension serves answers a call with its content, reports its
// own failure as isError, and names itself in the handshake without the
// Handler having to repeat Options.Tools.
func TestToolsAreDeclaredAndServed(t *testing.T) {
	tools := map[string]ToolFunc{
		"lookup": func(_ context.Context, args json.RawMessage) (string, error) {
			return "found " + string(args), nil
		},
		"broken": func(context.Context, json.RawMessage) (string, error) {
			return "", errors.New("the index is not built yet")
		},
		"disabled": nil,
	}
	host, _ := startFakeHost(t, basicHandler(), Options{Tools: tools})
	init := host.handshake(t)
	if len(init.Tools) != 2 || init.Tools[0] != "broken" || init.Tools[1] != "lookup" {
		t.Fatalf("handshake tools = %v, want both, sorted", init.Tools)
	}

	var out ToolCallResult
	resp := host.request(MethodExtensionToolCall, ToolCallParams{Name: "lookup", Arguments: json.RawMessage(`{"q":"x"}`), TimeoutMillis: 1000})
	if err := json.Unmarshal(resp.Result, &out); err != nil || out.IsError || out.Content != `found {"q":"x"}` {
		t.Fatalf("lookup = %+v err=%v respErr=%+v", out, err, resp.Err)
	}
	resp = host.request(MethodExtensionToolCall, ToolCallParams{Name: "broken", Arguments: json.RawMessage(`{}`)})
	if err := json.Unmarshal(resp.Result, &out); err != nil || !out.IsError || out.Content != "the index is not built yet" {
		t.Fatalf("broken = %+v err=%v", out, err)
	}
	resp = host.request(MethodExtensionToolCall, ToolCallParams{Name: "absent", Arguments: json.RawMessage(`{}`)})
	if resp.Err == nil || resp.Err.Code != CodeMethodNotFound {
		t.Fatalf("an unserved tool = %+v, want unknown_method", resp.Err)
	}
	resp = host.request(MethodExtensionToolCall, ToolCallParams{Name: "disabled", Arguments: json.RawMessage(`{}`)})
	if resp.Err == nil || resp.Err.Code != CodeMethodNotFound {
		t.Fatalf("a nil tool handler = %+v, want unknown_method", resp.Err)
	}
}

func TestNilToolHandlersDoNotDeclareTools(t *testing.T) {
	for _, tools := range []map[string]ToolFunc{nil, {}, {"disabled": nil}} {
		host, _ := startFakeHost(t, basicHandler(), Options{Tools: tools})
		init := host.handshake(t)
		if len(init.Tools) != 0 {
			t.Fatalf("nil or empty handlers declared tools: %v", init.Tools)
		}
		resp := host.request(MethodExtensionToolCall, ToolCallParams{Name: "disabled", Arguments: json.RawMessage(`{}`)})
		if resp.Err == nil || resp.Err.Code != CodeMethodNotFound {
			t.Fatalf("a nil tool handler = %+v, want unknown_method", resp.Err)
		}
	}
}
