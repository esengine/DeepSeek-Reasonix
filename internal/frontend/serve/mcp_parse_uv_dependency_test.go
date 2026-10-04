package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func TestMCPParseUVNameSkipsDependencyValues(t *testing.T) {
	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	for _, tc := range []struct {
		input   string
		command string
		args    []string
	}{
		{"uvx --with extra-package demo-server", "uvx", []string{"--with", "extra-package", "demo-server"}},
		{"uvx --with=extra-package demo-server", "uvx", []string{"--with=extra-package", "demo-server"}},
		{"uvx --from main-distribution --with extra-package demo-server", "uvx", []string{"--from", "main-distribution", "--with", "extra-package", "demo-server"}},
		{"uvx -w extra-package demo-server", "uvx", []string{"-w", "extra-package", "demo-server"}},
		{"uvx --with-requirements './extra requirements.txt' demo-server", "uvx", []string{"--with-requirements", "./extra requirements.txt", "demo-server"}},
		{"uv run --with demo-distribution demo-server", "uv", []string{"run", "--with", "demo-distribution", "demo-server"}},
		{"uv run --with=demo-distribution demo-server", "uv", []string{"run", "--with=demo-distribution", "demo-server"}},
		{"uv run --with-requirements ./requirements.txt demo-server", "uv", []string{"run", "--with-requirements", "./requirements.txt", "demo-server"}},
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
		if got.Servers[0].Name != "demo-server" || got.Servers[0].Transport != "stdio" || got.Servers[0].Command != tc.command || !reflect.DeepEqual(got.Servers[0].Args, tc.args) {
			t.Errorf("preview %q = %+v", tc.input, got.Servers[0])
		}
		if len(got.Risks) != 1 || got.Risks[0].Server != "demo-server" || got.Risks[0].Kind != "shell" || got.Risks[0].Detail != tc.command+" "+strings.Join(tc.args, " ") {
			t.Errorf("preview disclosure = %+v", got.Risks)
		}
	}
	if names := ctrl.ConfiguredMCPNames(); len(names) != 0 {
		t.Errorf("preview installed servers: %v", names)
	}
}
