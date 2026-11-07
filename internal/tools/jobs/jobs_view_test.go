package jobs

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"reasonix/internal/contract/event"
)

// A finished job stays on the session's surface until the session closes, with
// how it ended, when, the command's exit status and its last line of output.
func TestEndedForSessionKeepsHowEachJobEnded(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()
	ok := m.StartForSession("s", "bash", "npm run build", func(_ context.Context, w io.Writer) (string, error) {
		_, _ = io.WriteString(w, "compiling\nbuilt in 1.2s\n\n")
		return "", nil
	})
	bad := m.StartForSession("s", "bash", "go test ./...", func(_ context.Context, w io.Writer) (string, error) {
		_, _ = io.WriteString(w, "=== RUN TestX\nFAIL TestCompactKeepsPins (0.31s)\n")
		return "", &ExitError{Code: 1, Err: errors.New("exit status 1")}
	})
	gone := m.StartForSession("other", "bash", "elsewhere", func(context.Context, io.Writer) (string, error) { return "", nil })
	release := make(chan struct{})
	live := m.StartForSession("s", "bash", "npm run dev", func(_ context.Context, w io.Writer) (string, error) {
		_, _ = io.WriteString(w, "watching for changes\n")
		<-release
		return "", nil
	})
	defer close(release)
	if res, _ := m.WaitForSession(context.Background(), "s", []string{ok.ID, bad.ID}, WaitOptions{Timeout: 5 * time.Second}); len(res) != 2 {
		t.Fatalf("waited for %d jobs, want 2", len(res))
	}
	_, _ = m.WaitForSession(context.Background(), "other", []string{gone.ID}, WaitOptions{Timeout: 5 * time.Second})

	ended := m.EndedForSession("s")
	if len(ended) != 2 {
		t.Fatalf("ended = %+v, want the two finished jobs of this session only", ended)
	}
	by := map[string]View{}
	for _, v := range ended {
		by[v.ID] = v
	}
	if v := by[ok.ID]; v.Status != string(Done) || v.ExitCode == nil || *v.ExitCode != 0 || v.Tail != "built in 1.2s" || v.EndedAt < v.StartedAt || v.EndedAt == 0 {
		t.Fatalf("finished job view = %+v", v)
	}
	if v := by[bad.ID]; v.Status != string(Failed) || v.ExitCode == nil || *v.ExitCode != 1 || v.Tail != "FAIL TestCompactKeepsPins (0.31s)" {
		t.Fatalf("failed job view = %+v", v)
	}
	var running []View
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if running = m.RunningForSession("s"); len(running) == 1 && running[0].Tail != "" {
			break
		}
	}
	if len(running) != 1 || running[0].ID != live.ID || running[0].Tail != "watching for changes" || running[0].EndedAt != 0 || running[0].ExitCode != nil {
		t.Fatalf("running view = %+v, want the live job with its last line and no end", running)
	}
}

// A task job runs no command, so it has no exit status to report.
func TestEndedTaskJobCarriesNoExitStatus(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()
	j := m.StartForSession("s", "task", "review", func(context.Context, io.Writer) (string, error) { return "all good", nil })
	_, _ = m.WaitForSession(context.Background(), "s", []string{j.ID}, WaitOptions{Timeout: 5 * time.Second})
	ended := m.EndedForSession("s")
	if len(ended) != 1 || ended[0].ExitCode != nil {
		t.Fatalf("ended task = %+v, want no exit status", ended)
	}
}
