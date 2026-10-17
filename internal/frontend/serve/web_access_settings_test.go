package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
	"reasonix/internal/tools/websearch"
)

func webAccessFile(t *testing.T) config.WebAccessSettings {
	t.Helper()
	return config.LoadForEdit(config.UserConfigPath()).Tools.WebAccessSettings
}

func webAccessRefusal(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return body.Code
}

func TestWebAccessSettingsStartEmptyAndNameWhatCanBeChosen(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	got := readJSON[control.WebAccessSettings](t, srv.URL, "/web-access-settings")
	if len(got.SearchBackends)+len(got.FetchBackends)+len(got.SearchPaidAllowed)+len(got.FetchPaidAllowed) != 0 || got.SearxngURL != "" {
		t.Fatalf("an unset file reads %+v, want everything off", got)
	}
	if !slices.Equal(got.KnownSearchBackends, config.KnownSearchBackends()) || !slices.Equal(got.KnownFetchBackends, config.KnownFetchBackends()) ||
		!slices.Equal(got.PaidBackends, config.PaidWebBackends()) {
		t.Fatalf("the choices a window may offer are %+v", got)
	}
	if got.DeepSeekAPIReady {
		t.Fatal("no provider reaches api.deepseek.com here, so the paid backend must read as not ready")
	}
}

func TestWebAccessSettingsPersistInTheUserFileAndPatchOnlyWhatIsSent(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	full := `{"search_backends":["searxng","deepseek_api","searxng"],"searxng_url":"http://127.0.0.1:8888","search_paid_allowed":["deepseek_api"],"fetch_backends":["local","deepseek_api"],"fetch_paid_allowed":["deepseek_api"]}`
	resp := postProvider(t, srv.URL, "/web-access-settings", full)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save = %d", resp.StatusCode)
	}
	want := config.WebAccessSettings{
		WebSearchBackends: []string{"searxng", "deepseek_api"}, SearxngURL: "http://127.0.0.1:8888",
		WebSearchPaidAllowed: []string{"deepseek_api"}, WebFetchBackends: []string{"local", "deepseek_api"},
		WebFetchPaidAllowed: []string{"deepseek_api"},
	}
	if got := webAccessFile(t); !slices.Equal(got.WebSearchBackends, want.WebSearchBackends) || got.SearxngURL != want.SearxngURL ||
		!slices.Equal(got.WebSearchPaidAllowed, want.WebSearchPaidAllowed) || !slices.Equal(got.WebFetchBackends, want.WebFetchBackends) ||
		!slices.Equal(got.WebFetchPaidAllowed, want.WebFetchPaidAllowed) {
		t.Fatalf("file holds %+v, want %+v (repeats dropped, order kept)", got, want)
	}

	resp = postProvider(t, srv.URL, "/web-access-settings", `{"search_paid_allowed":[]}`)
	resp.Body.Close()
	got := webAccessFile(t)
	if len(got.WebSearchPaidAllowed) != 0 || !slices.Equal(got.WebFetchPaidAllowed, want.WebFetchPaidAllowed) || !slices.Equal(got.WebSearchBackends, want.WebSearchBackends) {
		t.Fatalf("revoking the search consent must touch nothing else: %+v", got)
	}
	back := readJSON[control.WebAccessSettings](t, srv.URL, "/web-access-settings")
	if !slices.Equal(back.FetchPaidAllowed, want.WebFetchPaidAllowed) || back.SearxngURL != want.SearxngURL {
		t.Fatalf("read back %+v", back)
	}
}

func TestWebAccessSettingsRefuseWhatNoSessionCouldBuildAndWriteNothing(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	before, _ := os.ReadFile(config.UserConfigPath())

	cases := []struct{ body, code string }{
		{`{"search_backends":["bing"]}`, "web_access.unknown_backend"},
		{`{"fetch_backends":["searxng"]}`, "web_access.unknown_backend"},
		{`{"search_paid_allowed":["searxng"]}`, "web_access.unknown_backend"},
		{`{"searxng_url":"ftp://x"}`, "web_access.bad_searxng_url"},
		{`{"searxng_url":"http://user:pw@host"}`, "web_access.bad_searxng_url"},
		{`{"searxng_url":"http://host/?q=1"}`, "web_access.bad_searxng_url"},
		{`{}`, "request.bad_body"},
		{`{"search_backends":"searxng"}`, "request.bad_body"},
	}
	for _, c := range cases {
		resp := postProvider(t, srv.URL, "/web-access-settings", c.body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", c.body, resp.StatusCode)
		}
		if got := webAccessRefusal(t, resp); got != c.code {
			t.Fatalf("%s refused as %q, want %q", c.body, got, c.code)
		}
	}
	after, _ := os.ReadFile(config.UserConfigPath())
	if string(before) != string(after) {
		t.Fatal("a refused request changed the user file")
	}

	resp := postProvider(t, srv.URL, "/web-access-settings", `{"searxng_url":""}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clearing the address = %d, want 200", resp.StatusCode)
	}
}

// The settings carry a standing consent to be charged and the address queries
// go to, so they are written only where the window granted editing.
func TestWebAccessSettingsAreWrittenOnlyWithTheEditGrant(t *testing.T) {
	body := `{"search_paid_allowed":["deepseek_api"],"searxng_url":"http://10.0.0.5:8080"}`
	for name, wrap := range map[string]func(http.Handler) http.Handler{
		"no grant": func(h http.Handler) http.Handler { return h },
		"a paired device the window did grant": func(h http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				h.ServeHTTP(w, r.WithContext(withDeviceReach(r.Context(), "phone", 1)))
			})
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newProviderEditServer(t)
			if name != "no grant" {
				s.AllowProviderEdit()
			}
			before, _ := os.ReadFile(config.UserConfigPath())
			srv := httptest.NewServer(wrap(operatorHandler(s)))
			defer srv.Close()
			resp := postProvider(t, srv.URL, "/web-access-settings", body)
			if resp.StatusCode != http.StatusForbidden || webAccessRefusal(t, resp) != "web_access.editing_disabled" {
				t.Fatalf("%s: status %d, want 403 web_access.editing_disabled", name, resp.StatusCode)
			}
			after, _ := os.ReadFile(config.UserConfigPath())
			if string(before) != string(after) {
				t.Fatalf("%s: a refused request changed the user file", name)
			}
			if got := readJSON[control.WebAccessSettings](t, srv.URL, "/web-access-settings"); got.SearxngURL != "" {
				t.Fatalf("%s: reading must stay open and show the file unchanged, got %+v", name, got)
			}
		})
	}
}

func searxngAt(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

func probeBody(t *testing.T, resp *http.Response) websearch.SearxngProbe {
	t.Helper()
	defer resp.Body.Close()
	var out websearch.SearxngProbe
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestProbeNamesWhyAnInstanceFailsWithoutSavingAnything(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	before, _ := os.ReadFile(config.UserConfigPath())

	refusing := searxngAt(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	resp := postProvider(t, srv.URL, "/web-access-settings/probe", `{"url":"`+refusing+`"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("probe = %d", resp.StatusCode)
	}
	if got := probeBody(t, resp); got.OK || got.Code != websearch.CodeSearxngJSONDisabled || got.Status != http.StatusForbidden {
		t.Fatalf("probe = %+v, want the JSON format reported as not enabled", got)
	}

	working := searxngAt(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"results":[]}`)) })
	if got := probeBody(t, postProvider(t, srv.URL, "/web-access-settings/probe", `{"url":"`+working+`"}`)); !got.OK {
		t.Fatalf("an instance that answers must read as ok: %+v", got)
	}
	if after, _ := os.ReadFile(config.UserConfigPath()); string(after) != string(before) {
		t.Fatal("a probe must not write the user file")
	}
}

func TestProbeUsesTheSavedAddressWhenTheRequestNamesNone(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	saved := searxngAt(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })
	postProvider(t, srv.URL, "/web-access-settings", `{"searxng_url":"`+saved+`"}`).Body.Close()
	if got := probeBody(t, postProvider(t, srv.URL, "/web-access-settings/probe", `{}`)); got.Code != websearch.CodeSearxngStatus || got.Status != http.StatusTooManyRequests {
		t.Fatalf("probe = %+v, want the saved instance's refusal", got)
	}
}

func TestProbeRefusesWithoutTheWindowsGrantAndForABadAddress(t *testing.T) {
	denied := httptest.NewServer(operatorHandler(newProviderEditServer(t)))
	defer denied.Close()
	reached := make(chan struct{}, 1)
	target := searxngAt(t, func(http.ResponseWriter, *http.Request) { reached <- struct{}{} })
	resp := postProvider(t, denied.URL, "/web-access-settings/probe", `{"url":"`+target+`"}`)
	if resp.StatusCode != http.StatusForbidden || webAccessRefusal(t, resp) != "web_access.editing_disabled" {
		t.Fatalf("a server that did not open provider editing must refuse the probe: %d", resp.StatusCode)
	}
	select {
	case <-reached:
		t.Fatal("a refused probe must not send a request")
	default:
	}

	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	allowed := httptest.NewServer(operatorHandler(s))
	defer allowed.Close()
	resp = postProvider(t, allowed.URL, "/web-access-settings/probe", `{"url":"file:///etc/passwd"}`)
	if resp.StatusCode != http.StatusBadRequest || webAccessRefusal(t, resp) != "web_access.bad_searxng_url" {
		t.Fatalf("a non-http address: %d", resp.StatusCode)
	}
}
