package serve

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
	"reasonix/internal/tools/jobs"
)

type jobCanceller struct {
	control.SessionAPI
	running   map[string]bool
	unwinding map[string]bool
	asked     []string
}

func (c *jobCanceller) CancelJob(id string) bool {
	c.asked = append(c.asked, id)
	was := c.running[id]
	delete(c.running, id)
	return was
}

func (c *jobCanceller) Jobs() []jobs.View {
	var out []jobs.View
	for id := range c.unwinding {
		out = append(out, jobs.View{ID: id, Kind: "bash", Status: string(jobs.Running)})
	}
	return out
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

// A job already asked to stop stays listed until its process tree has gone. A
// second stop on it says so instead of claiming the job is not running, which
// read as "the list is wrong" while the list was right.
func TestCancelJobSaysSoWhenTheStopIsAlreadyUnderWay(t *testing.T) {
	ctrl := &jobCanceller{unwinding: map[string]bool{"job-9": true}}
	s := &Server{ctrl: ctrl}
	mux := http.NewServeMux()
	s.registerJobRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/jobs/job-9/cancel", nil))
	var reason Reason
	_ = json.Unmarshal(rec.Body.Bytes(), &reason)
	if rec.Code != http.StatusConflict || reason.Code != "job.stop_in_progress" {
		t.Fatalf("stop of a job already shutting down: status %d code %q, want 409 job.stop_in_progress", rec.Code, reason.Code)
	}
	delete(ctrl.unwinding, "job-9")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/jobs/job-9/cancel", nil))
	_ = json.Unmarshal(rec.Body.Bytes(), &reason)
	if reason.Code != "job.not_running" {
		t.Fatalf("once it left the list: code %q, want job.not_running", reason.Code)
	}
}

// /status lists the session's finished jobs apart from the running ones, with
// how each ended, so a window can say what became of a job after it stopped.
func TestStatusListsEndedJobsApartFromRunningOnes(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	m := jobs.NewManager(event.Discard)
	t.Cleanup(m.Close)
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, Jobs: m})
	t.Cleanup(ctrl.Close)
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	t.Cleanup(srv.Close)
	j := m.Start("bash", "go test ./...", func(_ context.Context, w io.Writer) (string, error) {
		_, _ = io.WriteString(w, "FAIL TestX\n")
		return "", &jobs.ExitError{Code: 2, Err: errors.New("exit status 2")}
	})
	_, _ = m.Wait(context.Background(), []string{j.ID}, jobs.WaitOptions{Timeout: 5 * time.Second})

	res, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Jobs      []jobs.View `json:"jobs"`
		EndedJobs []jobs.View `json:"endedJobs"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Jobs) != 0 {
		t.Fatalf("running jobs = %+v, want none", body.Jobs)
	}
	if len(body.EndedJobs) != 1 || body.EndedJobs[0].Status != string(jobs.Failed) || body.EndedJobs[0].ExitCode == nil || *body.EndedJobs[0].ExitCode != 2 || body.EndedJobs[0].Tail != "FAIL TestX" {
		t.Fatalf("ended jobs = %+v, want the failed one with its exit status and last line", body.EndedJobs)
	}
}
