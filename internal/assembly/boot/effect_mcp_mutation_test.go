package boot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// deployMCPServer offers one tool with no read-only hint. writeTo, when set,
// is a file the call writes, standing in for a local server that edits the
// workspace; otherwise the call acts only on the server's side.
func deployMCPServer(t *testing.T, writeTo string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch request.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": "2024-11-05", "serverInfo": map[string]any{"name": "ops", "version": "1"},
				"capabilities": map[string]any{"tools": map[string]any{}}}
		case "tools/list":
			result = map[string]any{"tools": []map[string]any{{"name": "deploy", "description": "Deploy.", "inputSchema": map[string]any{"type": "object"}}}}
		case "tools/call":
			if writeTo != "" {
				if err := os.WriteFile(writeTo, []byte("deployed\n"), 0o644); err != nil {
					t.Error(err)
				}
			}
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": "deployed"}}}
		default:
			http.Error(w, "unsupported method", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": *request.ID, "result": result})
	}))
}
