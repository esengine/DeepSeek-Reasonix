package serve

import (
	"net/http"
	"os"
	"reasonix/internal/session/control"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

func TestEditProviderStoresPerModelLimitsAndClearsThem(t *testing.T) {
	srv := newRichProviderServer(t)
	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","vision":[],
		"modelLimits":{"alpha":{"contextWindow":32000,"maxOutputTokens":4096}}
	}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /providers/edit = %d", resp.StatusCode)
	}

	alpha, _ := loadEntry(t, "rich/alpha")
	if alpha.ContextWindow != 32000 || alpha.MaxOutputTokens != 4096 {
		t.Fatalf("alpha resolves to %d/%d, want 32000/4096", alpha.ContextWindow, alpha.MaxOutputTokens)
	}
	beta, _ := loadEntry(t, "rich/beta")
	if beta.ContextWindow != 131072 || beta.MaxOutputTokens != 0 {
		t.Fatalf("beta resolves to %d/%d, want the connection's 131072/0", beta.ContextWindow, beta.MaxOutputTokens)
	}
	raw, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "context_window = 32000") {
		t.Fatalf("the override never reached config.toml:\n%s", raw)
	}

	v := listedRich(t, srv.URL)
	if got := v.ModelLimits["alpha"]; got.ContextWindow != 32000 || got.MaxOutputTokens != 4096 {
		t.Fatalf("alpha's own limits = %+v", got)
	}
	if _, ok := v.ModelLimits["beta"]; ok {
		t.Fatalf("beta declares nothing but is listed: %+v", v.ModelLimits)
	}
	if got := v.InheritedLimits["alpha"]; got.ContextWindow != 131072 {
		t.Fatalf("alpha's inherited window = %+v, want the connection's", got)
	}
	if got := v.ModelEfforts["beta"]; len(got.SupportedEfforts) == 0 {
		t.Fatalf("saving limits dropped beta's effort levels: %+v", v.ModelEfforts)
	}

	resp = postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","vision":[],"modelLimits":{}
	}`)
	resp.Body.Close()
	alpha, _ = loadEntry(t, "rich/alpha")
	if alpha.ContextWindow != 131072 {
		t.Fatalf("clearing left alpha on %d", alpha.ContextWindow)
	}
	raw, _ = os.ReadFile(config.UserConfigPath())
	if strings.Contains(string(raw), "32000") || strings.Contains(string(raw), "model_overrides.alpha") {
		t.Fatalf("the cleared override is still on file:\n%s", raw)
	}
}

func TestEditProviderLeavesModelLimitsAloneWhenNotSent(t *testing.T) {
	srv := newRichProviderServer(t)
	postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","vision":[],
		"modelLimits":{"beta":{"contextWindow":64000}}
	}`).Body.Close()
	postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","vision":[]
	}`).Body.Close()
	if beta, _ := loadEntry(t, "rich/beta"); beta.ContextWindow != 64000 {
		t.Fatalf("an edit that sent no limits changed beta to %d", beta.ContextWindow)
	}
}

func TestEditProviderRefusesBadModelLimits(t *testing.T) {
	srv := newRichProviderServer(t)
	for _, tc := range []struct{ body, code string }{
		{`"modelLimits":{"alpha":{"contextWindow":-5}}`, "provider.bad_model_context_window"},
		{`"modelLimits":{"gamma":{"contextWindow":5}}`, "provider.model_limits_unlisted"},
	} {
		resp := postProvider(t, srv.URL, "/providers/edit",
			`{"name":"rich","models":["alpha","beta"],"default":"alpha","vision":[],`+tc.body+`}`)
		b, _ := readAllString(resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(b, tc.code) {
			t.Fatalf("%s: got %d %s, want 400 %s", tc.body, resp.StatusCode, b, tc.code)
		}
	}
}

func TestEditProviderModelLimitsReachTheRunningConversation(t *testing.T) {
	srv := newRichProviderServer(t)
	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","vision":[],
		"modelLimits":{"alpha":{"contextWindow":32000}}
	}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /providers/edit = %d", resp.StatusCode)
	}
	if got := gaugeWindow(t, srv.URL); got != 32000 {
		t.Fatalf("the running conversation's window = %d, want alpha's 32000", got)
	}
	postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","vision":[],"modelLimits":{}
	}`).Body.Close()
	if got := gaugeWindow(t, srv.URL); got != 131072 {
		t.Fatalf("after clearing, the window = %d, want the connection's 131072", got)
	}
}

func TestEditProviderModelLimitsMidTurnAreSavedNotApplied(t *testing.T) {
	srv := newRichProviderServerAs(t, func(c control.SessionAPI) control.SessionAPI { return midTurn{c} })
	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","modelLimits":{"alpha":{"contextWindow":32000}}
	}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("mid-turn = %d, want 409", resp.StatusCode)
	}
	if alpha, _ := loadEntry(t, "rich/alpha"); alpha.ContextWindow != 32000 {
		t.Fatalf("window on disk = %d", alpha.ContextWindow)
	}
}

// A negative output cap is the stored spelling of "send no cap"; the endpoint
// accepts it as such, unlike a negative window, which has no meaning.
func TestEditProviderStoresNegativeModelOutputAsNoCap(t *testing.T) {
	srv := newRichProviderServer(t)
	resp := postProvider(t, srv.URL, "/providers/edit", `{
		"name":"rich","models":["alpha","beta"],"default":"alpha","vision":[],
		"modelLimits":{"beta":{"maxOutputTokens":-1}}
	}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /providers/edit = %d", resp.StatusCode)
	}
	if beta, _ := loadEntry(t, "rich/beta"); beta.MaxOutputTokens != -1 || beta.ContextWindow != 131072 {
		t.Fatalf("beta resolves to %d/%d, want 131072/-1", beta.ContextWindow, beta.MaxOutputTokens)
	}
}
