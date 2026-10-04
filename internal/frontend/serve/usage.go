// usage.go — what this machine has spent, read back out of the stats files.
package serve

import (
	"fmt"
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
	q := r.URL.Query()
	fromRaw, toRaw := q.Get("from"), q.Get("to")
	if fromRaw != "" || toRaw != "" {
		if fromRaw == "" || toRaw == "" {
			return time.Time{}, time.Time{}, fmt.Errorf("from and to must be provided together")
		}
		loc := time.Now().Location()
		from, err := time.ParseInLocation(usageDayLayout, fromRaw, loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("from must be YYYY-MM-DD")
		}
		toDay, err := time.ParseInLocation(usageDayLayout, toRaw, loc)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("to must be YYYY-MM-DD")
		}
		if toDay.Before(from) || from.Before(toDay.AddDate(0, 0, -(usageMaxDays-1))) {
			return time.Time{}, time.Time{}, fmt.Errorf("range must be between 1 and 365 days")
		}
		to := time.Date(toDay.Year(), toDay.Month(), toDay.Day(), 23, 59, 59, 0, loc)
		return from, to, nil
	}

	days := usageDefaultDays
	if raw := q.Get("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > usageMaxDays {
			return time.Time{}, time.Time{}, fmt.Errorf("days must be between 1 and 365")
		}
		days = parsed
	}
	// Whole days in local time: a range that ended mid-afternoon would drop the
	// morning's turns from "today".
	now := time.Now()
	to := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, now.Location())
	return to.AddDate(0, 0, -(days - 1)), to, nil
}

// usage answers the panel's one question: what did the last N days cost, in
// tokens and in money. Money reads in the currency this session's costs do, so
// the panel and the pane never disagree. Nothing here writes.
func (s *Server) usage(w http.ResponseWriter, r *http.Request) {
	from, to, err := usageWindow(r)
	if err != nil {
		refuse(w, http.StatusBadRequest, codeBadValue, err.Error(), nil)
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
