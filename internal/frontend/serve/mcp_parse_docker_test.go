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

func TestMCPParseDockerNameUsesImageOperand(t *testing.T) {
	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	for _, tc := range []struct {
		input  string
		name   string
		detail string
	}{
		{"docker run --rm -i mcp/time", "time", ""},
		{"docker run --rm -i mcp/fetch:latest", "fetch", ""},
		{"docker run -e MODE=test -v /data:/data mcp/filesystem /data", "filesystem", "docker run -e <redacted> -v /data:/data mcp/filesystem /data"},
		{"docker run --network none registry.example:5000/tools/time:stable", "time", ""},
		{"docker container run --rm -i mcp/time@sha256:" + strings.Repeat("0123456789abcdef", 4), "time", ""},
		{"docker run --unknown option-value mcp/time", "mcp-server", ""},
		{"docker version", "mcp-server", ""},
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
		if entry.Name != tc.name || entry.Transport != "stdio" || entry.Command+" "+strings.Join(entry.Args, " ") != tc.input {
			t.Errorf("preview %q = %+v, want name %q and original argv", tc.input, entry, tc.name)
		}
		detail := tc.detail
		if detail == "" {
			detail = tc.input
		}
		if len(got.Risks) != 1 || got.Risks[0].Server != tc.name || got.Risks[0].Kind != "shell" || got.Risks[0].Detail != detail {
			t.Errorf("preview disclosure = %+v", got.Risks)
		}
	}
	if names := ctrl.ConfiguredMCPNames(); len(names) != 0 {
		t.Errorf("preview installed servers: %v", names)
	}
}
