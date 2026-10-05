package jobs

import (
	"context"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
)

// A long job can be waited on once instead of polled, if the caller can say
// what it is waiting for. The pattern comes from the caller because only the
// caller knows which line matters — a host deciding that would be guessing.
func TestWaitReturnsWhenAJobWritesWhatTheCallerAskedFor(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	release := make(chan struct{})
	j := m.Start("bash", "render", func(ctx context.Context, w io.Writer) (string, error) {
		_, _ = io.WriteString(w, "Downloading 9.5 Mb of 113 Mb\n")
		_, _ = io.WriteString(w, "Downloading 28.6 Mb of 113 Mb\n")
		_, _ = io.WriteString(w, "Rendered 120 frames\n")
		<-release // still running: the wait must return on the line, not the exit
		return "done", nil
	})
	defer close(release)
	defer m.Kill(j.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results, outcome := m.WaitForSession(ctx, "", []string{j.ID}, WaitOptions{
		Match: regexp.MustCompile(`Rendered \d+ frames`),
	})
	if outcome != WaitMatched {
		t.Fatalf("outcome = %q, want %q", outcome, WaitMatched)
	}
	if len(results) != 1 || results[0].Status != Running {
		t.Fatalf("results = %+v, want one still-running job", results)
	}
	// Delta is timing-dependent here — the line may already have been written
	// when the wait began. TestProgressReportsWhatArrivedDuringThisWait owns
	// that reading; this test owns the early return.
	if p := results[0].Progress; p == nil || p.Produced <= 0 {
		t.Fatalf("progress = %+v, want what the job produced", p)
	}
}

// Without a pattern the wait is what it always was: it ends when the job does.
// A match that never appears must not shorten it either.
func TestWaitWithoutAMatchStillWaitsForTheJob(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	j := m.Start("bash", "quick", func(ctx context.Context, w io.Writer) (string, error) {
		_, _ = io.WriteString(w, "working\n")
		return "finished", nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results, outcome := m.WaitForSession(ctx, "", []string{j.ID}, WaitOptions{
		Match: regexp.MustCompile(`this never appears`),
	})
	if outcome != WaitFinished {
		t.Fatalf("outcome = %q, want %q", outcome, WaitFinished)
	}
	if len(results) != 1 || results[0].Status != Done {
		t.Fatalf("results = %+v, want one finished job", results)
	}
}

// Output arrives in whatever chunks the writer chose, so a line the caller is
// waiting for can be split across two of them. Missing it would send the
// caller back to polling, which is the thing this replaces.
func TestAMatchSplitAcrossTwoWritesIsStillSeen(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	release := make(chan struct{})
	j := m.Start("bash", "chunked", func(ctx context.Context, w io.Writer) (string, error) {
		_, _ = io.WriteString(w, "BUILD SUCC")
		time.Sleep(10 * time.Millisecond)
		_, _ = io.WriteString(w, "EEDED in 3s\n")
		<-release
		return "done", nil
	})
	defer close(release)
	defer m.Kill(j.ID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, outcome := m.WaitForSession(ctx, "", []string{j.ID}, WaitOptions{
		Match: regexp.MustCompile(`BUILD SUCCEEDED`),
	})
	if outcome != WaitMatched {
		t.Fatalf("outcome = %q, want the split line to have matched", outcome)
	}
}

// Delta is what separates a task making slow progress from one that stopped,
// and it is a reading rather than a verdict: the same number means different
// things for a download and for a compiler, and only the caller knows which.
func TestProgressReportsWhatArrivedDuringThisWait(t *testing.T) {
	m := NewManager(event.Discard)
	defer m.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	j := m.Start("bash", "slow", func(ctx context.Context, w io.Writer) (string, error) {
		_, _ = io.WriteString(w, strings.Repeat("a", 100))
		close(started)
		<-release
		return "done", nil
	})
	defer close(release)
	defer m.Kill(j.ID)
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// The first hundred bytes landed before this wait began, so they count as
	// produced but not as arrived.
	results, outcome := m.WaitForSession(ctx, "", []string{j.ID}, WaitOptions{Timeout: 50 * time.Millisecond})
	if outcome != WaitTimedOut {
		t.Fatalf("outcome = %q, want %q", outcome, WaitTimedOut)
	}
	p := results[0].Progress
	if p == nil || p.Produced < 100 {
		t.Fatalf("progress = %+v, want everything the job produced", p)
	}
	if p.Delta != 0 {
		t.Fatalf("delta = %d, want nothing to have arrived during this wait", p.Delta)
	}
}
