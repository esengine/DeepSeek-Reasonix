package serve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
)

type homeProvider struct{}

func (homeProvider) Name() string { return "home" }

func (homeProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk)
	close(ch)
	return ch, nil
}

func homeResolver() *provider.StaticResolver {
	return &provider.StaticResolver{
		Descriptors: []provider.Descriptor{
			{Ref: "home/chat-a", DisplayName: "home", Model: "chat-a"},
			{Ref: "home/chat-b", DisplayName: "home", Model: "chat-b", Default: true},
		},
		Providers: map[string]provider.Provider{"home/chat-a": homeProvider{}, "home/chat-b": homeProvider{}},
	}
}

type resolverModelsBody struct {
	Current string `json:"current"`
	Default string `json:"default"`
	Models  []struct {
		Ref     string `json:"ref"`
		Default bool   `json:"default"`
	} `json:"models"`
}

// A pane a hub opens through a resolver lives on that resolver end to end: it
// starts on the resolver's default, offers the resolver's models, and a switch
// rebuilds onto the same resolver. This machine's config names other models —
// the ones a bootstrapped host carries of its own — and none of them may leak
// in, because the resolver is where every request of this pane is answered.
func TestHubResolverPaneNeverFallsBackToThisMachinesConfig(t *testing.T) {
	writeServeModelConfig(t)
	closeSharedCatalogsOnCleanup(t)

	h := NewHub(HubOptions{ProviderResolver: homeResolver()})
	defer h.Shutdown()
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()

	rt, err := h.Open(context.Background(), OpenRequest{Root: testenv.TempDir(t)})
	if err != nil {
		t.Fatal(err)
	}
	if got := rt.Server.Controller().ModelRef(); got != "home/chat-b" {
		t.Fatalf("pane started on %q, want the resolver's default home/chat-b", got)
	}

	body := hubGet[resolverModelsBody](t, srv, "/rt/"+rt.ID+"/models")
	var refs []string
	for _, m := range body.Models {
		refs = append(refs, m.Ref)
	}
	if want := []string{"home/chat-a", "home/chat-b"}; !slices.Equal(refs, want) {
		t.Fatalf("picker offered %v, want only the resolver's %v", refs, want)
	}
	if body.Default != "home/chat-b" || body.Current != "home/chat-b" {
		t.Fatalf("default/current = %q/%q, want home/chat-b", body.Default, body.Current)
	}

	resp, err := http.Post(srv.URL+"/rt/"+rt.ID+"/model", "application/json", strings.NewReader(`{"ref":"home/chat-a"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /model = %d", resp.StatusCode)
	}
	switched := rt.Server.Controller()
	if got := switched.ModelRef(); got != "home/chat-a" {
		t.Fatalf("after the switch the pane is on %q, want home/chat-a", got)
	}
	refs, _ = descriptorRefs(switched.ProviderCatalog())
	if want := []string{"home/chat-a", "home/chat-b"}; !slices.Equal(refs, want) {
		t.Fatalf("the rebuilt pane resolves through %v, want the resolver's %v", refs, want)
	}
	if got := config.LoadForEdit(config.UserConfigPath()).DefaultModel; got != "default/shared-chat" {
		t.Fatalf("default_model = %q: a resolver pane's switch wrote this machine's config", got)
	}
}

func descriptorRefs(catalog []provider.Descriptor) ([]string, string) {
	var refs []string
	for _, d := range catalog {
		refs = append(refs, d.Ref)
	}
	return refs, provider.DefaultRef(catalog)
}
