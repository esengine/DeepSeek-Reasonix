package team

import "time"

// Bounds on what one agent may send. They stop runaway loops and fan-outs;
// the handoff tree's upstream rule is what stops a cycle.
const (
	MaxMessageRunes  = 8000
	MaxDepth         = 6
	MaxOpenPerSender = 16
	SendsPerMinute   = 12
	FanOutPerTurn    = 8
)

const maxTrackedTurns = 256

// sendLimits is the in-memory send history the rate bounds are judged on; it
// is not durable, so a restart forgives a burst.
type sendLimits struct {
	recent  map[string][]time.Time
	turns   map[string]int
	creates map[string]int
}

func (l *sendLimits) admit(sender, turn string, now time.Time) bool {
	if l.recent == nil {
		l.recent, l.turns = map[string][]time.Time{}, map[string]int{}
	}
	window := l.recent[sender][:0]
	for _, at := range l.recent[sender] {
		if now.Sub(at) < time.Minute {
			window = append(window, at)
		}
	}
	l.recent[sender] = window
	if len(window) >= SendsPerMinute || (turn != "" && l.turns[turn] >= FanOutPerTurn) {
		return false
	}
	l.recent[sender] = append(window, now)
	if turn != "" {
		if len(l.turns) >= maxTrackedTurns {
			clear(l.turns)
		}
		l.turns[turn]++
	}
	return true
}
