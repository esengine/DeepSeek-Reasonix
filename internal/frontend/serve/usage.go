// usage.go — what this machine has spent, read back out of the stats files.
package serve

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/state/stats"
)

const (
	usageDefaultDays = 30
	usageMaxDays     = 365
	usageDayLayout   = "2006-01-02"
)

// usageWindow reads either a fixed trailing window or an explicit inclusive
// date range. Explicit dates let the panel ask for a past calendar month
// without pretending it ended today.
func usageWindow(r *http.Request) (time.Time, time.Time, error) {
	return usageWindowIn(r, time.Local)
}

// usageWindowIn is usageWindow with the zone that trailing windows count whole
// days in; explicit ranges never consult it.
func usageWindowIn(r *http.Request, loc *time.Location) (time.Time, time.Time, error) {
	q := r.URL.Query()
	fromRaw, toRaw, daysRaw := q.Get("from"), q.Get("to"), q.Get("days")
	hasRange := fromRaw != "" || toRaw != ""
	if hasRange && daysRaw != "" {
		return time.Time{}, time.Time{}, usageBadValue(
			"from/to and days cannot be combined", "range", "days or from/to")
	}
	if hasRange {
		if fromRaw == "" || toRaw == "" {
			return time.Time{}, time.Time{}, usageBadValue(
				"from and to must be provided together", "range", "from and to together")
		}
		// A date input sends a calendar day, not an instant. UTC is only a
		// zone-less carrier; the stats query uses the formatted day, so a
		// remote serve process cannot reinterpret the browser's selection.
		from, err := time.Parse(usageDayLayout, fromRaw)
		if err != nil {
			return time.Time{}, time.Time{}, usageBadValue(
				"from must be YYYY-MM-DD", "from", "YYYY-MM-DD")
		}
		toDay, err := time.Parse(usageDayLayout, toRaw)
		if err != nil {
			return time.Time{}, time.Time{}, usageBadValue(
				"to must be YYYY-MM-DD", "to", "YYYY-MM-DD")
		}
		if toDay.Before(from) || from.Before(toDay.AddDate(0, 0, -(usageMaxDays-1))) {
			return time.Time{}, time.Time{}, usageBadValue(
				"range must be between 1 and 365 days", "range", "1-365")
		}
		to := time.Date(toDay.Year(), toDay.Month(), toDay.Day(), 23, 59, 59, 0, time.UTC)
		return from, to, nil
	}

	days := usageDefaultDays
	if daysRaw != "" {
		parsed, err := strconv.Atoi(daysRaw)
		if err != nil || parsed < 1 || parsed > usageMaxDays {
			return time.Time{}, time.Time{}, usageBadValue(
				"days must be between 1 and 365", "days", "1-365")
		}
		days = parsed
	}
	// Whole days in local time: a range that ended mid-afternoon would drop the
	// morning's turns from "today".
	now := time.Now().In(loc)
	to := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())
	return to.AddDate(0, 0, -(days - 1)), to, nil
}

// usageBadValue is a typed bad-value refusal. The caller renders it without
// having to read the message text to learn which query field was bad.
func usageBadValue(message, field, allowed string) error {
	return refusal(http.StatusBadRequest, codeBadValue, errors.New(message), map[string]any{
		"field": field, "allowed": allowed,
	})
}

// usage answers the panel's one question: what did the last N days cost, in
// tokens and in money. Money reads in the currency this session's costs do, so
// the panel and the pane never disagree. Nothing here writes.
func (s *Server) usage(w http.ResponseWriter, r *http.Request) {
	from, to, err := usageWindow(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	report, err := stats.NewWriter(config.StatsDir()).Query(stats.SourceFilter{
		Source:   r.URL.Query().Get("source"),
		From:     from,
		To:       to,
		Currency: s.bc.DisplayCurrency(),
	})
	if err != nil {
		refuse(w, http.StatusInternalServerError, "internal.failed", "could not read the usage records", nil)
		return
	}
	writeJSON(w, report)
}
