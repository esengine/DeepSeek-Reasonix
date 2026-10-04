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

func TestMCPParseKeepsPackageNameWithMixedCaseRunnerExtension(t *testing.T) {
	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	for _, input := range []string{"npx.CMD -y @example/demo-server@1.0.0", "python.EXE -m demo-server", "uv.Bat run demo-server"} {
		resp := postJSON(t, srv.URL+"/mcp/parse", map[string]string{"input": input})
		var got struct {
			Servers []draftServer `json:"servers"`
			Risks   []draftRisk   `json:"risks"`
		}
		err := json.NewDecoder(resp.Body).Decode(&got)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK || len(got.Servers) != 1 {
			t.Fatalf("preview %q: status=%d body=%+v err=%v", input, resp.StatusCode, got, err)
		}
		e := got.Servers[0]
		if e.Name != "demo-server" || e.Transport != "stdio" || e.Command+" "+strings.Join(e.Args, " ") != input {
			t.Errorf("preview %q = %+v, want package name and original argv", input, e)
		}
		if len(got.Risks) != 1 || got.Risks[0].Server != "demo-server" || got.Risks[0].Kind != "shell" || got.Risks[0].Detail != input {
			t.Errorf("preview %q disclosure = %+v", input, got.Risks)
		}
	}
	if names := ctrl.ConfiguredMCPNames(); len(names) != 0 {
		t.Errorf("parse installed servers: %v", names)
	}
}
