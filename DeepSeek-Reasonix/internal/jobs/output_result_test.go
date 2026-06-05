package jobs

import (
	"context"
	"io"
	"testing"

	"reasonix/internal/event"
)

// TestOutputSurfacesResultForBufferlessJob probes a task-style job: its run func
// returns a final answer and writes nothing to the streamed buffer (task ignores
// the io.Writer). bash_output reads only the buffer, so once such a job finishes
// its answer is invisible there — yet bash_output's description claims it works
// for task(run_in_background). wait sees the result; bash_output should too.
func TestOutputSurfacesResultForBufferlessJob(t *testing.T) {
	m := NewManager(event.Discard)
	j := m.Start("task", "demo", func(ctx context.Context, _ io.Writer) (string, error) {
		return "THE-ANSWER", nil
	})
	<-j.done

	text, status, ok := m.Output(j.ID)
	if !ok {
		t.Fatal("job not found")
	}
	if status != Done {
		t.Fatalf("status = %q, want done", status)
	}
	if text == "" {
		t.Errorf("bash_output returned no output for a finished task job — its answer %q is invisible", "THE-ANSWER")
	}
}
