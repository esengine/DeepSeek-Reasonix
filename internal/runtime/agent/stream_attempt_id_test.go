package agent

import "testing"

// Attempt ids key the stream events of one round. Two rounds that start within
// one clock tick must still get different ids, so nothing here may depend on
// the clock advancing between calls.
func TestStreamAttemptIDsNeverRepeat(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := newStreamAttemptID(1)
		if seen[id] {
			t.Fatalf("attempt id %q was issued twice", id)
		}
		seen[id] = true
	}
}
