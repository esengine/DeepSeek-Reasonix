package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebSearchRoleUsesTypedRefusal(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	if got, ok := readRoles(t, srv.URL)["web_search"]; !ok || got != "" {
		t.Fatalf("search role = %q, %v", got, ok)
	}
	resp := postProvider(t, srv.URL, "/roles", `{"role":"web_search","ref":"existing/model-a"}`)
	defer resp.Body.Close()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest || body.Code != "roles.search_model_unsupported" {
		t.Fatalf("refusal = %d, %q", resp.StatusCode, body.Code)
	}
	clear := postProvider(t, srv.URL, "/roles", `{"role":"web_search","ref":"auto"}`)
	defer clear.Body.Close()
	if clear.StatusCode != http.StatusNoContent {
		t.Fatalf("auto = %d", clear.StatusCode)
	}
	if got := readRoles(t, srv.URL)["web_search"]; got != "" {
		t.Fatalf("saved = %q", got)
	}
}
