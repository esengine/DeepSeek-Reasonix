package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/promptrefine"
	"reasonix/internal/session/control"
)

type refineProvider struct{ answer string }

func (refineProvider) Name() string { return "fake" }

func (p refineProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 1)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: p.answer}
	close(ch)
	return ch, nil
}

func refineServer(t *testing.T, refiner *promptrefine.Refiner) *httptest.Server {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, PromptRefiner: refiner})
	t.Cleanup(func() { ctrl.Close() })
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return srv
}

func postRefine(t *testing.T, srv *httptest.Server, body string) (int, map[string]any) {
	t.Helper()
	res, err := http.Post(srv.URL+"/prompt/refine", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func TestARefinedPromptComesBackAsText(t *testing.T) {
	srv := refineServer(t, promptrefine.New(refineProvider{answer: "Fix the login loop in auth/session.go."}, nil, "fake/m", nil))
	status, out := postRefine(t, srv, `{"draft":"fix that bug"}`)
	if status != http.StatusOK || out["text"] != "Fix the login loop in auth/session.go." {
		t.Fatalf("= %d %v", status, out)
	}
}

// Each reason a rewrite is not made is its own code, so the window can say
// which: nothing to rewrite, nothing to rewrite with, a request it cannot read.
func TestARefusedRefineSaysWhichReason(t *testing.T) {
	with := refineServer(t, promptrefine.New(refineProvider{answer: "x"}, nil, "fake/m", nil))
	without := refineServer(t, nil)
	cases := []struct {
		srv    *httptest.Server
		body   string
		status int
		code   string
	}{
		{with, `{"draft":"   "}`, http.StatusBadRequest, "prompt_refine.empty"},
		{with, `{"draft":"` + strings.Repeat("a", promptrefine.MaxDraftBytes+1) + `"}`, http.StatusRequestEntityTooLarge, "prompt_refine.too_long"},
		{with, `not json`, http.StatusBadRequest, "prompt_refine.bad_request"},
		{without, `{"draft":"fix it"}`, http.StatusConflict, "prompt_refine.no_model"},
	}
	for _, c := range cases {
		status, out := postRefine(t, c.srv, c.body)
		if status != c.status || out["code"] != c.code {
			t.Errorf("%.30s = %d %v, want %d %s", c.body, status, out["code"], c.status, c.code)
		}
	}
}
