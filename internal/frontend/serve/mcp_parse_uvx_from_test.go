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

func TestMCPParseUVXFromFlagKeepsCommandIdentity(t *testing.T) {
	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	for _, input := range []string{
		"uvx --from demo-distribution demo-command --stdio",
		"uvx --from=demo-distribution demo-command --stdio",
		"uvx --from demo-distribution==1.2.0 demo-command --stdio",
		"uvx --from=demo-distribution==1.2.0 demo-command --stdio",
	} {
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
		if e.Name != "demo-command" || e.Transport != "stdio" || e.Command+" "+strings.Join(e.Args, " ") != input {
			t.Errorf("preview %q = %+v, want command identity and original argv", input, e)
		}
		if len(got.Risks) != 1 || got.Risks[0].Server != "demo-command" || got.Risks[0].Kind != "shell" || got.Risks[0].Detail != input {
			t.Errorf("preview disclosure = %+v", got.Risks)
		}
	}
	if names := ctrl.ConfiguredMCPNames(); len(names) != 0 {
		t.Errorf("preview installed servers: %v", names)
	}
}
