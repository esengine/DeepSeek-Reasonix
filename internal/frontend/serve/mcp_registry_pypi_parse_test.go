package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/mcpregistry"
	"reasonix/internal/session/control"
)

func TestMCPParseKeepsRegistryPyPIPackageIdentity(t *testing.T) {
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0.1/servers" || r.URL.Query().Get("version") != "latest" {
			t.Errorf("registry request = %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"servers":[{"server":{"name":"io.example/demo-server","version":"1.2.0","packages":[{"registryType":"pypi","identifier":"demo-server","version":"1.2.0","transport":{"type":"stdio"}}]}}]}`))
	}))
	t.Cleanup(registry.Close)
	client := mcpregistry.New("")
	client.BaseURL = registry.URL
	entry, _, err := client.Resolve(t.Context(), "io.example/demo-server")
	if err != nil {
		t.Fatal(err)
	}
	configured, err := entry.PluginEntry("")
	if err != nil || configured.Command != "uvx" || !reflect.DeepEqual(configured.Args, []string{"demo-server==1.2.0"}) {
		t.Fatalf("registry configuration = %+v, %v", configured, err)
	}
	input := configured.Command + " " + strings.Join(configured.Args, " ")
	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	resp := postJSON(t, srv.URL+"/mcp/parse", map[string]string{"input": input})
	defer resp.Body.Close()
	var got struct {
		Servers []draftServer `json:"servers"`
		Risks   []draftRisk   `json:"risks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil || resp.StatusCode != http.StatusOK || len(got.Servers) != 1 {
		t.Fatalf("preview: status=%d body=%+v err=%v", resp.StatusCode, got, err)
	}
	e := got.Servers[0]
	if e.Name != configured.Name || e.Transport != "stdio" || e.Command != configured.Command || !reflect.DeepEqual(e.Args, configured.Args) {
		t.Errorf("preview = %+v, want registry identity %q and exact pinned command", e, configured.Name)
	}
	if len(got.Risks) != 1 || got.Risks[0].Server != configured.Name || got.Risks[0].Kind != "shell" || got.Risks[0].Detail != input {
		t.Errorf("preview disclosure = %+v", got.Risks)
	}
	if names := ctrl.ConfiguredMCPNames(); len(names) != 0 {
		t.Errorf("preview installed servers: %v", names)
	}
}
