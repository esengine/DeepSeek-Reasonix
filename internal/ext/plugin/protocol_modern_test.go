package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
)

// modernServer is a strict 2026-07-28 Streamable HTTP server: it refuses
// initialize, session ids, and any request whose headers do not mirror its
// body, and it asks for the client's roots once before answering a call.
type modernServer struct {
	t         *testing.T
	supported []string
	mu        sync.Mutex
	methods   []string
	headers   []http.Header
	rounds    int
	flood     bool // asks for more inputs in one round than a client answers
}

func (m *modernServer) reject(w http.ResponseWriter, id any, code int, msg string, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg, "data": data}})
}

func (m *modernServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     any            `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	m.methods = append(m.methods, req.Method)
	m.headers = append(m.headers, r.Header.Clone())
	m.mu.Unlock()
	meta, _ := req.Params["_meta"].(map[string]any)
	version, _ := meta[metaProtocolVersion].(string)
	switch {
	case req.Method == "initialize" || version == "":
		http.Error(w, "this server speaks 2026-07-28 only", http.StatusBadRequest)
		return
	case r.Header.Get("Mcp-Session-Id") != "":
		http.Error(w, "sessions do not exist", http.StatusBadRequest)
		return
	case r.Header.Get("MCP-Protocol-Version") != version || r.Header.Get("Mcp-Method") != req.Method:
		m.reject(w, req.ID, codeHeaderMismatch, "header mismatch", nil)
		return
	case meta[metaClientCapabilities] == nil:
		m.reject(w, req.ID, codeMissingClientCapability, "capabilities required", nil)
		return
	case !slices.Contains(m.supported, version):
		m.reject(w, req.ID, codeUnsupportedProtocolVersion, "Unsupported protocol version", map[string]any{"supported": m.supported, "requested": version})
		return
	}
	var result map[string]any
	switch req.Method {
	case discoverMethod:
		result = map[string]any{"supportedVersions": m.supported, "capabilities": map[string]any{"tools": map[string]any{}}, "instructions": "modern test server"}
	case "tools/list":
		result = map[string]any{"ttlMs": 0, "cacheScope": "private", "tools": []map[string]any{
			{"name": "query", "description": "Run a query.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"region": map[string]any{"type": "string", "x-mcp-header": "Region"},
				"sql":    map[string]any{"type": "string"},
			}}},
			{"name": "broken", "description": "Annotates an array item.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{
				"ids": map[string]any{"type": "array", "items": map[string]any{"type": "string", "x-mcp-header": "Id"}},
			}}},
			{"name": "form", "description": "Needs a person.", "inputSchema": map[string]any{"type": "object"}},
		}}
	case "tools/call":
		if r.Header.Get("Mcp-Name") != encodeHeaderValue(req.Params["name"].(string)) {
			m.reject(w, req.ID, codeHeaderMismatch, "Mcp-Name mismatch", nil)
			return
		}
		m.mu.Lock()
		m.rounds++
		m.mu.Unlock()
		if req.Params["name"] == "form" {
			if answers, ok := req.Params["inputResponses"].(map[string]any); ok {
				who, _ := json.Marshal(answers["who"])
				result = map[string]any{"resultType": "complete", "content": []map[string]any{{"type": "text", "text": string(who)}}}
				break
			}
			result = map[string]any{"resultType": "input_required", "inputRequests": map[string]any{
				"who": map[string]any{"method": "elicitation/create", "params": map[string]any{"mode": "form", "message": "Name?",
					"requestedSchema": map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}}}},
			}}
			break
		}
		if m.flood {
			asks := map[string]any{}
			for i := range maxInputRequests + 1 {
				asks[fmt.Sprintf("r%d", i)] = map[string]any{"method": "roots/list", "params": map[string]any{}}
			}
			result = map[string]any{"resultType": "input_required", "inputRequests": asks}
			break
		}
		responses, answered := req.Params["inputResponses"].(map[string]any)
		if !answered {
			result = map[string]any{"resultType": "input_required", "requestState": "opaque-1", "inputRequests": map[string]any{
				"roots": map[string]any{"method": "roots/list", "params": map[string]any{}},
			}}
			break
		}
		if req.Params["requestState"] != "opaque-1" || responses["roots"] == nil {
			m.reject(w, req.ID, -32602, "retry lost its state", nil)
			return
		}
		args, _ := req.Params["arguments"].(map[string]any)
		result = map[string]any{"resultType": "complete", "content": []map[string]any{{"type": "text", "text": "ran in " + args["region"].(string)}}}
	default:
		m.reject(w, req.ID, -32601, "Method not found", nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}

// A modern server is reached without a handshake: discovery, then requests
// that each carry their own _meta and the headers mirroring it; a tool whose
// header annotation breaks the rules is left out; a call the server needs the
// workspace roots for is answered and retried with its state.
func TestModernHTTPServerIsReachedWithoutAHandshake(t *testing.T) {
	m := &modernServer{t: t, supported: []string{modernProtocolVersion}}
	srv := httptest.NewServer(m)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host, tools, err := StartAll(ctx, []Spec{{Name: "m", Type: "http", URL: srv.URL, WorkspaceRoot: t.TempDir()}})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer host.Close()
	var query, form *remoteTool
	for _, tl := range tools {
		switch tl.Name() {
		case "mcp__m__query":
			query = tl.(*remoteTool)
		case "mcp__m__form":
			form = tl.(*remoteTool)
		case "mcp__m__broken":
			t.Fatal("a tool with an x-mcp-header on an array item was offered")
		}
	}
	if query == nil || form == nil {
		t.Fatalf("tools = %d, want query and form", len(tools))
	}
	out, err := query.Execute(ctx, json.RawMessage(`{"region":"亚太-1","sql":"select 1"}`))
	if err != nil || out != "ran in 亚太-1" {
		t.Fatalf("Execute = %q, %v", out, err)
	}
	m.mu.Lock()
	methods, headers := append([]string(nil), m.methods...), m.headers
	m.mu.Unlock()
	if methods[0] != discoverMethod || slices.Contains(methods, "initialize") {
		t.Fatalf("methods = %v, want discovery first and no initialize", methods)
	}
	last := headers[len(headers)-1]
	if got := last.Get("Mcp-Param-Region"); got != encodeHeaderValue("亚太-1") || !strings.HasPrefix(got, "=?base64?") {
		t.Fatalf("Mcp-Param-Region = %q, want the base64 form of a non-ASCII value", got)
	}
	if out, err := form.Execute(ctx, json.RawMessage(`{}`)); err != nil || out != `{"action":"decline"}` {
		t.Fatalf("a form with nobody to ask = %q, %v; want declined", out, err)
	}
	person := &fakeElicitor{replies: []tool.ElicitReply{{Values: map[string][]string{"name": {"Ada"}}}}}
	if out, err := form.Execute(tool.WithElicitor(ctx, person), json.RawMessage(`{}`)); err != nil || out != `{"action":"accept","content":{"name":"Ada"}}` {
		t.Fatalf("a form answered = %q, %v", out, err)
	}
}

// A modern server that shares no revision with this client is refused by
// name; one that still serves a legacy revision is spoken to in that one.
func TestModernServerVersionSelection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	future := httptest.NewServer(&modernServer{t: t, supported: []string{"2099-01-01"}})
	defer future.Close()
	if _, _, err := StartAll(ctx, []Spec{{Name: "f", Type: "http", URL: future.URL}}); !errors.Is(err, ErrUnsupportedProtocolVersion) {
		t.Fatalf("err = %v, want ErrUnsupportedProtocolVersion", err)
	}
	if v, _, err := pickEra([]string{"2099-01-01", "2025-11-25"}, nil); v != "" || err != nil {
		t.Fatalf("a server also serving 2025-11-25 got (%q, %v), want the legacy fallback", v, err)
	}
}

func TestParamHeaderRulesAndEncoding(t *testing.T) {
	c := &Client{transport: "http", modern: modernSession{version: modernProtocolVersion}}
	for _, bad := range []string{
		`{"type":"object","properties":{"n":{"type":"number","x-mcp-header":"N"}}}`,
		`{"type":"object","properties":{"a":{"type":"string","x-mcp-header":"X"},"b":{"type":"string","x-mcp-header":"x"}}}`,
		`{"type":"object","properties":{"a":{"type":"string","x-mcp-header":"Bad Name"}}}`,
		`{"type":"object","oneOf":[{"properties":{"a":{"type":"string","x-mcp-header":"A"}}}]}`,
	} {
		if _, err := c.toolParamHeaders(json.RawMessage(bad)); !errors.Is(err, errBadParamHeader) {
			t.Fatalf("%s accepted", bad)
		}
	}
	nested, err := c.toolParamHeaders(json.RawMessage(`{"type":"object","properties":{"o":{"type":"object","properties":{"id":{"type":"integer","x-mcp-header":"Id"}}}}}`))
	if err != nil || len(nested) != 1 || strings.Join(nested[0].path, ".") != "o.id" {
		t.Fatalf("nested = %+v, %v", nested, err)
	}
	stdio := &Client{transport: "stdio", modern: modernSession{version: modernProtocolVersion}}
	if h, err := stdio.toolParamHeaders(json.RawMessage(`{"properties":{"a":{"type":"number","x-mcp-header":"A"}}}`)); h != nil || err != nil {
		t.Fatal("a stdio connection judged header annotations it never sends")
	}
	for in, want := range map[string]string{
		"us-west1":           "us-west1",
		"Hello, 世界":          "=?base64?SGVsbG8sIOS4lueVjA==?=",
		" padded ":           "=?base64?IHBhZGRlZCA=?=",
		"line1\nline2":       "=?base64?bGluZTEKbGluZTI=?=",
		"=?base64?literal?=": "=?base64?PT9iYXNlNjQ/bGl0ZXJhbD89?=",
	} {
		if got := encodeHeaderValue(in); got != want {
			t.Fatalf("encode(%q) = %q, want %q", in, got, want)
		}
	}
	for v, ok := range map[any]bool{1.5: false, float64(42): true, true: true, nil: false, 9007199254740993.0: false} {
		if _, got := headerScalar(v); got != ok {
			t.Fatalf("headerScalar(%v) ok = %v", v, got)
		}
	}
}

// Over stdio the probe is what tells eras apart: a modern child answers
// discovery and is never sent initialize, and a legacy child (the default
// helper) answers discovery with nothing useful and gets the handshake.
func TestStdioProbeChoosesTheServersEra(t *testing.T) {
	for _, modern := range []bool{true, false} {
		env := map[string]string{"GO_WANT_HELPER_PROCESS": "1"}
		if modern {
			env["GO_WANT_HELPER_MODERN"] = "1"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		host, tools, err := StartAll(ctx, []Spec{{Name: "mock", Command: os.Args[0], Args: []string{"-test.run=TestHelperProcess", "--"}, Env: env}})
		if err != nil {
			cancel()
			t.Fatalf("modern=%v: StartAll: %v", modern, err)
		}
		echo := findToolByName(tools, "mcp__mock__echo")
		out, err := echo.Execute(ctx, json.RawMessage(`{"msg":"hi"}`))
		host.Close()
		cancel()
		if err != nil || out != "echo: hi" {
			t.Fatalf("modern=%v: Execute = %q, %v", modern, out, err)
		}
	}
}

// A legacy server may answer the pre-handshake probe with a code the modern
// revision also uses. Without a list of revisions that is not a modern answer,
// and the server still gets its handshake.
func TestLegacyServerUsingAModernCodeStillGetsTheHandshake(t *testing.T) {
	for _, reply := range []map[string]any{
		{"code": codeMissingClientCapability, "message": "unknown method"},
		{"code": codeUnsupportedProtocolVersion, "message": "no"},
		{"code": codeHeaderMismatch, "message": "no", "data": map[string]any{"supported": "garbage"}},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				ID     any    `json:"id"`
				Method string `json:"method"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			w.Header().Set("Content-Type", "application/json")
			var body map[string]any
			switch req.Method {
			case discoverMethod:
				body = map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": reply}
			case "initialize":
				body = map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{
					"protocolVersion": "2025-11-25", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "legacy"}}}
			case "tools/list":
				body = map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"tools": []map[string]any{
					{"name": "echo", "inputSchema": map[string]any{"type": "object"}}}}}
			default:
				w.WriteHeader(http.StatusAccepted)
				return
			}
			_ = json.NewEncoder(w).Encode(body)
		}))
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		host, tools, err := StartAll(ctx, []Spec{{Name: "l", Type: "http", URL: srv.URL}})
		cancel()
		srv.Close()
		if err != nil || len(tools) != 1 {
			t.Fatalf("probe reply %v: StartAll = %d tools, %v; want the legacy handshake", reply, len(tools), err)
		}
		host.Close()
	}
}

func TestModernInputRoundIsBounded(t *testing.T) {
	m := &modernServer{t: t, supported: []string{modernProtocolVersion}, flood: true}
	srv := httptest.NewServer(m)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host, tools, err := StartAll(ctx, []Spec{{Name: "m", Type: "http", URL: srv.URL, WorkspaceRoot: t.TempDir()}})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer host.Close()
	query := findToolByName(tools, "mcp__m__query")
	if _, err := query.Execute(ctx, json.RawMessage(`{"region":"x"}`)); !errors.Is(err, errMCPInputOverBounds) {
		t.Fatalf("err = %v, want the input round refused", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rounds != 1 {
		t.Fatalf("rounds = %d, want the flood refused before answering it", m.rounds)
	}
}
