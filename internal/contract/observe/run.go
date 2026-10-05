package observe

import (
	"strconv"
	"strings"
	"time"
)

// RunContext is what a scheduled run knows about itself. It reaches the model
// on the turn tail and nowhere else.
type RunContext struct {
	ScheduleID      string
	TriggerID       string
	Slot            time.Time
	RemainingTokens int64
}

// Block renders the standing rule for the run's turns. The identifiers are
// host-issued; control characters and angle brackets are removed anyway so a
// value can never close the block it sits in.
func (r RunContext) Block(p Posture) string {
	var b strings.Builder
	b.WriteString("schedule: " + clean(r.ScheduleID) + "\n")
	b.WriteString("trigger: " + clean(r.TriggerID) + "\n")
	if !r.Slot.IsZero() {
		b.WriteString("slot: " + r.Slot.UTC().Format(time.RFC3339) + "\n")
	}
	if r.RemainingTokens > 0 {
		b.WriteString("tokens_remaining: " + strconv.FormatInt(r.RemainingTokens, 10) + "\n")
	}
	b.WriteString("posture: " + p.Name + " (read-only; enforcement=" + p.Enforcement + "; sandbox=" + p.Sandbox + ")\n")
	b.WriteString("Unattended: nobody can answer this run. A request that needs a person is parked as a pending decision and is never granted here; do what needs none of them, then conclude with what you found and what is parked.")
	return b.String()
}

func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '<' || r == '>' {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
}
