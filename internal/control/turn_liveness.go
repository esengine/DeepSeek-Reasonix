package control

import (
	"fmt"
	"sync/atomic"
	"time"

	"reasonix/internal/event"
)

// turnStallThreshold is the silence after which a running turn is reported as
// possibly stuck. It only warns: the user decides whether to stop, because a
// legitimately long tool and a wedged one look identical from here.
var turnStallThreshold atomic.Int64

func init() { turnStallThreshold.Store(int64(10 * time.Minute)) }

// turnLiveness remembers the last event a running turn produced so a silent
// stretch can be surfaced instead of leaving "working" unexplained.
type turnLiveness struct {
	lastEvent atomic.Int64
	warned    atomic.Bool
}

func (l *turnLiveness) reset(now time.Time) {
	l.lastEvent.Store(now.UnixNano())
	l.warned.Store(false)
}

func (l *turnLiveness) observe(e event.Event, now time.Time) {
	if e.Kind == event.Notice && e.Code == event.NoticeCodeTurnStalled {
		return
	}
	l.lastEvent.Store(now.UnixNano())
	l.warned.Store(false)
}

// stalledFor claims the single warning for the current silence.
func (l *turnLiveness) stalledFor(now time.Time) (time.Duration, bool) {
	last := l.lastEvent.Load()
	if last == 0 {
		return 0, false
	}
	silence := now.Sub(time.Unix(0, last))
	if silence < time.Duration(turnStallThreshold.Load()) {
		return 0, false
	}
	return silence, l.warned.CompareAndSwap(false, true)
}

func (c *Controller) warnIfTurnStalled(now time.Time) {
	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	if !running {
		return
	}
	silence, ok := c.liveness.stalledFor(now)
	if !ok {
		return
	}
	c.sink.Emit(event.Event{
		Kind:  event.Notice,
		Code:  event.NoticeCodeTurnStalled,
		Level: event.LevelWarn,
		Text:  fmt.Sprintf("No progress for %s. The turn is still running; press Stop if it looks stuck.", silence.Round(time.Minute)),
	})
}
