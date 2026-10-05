package agent

import (
	"fmt"
	"testing"
)

// planCompaction runs on the auto-fold path, where the user is already waiting.
// Measuring each candidate tail whole made its cost the transcript's size times
// the number of messages the boundary had to move — and a copy of the suffix
// per step once any message carried a decision receipt. If either returns, this
// benchmark grows superlinearly in turns and allocates again.
func BenchmarkPlanCompaction(b *testing.B) {
	for _, tc := range []struct {
		turns    int
		cjk      bool
		receipts bool
	}{
		{200, false, false},
		{200, false, true},
		{200, true, false},
		{600, false, true},
	} {
		msgs := foldFixture(tc.turns, tc.cjk, tc.receipts)
		a := calibratedFoldAgent(msgs)
		b.Run(fmt.Sprintf("turns=%d/cjk=%v/receipts=%v", tc.turns, tc.cjk, tc.receipts), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				a.window().planCompaction(msgs, minCompactMessages, false)
			}
		})
	}
}
