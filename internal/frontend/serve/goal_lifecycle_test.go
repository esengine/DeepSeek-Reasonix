package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func postGoalLifecycle(t *testing.T, url string) goalLifecycleResult {
	t.Helper()
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s status = %d, want 200", url, resp.StatusCode)
	}
	var out goalLifecycleResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("%s body: %v", url, err)
	}
	return out
}

func TestServeGoalPauseAndResumeKeepTheObjective(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer srv.Close()

	if got := postGoalLifecycle(t, srv.URL+"/goal/resume"); got.Changed {
		t.Fatalf("resume without a goal = %+v, want unchanged", got)
	}
	ctrl.SetGoal("ship the release")
	if got := postGoalLifecycle(t, srv.URL+"/goal/resume"); got.Changed || got.Status != control.GoalStatusRunning {
		t.Fatalf("resume of a running goal = %+v, want unchanged and running", got)
	}
	paused := postGoalLifecycle(t, srv.URL+"/goal/pause")
	if !paused.Changed || paused.Status == control.GoalStatusRunning || paused.Goal != "ship the release" {
		t.Fatalf("pause = %+v, want a paused goal that keeps its objective", paused)
	}
	if got := postGoalLifecycle(t, srv.URL+"/goal/pause"); got.Changed {
		t.Fatalf("second pause = %+v, want unchanged", got)
	}
	resumed := postGoalLifecycle(t, srv.URL+"/goal/resume")
	if !resumed.Changed || resumed.Status != control.GoalStatusRunning || resumed.Goal != "ship the release" {
		t.Fatalf("resume = %+v, want the same goal running", resumed)
	}
}
