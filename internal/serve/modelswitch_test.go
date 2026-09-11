package serve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/provider"
)

// TestControllerAccessorIsRaceSafe guards the switchModel concurrency contract:
// handlers read the controller through ctl() while a swap runs under the write
// lock. With the lock removed this fails under `go test -race` (the CI race job).
func TestControllerAccessorIsRaceSafe(t *testing.T) {
	a, b := &control.Controller{}, &control.Controller{}
	s := &Server{ctrl: a}

	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			if got := s.ctl(); got != a && got != b {
				t.Errorf("ctl() returned a pointer that was never set")
			}
		})
	}
	for range 16 {
		wg.Go(func() {
			s.mu.Lock()
			if s.ctrl == a {
				s.ctrl = b
			} else {
				s.ctrl = a
			}
			s.mu.Unlock()
		})
	}
	wg.Wait()
}

func TestCanonicalRuntimeModelRefUsesCatalogOwnedValue(t *testing.T) {
	ctrl := control.New(control.Options{ProviderResolver: &provider.StaticResolver{
		Descriptors: []provider.Descriptor{{Ref: "safe/model"}},
	}})
	defer ctrl.Close()
	s := &Server{ctrl: ctrl}

	ref, err := s.canonicalRuntimeModelRef("  safe/model  ")
	if err != nil || ref != "safe/model" {
		t.Fatalf("canonical ref = %q, %v", ref, err)
	}
	if _, err := s.canonicalRuntimeModelRef("attacker/model"); err == nil {
		t.Fatal("unknown request model was not rejected")
	}
}

// TestModelAndEffortRoutesValidateInput pins the HTTP routes for model and
// effort switching: registered (not 404) and rejecting invalid bodies
// before any controller work. Switch semantics are covered by the
// switchModel / switch_recovery tests.
func TestModelAndEffortRoutesValidateInput(t *testing.T) {
	s := &Server{ctrl: &control.Controller{}, auth: newAuthGate(config.ServeConfig{AuthMode: "none"})}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()

	for _, tc := range []struct{ path, body string }{
		{"/model", `{"ref":""}`},
		{"/model", `not-json`},
		{"/effort", `{"level":""}`},
		{"/effort", `not-json`},
	} {
		resp, err := http.Post(srv.URL+tc.path, "application/json", strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("POST %s %s = %d, want 400", tc.path, tc.body, resp.StatusCode)
		}
	}
}
