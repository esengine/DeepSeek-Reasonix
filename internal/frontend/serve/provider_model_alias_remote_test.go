package serve

import (
	"net/http/httptest"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"testing"
)

func TestModelAliasesNeverOverlayRemoteCatalog(t *testing.T) {
	writeServeModelConfig(t)
	closeSharedCatalogsOnCleanup(t)
	cfg := config.LoadForEdit(config.UserConfigPath())
	cfg.Providers = append(cfg.Providers, config.ProviderEntry{Name: "home", Kind: "openai", BaseURL: "https://local.invalid", Models: []string{"chat-a", "chat-b"}, ModelAliases: map[string]string{"chat-a": "Local alias", "chat-b": "Local alias"}})
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	h := NewHub(HubOptions{ProviderResolver: homeResolver()})
	defer h.Shutdown()
	rt, err := h.Open(t.Context(), OpenRequest{Root: testenv.TempDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	var got struct {
		Current string       `json:"current"`
		Models  []modelEntry `json:"models"`
	}
	getJSON(t, srv.URL+"/rt/"+rt.ID+"/models", &got)
	if got.Current != "home/chat-b" || len(got.Models) != 2 {
		t.Fatalf("remote catalog changed: %+v", got)
	}
	for _, m := range got.Models {
		if m.ModelAlias != "" {
			t.Fatalf("local alias leaked into remote catalog: %+v", m)
		}
	}
}
