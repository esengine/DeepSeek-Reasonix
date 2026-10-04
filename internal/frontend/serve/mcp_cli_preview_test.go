package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func TestMCPParsePreviewsCopiedCLIEndpoint(t *testing.T) {
	home, workspace := testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))
	t.Chdir(workspace)
	ctrl := control.New(control.Options{WorkspaceRoot: workspace})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	input := `% reasonix mcp add docs --http https://mcp.example.test/endpoint --header "Authorization=Bearer fixture-token"`
	resp, err := http.Post(srv.URL+"/mcp/parse", "application/json", strings.NewReader(`{"input":`+mustJSON(t, input)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("preview=%d: %s", resp.StatusCode, body)
	}
	var got struct {
		Servers []draftServer `json:"servers"`
		Risks   []draftRisk   `json:"risks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Servers) != 1 {
		t.Fatalf("servers=%+v", got.Servers)
	}
	s := got.Servers[0]
	if s.Name != "docs" || s.Transport != "http" || s.URL != "https://mcp.example.test/endpoint" || s.Command != "" || len(s.Args) != 0 || s.Headers["Authorization"] != "Bearer fixture-token" {
		t.Errorf("wrong server preview: %+v", s)
	}
	var host, secret bool
	for _, risk := range got.Risks {
		if risk.Server != "docs" || risk.Kind == "shell" {
			t.Errorf("wrong disclosure: %+v", risk)
		}
		host = host || (risk.Kind == "unknown-host" && risk.Detail == s.URL)
		secret = secret || (risk.Kind == "secret" && risk.Field == "headers.Authorization")
	}
	if !host || !secret {
		t.Errorf("missing endpoint/credential disclosures: %+v", got.Risks)
	}
	if names := ctrl.ConfiguredMCPNames(); len(names) != 0 {
		t.Errorf("preview installed %v", names)
	}
	for _, root := range []string{home, workspace} {
		files, err := os.ReadDir(root)
		if err != nil || len(files) != 0 {
			t.Errorf("preview wrote files in %s: %v, %v", root, files, err)
		}
	}
}
