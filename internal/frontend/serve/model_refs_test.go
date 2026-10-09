package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

const extensionModelRef = "plugin/demo/cloud/extension-chat"

// A server whose picker stocks both catalogs: the configured provider and an
// extension-hosted one, which is the pairing every plugin install produces.
func newRoleServerWithExtensionModel(t *testing.T) *Server {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	configPath := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `default_model = "existing/model-a"

[[providers]]
name = "existing"
kind = "openai"
base_url = "https://example.invalid/v1"
models = ["model-a"]
default = "model-a"
api_key = "k"
`
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{
		Sink: bc, Label: "model-a", ModelRef: "existing/model-a", SessionDir: testenv.TempDir(t),
		ProviderResolver: &provider.StaticResolver{Descriptors: []provider.Descriptor{{
			Ref: extensionModelRef, Model: "extension-chat", DisplayName: "Extension Chat",
		}}},
	})
	t.Cleanup(ctrl.Close)
	closeSharedCatalogsOnCleanup(t)
	s := New(ctrl, bc, config.ServeConfig{})
	s.AllowProviderEdit()
	return s
}

func offeredModelRefs(t *testing.T, base string) []string {
	t.Helper()
	resp, err := http.Get(base + "/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Models []struct {
			Ref string `json:"ref"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	refs := make([]string, 0, len(body.Models))
	for _, m := range body.Models {
		refs = append(refs, m.Ref)
	}
	if len(refs) == 0 {
		t.Fatal("the picker offered nothing to assign")
	}
	return refs
}

// The picker and the role form read one list, so a model the picker offers has
// to be one the assignment accepts. Validating against the configuration alone
// broke exactly here: an extension-hosted model switched fine as the main model
// and was refused as a role, because no config file ever holds one.
func TestEveryModelThePickerOffersIsAssignableToARole(t *testing.T) {
	var offered []string
	func() {
		srv := httptest.NewServer(operatorHandler(newRoleServerWithExtensionModel(t)))
		defer srv.Close()
		offered = offeredModelRefs(t, srv.URL)
	}()

	// A fresh server per ref: assigning a role rebuilds the runtime, and a
	// later ref must be judged by the picker's offer rather than by what an
	// earlier assignment left behind.
	for _, ref := range offered {
		t.Run(ref, func(t *testing.T) {
			srv := httptest.NewServer(operatorHandler(newRoleServerWithExtensionModel(t)))
			defer srv.Close()
			payload, err := json.Marshal(map[string]string{"role": "planner", "ref": ref})
			if err != nil {
				t.Fatal(err)
			}
			resp := postProvider(t, srv.URL, "/roles", string(payload))
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				got, _ := readAllString(resp)
				t.Fatalf("POST /roles %s = %d: %s", ref, resp.StatusCode, got)
			}
			if got := readRoles(t, srv.URL).Roles["planner"]; got != ref {
				t.Fatalf("planner = %q after assigning %q", got, ref)
			}
		})
	}
}

// Neither catalog vouching for a ref is still a refusal: the guard reads the
// catalogs, so it must not have widened into accepting anything with slashes.
func TestRoleStillRefusesARefNoCatalogCarries(t *testing.T) {
	s := newRoleServerWithExtensionModel(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	for _, ref := range []string{"plugin/ghost/cloud/nothing", "nosuch/model", "plugin/two"} {
		resp := postProvider(t, srv.URL, "/roles", `{"role":"planner","ref":"`+ref+`"}`)
		status := resp.StatusCode
		resp.Body.Close()
		if status != http.StatusBadRequest {
			t.Fatalf("POST /roles %s = %d, want 400", ref, status)
		}
	}
}
