package serve

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/session/control"
	"strings"
	"testing"
)

type recoverySource struct {
	control.SessionAPI
	plans []delegation.FleetRecoveryPlan
	err   error
	calls []string
}

func (s *recoverySource) FleetRecoveryPlans(ref string) ([]delegation.FleetRecoveryPlan, error) {
	s.calls = append(s.calls, ref)
	return s.plans, s.err
}
func (s *recoverySource) RequestFleetRecovery(ref, id, snapshot string) error {
	s.calls = append(s.calls, ref+":"+id+":"+snapshot)
	return s.err
}

func TestRecoveryRoutesKeepHostBoundaryAndStrictRequestShape(t *testing.T) {
	s := &recoverySource{plans: []delegation.FleetRecoveryPlan{{ID: "fr_one", Tasks: []delegation.FleetRecoveryTask{{Prompt: "secret sk-abcdefghijklmnop1234"}}}}}
	mux := http.NewServeMux()
	(&Server{ctrl: s}).registerSubagentRoutes(mux)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		r := httptest.NewRequest(method, "/subagents/sa_one/recovery", strings.NewReader(`{"id":"fr_one","snapshot":"v1"}`))
		r = r.WithContext(withDeviceReach(r.Context(), "phone", 1))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != 404 || len(s.calls) != 0 {
			t.Fatalf("device admitted: %d", w.Code)
		}
	}
	w := get(mux, "/subagents/sa_one/recovery")
	if w.Code != 200 || strings.Contains(w.Body.String(), "sk-abcdefghijklmnop1234") {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	for _, body := range []string{`{}`, `{"id":"fr_one","snapshot":"v1","grant":true}`, `{"id":"fr_one","snapshot":"v1"}{}`} {
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/subagents/sa_one/recovery", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("malformed admitted: %d", w.Code)
		}
	}
	for _, fail := range []bool{false, true} {
		want := 202
		if fail {
			s.err = errors.New("plan changed")
			want = 409
		}
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/subagents/sa_one/recovery", strings.NewReader(`{"id":"fr_one","snapshot":"v1"}`)))
		if w.Code != want || s.calls[len(s.calls)-1] != "sa_one:fr_one:v1" {
			t.Fatalf("submit: %d %s", w.Code, w.Body.String())
		}
	}
}
