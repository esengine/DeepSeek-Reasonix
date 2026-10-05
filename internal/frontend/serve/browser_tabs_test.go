package serve

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// A pane with no browser answers an empty list, never null: the page reads the
// tabs on every change frame and a null is a crash one frame later.
func TestBrowserTabsIsAnEmptyListWithoutABrowser(t *testing.T) {
	ctrl := control.New(control.Options{})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/browser/tabs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("GET /browser/tabs = %d %q", resp.StatusCode, body)
	}
}
