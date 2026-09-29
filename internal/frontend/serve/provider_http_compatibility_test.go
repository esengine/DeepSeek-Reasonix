package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/config"
)

func TestProviderHTTPCompatibilityPersistsAndRetainsOmittedField(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	for _, tc := range []struct {
		field string
		want  bool
	}{{`,"http1Only":true`, true}, {"", true}, {`,"http1Only":false`, false}} {
		resp := postProvider(t, srv.URL, "/providers/edit", `{"name":"existing","models":["model-a"],"default":"model-a"`+tc.field+`}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("save = %d", resp.StatusCode)
		}
		cfg := config.LoadForEdit(config.UserConfigPath())
		entry, _ := cfg.Provider("existing")
		if entry.HTTP1Only != tc.want {
			t.Fatalf("saved policy = %v", entry.HTTP1Only)
		}
		shape := assemblyShape(entry)
		entry.HTTP1Only = !entry.HTTP1Only
		if shape == assemblyShape(entry) {
			t.Fatal("protocol changes do not rebuild the client")
		}
		response, err := http.Get(srv.URL + "/providers")
		if err != nil {
			t.Fatal(err)
		}
		var views []providerView
		err = json.NewDecoder(response.Body).Decode(&views)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, view := range views {
			if view.Name == "existing" {
				found = true
				if view.HTTP1Only != tc.want {
					t.Fatal("view lost policy")
				}
			}
		}
		if !found {
			t.Fatal("provider missing")
		}
	}
}

func TestProviderHTTPCompatibilityCoversProbeRoutes(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	var want atomic.Int32
	want.Store(1)
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != int(want.Load()) {
			t.Errorf("protocol = %s, want %d", r.Proto, want.Load())
		}
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, `{"data":[{"id":"model-a"}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	upstream.EnableHTTP2 = true
	upstream.StartTLS()
	defer upstream.Close()
	original := http.DefaultTransport
	base := original.(*http.Transport).Clone()
	base.TLSClientConfig = upstream.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	http.DefaultTransport = base
	defer func() { http.DefaultTransport = original; base.CloseIdleConnections() }()
	cfg := config.LoadForEdit(config.UserConfigPath())
	entry, _ := cfg.Provider("existing")
	entry.BaseURL, entry.HTTP1Only = upstream.URL, true
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	for _, tc := range []struct {
		path, body string
		major      int32
	}{
		{"/providers/check", `{"name":"existing"}`, 1},
		{"/providers/check", `{"name":"existing","http1Only":false}`, 2},
		{"/providers/probe", fmt.Sprintf(`{"baseUrl":%q,"apiKey":"fixture","http1Only":true}`, upstream.URL), 1},
		{"/providers/check/model", `{"name":"existing","model":"model-a","http1Only":true}`, 1},
	} {
		want.Store(tc.major)
		resp := postProvider(t, srv.URL, tc.path, tc.body)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s = %d %s", tc.path, resp.StatusCode, body)
		}
		var result struct {
			OK     bool     `json:"ok"`
			Status string   `json:"status"`
			Models []string `json:"models"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatal(err)
		}
		if (tc.path == "/providers/check" && !result.OK) ||
			(tc.path == "/providers/probe" && len(result.Models) == 0) ||
			(tc.path == "/providers/check/model" && result.Status != "available") {
			t.Fatalf("%s did not succeed: %s", tc.path, body)
		}
	}
	cfg = config.LoadForEdit(config.UserConfigPath())
	entry, _ = cfg.Provider("existing")
	if !entry.HTTP1Only {
		t.Fatal("draft probe mutated saved policy")
	}
}
