package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/session/control"
)

type jobCanceller struct {
	control.SessionAPI
	running map[string]bool
	asked   []string
}

func (c *jobCanceller) CancelJob(id string) bool {
	c.asked = append(c.asked, id)
	was := c.running[id]
	delete(c.running, id)
	return was
}

func TestCancelJobStopsARunningJobAndRefusesOneThatEnded(t *testing.T) {
	ctrl := &jobCanceller{running: map[string]bool{"job 1": true}}
	s := &Server{ctrl: ctrl}
	mux := http.NewServeMux()
	s.registerJobRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/jobs/job%201/cancel", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("first cancel: status %d, want 204", rec.Code)
	}
	if len(ctrl.asked) != 1 || ctrl.asked[0] != "job 1" {
		t.Fatalf("controller asked %q, want the decoded id", ctrl.asked)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/jobs/job%201/cancel", nil))
	var reason Reason
	_ = json.Unmarshal(rec.Body.Bytes(), &reason)
	if rec.Code != http.StatusConflict || reason.Code != "job.not_running" {
		t.Fatalf("second cancel: status %d code %q, want 409 job.not_running", rec.Code, reason.Code)
	}
}
