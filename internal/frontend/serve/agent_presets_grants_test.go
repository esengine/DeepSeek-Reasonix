package serve

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func presetFiles(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			body, err := os.ReadFile(path)
			out[path] = string(body)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestPairedCompositionProxyCannotBorrowRemoteEditAuthority(t *testing.T) {
	var calls atomic.Int32
	far := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(http.StatusNoContent) }))
	defer far.Close()
	proxy := remoteProxy(RemoteEndpoint{Addr: strings.TrimPrefix(far.URL, "http://"), Token: "fixture-remote-token"})
	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/agent-presets"}, {http.MethodDelete, "/agent-presets/user:x"},
		{http.MethodPost, "/agent-presets/default"}, {http.MethodPost, "/agent-presets/import"},
		{http.MethodPost, "/agent-composition"}, {http.MethodPost, "/./agent-presets/default"},
	} {
		before := calls.Load()
		r := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
		r = r.WithContext(withDeviceReach(r.Context(), "phone", 1))
		w := httptest.NewRecorder()
		proxy.ServeHTTP(w, r)
		var reason Reason
		_ = json.Unmarshal(w.Body.Bytes(), &reason)
		if w.Code != http.StatusForbidden || reason.Code != "config.editing_disabled" {
			t.Errorf("paired %s %s = %d %s", route.method, route.path, w.Code, w.Body.String())
		}
		if calls.Load() != before {
			t.Error("paired mutation reached remote kernel under host credential")
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		before := calls.Load()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/agent-presets", strings.NewReader(`{}`))
		if method == http.MethodGet {
			r = r.WithContext(withDeviceReach(r.Context(), "phone", 1))
		}
		proxy.ServeHTTP(w, r)
		if w.Code != http.StatusNoContent || calls.Load() != before+1 {
			t.Error("remote read or host request stopped working")
		}
	}
}

func TestEveryAgentPresetMutationRequiresTheRequestEditGrant(t *testing.T) {
	for _, client := range []string{"network", "paired", "window"} {
		paired := client == "paired"
		for _, route := range []string{"save-user", "save-project", "delete", "default-user", "default-workspace", "import", "select"} {
			t.Run(route+"/"+client, func(t *testing.T) {
				t.Setenv("REASONIX_STATE_HOME", testenv.TempDir(t))
				root := testenv.TempDir(t)
				s := newProviderEditServer(t, root)
				if client != "network" {
					s.AllowProviderEdit()
				}
				ctrl := s.ctl().(*control.Controller)
				selections := 0
				ctrl.ConfigureCompositions(config.ReasonixHomeDir(), func(context.Context, string) error { selections++; return nil })
				store := ctrl.PresetStore()
				d := agentcomposition.Builtins()[0]
				d.ID, d.Name = "user:existing", "Existing"
				v, err := store.Save(d, "")
				if err != nil {
					t.Fatal(err)
				}
				method, path, body := "POST", "/agent-presets", ""
				switch route {
				case "save-user", "save-project":
					d.ID = strings.TrimPrefix(route, "save-") + ":new"
					raw, _ := json.Marshal(map[string]any{"definition": d, "expectedRevision": ""})
					body = string(raw)
				case "delete":
					method, path = "DELETE", "/agent-presets/user:existing?revision="+v.Revision
				case "default-user", "default-workspace":
					path = "/agent-presets/default"
					raw, _ := json.Marshal(map[string]any{"id": d.ID, "workspace": route == "default-workspace"})
					body = string(raw)
				case "import":
					path = "/agent-presets/import"
					raw, _ := json.Marshal(map[string]any{"content": d})
					body = string(raw)
				case "select":
					path, body = "/agent-composition", `{"id":"user:existing"}`
				}
				roots := []string{store.UserDirectory(), filepath.Join(root, ".reasonix", "agent-presets")}
				before := presetFiles(t, roots...)
				handler := operatorHandler(s)
				req := httptest.NewRequest(method, path, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				if paired {
					req = req.WithContext(withDeviceReach(req.Context(), "phone", 1))
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				var reason Reason
				_ = json.Unmarshal(w.Body.Bytes(), &reason)
				if client == "window" {
					want := http.StatusOK
					if route == "delete" || strings.HasPrefix(route, "default-") {
						want = http.StatusNoContent
					}
					if w.Code != want {
						t.Fatalf("granted mutation = %d %s", w.Code, w.Body.String())
					}
					if route == "select" && selections != 1 {
						t.Error("granted selection did not reach admission")
					}
					if route != "select" && route != "import" && reflect.DeepEqual(before, presetFiles(t, roots...)) {
						t.Error("granted mutation wrote nothing")
					}
					return
				}
				if w.Code != http.StatusForbidden || reason.Code != "config.editing_disabled" {
					t.Errorf("mutation = %d %s, want typed edit refusal", w.Code, w.Body.String())
				}
				if after := presetFiles(t, roots...); !reflect.DeepEqual(before, after) {
					t.Error("refused route changed preset files")
				}
				if selections != 0 || ctrl.CompositionBinding() != nil {
					t.Error("refused route reached composition admission")
				}
				for _, read := range []string{"/agent-presets", "/agent-presets/user:existing", "/agent-presets/user:existing/export", "/agent-composition"} {
					r := httptest.NewRequest(http.MethodGet, read, nil)
					if paired {
						r = r.WithContext(withDeviceReach(r.Context(), "phone", 1))
					}
					out := httptest.NewRecorder()
					handler.ServeHTTP(out, r)
					if out.Code != http.StatusOK {
						t.Errorf("read %s = %d", read, out.Code)
					}
				}
			})
		}
	}
}
