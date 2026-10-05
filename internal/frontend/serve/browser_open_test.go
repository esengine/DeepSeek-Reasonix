package serve

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/platform/browser"
	"reasonix/internal/session/control"
)

// refusingPage answers the commands a Session sends, and fails the navigation
// the way a dev server that is not listening does.
type refusingPage struct {
	in     chan []byte
	closed chan struct{}
	once   sync.Once
}

func (p *refusingPage) ReadMessage() ([]byte, error) {
	select {
	case m := <-p.in:
		return m, nil
	case <-p.closed:
		return nil, io.EOF
	}
}

func (p *refusingPage) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

func (p *refusingPage) WriteMessage(raw []byte) error {
	var msg struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return err
	}
	result := map[string]any{}
	switch msg.Method {
	case "Target.createTarget":
		result["targetId"] = "view-1"
	case "Target.attachToTarget":
		result["sessionId"] = "S-view-1"
	case "Page.getFrameTree":
		result["frameTree"] = map[string]any{"frame": map[string]any{"id": "F1", "url": "about:blank"}}
	case "Page.navigate":
		result["errorText"] = "net::ERR_CONNECTION_REFUSED"
	}
	reply, _ := json.Marshal(map[string]any{"id": msg.ID, "result": result})
	p.in <- reply
	return nil
}

// A page that fails to load still has its tab in the window, so the answer
// must say so: a caller that reads the refusal as "nothing opened" sends the
// same address to the machine's own browser as well.
func TestBrowserOpenNamesTheTabAFailedNavigationLeftBehind(t *testing.T) {
	pool := &browser.Pool{}
	pool.SetEndpoint(func(context.Context, string) (browser.Endpoint, error) {
		return &refusingPage{in: make(chan []byte, 64), closed: make(chan struct{})}, nil
	})
	session := browser.NewSession(browser.Config{Launch: browser.LaunchSpec{ProfileDir: "/profiles/w1"}, Pool: pool})
	ctrl := control.New(control.Options{BrowserSession: session})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/browser/open", "application/json", strings.NewReader(`{"url":"http://localhost:5173/","newTab":true}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var reason Reason
	if err := json.NewDecoder(resp.Body).Decode(&reason); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest || reason.Code != "browser.open_failed" {
		t.Fatalf("POST /browser/open = %d %+v", resp.StatusCode, reason)
	}
	if tab, _ := reason.Params["tab"].(string); tab == "" {
		t.Fatalf("params = %v, want the id of the tab that holds the failed page", reason.Params)
	}
	if tabs := session.Tabs(); len(tabs) != 1 {
		t.Fatalf("tabs = %+v, want the failed page's tab to exist", tabs)
	}
}
