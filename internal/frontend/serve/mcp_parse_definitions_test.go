package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func TestMCPParseOmitsEmptyServerDefinitions(t *testing.T) {
	home, workspace := testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	ctrl := control.New(control.Options{WorkspaceRoot: workspace})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	for _, wrapped := range []bool{false, true} {
		for _, spec := range []string{`{}`, `null`, `{"type":"http"}`, `{"env":{"LABEL":"example"}}`} {
			input := `{"empty":` + spec + `}`
			if wrapped {
				input = `{"mcpServers":` + input + `}`
			}
			resp := postJSON(t, srv.URL+"/mcp/parse", map[string]string{"input": input})
			var body struct {
				Code string `json:"code"`
			}
			err := json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != http.StatusBadRequest || body.Code != "mcp.bad_declaration" {
				t.Errorf("parse %s: status=%d code=%s err=%v; want 400 mcp.bad_declaration", input, resp.StatusCode, body.Code, err)
			}
		}
		input := `{"empty":{},"local":{"command":"example-mcp"},"remote":{"url":"https://example.com/mcp"}}`
		if wrapped {
			input = `{"mcpServers":` + input + `}`
		}
		resp := postJSON(t, srv.URL+"/mcp/parse", map[string]string{"input": input})
		var body struct {
			Servers []draftServer `json:"servers"`
			Risks   []draftRisk   `json:"risks"`
		}
		err := json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("mixed definitions: status=%d err=%v", resp.StatusCode, err)
		}
		if len(body.Servers) != 2 || body.Servers[0].Name != "local" || body.Servers[0].Transport != "stdio" || body.Servers[0].Command != "example-mcp" || body.Servers[1].Name != "remote" || body.Servers[1].Transport != "http" || body.Servers[1].URL != "https://example.com/mcp" {
			t.Errorf("mixed preview = %+v, want only local and remote", body.Servers)
		}
		if len(body.Risks) != 2 || body.Risks[0].Server != "local" || body.Risks[0].Kind != "shell" || body.Risks[1].Server != "remote" || body.Risks[1].Kind != "unknown-host" {
			t.Errorf("mixed preview risks = %+v", body.Risks)
		}
	}
	if names := ctrl.ConfiguredMCPNames(); len(names) != 0 {
		t.Errorf("preview installed servers: %v", names)
	}
	for _, root := range []string{home, workspace} {
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 0 {
			t.Errorf("preview wrote to %s: %v, err=%v", root, entries, err)
		}
	}
}
