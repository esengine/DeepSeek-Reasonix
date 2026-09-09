package agent

import (
	"context"
	"sync/atomic"
	"time"
)

// runStragglers counts parallel tool goroutines that outlived their batch's
// grace period. The batch already reported their result as unknown, but they
// can still be inside the agent when the next turn resets per-turn state.
type runStragglers struct{ live atomic.Int64 }

func (s *runStragglers) enter() { s.live.Add(1) }
func (s *runStragglers) leave() { s.live.Add(-1) }

// drain waits up to limit for abandoned goroutines to leave the agent, so the
// next turn never zeroes per-turn state under a live reader. A goroutine that
// ignores its cancelled context past the limit is left to the batch's unknown
// outcome, exactly as before.
func (s *runStragglers) drain(ctx context.Context, limit time.Duration) {
	if s.live.Load() == 0 {
		return
	}
	deadline := time.Now().Add(limit)
	for s.live.Load() > 0 && time.Now().Before(deadline) && ctx.Err() == nil {
		time.Sleep(10 * time.Millisecond)
	}
}
