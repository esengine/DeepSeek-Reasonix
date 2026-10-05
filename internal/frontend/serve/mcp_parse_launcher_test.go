package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/mcpsetup"
	"reasonix/internal/session/control"
)

func TestMCPParseLauncherOptionsKeepProposedNames(t *testing.T) {
	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	for _, tc := range []struct {
		input string
		name  string
	}{
		{"docker run -p 8080:80 img", "img"},
		{"docker run localhost:5000/img:tag", "img"},
		{"docker run --cap-add SYS_ADMIN img", "mcp-server"},
		{"docker run img@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "img"},
		{"node --experimental-vm-modules server.js", "server"},
		{"node --max-old-space-size=4096 server.js", "server"},
		{"npx --unknown option-value pkg", "option-value"},
		{"python3 --unknown option-value server.py", "option-value"},
		{"node --unknown option-value server.js", "option-value"},
		{"uv --unknown option-value run pkg", "mcp-server"},
		{"uvx --unknown option-value pkg", "option-value"},
		{"pnpm --dir x dlx pkg", "pkg"},
		{"npm --prefix x exec -- pkg", "pkg"},
		{"podman run --rm -i mcp/time", "time"},
		{"nerdctl --namespace x run --rm -i mcp/fetch:latest", "fetch"},
		{`"C:\Program Files\nodejs\pnpm.CMD" --dir x dlx pkg`, "pkg"},
		{`C:\Go\bin\go.EXE run .\cmd\server`, "server"},
		{"go run .", "mcp-server"},
		{"docker run --unknown option-value img", "mcp-server"},
		{"pnpm --unknown option-value dlx pkg", "mcp-server"},
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
		argv := mcpsetup.Tokenize(tc.input)
		entry := got.Servers[0]
		if entry.Name != tc.name || entry.Transport != "stdio" || entry.Command != argv[0] || strings.Join(entry.Args, " ") != strings.Join(argv[1:], " ") {
			t.Errorf("preview %q = %+v, want name %q and original argv", tc.input, entry, tc.name)
		}
		if len(got.Risks) != 1 || got.Risks[0].Server != tc.name || got.Risks[0].Kind != "shell" || got.Risks[0].Detail != strings.Join(argv, " ") {
			t.Errorf("preview disclosure = %+v", got.Risks)
		}
	}
	if names := ctrl.ConfiguredMCPNames(); len(names) != 0 {
		t.Errorf("preview installed servers: %v", names)
	}
}
