package serve

import (
	"encoding/json"
	"net/http"
	"testing"

	"reasonix/internal/session/control"
)

type idleWithJobs struct {
	control.SessionAPI
	jobs int
}

func (c idleWithJobs) RuntimeStatus() control.RuntimeStatus {
	return control.RuntimeStatus{BackgroundJobs: c.jobs}
}

type switchRefusal struct {
	Code   string         `json:"code"`
	Params map[string]any `json:"params"`
}

func switchRefusedAs(t *testing.T, wrap func(control.SessionAPI) control.SessionAPI) switchRefusal {
	t.Helper()
	srv := newRichProviderServerAs(t, wrap)
	resp := postProvider(t, srv.URL, "/model", `{"ref":"rich/beta"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		b, _ := readAllString(resp)
		t.Fatalf("POST /model = %d, want 409: %s", resp.StatusCode, b)
	}
	var got switchRefusal
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got
}

// A turn in flight and jobs left running after a turn ended are different
// states with different ways out, so the refusal names which one it is.
func TestModelSwitchRefusalSaysTurnOrJobs(t *testing.T) {
	if got := switchRefusedAs(t, func(c control.SessionAPI) control.SessionAPI { return midTurn{c} }); got.Code != "busy.switch_model" {
		t.Fatalf("mid-turn code = %q, want busy.switch_model", got.Code)
	}
	got := switchRefusedAs(t, func(c control.SessionAPI) control.SessionAPI { return idleWithJobs{c, 2} })
	if got.Code != "busy.switch_model_jobs" {
		t.Fatalf("jobs-only code = %q, want busy.switch_model_jobs", got.Code)
	}
	if n, _ := got.Params["count"].(float64); n != 2 {
		t.Fatalf("jobs-only params = %v, want count 2", got.Params)
	}
}

// Saving a window rebuilds the runtime; with only jobs running that is still
// a save waiting on work, not a malfunction.
func TestContextWindowSaveWithJobsOnlySaysItAppliesLater(t *testing.T) {
	srv := newRichProviderServerAs(t, func(c control.SessionAPI) control.SessionAPI { return idleWithJobs{c, 1} })
	resp := declareWindow(t, srv.URL, `{"window":200000}`)
	defer resp.Body.Close()
	var got switchRefusal
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusConflict || got.Code != "context.window_after_this_turn" {
		t.Fatalf("status %d code %q, want 409 context.window_after_this_turn", resp.StatusCode, got.Code)
	}
}
