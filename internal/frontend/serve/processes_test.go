package serve

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/process"
	"reasonix/internal/session/control"
	"reasonix/internal/tools/jobs"
)

type processSource struct {
	control.SessionAPI
	procs []process.Process
	err   error
	calls int
}

func (c *processSource) Processes() ([]process.Process, error) {
	c.calls++
	return c.procs, c.err
}

func processMux(src *processSource) *http.ServeMux {
	mux := http.NewServeMux()
	(&Server{ctrl: src}).registerRunningWorkRoutes(mux)
	return mux
}

func TestProcessesAreListedWithStateAndCause(t *testing.T) {
	started := time.UnixMilli(1_760_000_000_000)
	src := &processSource{procs: []process.Process{
		{ID: "subagent:sa_1", Kind: process.KindSubagent, Parent: "turn:t3", Name: "audit", State: process.Queued, Cause: process.CauseWriters, Started: started},
		{ID: "job:bash-1", Kind: process.KindJob, Name: "sleep 60", State: process.Running, Started: started},
	}}
	rec := get(processMux(src), "/processes")
	var got []processView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != 200 {
		t.Fatalf("status %d body %s err %v", rec.Code, rec.Body.String(), err)
	}
	if len(got) != 2 || got[0].ID != "subagent:sa_1" || got[0].State != "queued" || got[0].Cause != "writers" || got[0].Parent != "turn:t3" || got[0].StartedAt != 1_760_000_000_000 {
		t.Fatalf("processes = %+v", got)
	}
	if got[1].Cause != "" || got[1].Parent != "" || got[1].Kind != "job" {
		t.Fatalf("a job has no parent and no cause, got %+v", got[1])
	}
}

func TestNoProcessesIsAnEmptyListAndNothingIsReadUnasked(t *testing.T) {
	src := &processSource{}
	mux := processMux(src)
	if src.calls != 0 {
		t.Fatalf("registering the route read the process table %d times", src.calls)
	}
	if rec := get(mux, "/processes"); rec.Body.String() != "[]\n" {
		t.Fatalf("no processes must be an empty list, got %q", rec.Body.String())
	}
	if src.calls != 1 {
		t.Fatalf("one request read the table %d times, want 1", src.calls)
	}
}

func TestAFailedReadIsACodedRefusalNotAnEmptyList(t *testing.T) {
	rec := get(processMux(&processSource{err: errors.New("disk gone")}), "/processes")
	var reason Reason
	if err := json.Unmarshal(rec.Body.Bytes(), &reason); err != nil || rec.Code != http.StatusInternalServerError || reason.Code != "process.read_failed" {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestARealControllerReportsItsRunningJobOverTheRoute(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	t.Cleanup(m.Close)
	job := m.Start("bash", "sleep 60", func(ctx context.Context, _ io.Writer) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	ctrl := control.New(control.Options{Sink: event.Discard, Jobs: m})
	mux := http.NewServeMux()
	(&Server{ctrl: ctrl}).registerRunningWorkRoutes(mux)

	var got []processView
	rec := get(mux, "/processes")
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != 200 {
		t.Fatalf("status %d body %s err %v", rec.Code, rec.Body.String(), err)
	}
	if len(got) != 1 || got[0].ID != "job:"+job.ID || got[0].State != "running" || got[0].Name != "sleep 60" || got[0].StartedAt == 0 {
		t.Fatalf("processes = %+v, want the running job", got)
	}
	m.Kill(job.ID)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if rec := get(mux, "/processes"); rec.Body.String() == "[]\n" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("a killed job is still listed as running")
}

func BenchmarkProcessesRoute(b *testing.B) {
	m := jobs.NewManager(event.Discard)
	b.Cleanup(m.Close)
	for range 20 {
		m.Start("bash", "sleep 60", func(ctx context.Context, _ io.Writer) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		})
	}
	mux := http.NewServeMux()
	(&Server{ctrl: control.New(control.Options{Sink: event.Discard, Jobs: m})}).registerRunningWorkRoutes(mux)
	b.ReportAllocs()
	for range b.N {
		if rec := get(mux, "/processes"); rec.Code != 200 {
			b.Fatalf("status %d", rec.Code)
		}
	}
}
