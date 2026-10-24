package builtin

import (
	"strings"
	"testing"
	"time"

	"reasonix/internal/tools/jobs"
)

// What a wait reports must separate the two readings a caller acts on: a job
// that produced output text, and one that advanced without writing here at all.
func TestWaitProgressDistinguishesOutputFromSteps(t *testing.T) {
	cases := []struct {
		name     string
		progress *jobs.Progress
		want     []string
		absent   []string
	}{
		{
			name:     "no readings yet",
			progress: &jobs.Progress{Running: 90 * time.Second},
			want:     []string{"no output text and no steps reported yet"},
			absent:   []string{"step(s) reported"},
		},
		{
			name:     "steps without output text",
			progress: &jobs.Progress{Running: 90 * time.Second, Steps: 12, StepDelta: 3},
			want:     []string{"no output text (it does not write here)", "12 step(s) reported", "3 while waiting"},
			absent:   []string{"of output"},
		},
		{
			name:     "output text",
			progress: &jobs.Progress{Running: 90 * time.Second, Produced: 2048, Delta: 1024},
			want:     []string{"2.0 KB of output", "1.0 KB of it while waiting"},
			absent:   []string{"step(s) reported"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := stillRunning(tc.progress)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Fatalf("stillRunning = %q, want %q", got, w)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(got, a) {
					t.Fatalf("stillRunning = %q, want no %q", got, a)
				}
			}
		})
	}
}
