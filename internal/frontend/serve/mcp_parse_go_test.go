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

func TestMCPParseGoNameUsesRunPackage(t *testing.T) {
	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	for _, tc := range []struct {
		input string
		name  string
	}{
		{"go run example.com/tools/reasonix-server@latest --stdio", "reasonix-server"},
		{"go run -tags=integration example.com/tools/reasonix-server@v1.2.3 --stdio", "reasonix-server"},
		{"go run -tags integration example.com/tools/reasonix-server@v1.2.3 --stdio", "reasonix-server"},
		{"go -C workspace run ./cmd/reasonix-server client-mode", "reasonix-server"},
		{"/usr/local/go/bin/go run ./cmd/reasonix-server client-mode", "reasonix-server"},
		{"go run -tags integration", "mcp-server"},
		{"go test ./cmd/reasonix-server", "mcp-server"},
		{"reasonix mcp add manual -- go run ./cmd/reasonix-server", "manual"},
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
		entry := got.Servers[0]
		command := strings.TrimPrefix(tc.input, "reasonix mcp add manual -- ")
		if entry.Name != tc.name || entry.Transport != "stdio" || entry.Command+" "+strings.Join(entry.Args, " ") != command {
			t.Errorf("preview %q = %+v, want name %q and original argv", tc.input, entry, tc.name)
		}
		if len(got.Risks) != 1 || got.Risks[0].Server != tc.name || got.Risks[0].Kind != "shell" || got.Risks[0].Detail != command {
			t.Errorf("preview disclosure = %+v", got.Risks)
		}
	}
	if names := ctrl.ConfiguredMCPNames(); len(names) != 0 {
		t.Errorf("preview installed servers: %v", names)
	}
}
