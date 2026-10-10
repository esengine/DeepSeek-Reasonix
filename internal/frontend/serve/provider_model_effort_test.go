package serve

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"reasonix/internal/contract/config"
)

func listedRich(t *testing.T, base string) providerView {
	t.Helper()
	resp, err := http.Get(base + "/providers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var views []providerView
	if err := json.NewDecoder(resp.Body).Decode(&views); err != nil {
		t.Fatal(err)
	}
	for _, v := range views {
		if v.Name == "rich" {
			return v
		}
	}
	t.Fatal("rich is not listed")
	return providerView{}
}

func TestProvidersListsEachModelsOwnAndInheritedEfforts(t *testing.T) {
	srv := newRichProviderServer(t)
	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","vision":[],
		"supportedEfforts":["low","medium","high"],"defaultEffort":"medium"
	}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /providers/edit = %d", resp.StatusCode)
	}

	v := listedRich(t, srv.URL)
	if got := v.ModelEfforts["beta"]; !slices.Equal(got.SupportedEfforts, []string{"low", "high"}) || got.DefaultEffort != "high" {
		t.Fatalf("beta's own levels = %+v", got)
	}
	if _, ok := v.ModelEfforts["alpha"]; ok {
		t.Fatalf("alpha declares nothing of its own but is listed: %+v", v.ModelEfforts)
	}
	for _, model := range []string{"alpha", "beta"} {
		got := v.InheritedEfforts[model]
		if !slices.Equal(got.SupportedEfforts, []string{"low", "medium", "high"}) || got.DefaultEffort != "medium" {
			t.Fatalf("%s inherits %+v, want the connection's levels", model, got)
		}
	}
}

// The form's per-model answer is the whole answer for the models it lists: one
// sent with levels declares them, one left out inherits again.
func TestEditProviderStoresPerModelEfforts(t *testing.T) {
	srv := newRichProviderServer(t)
	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","vision":[],
		"modelEfforts":{"alpha":{"supportedEfforts":["None","xhigh","auto"],"defaultEffort":"xhigh"}}
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		b, _ := readAllString(resp)
		t.Fatalf("POST /providers/edit = %d: %s", resp.StatusCode, b)
	}

	alpha, ok := loadEntry(t, "rich/alpha")
	if !ok {
		t.Fatal("rich/alpha does not resolve")
	}
	if got := config.EffortCapabilityForEntry(alpha); !slices.Equal(got.Levels, []string{"auto", "none", "xhigh"}) || got.Default != "xhigh" {
		t.Fatalf("alpha's picker = %+v, want its own levels", got)
	}
	if got := config.RequestEffortLevels(alpha); !slices.Equal(got, []string{"none", "xhigh"}) {
		t.Fatalf("alpha's request vocabulary = %v", got)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	rich, _ := cfg.Provider("rich")
	if _, ok := rich.ModelOverrides["beta"]; ok {
		t.Fatalf("beta was left out of the answer and should inherit again: %+v", rich.ModelOverrides)
	}
}

func TestEditProviderRefusesAPerModelEffortItCannotApply(t *testing.T) {
	cases := []struct{ body, code string }{
		{`{"name":"rich","models":["alpha"],"default":"alpha","vision":[],
			"modelEfforts":{"alpha":{"supportedEfforts":["low","high"],"defaultEffort":"max"}}}`,
			"provider.model_default_effort_not_listed"},
		{`{"name":"rich","models":["alpha"],"default":"alpha","vision":[],
			"modelEfforts":{"beta":{"supportedEfforts":["low"]}}}`,
			"provider.model_effort_unlisted"},
	}
	for _, tc := range cases {
		srv := newRichProviderServer(t)
		resp := postProvider(t, srv.URL, "/providers/edit", tc.body)
		var reason Reason
		if err := json.NewDecoder(resp.Body).Decode(&reason); err != nil {
			t.Fatalf("decode reason: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || reason.Code != tc.code {
			t.Fatalf("status %d code %q, want 400 %s", resp.StatusCode, reason.Code, tc.code)
		}
		cfg, err := config.Load()
		if err != nil {
			t.Fatal(err)
		}
		rich, _ := cfg.Provider("rich")
		if got := rich.ModelOverrides["beta"].SupportedEfforts; !slices.Equal(got, []string{"low", "high"}) {
			t.Fatalf("a refused edit changed the file: beta = %v", got)
		}
	}
}

func TestProvidersMarksAnInheritedVendorContractOfficial(t *testing.T) {
	srv := newRichProviderServer(t)
	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["glm-5.3","glm-4.5"],"default":"glm-5.3","vision":[],
		"reasoningProtocol":"glm"
	}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /providers/edit = %d", resp.StatusCode)
	}
	v := listedRich(t, srv.URL)
	got := v.InheritedEfforts["glm-5.3"]
	if !got.Official || !slices.Equal(got.SupportedEfforts, []string{"low", "high", "max"}) || got.DefaultEffort != "max" {
		t.Fatalf("glm-5.3 inherits %+v, want the official low/high/max default max", got)
	}
	if other := v.InheritedEfforts["glm-4.5"]; other.Official {
		t.Fatalf("glm-4.5 has no documented contract but is marked official: %+v", other)
	}
}
