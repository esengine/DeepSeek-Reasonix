package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	mcpjsonrpc "github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"reasonix/internal/mcpinteraction"
)

// scriptedBroker answers every elicitation with a canned result.
type scriptedBroker struct {
	mu       sync.Mutex
	got      []mcpinteraction.Request
	answer   mcpinteraction.Result
	answerFn func(mcpinteraction.Request) mcpinteraction.Result
}

func (b *scriptedBroker) Interact(_ context.Context, req mcpinteraction.Request) (mcpinteraction.Result, error) {
	b.mu.Lock()
	b.got = append(b.got, req)
	answer := b.answer
	if b.answerFn != nil {
		answer = b.answerFn(req)
	}
	b.mu.Unlock()
	return answer, nil
}

func (b *scriptedBroker) requests() []mcpinteraction.Request {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]mcpinteraction.Request(nil), b.got...)
}

// mrtrFixtureServer builds a server whose tool returns an input-required
// elicitation on the first call and completes on the second, recording the
// requestState of each attempt so the retry contract is observable.
type mrtrFixtureServer struct {
	mu        sync.Mutex
	attempts  int
	states    []string
	numInputs int // how many parallel input requests to return (multi-input test)
}

func (f *mrtrFixtureServer) handler(ctx context.Context, req *mcpsdk.CallToolRequest) (*mcpsdk.CallToolResult, error) {
	f.mu.Lock()
	f.attempts++
	f.mu.Unlock()
	ss := req.GetSession().(*mcpsdk.ServerSession)
	state := ""
	if ip := ss.InitializeParams(); ip != nil {
		state = ip.ProtocolVersion
	}
	f.mu.Lock()
	f.states = append(f.states, state)
	attempt := f.attempts
	f.mu.Unlock()
	if attempt == 1 {
		f.mu.Lock()
		n := f.numInputs
		f.mu.Unlock()
		elicitations := make([]*mcpsdk.ElicitParams, n)
		for i := range elicitations {
			elicitations[i] = &mcpsdk.ElicitParams{
				Message: fmt.Sprintf("question %d", i+1),
				RequestedSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"answer": map[string]any{"type": "string"},
					},
					"required": []any{"answer"},
				},
			}
		}
		requests := make(mcpsdk.InputRequestMap, len(elicitations))
		for i, e := range elicitations {
			requests[fmt.Sprintf("input-%d", i+1)] = e
		}
		return &mcpsdk.CallToolResult{InputRequests: requests}, nil
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: "done"}},
	}, nil
}

func (f *mrtrFixtureServer) server() *mcpsdk.Server {
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "mrtr-fixture", Version: "1"}, nil)
	server.AddTool(&mcpsdk.Tool{
		Name: "ask_then_do", Description: "elicits then completes",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, f.handler)
	return server
}

// runMRTTool drives one tools/call through the profile-aware SDK session with
// the broker attached to the call context, exactly as the agent does.
func runMRTTool(t *testing.T, broker mcpinteraction.Broker, fixture *mrtrFixtureServer) (*mcpsdk.CallToolResult, error) {
	t.Helper()
	lifeCtx, cancelLife := context.WithCancel(context.Background())
	transport := &sdkSessionTransport{
		name: "mrtr", spec: Spec{Name: "mrtr", Type: "http"}, profile: HostProfileInteractive,
		lifeCtx: lifeCtx, cancel: cancelLife, state: SessionStateConnecting,
	}
	transport.endpointFactory = func(ctx context.Context) (sdkEndpoint, error) {
		clientSide, serverSide := mcpsdk.NewInMemoryTransports()
		go func() { _ = fixture.server().Run(ctx, serverSide) }()
		return sdkEndpoint{transport: clientSide}, nil
	}
	t.Cleanup(transport.close)

	managed, err := transport.acquire(t.Context())
	if err != nil {
		return nil, err
	}
	callCtx := t.Context()
	if broker != nil {
		callCtx = mcpinteraction.WithBroker(callCtx, broker)
	}
	raw, err := invokeSDKMethod(callCtx, managed.session, "tools/call", map[string]any{
		"name": "ask_then_do", "arguments": map[string]any{},
	})
	if err != nil {
		return nil, err
	}
	var result mcpsdk.CallToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func TestMRTRFormElicitationRetriesWithUserAnswer(t *testing.T) {
	fixture := &mrtrFixtureServer{numInputs: 1}
	broker := &scriptedBroker{answer: mcpinteraction.Result{
		Action:  mcpinteraction.ActionAccept,
		Content: map[string]any{"answer": "from the user"},
	}}
	res, err := runMRTTool(t, broker, fixture)
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatal("no content on completed result")
	}
	if got := fixture.attempts; got != 2 {
		t.Fatalf("attempts = %d, want 2 (retry after elicitation)", got)
	}
	reqs := broker.requests()
	if len(reqs) != 1 {
		t.Fatalf("broker saw %d elicitations, want 1", len(reqs))
	}
	if reqs[0].Server != "mrtr" || reqs[0].Mode != "form" || reqs[0].Message != "question 1" {
		t.Fatalf("broker request = %+v", reqs[0])
	}
	var schema map[string]any
	if err := json.Unmarshal(reqs[0].RequestedSchema, &schema); err != nil {
		t.Fatalf("requested schema not valid JSON: %v", err)
	}
}

func TestMRTRDeclineAndCancelReachServer(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer mcpinteraction.Result
	}{
		{"decline", mcpinteraction.Result{Action: mcpinteraction.ActionDecline}},
		{"cancel", mcpinteraction.Result{Action: mcpinteraction.ActionCancel}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := &mrtrFixtureServer{numInputs: 1}
			res, err := runMRTTool(t, &scriptedBroker{answer: tc.answer}, fixture)
			if err != nil {
				t.Fatalf("tools/call: %v", err)
			}
			if fixture.attempts != 2 {
				t.Fatalf("attempts = %d, want 2: decline/cancel answers must still complete the round trip", fixture.attempts)
			}
			if res == nil {
				t.Fatal("nil result")
			}
		})
	}
}

func TestMRTRMultipleInputRequestsOneRound(t *testing.T) {
	fixture := &mrtrFixtureServer{numInputs: 3}
	broker := &scriptedBroker{answer: mcpinteraction.Result{
		Action:  mcpinteraction.ActionAccept,
		Content: map[string]any{"answer": "ok"},
	}}
	if _, err := runMRTTool(t, broker, fixture); err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if got := len(broker.requests()); got != 3 {
		t.Fatalf("broker saw %d elicitations, want 3", got)
	}
	if fixture.attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (one retry carries all three answers)", fixture.attempts)
	}
}

func TestMRTRBrokerTravelsWithCallContext(t *testing.T) {
	fixture := &mrtrFixtureServer{numInputs: 1}
	// No broker on the call ctx: the handler must cancel, not guess, and the
	// call still completes through the SDK's input-response path.
	res, err := runMRTTool(t, nil, fixture)
	if err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
}

func TestLegacy20251125PushElicitationUsesOnlyUnambiguousCall(t *testing.T) {
	transport := &sdkSessionTransport{name: "legacy-push"}
	broker := &scriptedBroker{answer: mcpinteraction.Result{
		Action:  mcpinteraction.ActionAccept,
		Content: map[string]any{"answer": "legacy"},
	}}
	callCtx := mcpinteraction.WithBroker(t.Context(), broker)
	unregister := transport.registerLegacyElicitationCall(callCtx, "2025-11-25", "tools/call")
	defer unregister()

	res, err := transport.handleElicitation(context.Background(), &mcpsdk.ElicitRequest{Params: &mcpsdk.ElicitParams{
		Message: "legacy form",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != mcpinteraction.ActionAccept || res.Content["answer"] != "legacy" {
		t.Fatalf("legacy response = %+v", res)
	}
	if got := len(broker.requests()); got != 1 {
		t.Fatalf("legacy broker requests = %d, want 1", got)
	}
}

func TestLegacy20251125PushElicitationEndToEnd(t *testing.T) {
	lifeCtx, cancelLife := context.WithCancel(context.Background())
	transport := &sdkSessionTransport{
		name: "legacy-push", spec: Spec{Name: "legacy-push", Type: "http"}, profile: HostProfileInteractive,
		lifeCtx: lifeCtx, cancel: cancelLife, state: SessionStateConnecting,
	}
	transport.endpointFactory = func(ctx context.Context) (sdkEndpoint, error) {
		clientSide, serverSide := mcpsdk.NewInMemoryTransports()
		go serveLegacyPushFixture(ctx, serverSide)
		return sdkEndpoint{transport: clientSide}, nil
	}
	t.Cleanup(transport.close)

	broker := &scriptedBroker{answer: mcpinteraction.Result{
		Action: mcpinteraction.ActionAccept, Content: map[string]any{"answer": "legacy accepted"},
	}}
	callCtx := mcpinteraction.WithBroker(t.Context(), broker)
	raw, err := transport.call(callCtx, "tools/call", map[string]any{"name": "legacy_ask", "arguments": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := transport.sessionDiagnostics().ProtocolVersion; got != "2025-11-25" {
		t.Fatalf("protocol = %q, want 2025-11-25", got)
	}
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "legacy accepted" || len(broker.requests()) != 1 {
		t.Fatalf("legacy push result = %s, broker requests = %d", raw, len(broker.requests()))
	}
}

func serveLegacyPushFixture(ctx context.Context, transport mcpsdk.Transport) {
	conn, err := transport.Connect(ctx)
	if err != nil {
		return
	}
	defer conn.Close()
	for {
		message, err := conn.Read(ctx)
		if err != nil {
			return
		}
		request, ok := message.(*mcpjsonrpc.Request)
		if !ok {
			continue
		}
		switch request.Method {
		case "server/discover":
			_ = writeLegacyFixtureMessage(ctx, conn, map[string]any{
				"jsonrpc": "2.0", "id": request.ID.Raw(),
				"error": map[string]any{"code": mcpjsonrpc.CodeMethodNotFound, "message": "method not found"},
			})
		case "initialize":
			_ = writeLegacyFixtureMessage(ctx, conn, map[string]any{
				"jsonrpc": "2.0", "id": request.ID.Raw(),
				"result": map[string]any{
					"protocolVersion": "2025-11-25",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "legacy-push", "version": "1"},
				},
			})
		case "tools/call":
			if err := writeLegacyFixtureMessage(ctx, conn, map[string]any{
				"jsonrpc": "2.0", "id": 700, "method": "elicitation/create",
				"params": map[string]any{
					"message": "legacy question",
					"requestedSchema": map[string]any{
						"type":       "object",
						"properties": map[string]any{"answer": map[string]any{"type": "string"}},
						"required":   []any{"answer"},
					},
				},
			}); err != nil {
				return
			}
			answerMessage, err := conn.Read(ctx)
			if err != nil {
				return
			}
			answer, ok := answerMessage.(*mcpjsonrpc.Response)
			if !ok || answer.Error != nil {
				return
			}
			var elicitation struct {
				Action  string         `json:"action"`
				Content map[string]any `json:"content"`
			}
			if json.Unmarshal(answer.Result, &elicitation) != nil {
				return
			}
			text, _ := elicitation.Content["answer"].(string)
			_ = writeLegacyFixtureMessage(ctx, conn, map[string]any{
				"jsonrpc": "2.0", "id": request.ID.Raw(),
				"result": map[string]any{
					"content": []any{map[string]any{"type": "text", "text": text}},
				},
			})
		}
	}
}

func writeLegacyFixtureMessage(ctx context.Context, conn mcpsdk.Connection, wire map[string]any) error {
	encoded, err := json.Marshal(wire)
	if err != nil {
		return err
	}
	message, err := mcpjsonrpc.DecodeMessage(encoded)
	if err != nil {
		return err
	}
	return conn.Write(ctx, message)
}

func TestLegacyPushElicitationCancelsAmbiguousConcurrentCalls(t *testing.T) {
	transport := &sdkSessionTransport{name: "legacy-concurrent"}
	first := &scriptedBroker{answer: mcpinteraction.Result{Action: mcpinteraction.ActionAccept}}
	second := &scriptedBroker{answer: mcpinteraction.Result{Action: mcpinteraction.ActionAccept}}
	unregisterFirst := transport.registerLegacyElicitationCall(mcpinteraction.WithBroker(t.Context(), first), "2025-11-25", "tools/call")
	defer unregisterFirst()
	unregisterSecond := transport.registerLegacyElicitationCall(mcpinteraction.WithBroker(t.Context(), second), "2025-11-25", "tools/call")
	defer unregisterSecond()

	res, err := transport.handleElicitation(context.Background(), &mcpsdk.ElicitRequest{Params: &mcpsdk.ElicitParams{Message: "ambiguous"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != mcpinteraction.ActionCancel {
		t.Fatalf("ambiguous action = %q, want cancel", res.Action)
	}
	if len(first.requests()) != 0 || len(second.requests()) != 0 {
		t.Fatal("ambiguous legacy elicitation crossed into a call broker")
	}
}

func TestURLModeElicitationReachesBrokerSafely(t *testing.T) {
	lifeCtx, cancelLife := context.WithCancel(context.Background())
	transport := &sdkSessionTransport{
		name: "url-mode", spec: Spec{Name: "url-mode", Type: "http"}, profile: HostProfileInteractive,
		lifeCtx: lifeCtx, cancel: cancelLife, state: SessionStateConnecting,
	}
	t.Cleanup(transport.close)
	broker := &scriptedBroker{answer: mcpinteraction.Result{Action: mcpinteraction.ActionAccept}}
	ctx := mcpinteraction.WithBroker(t.Context(), broker)

	res, err := transport.handleElicitation(ctx, &mcpsdk.ElicitRequest{Params: &mcpsdk.ElicitParams{
		Mode: "url", Message: "finish signup", URL: "https://auth.example.com/consent?state=xyz",
	}})
	if err != nil {
		t.Fatalf("handleElicitation: %v", err)
	}
	if res.Action != mcpinteraction.ActionAccept {
		t.Fatalf("action = %q", res.Action)
	}
	reqs := broker.requests()
	if len(reqs) != 1 || reqs[0].URL != "https://auth.example.com/consent?state=xyz" || reqs[0].ElicitationID != "" {
		t.Fatalf("broker request = %+v", reqs)
	}

	// Dangerous URLs are cancelled before any UI sees them.
	broker2 := &scriptedBroker{answer: mcpinteraction.Result{Action: mcpinteraction.ActionAccept}}
	ctx2 := mcpinteraction.WithBroker(t.Context(), broker2)
	res, err = transport.handleElicitation(ctx2, &mcpsdk.ElicitRequest{Params: &mcpsdk.ElicitParams{
		Mode: "url", Message: "bad", URL: "javascript:alert(1)",
	}})
	if err != nil {
		t.Fatalf("handleElicitation: %v", err)
	}
	if res.Action != mcpinteraction.ActionCancel {
		t.Fatalf("dangerous URL action = %q, want cancel", res.Action)
	}
	if len(broker2.requests()) != 0 {
		t.Fatal("dangerous URL reached the broker")
	}
}
