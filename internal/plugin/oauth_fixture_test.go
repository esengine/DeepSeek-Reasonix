package plugin

import (
	"encoding/json"
	"net/http"
)

func writeOAuthMCPFixtureResponse(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	case http.MethodDelete:
		w.WriteHeader(http.StatusOK)
		return
	}
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "bad MCP request", http.StatusBadRequest)
		return
	}
	if len(request.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
	switch request.Method {
	case "server/discover":
		response["error"] = map[string]any{"code": -32601, "message": "Method not found"}
	case "initialize":
		response["result"] = map[string]any{
			"protocolVersion": testLegacyProtocolVersion,
			"serverInfo":      map[string]any{"name": "oauth-fixture", "version": "1"},
			"capabilities":    map[string]any{},
		}
	case "ping":
		response["result"] = map[string]any{}
	default:
		response["error"] = map[string]any{"code": -32601, "message": "Method not found"}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}
