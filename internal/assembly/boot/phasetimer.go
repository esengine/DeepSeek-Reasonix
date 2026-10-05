package boot

import (
	"log/slog"
	"strings"
	"time"
)

// Phase is one named stretch of an assembly, in the order it ran. A model or
// effort switch is a full rebuild, so this is what a user experiences as the
// switch being slow.
type Phase struct {
	Name string        `json:"name"`
	D    time.Duration `json:"d"`
}

type phaseTimer struct {
	start   time.Time
	last    time.Time
	phases  []Phase
	observe func(Phase)
}

func newPhaseTimer() *phaseTimer {
	now := time.Now()
	return &phaseTimer{start: now, last: now}
}

// mark closes the stretch that ended here and names it.
func (t *phaseTimer) mark(name string) {
	if t == nil {
		return
	}
	now := time.Now()
	t.phases = append(t.phases, Phase{Name: name, D: now.Sub(t.last)})
	t.last = now
	if t.observe != nil {
		t.observe(t.phases[len(t.phases)-1])
	}
}

// done closes the last stretch and logs the assembly as one record. Phases are
// logged in run order, not sorted: what is slow matters less than where in the
// sequence it sits, since everything here is sequential.
func (t *phaseTimer) done(name string) []Phase {
	if t == nil {
		return nil
	}
	t.mark(name)
	var b strings.Builder
	for i, p := range t.phases {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(p.Name)
		b.WriteByte('=')
		b.WriteString(p.D.Round(time.Millisecond).String())
	}
	slog.Debug("boot: assembly timing", "total", time.Since(t.start).Round(time.Millisecond).String(), "phases", b.String())
	return t.phases
}
