package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// pagedMCPServer serves tools/list and prompts/list in pages keyed by cursor;
// next maps each cursor ("" is the first page) to the one the page hands out.
func pagedMCPServer(t *testing.T, next map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
			Params struct {
				Cursor string `json:"cursor"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		page := func(field string, item map[string]any) map[string]any {
			out := map[string]any{field: []map[string]any{item}}
			if c := next[req.Params.Cursor]; c != "" {
				out["nextCursor"] = c
			}
			return out
		}
		suffix := strings.TrimPrefix(req.Params.Cursor, "c")
		if suffix == "" {
			suffix = "0"
		}
		switch req.Method {
		case "initialize":
			writeHTTPRPCResult(w, req.ID, map[string]any{
				"protocolVersion": "2025-06-18",
				"serverInfo":      map[string]any{"name": "p", "version": "0"},
				"capabilities":    map[string]any{"tools": map[string]any{}, "prompts": map[string]any{}},
			})
		case "tools/list":
			writeHTTPRPCResult(w, req.ID, page("tools", map[string]any{
				"name": "t" + suffix, "inputSchema": map[string]any{"type": "object"},
			}))
		case "prompts/list":
			writeHTTPRPCResult(w, req.ID, page("prompts", map[string]any{"name": "p" + suffix}))
		default:
			writeHTTPRPCResult(w, req.ID, map[string]any{})
		}
	}))
}

func TestListFollowsCursorsAcrossPages(t *testing.T) {
	srv := pagedMCPServer(t, map[string]string{"": "c1", "c1": "c2"})
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	host, tools, err := StartAll(ctx, []Spec{{Name: "p", Type: "http", URL: srv.URL}})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer host.Close()

	if got := strings.Join(names(tools), ","); got != "mcp__p__t0,mcp__p__t1,mcp__p__t2" {
		t.Fatalf("tools = %s, want every page's tool", got)
	}
	listed, err := host.clients[0].listPrompts(ctx)
	if err != nil {
		t.Fatalf("listPrompts: %v", err)
	}
	var prompts []string
	for _, p := range listed {
		prompts = append(prompts, p.Raw)
	}
	if got := strings.Join(prompts, ","); got != "p0,p1,p2" {
		t.Fatalf("prompts = %s, want every page's prompt", got)
	}
}

func TestListRefusesARepeatedCursor(t *testing.T) {
	srv := pagedMCPServer(t, map[string]string{"": "c1", "c1": "c1"})
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	host, _, err := StartAll(ctx, []Spec{{Name: "p", Type: "http", URL: srv.URL}})
	if host != nil {
		defer host.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("StartAll err = %v, want the cursor loop reported", err)
	}
}
