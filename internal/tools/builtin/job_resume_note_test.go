package builtin

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/tools/jobs"
)

type stubCompletionObserver struct{}

func (stubCompletionObserver) OnJobCompletion(context.Context, jobs.CompletionEvent) error {
	return nil
}

const resumeMarker = "a new turn starts"

func startHeldJob(t *testing.T, m *jobs.Manager, session string) (*jobs.Job, func()) {
	t.Helper()
	release := make(chan struct{})
	j := m.StartForSession(session, "bash", "npm run build", func(jobCtx context.Context, _ io.Writer) (string, error) {
		select {
		case <-release:
		case <-jobCtx.Done():
		}
		return "", nil
	})
	return j, func() { close(release); m.Kill(j.ID) }
}

// A running job's readers say the host resumes the session when it ends, so a
// model that stops polling reports the job as running rather than abandoned.
func TestRunningJobReadersStateTheResume(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	defer m.Close()
	m.SetActiveSessionPath("session", "")
	m.SetCompletionObserver(stubCompletionObserver{})
	ctx := jobs.WithSession(jobs.WithManager(context.Background(), m), "session")
	j, stop := startHeldJob(t, m, "session")
	defer stop()

	out, err := (bashOutput{}).Execute(ctx, []byte(`{"job_id":"`+j.ID+`"}`))
	if err != nil || !strings.Contains(out, resumeMarker) {
		t.Fatalf("bash_output = %q, %v; want the resume note", out, err)
	}
	out, err = (waitJob{}).Execute(ctx, []byte(`{"job_ids":["`+j.ID+`"],"timeout_seconds":1}`))
	if err != nil || !strings.Contains(out, resumeMarker) {
		t.Fatalf("wait = %q, %v; want the resume note", out, err)
	}
}

// The note is the manager's promise, so it is absent wherever the manager
// would not keep it: no observer, another session's job, or a finished job.
func TestResumeNoteOnlyWhereTheHostKeepsIt(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	defer m.Close()
	m.SetActiveSessionPath("session", "")
	ctx := jobs.WithSession(jobs.WithManager(context.Background(), m), "session")
	j, stop := startHeldJob(t, m, "session")
	defer stop()

	if out, _ := (bashOutput{}).Execute(ctx, []byte(`{"job_id":"`+j.ID+`"}`)); strings.Contains(out, resumeMarker) {
		t.Fatalf("no observer: bash_output = %q, want no resume note", out)
	}

	m.SetCompletionObserver(stubCompletionObserver{})
	other := jobs.WithSession(jobs.WithManager(context.Background(), m), "child")
	oj, ostop := startHeldJob(t, m, "child")
	defer ostop()
	if out, _ := (bashOutput{}).Execute(other, []byte(`{"job_id":"`+oj.ID+`"}`)); strings.Contains(out, resumeMarker) {
		t.Fatalf("other session: bash_output = %q, want no resume note", out)
	}

	done := m.StartForSession("session", "bash", "true", func(context.Context, io.Writer) (string, error) { return "ok", nil })
	m.Wait(context.Background(), []string{done.ID}, jobs.WaitOptions{Timeout: 5 * time.Second})
	if out, _ := (bashOutput{}).Execute(ctx, []byte(`{"job_id":"`+done.ID+`"}`)); strings.Contains(out, resumeMarker) {
		t.Fatalf("finished job: bash_output = %q, want no resume note", out)
	}
}
