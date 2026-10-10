package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func TestMCPParseURLSchemeCasePreservesRemotePreview(t *testing.T) {
	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	for _, scheme := range []string{"http", "HTTP", "HtTp", "https", "HTTPS", "hTtPs"} {
		endpoint := scheme + "://MCP.Example.com/CaseSensitive/MCP?api_key=fixture-key"
		resp := postJSON(t, srv.URL+"/mcp/parse", map[string]string{"input": "$ " + endpoint + " --header 'X-Client=KeepCase'"})
		var got struct {
			Servers []draftServer `json:"servers"`
			Risks   []draftRisk   `json:"risks"`
		}
		err := json.NewDecoder(resp.Body).Decode(&got)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || len(got.Servers) != 1 {
			t.Fatalf("preview %q: status=%d body=%+v err=%v", scheme, resp.StatusCode, got, err)
		}
		entry := got.Servers[0]
		if entry.Name != "mcp" || entry.Transport != "http" || entry.URL != endpoint || entry.Command != "" || entry.Headers["X-Client"] != "KeepCase" {
			t.Errorf("preview %q = %+v", scheme, entry)
		}
		if len(got.Risks) != 1 || got.Risks[0].Server != "mcp" || got.Risks[0].Kind != "unknown-host" || !strings.Contains(got.Risks[0].Detail, "/CaseSensitive/MCP") || strings.Contains(got.Risks[0].Detail, "fixture-key") {
			t.Errorf("preview disclosure = %+v", got.Risks)
		}
	}
	if names := ctrl.ConfiguredMCPNames(); len(names) != 0 {
		t.Errorf("preview installed servers: %v", names)
	}
}
