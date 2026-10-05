package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
)

func TestModernHTTPURLElicitationRoundTrip(t *testing.T) {
	for _, action := range []string{"accept", "decline", "cancel", "headless"} {
		t.Run(action, func(t *testing.T) { testModernHTTPURL(t, action) })
	}
}

func testModernHTTPURL(t *testing.T, action string) {
	var pageRequests atomic.Int32
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer page.Close()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     any                        `json:"id"`
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var result any
		switch req.Method {
		case discoverMethod:
			result = map[string]any{"supportedVersions": []string{modernProtocolVersion}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "connect", "description": "Complete an external step", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			calls.Add(1)
			if responses, ok := req.Params["inputResponses"]; ok {
				if string(req.Params["requestState"]) != `"opaque-url-1"` {
					t.Errorf("lost state: %s", req.Params["requestState"])
				}
				result = map[string]any{"resultType": "complete", "content": []any{map[string]any{"type": "text", "text": string(responses)}}}
			} else {
				result = map[string]any{"resultType": "input_required", "requestState": "opaque-url-1", "inputRequests": map[string]any{
					"external": map[string]any{"method": elicitMethod, "params": map[string]any{"mode": "url", "message": "Finish on the server's page", "url": page.URL + "/connect"}},
				}}
			}
		default:
			t.Errorf("unexpected method %s", req.Method)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	host, tools, err := StartAll(ctx, []Spec{{Name: "external", Type: "http", URL: server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	if len(tools) != 1 {
		t.Fatalf("tools=%d", len(tools))
	}
	person := &fakeElicitor{replies: []tool.ElicitReply{{Action: action}}}
	var elicitor tool.Elicitor = person
	if action == "headless" {
		elicitor = nil
	}
	out, err := tools[0].Execute(tool.WithElicitor(ctx, elicitor), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("real HTTP URL-mode call failed before asking or retrying: %v", err)
	}
	wantAction := action
	if action == "headless" {
		wantAction = "decline"
	}
	if out != `{"external":{"action":"`+wantAction+`"}}` {
		t.Fatalf("URL result=%s, want accept without content", out)
	}
	if action != "headless" && (len(person.seen) != 1 || person.seen[0].Source != "external" || person.seen[0].URL != page.URL+"/connect" || len(person.seen[0].Fields) != 0) {
		t.Fatalf("requests=%+v", person.seen)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls=%d, want one manual consent followed by retry", calls.Load())
	}
	if pageRequests.Load() != 0 {
		t.Fatalf("client fetched the external page %d times", pageRequests.Load())
	}
}

func TestStdioURLElicitationReturnsOnlyAnAction(t *testing.T) {
	for _, action := range []string{"accept", "decline", "cancel", "headless"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			host, tools, err := StartAll(ctx, []Spec{{Name: "mock", Command: os.Args[0], Args: []string{"-test.run=TestHelperProcess", "--"}, Env: map[string]string{
				"GO_WANT_HELPER_PROCESS": "1", "GO_WANT_HELPER_ELICIT": "1", "GO_WANT_HELPER_URL_ELICIT": "https://example.invalid/connect?state=private",
			}}})
			if err != nil {
				t.Fatal(err)
			}
			defer host.Close()
			person := &fakeElicitor{replies: []tool.ElicitReply{{Action: action}}}
			if action != "headless" {
				ctx = tool.WithElicitor(ctx, person)
			}
			out, err := findToolByName(tools, "mcp__mock__echo").Execute(ctx, json.RawMessage(`{"msg":"hi"}`))
			want := action
			if action == "headless" {
				want = "decline"
			}
			if err != nil || out != `elicited: {"action":"`+want+`"}` {
				t.Fatalf("response=%q, %v", out, err)
			}
		})
	}
}

func TestURLElicitationValidatesBeforePrompting(t *testing.T) {
	for _, target := range []string{"", "file:///secret", "javascript:alert(1)", "https://user:secret@example.com", "https://", "//example.com", "https://example.com/\nsecret", "https://example.com/\u202eprivate", "https://example.com/" + strings.Repeat("a", 8192)} {
		person := &fakeElicitor{}
		params, _ := json.Marshal(map[string]any{"mode": "url", "message": "secret-message", "url": target})
		_, err := elicit(t.Context(), person, "s", params)
		if !errors.Is(err, errBadElicitation) {
			t.Fatalf("invalid URL accepted: %v", err)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe error: %v", err)
		}
		if len(person.seen) != 0 {
			t.Fatal("invalid request was prompted")
		}
	}
	for _, params := range []string{`{"mode":"url","url":"https://example.com"}`, `{"mode":"url","message":"x","url":"https://example.com","requestedSchema":{}}`} {
		if _, err := elicit(t.Context(), nil, "s", json.RawMessage(params)); !errors.Is(err, errBadElicitation) {
			t.Fatalf("invalid params=%s: %v", params, err)
		}
	}
}
