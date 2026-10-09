package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/session/control"
)

func conflictCodeOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	var got struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got.Code
}

// The write is on disk by the time the rebuild is refused, so a runtime that
// still has work is a save that applies later, not a malfunction: every route
// that saves then rebuilds must answer with that identity.
func TestSavesWhileBusyReportSavedNotFailed(t *testing.T) {
	for _, tc := range []struct{ name, path, body string }{
		{"compaction", "/compaction", `{"soft_limit_tokens":100000}`},
		{"shell", "/shell", `{"prefer":"","path":""}`},
		{"roles", "/roles", `{"role":"planner","ref":""}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newRichProviderServerAs(t, func(c control.SessionAPI) control.SessionAPI { return midTurn{c} })
			if got := conflictCodeOf(t, postProvider(t, srv.URL, tc.path, tc.body)); got != "runtime.saved_while_running" {
				t.Fatalf("code = %q, want runtime.saved_while_running", got)
			}
		})
	}
}

func TestRebuildFailedKeepsGenuineFailuresDistinct(t *testing.T) {
	rec := httptest.NewRecorder()
	rebuildFailed(rec, errors.New("boom"))
	if got := conflictCodeOf(t, rec.Result()); got != "runtime.rebuild_failed" {
		t.Fatalf("code = %q, want runtime.rebuild_failed", got)
	}
}
