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
	"reasonix/internal/session/control"
)

const listingConfig = `default_model = "plain/beta"

[[providers]]
name = "plain"
kind = "openai"
base_url = "https://plain.invalid/v1"
models = ["beta"]
default = "beta"
api_key_env = "LISTING_API_KEY"

[[providers]]
name = "verdicts"
kind = "typesafe"
base_url = "https://decide.invalid"
models = ["system-one"]
default = "system-one"
api_key_env = "LISTING_API_KEY"
`

func TestProvidersSayWhichEntriesHaveAModelListing(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	if _, err := config.SetCredential("LISTING_API_KEY", "sk-listing"); err != nil {
		t.Fatal(err)
	}
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(listingConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc, Label: "beta", ModelRef: "plain/beta", SessionDir: testenv.TempDir(t)})
	s := New(ctrl, bc, config.ServeConfig{})
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/providers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []struct {
		Name          string `json:"name"`
		CanListModels *bool  `json:"canListModels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range out {
		if p.CanListModels == nil {
			t.Fatalf("%s: canListModels is not sent", p.Name)
		}
		got[p.Name] = *p.CanListModels
	}
	if !got["plain"] || got["verdicts"] {
		t.Fatalf("canListModels = %v, want plain=true verdicts=false", got)
	}
}
