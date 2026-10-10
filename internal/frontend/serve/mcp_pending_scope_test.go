package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/session/control"
)

func TestMCPAuthenticationPendingInstallKeepsChosenScope(t *testing.T) {
	for _, scope := range []string{"local", "user", "project"} {
		t.Run(scope, func(t *testing.T) {
			home, workspace, other := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", home)
			t.Setenv("REASONIX_STATE_HOME", home)
			t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))
			t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
			t.Chdir(workspace)
			if !strings.HasPrefix(config.UserConfigPath(), home+string(filepath.Separator)) {
				t.Fatalf("user config escaped isolation: %s", config.UserConfigPath())
			}
			var attempts atomic.Int64
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.Header().Set("WWW-Authenticate", `Bearer realm="scope-fixture"`)
				w.WriteHeader(http.StatusUnauthorized)
			}))
			t.Cleanup(remote.Close)
			host := plugin.NewHost()
			t.Cleanup(host.Close)
			ctrl := control.New(control.Options{Host: host, Registry: tool.NewRegistry(), PluginCtx: t.Context(), WorkspaceRoot: workspace})
			t.Cleanup(ctrl.Close)
			srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
			t.Cleanup(srv.Close)
			resp := postJSON(t, srv.URL+"/mcp/install", map[string]any{
				"scope":  scope,
				"server": map[string]any{"name": "scope-auth", "transport": "http", "url": remote.URL},
			})
			defer resp.Body.Close()
			var result plugin.MCPInstallResult
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil || resp.StatusCode != http.StatusOK || result.State != "action_required" || result.Name != "scope-auth" {
				t.Fatalf("install status=%d result=%+v err=%v", resp.StatusCode, result, err)
			}
			if attempts.Load() == 0 {
				t.Fatal("install did not reach the actual authentication challenge")
			}
			check := func(root string, present, enabled, local bool) {
				t.Helper()
				fresh := control.New(control.Options{WorkspaceRoot: root})
				defer fresh.Close()
				var state *control.MCPServerState
				for _, candidate := range fresh.ConfiguredMCPServers() {
					if candidate.Entry.Name == "scope-auth" {
						state = &candidate
					}
				}
				if !present {
					if state != nil {
						t.Errorf("other project acquired a repository declaration: %+v", state)
					}
					return
				}
				if state == nil || state.Enabled != enabled || state.LocalOverride != local {
					t.Errorf("root=%s state=%+v, want enabled=%v local=%v", root, state, enabled, local)
				}
			}
			check(workspace, true, true, scope != "user")
			check(other, scope != "project", scope == "user", false)
		})
	}
}
