package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/extension/uihub"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/session/control"
)

type frameCtl struct {
	control.SessionAPI
	asked struct {
		plugin, frame string
		generation    uint64
	}
	doc []byte
	err error
}

func (c *frameCtl) SessionDir() string { return "" }

func (c *frameCtl) ExtensionFrameDocument(pluginID, frameID string, generation uint64) ([]byte, error) {
	c.asked.plugin, c.asked.frame, c.asked.generation = pluginID, frameID, generation
	return c.doc, c.err
}

func frameServer(t *testing.T, ctl *frameCtl) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(operatorHandler(New(ctl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return srv
}

func TestFrameDocumentIsServedIntoAnOpaqueSandboxWithNothingToReach(t *testing.T) {
	ctl := &frameCtl{doc: []byte("<!doctype html><p>board</p>")}
	srv := frameServer(t, ctl)
	resp, err := http.Get(srv.URL + "/extensions/frames/board/kanban?generation=7")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "<!doctype html><p>board</p>" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	if ctl.asked.plugin != "board" || ctl.asked.frame != "kanban" || ctl.asked.generation != 7 {
		t.Fatalf("asked = %+v", ctl.asked)
	}
	want := map[string]string{
		"Content-Security-Policy":      frameCSP,
		"Content-Type":                 "text/html; charset=utf-8",
		"X-Content-Type-Options":       "nosniff",
		"Cache-Control":                "no-store",
		"Cross-Origin-Resource-Policy": "same-origin",
		"Referrer-Policy":              "no-referrer",
	}
	for k, v := range want {
		if got := resp.Header.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if resp.Header.Get("ETag") == "" {
		t.Error("no ETag")
	}
	for _, directive := range []string{"sandbox allow-scripts", "default-src 'none'", "connect-src 'none'", "form-action 'none'", "frame-ancestors 'self'", "base-uri 'none'"} {
		if !strings.Contains(frameCSP, directive) {
			t.Errorf("CSP lacks %q", directive)
		}
	}
	if strings.Contains(frameCSP, "allow-same-origin") {
		t.Error("the sandbox keeps the page's origin")
	}
}

func TestFrameDocumentRefusalsCarryTheirCause(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{uihub.ErrFrameStale, http.StatusConflict, "ui.frame_stale"},
		{fmt.Errorf("x: %w", pluginpkg.ErrFrameNotDeclared), http.StatusNotFound, "ui.frame_not_declared"},
		{fmt.Errorf("x: %w", pluginpkg.ErrFrameMissing), http.StatusNotFound, "ui.frame_missing"},
		{fmt.Errorf("x: %w", pluginpkg.ErrFrameOutsideRoot), http.StatusForbidden, "ui.frame_outside_root"},
		{fmt.Errorf("x: %w", pluginpkg.ErrFrameTooLarge), http.StatusRequestEntityTooLarge, "ui.frame_too_large"},
		{fmt.Errorf("x: %w", pluginpkg.ErrFrameNotUTF8), http.StatusUnsupportedMediaType, "ui.frame_not_utf8"},
		{fmt.Errorf("disk"), http.StatusInternalServerError, "ui.frame_failed"},
	}
	for _, c := range cases {
		srv := frameServer(t, &frameCtl{err: c.err})
		resp, err := http.Get(srv.URL + "/extensions/frames/board/kanban?generation=7")
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != c.status || body.Code != c.code {
			t.Errorf("%v: %d %q, want %d %q", c.err, resp.StatusCode, body.Code, c.status, c.code)
		}
	}
}

func TestFrameDocumentNeedsAGenerationAndIsOnlyRead(t *testing.T) {
	srv := frameServer(t, &frameCtl{doc: []byte("<p>x</p>")})
	for _, q := range []string{"", "?generation=", "?generation=x", "?generation=-1"} {
		resp, err := http.Get(srv.URL + "/extensions/frames/board/kanban" + q)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%q: status %d", q, resp.StatusCode)
		}
	}
	resp, err := http.Post(srv.URL+"/extensions/frames/board/kanban?generation=7", "text/html", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatal("a POST was served")
	}
}
