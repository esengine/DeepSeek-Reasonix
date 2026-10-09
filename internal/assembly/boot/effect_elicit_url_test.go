package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/observe"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
)

func TestEffectYoloURLInteractionWaitsForHumanThroughBuild(t *testing.T) {
	root := observeProject(t)
	prov := testutil.NewMock("elicit", call("url", "use_capability", `{"action":"call","capability_id":"mcp-tool:external/connect","arguments":{}}`), testutil.Turn{Text: "Done"})
	setBootTokenProfileTestProvider(t, prov)
	var pageRequests, calls atomic.Int32
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { pageRequests.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer page.Close()
	server := urlInteractionServer(t, page.URL+"/connect?state=PRIVATE_URL", &calls)
	defer server.Close()
	writeUserConfig(t, userModel+fmt.Sprintf("\n[[plugins]]\nname=\"external\"\ntype=\"http\"\nurl=%q\n", server.URL))
	approveWorkspace(t, root)
	asks := make(chan event.Ask, 4)
	ctrl, err := Build(t.Context(), Options{WorkspaceRoot: root, HeadlessApprovalMode: control.ToolApprovalYolo, Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.AskRequest {
			asks <- e.Ask
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	ctrl.EnableInteractiveApproval()
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	waitForCond(t, "lazy MCP server swapped its real tools in", 10*time.Second, func() bool { return ctrl.MCPCatalogTools()["external"] > 0 })
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ctrl.Run(ctx, "Connect") }()
	var ask event.Ask
	select {
	case ask = <-asks:
	case err := <-done:
		t.Fatalf("run ended without a human decision: %v; results=%+v", err, toolResults(prov.Requests()))
	case <-ctx.Done():
		t.Fatal("no human prompt")
	}
	if calls.Load() != 1 || pageRequests.Load() != 0 || ask.Origin == nil || ask.Origin.URL == "" {
		t.Fatalf("before consent: calls=%d, page=%d, ask=%+v", calls.Load(), pageRequests.Load(), ask)
	}
	select {
	case err := <-done:
		t.Fatalf("yolo answered automatically: %v", err)
	default:
	}
	ctrl.AnswerQuestion(ask.ID, []event.AskAnswer{{QuestionID: "mcp.url", Selected: []string{"accept"}}})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("human action did not resume the MCP call")
	}
	if calls.Load() != 2 || pageRequests.Load() != 0 {
		t.Fatalf("after action: calls=%d, page=%d", calls.Load(), pageRequests.Load())
	}
	requests, _ := json.Marshal(prov.Requests())
	if strings.Contains(string(requests), "PRIVATE_") || !strings.Contains(string(requests), `\"action\":\"accept\"`) {
		t.Fatalf("unsafe or missing provider result: %s", requests)
	}
}

func TestEffectObserveURLInteractionDeclinesThroughBuild(t *testing.T) {
	root := observeProject(t)
	setBootTokenProfileTestProvider(t, &testutil.MockProvider{})
	var asks atomic.Int32
	ctrl, err := Build(t.Context(), Options{WorkspaceRoot: root, Observe: &ObserveOptions{Pending: observe.NewLedger(nil)}, Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.AskRequest {
			asks.Add(1)
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	reply, err := ctrl.Elicit(ctx, tool.ElicitRequest{Source: "external", URL: "https://example.invalid/?state=PRIVATE_URL", Message: "PRIVATE_MESSAGE"})
	if err != nil || !reply.Declined || reply.Action != "decline" || asks.Load() != 0 {
		t.Fatalf("unattended elicitation: reply=%+v, err=%v, asks=%d", reply, err, asks.Load())
	}
}

func urlInteractionServer(t *testing.T, target string, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     any                        `json:"id"`
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch req.Method {
		case "server/discover":
			result = map[string]any{"supportedVersions": []string{"2026-07-28"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "connect", "description": "Connect", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			calls.Add(1)
			if responses, ok := req.Params["inputResponses"]; ok {
				if string(req.Params["requestState"]) != `"PRIVATE_STATE"` || string(responses) != `{"request":{"action":"accept"}}` {
					t.Errorf("unsafe retry: %+v", req.Params)
				}
				result = map[string]any{"resultType": "complete", "content": []any{map[string]any{"type": "text", "text": string(responses)}}}
			} else {
				result = map[string]any{"resultType": "input_required", "requestState": "PRIVATE_STATE", "inputRequests": map[string]any{"request": map[string]any{"method": "elicitation/create", "params": map[string]any{"mode": "url", "url": target, "message": "PRIVATE_MESSAGE"}}}}
			}
		case "prompts/list":
			result = map[string]any{"prompts": []any{}}
		case "resources/list":
			result = map[string]any{"resources": []any{}}
		default:
			t.Errorf("unexpected method: %s", req.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
}
