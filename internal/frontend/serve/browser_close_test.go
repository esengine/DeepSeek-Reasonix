package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/platform/browser"
	"reasonix/internal/session/control"
)

type closingPage struct {
	*refusingPage
	targets []string
}

func (p *closingPage) WriteMessage(raw []byte) error {
	var msg struct {
		ID     int
		Method string
		Params struct {
			TargetID string `json:"targetId"`
		}
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return err
	}
	if msg.Method == "Target.closeTarget" {
		p.targets = append(p.targets, msg.Params.TargetID)
		reply, _ := json.Marshal(map[string]any{"id": msg.ID, "result": map[string]any{"success": true}})
		p.in <- reply
		return nil
	}
	return p.refusingPage.WriteMessage(raw)
}

func TestBrowserCloseReleasesTheNativeTarget(t *testing.T) {
	page := &closingPage{refusingPage: &refusingPage{in: make(chan []byte, 64), closed: make(chan struct{})}}
	pool := &browser.Pool{}
	pool.SetEndpoint(func(context.Context, string) (browser.Endpoint, error) { return page, nil })
	session := browser.NewSession(browser.Config{Launch: browser.LaunchSpec{ProfileDir: "/profiles/w1"}, Pool: pool})
	ctrl := control.New(control.Options{BrowserSession: session})
	t.Cleanup(ctrl.Close)
	// A failed navigation still owns the native page that must be released.
	_, _ = ctrl.BrowserOpen(t.Context(), "http://localhost:5173/", "", true)
	tabs := ctrl.BrowserTabs()
	if len(tabs) != 1 {
		t.Fatalf("tabs = %+v", tabs)
	}
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	body, _ := json.Marshal(map[string]string{"tab": tabs[0].ID})
	resp, err := http.Post(srv.URL+"/browser/close", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("close status = %d", resp.StatusCode)
	}
	if len(ctrl.BrowserTabs()) != 0 {
		t.Fatal("closed tab still in session")
	}
	if len(page.targets) != 1 || page.targets[0] != tabs[0].Target {
		t.Fatalf("native targets = %+v", page.targets)
	}
}

func TestBrowserCloseRequiresOperatorAndValidInput(t *testing.T) {
	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	server := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	for _, tc := range []struct {
		name, body string
		operator   bool
		want       int
	}{
		{"ungated", `{"tab":"t1"}`, false, http.StatusForbidden},
		{"malformed", `{`, true, http.StatusBadRequest},
		{"missing", `{"tab":" "}`, true, http.StatusBadRequest},
		{"unavailable", `{"tab":"t1"}`, true, http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := server.Handler()
			if tc.operator {
				h = operatorHandler(server)
			}
			req := httptest.NewRequest(http.MethodPost, "http://localhost/browser/close", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

type failingBrowserClose struct {
	control.SessionAPI
	err error
}

func (c failingBrowserClose) BrowserClose(context.Context, string) error { return c.err }

func TestBrowserCloseMapsTypedFailures(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"engine", &browser.Failure{Code: browser.CodeEngineFailed}, http.StatusBadGateway},
		{"missing engine", &browser.Failure{Code: browser.CodeEngineMissing}, http.StatusServiceUnavailable},
		{"busy profile", &browser.Failure{Code: browser.CodeProfileBusy}, http.StatusConflict},
		{"no tab", &browser.Failure{Code: browser.CodeNoTab}, http.StatusNotFound},
		{"deadline", context.DeadlineExceeded, http.StatusGatewayTimeout},
		{"cancelled", context.Canceled, http.StatusRequestTimeout},
		{"invalid step", &browser.Failure{Code: browser.CodeBadStep}, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := control.New(control.Options{})
			t.Cleanup(ctrl.Close)
			server := New(failingBrowserClose{SessionAPI: ctrl, err: tc.err}, NewBroadcaster(), config.ServeConfig{})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/browser/close", strings.NewReader(`{"tab":"t1"}`))
			server.browserClose(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			var body struct {
				Params map[string]any `json:"params"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if code := browser.CodeOf(tc.err); code != "" && body.Params["cause"] != string(code) {
				t.Fatalf("cause = %+v, want %s", body.Params, code)
			}
		})
	}
}
