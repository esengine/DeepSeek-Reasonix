package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func TestMCPParsePreviewsContinuedSetupCommands(t *testing.T) {
	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	for _, tc := range []struct {
		name   string
		input  string
		server draftServer
		risk   draftRisk
	}{
		{
			name:  "README launch command",
			input: "$ npx -y \\\n  @reasonix/fixture-server@1.2.3 \\\n  --mode read",
			server: draftServer{
				Name: "fixture-server", Transport: "stdio", Command: "npx",
				Args:           []string{"-y", "@reasonix/fixture-server@1.2.3", "--mode", "read"},
				DisplayCommand: "npx", DisplayArgs: []string{"-y", "@reasonix/fixture-server@1.2.3", "--mode", "read"},
			},
			risk: draftRisk{Server: "fixture-server", Kind: "shell", Field: "command", Detail: "npx -y @reasonix/fixture-server@1.2.3 --mode read"},
		},
		{
			name:  "copied CLI endpoint",
			input: "reasonix mcp add docs \\\n  --http https://mcp.example.test/endpoint \\\n  --header \"X-Label=word\\\nwrap\"",
			server: draftServer{
				Name: "docs", Transport: "http", URL: "https://mcp.example.test/endpoint",
				Headers:    map[string]string{"X-Label": "wordwrap"},
				DisplayURL: "https://mcp.example.test/endpoint", DisplayHeaders: map[string]string{"X-Label": "wordwrap"},
			},
			risk: draftRisk{Server: "docs", Kind: "unknown-host", Field: "url", Detail: "https://mcp.example.test/endpoint"},
		},
		{
			name:  "literal single quoted continuation",
			input: "server 'first\\\nsecond'",
			server: draftServer{
				Name: "server", Transport: "stdio", Command: "server", Args: []string{"first\\\nsecond"},
				DisplayCommand: "server", DisplayArgs: []string{"first\\\nsecond"},
			},
			risk: draftRisk{Server: "server", Kind: "shell", Field: "command", Detail: "server first\\\nsecond"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := postJSON(t, srv.URL+"/mcp/parse", map[string]string{"input": tc.input})
			defer resp.Body.Close()
			var got struct {
				Servers []draftServer `json:"servers"`
				Risks   []draftRisk   `json:"risks"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK || !reflect.DeepEqual(got.Servers, []draftServer{tc.server}) || !reflect.DeepEqual(got.Risks, []draftRisk{tc.risk}) {
				t.Errorf("preview status=%d, servers=%+v, risks=%+v; want server=%+v, risk=%+v", resp.StatusCode, got.Servers, got.Risks, tc.server, tc.risk)
			}
		})
	}
	if names := ctrl.ConfiguredMCPNames(); len(names) != 0 {
		t.Errorf("preview installed servers: %v", names)
	}
}
