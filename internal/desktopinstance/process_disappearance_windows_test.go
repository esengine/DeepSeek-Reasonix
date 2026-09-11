//go:build windows

package desktopinstance

import (
	"errors"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestAccessDeniedCandidateRequiresFreshProcessEvidence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []windows.ProcessEntry32
		err     error
		blocked bool
	}{
		{"exited since snapshot", nil, nil, false},
		{"still present", []windows.ProcessEntry32{{ProcessID: 42}}, nil, true},
		{"cannot refresh snapshot", nil, errors.New("snapshot denied"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := deniedCandidate(42, func() ([]windows.ProcessEntry32, error) { return tc.entries, tc.err })
			if (err != nil) != tc.blocked {
				t.Fatalf("blocked=%t, error=%v", tc.blocked, err)
			}
		})
	}
}

func TestStartupInspectionWaitsForCompleteSnapshotWithinDeadline(t *testing.T) {
	for _, tc := range []struct {
		name      string
		code      Code
		clears    bool
		wantCalls int
	}{
		{"transient owner", UnknownOwner, true, 2},
		{"persistent owner", UnknownOwner, false, 3},
		{"other installation", OtherInstallation, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			left := 400 * time.Millisecond
			_, err := waitForInspectable(func() ([]*process, error) {
				calls++
				if tc.clears && calls > 1 {
					return nil, nil
				}
				return nil, outcome(tc.code, "unverified")
			}, func() time.Duration { return left }, func(d time.Duration) { left -= d })
			if calls != tc.wantCalls || (err == nil) != tc.clears {
				t.Fatalf("calls=%d, error=%v", calls, err)
			}
		})
	}
}
