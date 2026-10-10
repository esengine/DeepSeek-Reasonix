package control

import "time"

const currentDateTag = "current-date"

// dateAnchor is the session's calendar date as the model is told it. The date
// is delivered once and re-owed only when it differs from what the model already
// holds, so every turn on one day carries identical bytes and a turn after
// midnight carries the new date.
type dateAnchor struct {
	now  func() time.Time
	debt projectionDebt
}

// A controller built without a clock tells the model no date: the assembly
// that owns the host decides what the clock is, and this layer never reads the
// system's.
func newDateAnchor(clock func() time.Time) dateAnchor { return dateAnchor{now: clock} }

// owed returns the block this turn owes, empty when the model already has
// today's date.
func (d *dateAnchor) owed() string {
	if d.now == nil {
		return ""
	}
	t := d.now()
	return d.debt.owed("Today's date is " + t.Format("2006-01-02") + " (" + t.Weekday().String() +
		"); the local time zone is UTC" + t.Format("-07:00") + ".")
}
