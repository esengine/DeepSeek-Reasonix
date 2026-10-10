package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
)

var decisionPickSeq atomic.Uint64

func newDecisionPickServer(t *testing.T) *httptest.Server {
	t.Helper()
	home, root := testenv.TempDir(t), testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_STATE_HOME", home)
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Chdir(root)
	closeSharedCatalogsOnCleanup(t)
	kind := fmt.Sprintf("decision-pick-%d", decisionPickSeq.Add(1))
	provider.Register(kind, func(provider.Config) (provider.Provider, error) {
		return testutil.NewMock(kind, testutil.Turn{Text: "ok"}), nil
	})
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `default_model = "chat/m1"
[agent]
decision_model = "laya/typed-decisions"
[environment]
enabled = false
[codegraph]
enabled = false
[[providers]]
name = "chat"
kind = "` + kind + `"
models = ["m1", "m2"]
default = "m1"
[[providers]]
name = "laya"
kind = "typesafe"
base_url = "http://127.0.0.1:8700"
models = ["typed-decisions"]
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	initial, err := boot.BuildRuntime(t.Context(), boot.Options{Sink: bc, WorkspaceRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	s := New(initial.Controller, bc, config.ServeConfig{})
	s.AdoptRuntime(initial)
	s.AllowProviderEdit()
	leases := control.NewSessionLeaseKeeper()
	s.SetSessionLeases(leases)
	t.Cleanup(leases.Release)
	t.Cleanup(func() { s.ctl().Close() })
	srv := httptest.NewServer(operatorHandler(s))
	t.Cleanup(srv.Close)
	return srv
}

func listedRefs(t *testing.T, url string) []string {
	t.Helper()
	resp, err := http.Get(url)
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
	refs := []string{}
	for _, m := range body.Models {
		refs = append(refs, m.Ref)
	}
	slices.Sort(refs)
	return refs
}

func statusAndCode(t *testing.T, resp *http.Response) (int, string) {
	t.Helper()
	defer resp.Body.Close()
	var r Reason
	_ = json.NewDecoder(resp.Body).Decode(&r)
	return resp.StatusCode, r.Code
}

func TestEffectDecisionSourceIsNotOfferedAsAConversationModel(t *testing.T) {
	srv := newDecisionPickServer(t)

	if got, want := listedRefs(t, srv.URL+"/models"), []string{"chat/m1", "chat/m2"}; !slices.Equal(got, want) {
		t.Fatalf("default /models = %v, want %v", got, want)
	}
	if got, want := listedRefs(t, srv.URL+"/models?answers=chat"), []string{"chat/m1", "chat/m2"}; !slices.Equal(got, want) {
		t.Fatalf("answers=chat = %v, want %v", got, want)
	}
	if got, want := listedRefs(t, srv.URL+"/models?answers=decision"), []string{"laya/typed-decisions"}; !slices.Equal(got, want) {
		t.Fatalf("answers=decision = %v, want %v", got, want)
	}
	if got, want := listedRefs(t, srv.URL+"/models?answers=all"), []string{"chat/m1", "chat/m2", "laya/typed-decisions"}; !slices.Equal(got, want) {
		t.Fatalf("answers=all = %v, want %v", got, want)
	}
}

func TestEffectDecisionSourceRefusesTheConversationJobs(t *testing.T) {
	srv := newDecisionPickServer(t)
	const laya = "laya/typed-decisions"

	status, code := statusAndCode(t, postProvider(t, srv.URL, "/model", `{"ref":"`+laya+`"}`))
	if status != http.StatusConflict || code != "model.decision_only" {
		t.Fatalf("POST /model = %d %q, want 409 model.decision_only", status, code)
	}
	status, code = statusAndCode(t, postProvider(t, srv.URL, "/default-model", `{"ref":"`+laya+`"}`))
	if status != http.StatusConflict || code != "model.decision_only" {
		t.Fatalf("POST /default-model = %d %q, want 409 model.decision_only", status, code)
	}
	for _, role := range []string{"planner", "subagent", "guardian", "vision"} {
		status, code = statusAndCode(t, postProvider(t, srv.URL, "/roles", `{"role":"`+role+`","ref":"`+laya+`"}`))
		if status != http.StatusConflict || code != "model.decision_only" {
			t.Fatalf("POST /roles %s = %d %q, want 409 model.decision_only", role, status, code)
		}
	}
	status, code = statusAndCode(t, postProvider(t, srv.URL, "/roles", `{"role":"decision","ref":"chat/m2"}`))
	if status != http.StatusConflict || code != "model.not_decision_source" {
		t.Fatalf("decision role on a chat model = %d %q, want 409 model.not_decision_source", status, code)
	}

	resp := postProvider(t, srv.URL, "/roles", `{"role":"decision","ref":"`+laya+`"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("decision role on the decision source = %d, want 204", resp.StatusCode)
	}
	resp = postProvider(t, srv.URL, "/model", `{"ref":"chat/m2"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("switching to a chat model = %d, want 204", resp.StatusCode)
	}
}
