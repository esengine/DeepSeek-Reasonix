package serve

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// A request's cost is counted in the work it does, not in seconds: wall time on a
// small home hides what Windows pays with thousands of sessions and a plugin full
// of skills. Each plugin package parse reads every skill file of the package, so
// parses are counted through the package store's seam; a budget that grows fails.
func TestHotEndpointsStayInsideTheirPackageParseBudget(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	var parses atomic.Int64
	prev := config.SetInstalledPackages(func(string) []config.InstalledPackage {
		parses.Add(1)
		return nil
	})
	defer config.SetInstalledPackages(prev)

	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: testenv.TempDir(t)})
	defer ctrl.Close()
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	defer srv.Close()

	budgets := []struct {
		path   string
		budget int64
	}{
		{"/status", 0},
		{"/context", 0},
		{"/slash", 0},
		{"/capability-scope", 0},
		{"/hooks", 0},
		{"/sessions", 0},
		{"/history", 0},
		{"/history/page", 0},
		{"/skills", 1},
		{"/complete?lang=en&line=/", 0},
		{"/complete?lang=en&line=/mo", 0},
		{"/complete?lang=en&line=/model%20", 3},
	}
	for _, b := range budgets {
		parses.Store(0)
		resp, err := http.Get(srv.URL + b.path)
		if err != nil {
			t.Fatalf("GET %s: %v", b.path, err)
		}
		resp.Body.Close()
		if got := parses.Load(); got > b.budget {
			t.Errorf("GET %s parsed the installed plugin packages %d times, budget %d: each parse reads every skill file of every enabled package, on every request", b.path, got, b.budget)
		}
	}
}
