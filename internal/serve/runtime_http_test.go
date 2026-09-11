package serve

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/jobs"
)

func postRuntimeJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestGoalPauseAndResumeRoutes(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	ctrl.SetGoal("ship the remote surface")
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer srv.Close()

	resp := postRuntimeJSON(t, srv.URL+"/goal/pause", `{}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || ctrl.GoalStatus() != control.GoalStatusBlocked {
		t.Fatalf("pause status/goal = %d/%q", resp.StatusCode, ctrl.GoalStatus())
	}
	resp = postRuntimeJSON(t, srv.URL+"/goal/resume", `{}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || ctrl.GoalStatus() != control.GoalStatusRunning {
		t.Fatalf("resume status/goal = %d/%q", resp.StatusCode, ctrl.GoalStatus())
	}
}

func TestQualityFloorRouteUpdatesStatus(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer srv.Close()

	resp := postRuntimeJSON(t, srv.URL+"/quality-floor", `{"floor":"delivery"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || ctrl.QualityFloor() != control.QualityFloorDelivery {
		t.Fatalf("quality floor status/value = %d/%q", resp.StatusCode, ctrl.QualityFloor())
	}
	status, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer status.Body.Close()
	var payload struct {
		QualityFloor string `json:"qualityFloor"`
	}
	if err := json.NewDecoder(status.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.QualityFloor != control.QualityFloorDelivery {
		t.Fatalf("status qualityFloor = %q", payload.QualityFloor)
	}

	bad := postRuntimeJSON(t, srv.URL+"/quality-floor", `{"floor":"turbo"}`)
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid quality floor status = %d", bad.StatusCode)
	}
}

func TestJobsCancelRouteCancelsOwnedJobs(t *testing.T) {
	bc := NewBroadcaster()
	manager := jobs.NewManager(bc)
	ctrl := control.New(control.Options{Sink: bc, Jobs: manager})
	defer ctrl.Close()
	job := manager.Start("task", "verify", func(ctx context.Context, _ io.Writer) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	defer srv.Close()

	resp := postRuntimeJSON(t, srv.URL+"/jobs/cancel", `{"ids":["`+job.ID+`","`+job.ID+`","missing"]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cancel status = %d", resp.StatusCode)
	}
	var result struct {
		Cancelled  []string `json:"cancelled"`
		NotRunning []string `json:"notRunning"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Cancelled) != 1 || result.Cancelled[0] != job.ID {
		t.Fatalf("cancelled = %v", result.Cancelled)
	}
	if len(result.NotRunning) != 1 || result.NotRunning[0] != "missing" {
		t.Fatalf("not running = %v", result.NotRunning)
	}
}
