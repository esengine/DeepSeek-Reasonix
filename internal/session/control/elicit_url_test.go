package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/hook"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/permission"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/trajectory"
)

func TestURLInteractionThroughMCPAndControllerKeepsSecretsOutOfRecords(t *testing.T) {
	const target = "https://example.invalid/connect?state=PRIVATE_URL"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     any                        `json:"id"`
			Method string                     `json:"method"`
			Params map[string]json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch request.Method {
		case "server/discover":
			result = map[string]any{"supportedVersions": []string{"2026-07-28"}, "capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []any{map[string]any{"name": "connect", "description": "Connect an account", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			if _, ok := request.Params["inputResponses"]; ok {
				if string(request.Params["requestState"]) != `"PRIVATE_STATE"` {
					t.Errorf("retry lost requestState: %s", request.Params["requestState"])
				}
				result = map[string]any{"resultType": "complete", "content": []any{map[string]any{"type": "text", "text": "Connected"}}}
			} else {
				result = map[string]any{"resultType": "input_required", "requestState": "PRIVATE_STATE", "inputRequests": map[string]any{"PRIVATE_REQUEST_ID": map[string]any{"method": "elicitation/create", "params": map[string]any{"mode": "url", "message": "PRIVATE_MESSAGE", "url": target}}}}
			}
		default:
			t.Errorf("unexpected request %s", request.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	host, tools, err := plugin.StartAll(ctx, []plugin.Spec{{Name: "external", Type: "http", URL: server.URL, Authorized: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	reg := tool.NewRegistry()
	for _, entry := range tools {
		reg.Add(entry)
	}
	prov := &recordingProvider{streams: [][]provider.Chunk{toolCallTurn("a1", "mcp__external__connect", "{}"), textTurn("Done.")}}
	sess := sessionstore.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleSystem, Content: "Test system prompt"})
	ag := agent.New(prov, reg, sess, agent.Options{}, event.Discard)
	dir := testenv.TempDir(t)
	asks := make(chan event.Ask, 1)
	receipts := make(chan *provider.DecisionReceipt, 1)
	recorder, err := trajectory.New(event.FuncSink(func(e event.Event) {
		if e.Kind == event.AskRequest {
			asks <- e.Ask
		}
		if e.DecisionReceipt != nil {
			receipts <- e.DecisionReceipt
		}
	}), filepath.Join(dir, "trajectory.jsonl"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	notifications := make(chan string, 1)
	hooks := hook.NewRunner([]hook.ResolvedHook{{HookConfig: hook.HookConfig{Command: "record-notification"}, Event: hook.Notification}}, "", func(_ context.Context, in hook.SpawnInput) hook.SpawnResult {
		notifications <- in.Stdin
		return hook.SpawnResult{ExitCode: 0}
	}, nil)
	c := New(Options{Runner: ag, Executor: ag, Policy: permission.New("ask", []string{"mcp__external__connect"}, nil, nil), Sink: recorder, Hooks: hooks, SessionDir: dir, SessionPath: filepath.Join(dir, "session.jsonl")})
	c.EnableInteractiveApproval()
	done := make(chan error, 1)
	go func() {
		done <- c.runOneTurn(ctx, orchestratedTurn{input: "Connect the account", raw: "Connect the account"})
	}()
	var ask event.Ask
	select {
	case ask = <-asks:
	case <-ctx.Done():
		t.Fatalf("no URL card: tools=%v, providerResult=%q", tools[0].Name(), lastToolResult(prov))
	}
	if ask.Origin == nil || ask.Origin.URL != target || ask.Origin.Message != "PRIVATE_MESSAGE" {
		t.Fatalf("live ask=%+v", ask)
	}
	questions, _ := json.Marshal(ask.Questions)
	if strings.Contains(string(questions), "PRIVATE_") {
		t.Fatalf("secrets in durable question/barrier: %s", questions)
	}
	c.AnswerQuestion(ask.ID, []event.AskAnswer{{QuestionID: "mcp.url", Selected: []string{"accept"}}})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("MCP call never resumed")
	}
	select {
	case receipt := <-receipts:
		if receipt.Outcome != "accepted" || strings.Contains(receipt.Subject, "PRIVATE_") {
			t.Fatalf("unsafe receipt=%+v", receipt)
		}
	case <-ctx.Done():
		t.Fatal("no receipt")
	}
	select {
	case notification := <-notifications:
		if strings.Contains(notification, "PRIVATE_") {
			t.Fatalf("unsafe Notification hook=%s", notification)
		}
	case <-ctx.Done():
		t.Fatal("no notification")
	}
	if err := sess.Save(filepath.Join(dir, "session.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
	for _, filename := range []string{"session.jsonl", "trajectory.jsonl"} {
		data, err := os.ReadFile(filepath.Join(dir, filename))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "PRIVATE_") {
			t.Fatalf("secrets persisted to %s", filename)
		}
	}
	for _, request := range prov.requests {
		data, _ := json.Marshal(request)
		if strings.Contains(string(data), "PRIVATE_") {
			t.Fatalf("secrets reached provider: %s", data)
		}
	}
	if !strings.Contains(lastToolResult(prov), "Connected") {
		t.Fatalf("provider result=%q", lastToolResult(prov))
	}
}

func TestURLInteractionRequiresOneRecognizedAction(t *testing.T) {
	for _, answers := range [][]event.AskAnswer{nil, {{QuestionID: "other", Selected: []string{"accept"}}}, {{QuestionID: "mcp.url", Selected: []string{"accept", "PRIVATE_VALUE"}}}, {{QuestionID: "mcp.url", Selected: []string{"unknown"}}}} {
		if got := elicitURLAction(answers); got != "decline" {
			t.Fatalf("invalid answer accepted: %q", got)
		}
	}
}
