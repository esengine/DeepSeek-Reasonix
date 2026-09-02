package serve

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

type checkedAnswerAPI struct {
	control.SessionAPI
	err error
}

func (s *checkedAnswerAPI) AnswerQuestionChecked(string, []event.AskAnswer) error { return s.err }

func TestServeComposerProfileAndCheckedAnswer(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	api := &checkedAnswerAPI{SessionAPI: ctrl}
	srv := httptest.NewServer(New(api, bc, config.ServeConfig{}).Handler())
	defer srv.Close()
	post := func(path, body string) int {
		resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if got := post("/composer-profile", `{"collaborationMode":"plan","toolApprovalMode":"yolo","goal":""}`); got != http.StatusOK {
		t.Fatalf("composer profile status = %d, want 200", got)
	}
	if !ctrl.PlanMode() || ctrl.ToolApprovalMode() != control.ToolApprovalYolo || ctrl.Goal() != "" {
		t.Fatalf("composer profile = plan:%v approval:%q goal:%q", ctrl.PlanMode(), ctrl.ToolApprovalMode(), ctrl.Goal())
	}
	api.err = errors.New("ledger unavailable")
	if got := post("/answer", `{"id":"ask-1","answers":[]}`); got != http.StatusServiceUnavailable {
		t.Fatalf("failed answer status = %d, want 503", got)
	}
	api.err = nil
	if got := post("/answer", `{"id":"ask-1","answers":[]}`); got != http.StatusNoContent {
		t.Fatalf("retried answer status = %d, want 204", got)
	}
}
