package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/tool"
)

func resourceMCPServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if request.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]any{"name": "docs", "version": "1"},
				"capabilities":    map[string]any{"resources": map[string]any{}, "tools": map[string]any{}},
				"instructions":    "Prefer doc:// URIs.",
			}
		case "tools/list":
			result = map[string]any{"tools": []any{}}
		case "resources/list":
			var params struct {
				Cursor string `json:"cursor"`
			}
			_ = json.Unmarshal(request.Params, &params)
			if params.Cursor == "next" {
				result = map[string]any{"resources": []any{map[string]any{"uri": "doc://b", "name": "B"}}}
				break
			}
			result = map[string]any{
				"resources":  []any{map[string]any{"uri": "doc://a", "name": "A"}},
				"nextCursor": "next",
			}
		case "resources/templates/list":
			result = map[string]any{"resourceTemplates": []any{map[string]any{
				"uriTemplate": "doc://{slug}", "name": "Doc",
			}}}
		case "resources/read":
			var params struct {
				URI string `json:"uri"`
			}
			_ = json.Unmarshal(request.Params, &params)
			if params.URI != "doc://a" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"error":{"code":-32002,"message":"missing"}}`, *request.ID)
				return
			}
			result = map[string]any{"contents": []any{map[string]any{
				"uri": "doc://a", "mimeType": "text/plain", "text": "hello",
				"blob": "QUJD",
			}}}
		default:
			result = map[string]any{}
		}
		response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": result})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response)
	}))
}

func TestMCPResourceToolsListTemplatesReadAndRedact(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	srv := resourceMCPServer(t)
	defer srv.Close()
	ctx := context.Background()
	host := NewHost()
	defer host.Close()
	if _, err := host.Add(ctx, Spec{Name: "docs", Type: "http", URL: srv.URL, Authorized: true}); err != nil {
		t.Fatalf("Host.Add: %v", err)
	}

	byName := map[string]tool.Tool{}
	for _, candidate := range NewMCPResourceTools(host) {
		byName[candidate.Name()] = candidate
		if !candidate.ReadOnly() {
			t.Fatalf("%s must be read-only", candidate.Name())
		}
	}
	listed, err := byName[ListMCPResourcesName].Execute(ctx, json.RawMessage(`{"server":"docs"}`))
	if err != nil || !strings.Contains(listed, `"uri":"doc://a"`) {
		t.Fatalf("list = %q err=%v", listed, err)
	}
	page, err := byName[ListMCPResourcesName].Execute(ctx, json.RawMessage(`{"server":"docs","cursor":"next"}`))
	if err != nil || !strings.Contains(page, `"uri":"doc://b"`) {
		t.Fatalf("cursor list = %q err=%v", page, err)
	}
	templates, err := byName[ListMCPResourceTemplatesName].Execute(ctx, json.RawMessage(`{"server":"docs"}`))
	if err != nil || !strings.Contains(templates, `"uriTemplate":"doc://{slug}"`) {
		t.Fatalf("templates = %q err=%v", templates, err)
	}
	body, err := byName[ReadMCPResourceName].Execute(ctx, json.RawMessage(`{"server":"docs","uri":"doc://a"}`))
	if err != nil || !strings.Contains(body, "hello") || strings.Contains(body, `"blob":"QUJD"`) {
		t.Fatalf("read = %q err=%v", body, err)
	}
	if !strings.Contains(body, "4 base64 characters") {
		t.Fatalf("read missing blob redaction: %q", body)
	}
	if _, err := byName[ReadMCPResourceName].Execute(ctx, json.RawMessage(`{"server":"missing","uri":"doc://a"}`)); err == nil || !strings.Contains(err.Error(), `no MCP server named "missing"`) {
		t.Fatalf("missing server err = %v", err)
	}
	guide := host.ServerGuide()
	if !strings.Contains(guide, `["docs"]`) || !strings.Contains(guide, "Prefer doc:// URIs.") {
		t.Fatalf("server guide = %q", guide)
	}
}

func TestMCPResourceToolsRequireServer(t *testing.T) {
	host := NewHost()
	defer host.Close()
	tl := NewMCPResourceTools(host)[0]
	if _, err := tl.Execute(context.Background(), json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "server is required") {
		t.Fatalf("empty server err = %v", err)
	}
}
