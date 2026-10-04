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

func TestMCPParsePNPMDlxKeepsDistinctServerNames(t *testing.T) {
	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	for _, tc := range []struct {
		input string
		name  string
	}{
		{"pnpm dlx @example/docs-mcp@1.2.0 --port 3456", "docs-mcp"},
		{"pnpm dlx search-mcp@latest", "search-mcp"},
		{"pnpm --package @example/client dlx docs-mcp", "docs-mcp"},
		{"pnpm --reporter append-only dlx --allow-build @example/client docs-mcp", "docs-mcp"},
		{"pnpm dlx -c docs-mcp", "docs-mcp"},
	} {
		resp := postJSON(t, srv.URL+"/mcp/parse", map[string]string{"input": tc.input})
		var got struct {
			Servers []draftServer `json:"servers"`
			Risks   []draftRisk   `json:"risks"`
		}
		err := json.NewDecoder(resp.Body).Decode(&got)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || len(got.Servers) != 1 {
			t.Fatalf("preview %q: status=%d body=%+v err=%v", tc.input, resp.StatusCode, got, err)
		}
		e := got.Servers[0]
		if e.Name != tc.name || e.Transport != "stdio" || e.Command+" "+strings.Join(e.Args, " ") != tc.input {
			t.Errorf("preview %q = %+v, want name %q and original argv", tc.input, e, tc.name)
		}
		if len(got.Risks) != 1 || got.Risks[0].Server != tc.name || got.Risks[0].Kind != "shell" || got.Risks[0].Detail != tc.input {
			t.Errorf("preview disclosure = %+v", got.Risks)
		}
	}
	if names := ctrl.ConfiguredMCPNames(); len(names) != 0 {
		t.Errorf("preview installed servers: %v", names)
	}
}
