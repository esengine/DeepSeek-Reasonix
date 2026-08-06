package autoresearch

import "testing"

func TestNextRequiredAction(t *testing.T) {
	tests := []struct {
		name     string
		progress Progress
		want     string
	}{
		{
			name:     "blocked wins over stale escalation",
			progress: Progress{Status: StatusBlocked, StaleCount: 9},
			want:     "resolve blocker before continuing",
		},
		{
			name:     "blocked with zero stale",
			progress: Progress{Status: StatusBlocked},
			want:     "resolve blocker before continuing",
		},
		{
			name:     "stale exactly at escalation threshold",
			progress: Progress{Status: StatusRunning, StaleCount: 4},
			want:     "ask for the smallest external input needed",
		},
		{
			name:     "stale above escalation threshold",
			progress: Progress{Status: StatusRunning, StaleCount: 7},
			want:     "ask for the smallest external input needed",
		},
		{
			name:     "stale between thresholds",
			progress: Progress{Status: StatusRunning, StaleCount: 3},
			want:     "make a structural pivot before continuing",
		},
		{
			name:     "stale exactly at pivot threshold",
			progress: Progress{Status: StatusRunning, StaleCount: 2},
			want:     "make a structural pivot before continuing",
		},
		{
			name:     "stale just below pivot threshold",
			progress: Progress{Status: StatusRunning, StaleCount: 1},
			want:     "continue with the next evidence-producing step",
		},
		{
			name:     "stale zero",
			progress: Progress{Status: StatusRunning},
			want:     "continue with the next evidence-producing step",
		},
		{
			name:     "negative stale treated as zero",
			progress: Progress{Status: StatusRunning, StaleCount: -3},
			want:     "continue with the next evidence-producing step",
		},
		{
			name:     "complete status falls through to stale logic",
			progress: Progress{Status: StatusComplete, StaleCount: 4},
			want:     "ask for the smallest external input needed",
		},
		{
			name:     "complete status with no stale",
			progress: Progress{Status: StatusComplete},
			want:     "continue with the next evidence-producing step",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextRequiredAction(tt.progress); got != tt.want {
				t.Errorf("nextRequiredAction(%+v) = %q, want %q", tt.progress, got, tt.want)
			}
		})
	}
}
