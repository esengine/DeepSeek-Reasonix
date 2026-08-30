package main

import (
	"strconv"
	"strings"
	"time"
)

// parseInterval converts a string like "5m", "1h", "30s" to time.Duration.
// Suffix after '|' is stripped (e.g. "24h|daily@09:00" -> "24h").
// Empty or invalid strings return 0, nil (task will be skipped).
func parseInterval(s string) (time.Duration, error) {
	if idx := strings.Index(s, "|"); idx >= 0 {
		s = s[:idx]
	}
	if len(s) == 0 {
		return 0, nil
	}
	// Support common suffixed intervals
	switch s[len(s)-1] {
	case 's', 'm', 'h':
		return time.ParseDuration(s)
	default:
		// Try "Xm" as default assumption
		return time.ParseDuration(s + "m")
	}
}

// isCronExpr returns true when s looks like a valid 5-field cron expression
// (e.g. "0 * * * *", "*/15 * * * *", "0 9 * * 1-5"). Rejects expressions with
// out-of-range values (e.g. "99 * * * *") that would never match.
func isCronExpr(s string) bool {
	fields := strings.Fields(s)
	if len(fields) != 5 {
		return false
	}
	// min, hour, dom, month, dow — dom/month are 1-based (0 can never match
	// time.Day()/time.Month()), dow is 0-7 (7 = Sunday alias, matched in cronDue).
	limits := []int{59, 23, 31, 12, 7}
	mins := []int{0, 0, 1, 1, 0}
	for i, f := range fields {
		if f == "" {
			return false
		}
		for _, c := range f {
			if !strings.ContainsRune("0123456789*/-,", c) {
				return false
			}
		}
		// Reject out-of-range values in each field, plus zero/empty step
		// values ("*/0" never fires) and descending ranges ("5-1" never matches).
		for part := range strings.SplitSeq(f, ",") {
			part = strings.TrimSpace(part)
			base := part
			if stepBase, stepText, ok := strings.Cut(part, "/"); ok {
				step, err := strconv.Atoi(stepText)
				if err != nil || step < 1 {
					return false
				}
				base = stepBase
			}
			if base == "*" {
				continue
			}
			if loText, hiText, ok := strings.Cut(base, "-"); ok {
				lo, err1 := strconv.Atoi(loText)
				hi, err2 := strconv.Atoi(hiText)
				if err1 != nil || err2 != nil || lo < mins[i] || hi > limits[i] || lo > hi {
					return false
				}
			} else {
				v, err := strconv.Atoi(base)
				if err != nil || v < mins[i] || v > limits[i] {
					return false
				}
			}
		}
	}
	return true
}

// cronMatchField checks whether a single value matches a cron field pattern.
func cronMatchField(pattern string, value, minValue, maxValue int) bool {
	// Handle comma-separated lists
	for part := range strings.SplitSeq(pattern, ",") {
		part = strings.TrimSpace(part)
		if cronMatchSingle(part, value, minValue, maxValue) {
			return true
		}
	}
	return false
}

func cronMatchSingle(pattern string, value, minValue, maxValue int) bool {
	// Handle step values: */15, 1-10/2, 1/2. Wildcard steps are
	// anchored at the field minimum, which matters for 1-based fields.
	if rangePart, stepStr, ok := strings.Cut(pattern, "/"); ok {
		step, err := strconv.Atoi(stepStr)
		if err != nil || step <= 0 {
			return false
		}
		if rangePart == "*" {
			return value >= minValue && value <= maxValue && (value-minValue)%step == 0
		}
		// Range with step: 1-10/2
		if lowText, highText, ok := strings.Cut(rangePart, "-"); ok {
			low, _ := strconv.Atoi(lowText)
			high, _ := strconv.Atoi(highText)
			if value < low || value > high {
				return false
			}
			return (value-low)%step == 0
		}
		low, err := strconv.Atoi(rangePart)
		if err != nil || value < low || value > maxValue {
			return false
		}
		return (value-low)%step == 0
	}
	// Handle ranges: 1-5
	if lowText, highText, ok := strings.Cut(pattern, "-"); ok {
		low, err1 := strconv.Atoi(lowText)
		high, err2 := strconv.Atoi(highText)
		if err1 != nil || err2 != nil {
			return false
		}
		return value >= low && value <= high
	}
	// Handle wildcard
	if pattern == "*" {
		return true
	}
	// Handle literal value
	v, err := strconv.Atoi(pattern)
	if err != nil {
		return false
	}
	return v == value
}

// cronDue checks whether a 5-field cron expression should fire at the given time.
func cronDue(expr string, t time.Time) bool {
	if !isCronExpr(expr) {
		return false
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}
	// Minute/hour/month must all match. When both day fields are restricted,
	// standard cron ORs them: "0 9 1 * 1" fires on the 1st or on Mondays,
	// rather than only when the 1st happens to be a Monday.
	domRestricted := fields[2] != "*"
	dowRestricted := fields[4] != "*"
	domMatch := cronMatchField(fields[2], t.Day(), 1, 31)
	// Standard cron allows 7 as a Sunday alias in the dow field (weekday is
	// 0-6 in Go); a pattern like "0 9 * * 7" must still fire on Sundays.
	weekday := int(t.Weekday())
	dowMatch := cronMatchField(fields[4], weekday, 0, 7) || (weekday == 0 && cronMatchField(fields[4], 7, 0, 7))
	dayMatch := !domRestricted || !dowRestricted
	if domRestricted && dowRestricted {
		dayMatch = domMatch || dowMatch
	} else if domRestricted {
		dayMatch = domMatch
	} else if dowRestricted {
		dayMatch = dowMatch
	}
	return cronMatchField(fields[0], t.Minute(), 0, 59) &&
		cronMatchField(fields[1], t.Hour(), 0, 23) &&
		dayMatch &&
		cronMatchField(fields[3], int(t.Month()), 1, 12)
}

func heartbeatTaskDueAt(t HeartbeatTask, now time.Time) bool {
	// Try cron expression first
	if isCronExpr(t.Interval) {
		if cronDue(t.Interval, now) {
			if t.LastRunAt == 0 || time.UnixMilli(t.LastRunAt).Before(now.Truncate(time.Minute)) {
				return true
			}
		}
		return false
	}

	if scheduled, ok := previousHeartbeatScheduleAt(t, now); ok {
		if t.CreatedAt != 0 && scheduled.Before(time.UnixMilli(t.CreatedAt)) {
			return false
		}
		if t.LastRunAt != 0 && !time.UnixMilli(t.LastRunAt).Before(scheduled) {
			return false
		}
		return !scheduled.After(now)
	}

	d, err := parseInterval(t.Interval)
	if err != nil || d <= 0 {
		return false
	}
	baseMillis := t.LastRunAt
	if baseMillis == 0 {
		baseMillis = t.CreatedAt
	}
	hasTimeWindow := t.TimeWindowStart != "" || t.TimeWindowEnd != ""
	if baseMillis == 0 {
		if hasTimeWindow {
			return heartbeatWithinTimeWindow(t, now)
		}
		return true
	}
	if now.Sub(time.UnixMilli(baseMillis)) < d {
		return false
	}

	// For interval-based tasks with a time window, check if current time
	// falls within the configured window. If outside, defer until the next
	// tick that falls within the window.
	if hasTimeWindow {
		return heartbeatWithinTimeWindow(t, now)
	}

	return true
}

// heartbeatWithinTimeWindow returns true when now falls within the task's
// configured time window. If the window is empty it returns true.
// Format: "HH:MM" in 24-hour clock; start inclusive, end exclusive.
func heartbeatWithinTimeWindow(t HeartbeatTask, now time.Time) bool {
	startH, startM, startOK := parseHeartbeatClock(t.TimeWindowStart)
	endH, endM, endOK := parseHeartbeatClock(t.TimeWindowEnd)

	if !startOK && !endOK {
		return true // no window configured
	}

	minutes := now.Hour()*60 + now.Minute()

	// If only start is set: allow from start to end of day
	if startOK && !endOK {
		return minutes >= startH*60+startM
	}

	// If only end is set: allow from midnight to end
	if !startOK && endOK {
		return minutes < endH*60+endM
	}

	startMin := startH*60 + startM
	endMin := endH*60 + endM

	if startMin < endMin {
		// Normal window: 09:00-17:00
		return minutes >= startMin && minutes < endMin
	}
	// Cross-midnight window: 22:00-06:00
	return minutes >= startMin || minutes < endMin
}

type heartbeatSchedule struct {
	kind     string
	days     []time.Weekday
	month    int
	day      int
	hour     int
	minute   int
	hasRules bool
}

func parseHeartbeatSchedule(interval string) (heartbeatSchedule, bool) {
	_, after, ok0 := strings.Cut(interval, "|")
	if !ok0 {
		return heartbeatSchedule{}, false
	}
	raw := strings.TrimSpace(after)
	if raw == "" {
		return heartbeatSchedule{}, false
	}
	at := "09:00"
	if parts := strings.SplitN(raw, "@", 2); len(parts) == 2 {
		raw = parts[0]
		at = parts[1]
	}
	hour, minute, ok := parseHeartbeatClock(at)
	if !ok {
		return heartbeatSchedule{}, false
	}
	kind := raw
	rule := ""
	if parts := strings.SplitN(raw, ":", 2); len(parts) == 2 {
		kind = parts[0]
		rule = parts[1]
	}
	s := heartbeatSchedule{kind: kind, hour: hour, minute: minute, hasRules: true}
	switch kind {
	case "daily":
		return s, true
	case "weekly", "biweekly":
		for part := range strings.SplitSeq(rule, ",") {
			if wd, ok := parseHeartbeatWeekday(part); ok {
				s.days = append(s.days, wd)
			}
		}
		return s, len(s.days) > 0
	case "monthly":
		s.day = parsePositiveInt(rule, 1)
		return s, true
	case "yearly":
		parts := strings.SplitN(rule, "-", 2)
		s.month = parsePositiveInt(firstString(parts), 1)
		s.day = 1
		if len(parts) == 2 {
			s.day = parsePositiveInt(parts[1], 1)
		}
		if s.month < 1 {
			s.month = 1
		}
		if s.month > 12 {
			s.month = 12
		}
		return s, true
	default:
		return heartbeatSchedule{}, false
	}
}

func previousHeartbeatScheduleAt(t HeartbeatTask, now time.Time) (time.Time, bool) {
	s, ok := parseHeartbeatSchedule(t.Interval)
	if !ok || !s.hasRules {
		return time.Time{}, false
	}
	switch s.kind {
	case "daily":
		candidate := dateAt(now.Year(), now.Month(), now.Day(), s.hour, s.minute, now.Location())
		if candidate.After(now) {
			candidate = candidate.AddDate(0, 0, -1)
		}
		return candidate, true
	case "weekly":
		return previousHeartbeatWeeklyAt(s, now, 7, time.Time{})
	case "biweekly":
		anchor := heartbeatScheduleAnchor(t, now)
		return previousHeartbeatWeeklyAt(s, now, 14, anchor)
	case "monthly":
		return previousHeartbeatMonthlyAt(s, now), true
	case "yearly":
		return previousHeartbeatYearlyAt(s, now), true
	default:
		return time.Time{}, false
	}
}

func previousHeartbeatWeeklyAt(s heartbeatSchedule, now time.Time, windowDays int, anchor time.Time) (time.Time, bool) {
	var best time.Time
	for offset := range windowDays {
		day := now.AddDate(0, 0, -offset)
		for _, wd := range s.days {
			if day.Weekday() != wd {
				continue
			}
			candidate := dateAt(day.Year(), day.Month(), day.Day(), s.hour, s.minute, now.Location())
			if candidate.After(now) {
				continue
			}
			if !anchor.IsZero() && weeksBetween(weekStart(anchor), weekStart(candidate))%2 != 0 {
				continue
			}
			if best.IsZero() || candidate.After(best) {
				best = candidate
			}
		}
	}
	return best, !best.IsZero()
}

func previousHeartbeatMonthlyAt(s heartbeatSchedule, now time.Time) time.Time {
	candidate := monthlyCandidate(now.Year(), now.Month(), s.day, s.hour, s.minute, now.Location())
	if candidate.After(now) {
		prev := now.AddDate(0, -1, 0)
		candidate = monthlyCandidate(prev.Year(), prev.Month(), s.day, s.hour, s.minute, now.Location())
	}
	return candidate
}

func previousHeartbeatYearlyAt(s heartbeatSchedule, now time.Time) time.Time {
	month := time.Month(s.month)
	candidate := monthlyCandidate(now.Year(), month, s.day, s.hour, s.minute, now.Location())
	if candidate.After(now) {
		candidate = monthlyCandidate(now.Year()-1, month, s.day, s.hour, s.minute, now.Location())
	}
	return candidate
}

func heartbeatScheduleAnchor(t HeartbeatTask, now time.Time) time.Time {
	if t.CreatedAt != 0 {
		return time.UnixMilli(t.CreatedAt)
	}
	if t.LastRunAt != 0 {
		return time.UnixMilli(t.LastRunAt)
	}
	return now
}

func parseHeartbeatClock(s string) (int, int, bool) {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	hour := parsePositiveInt(parts[0], -1)
	minute := parsePositiveInt(parts[1], -1)
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, false
	}
	return hour, minute, true
}

func parseHeartbeatWeekday(s string) (time.Weekday, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "sun":
		return time.Sunday, true
	case "mon":
		return time.Monday, true
	case "tue":
		return time.Tuesday, true
	case "wed":
		return time.Wednesday, true
	case "thu":
		return time.Thursday, true
	case "fri":
		return time.Friday, true
	case "sat":
		return time.Saturday, true
	default:
		return time.Sunday, false
	}
}

func parsePositiveInt(s string, fallback int) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return fallback
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return fallback
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func dateAt(year int, month time.Month, day, hour, minute int, loc *time.Location) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, loc)
}

func monthlyCandidate(year int, month time.Month, day, hour, minute int, loc *time.Location) time.Time {
	if day < 1 {
		day = 1
	}
	if max := daysInMonth(year, month, loc); day > max {
		day = max
	}
	return dateAt(year, month, day, hour, minute, loc)
}

func daysInMonth(year int, month time.Month, loc *time.Location) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, loc).Day()
}

func weekStart(t time.Time) time.Time {
	dayOffset := (int(t.Weekday()) + 6) % 7
	base := dateAt(t.Year(), t.Month(), t.Day(), 0, 0, t.Location())
	return base.AddDate(0, 0, -dayOffset)
}

func weeksBetween(a, b time.Time) int {
	// Compare civil dates in UTC rather than elapsed local hours. Local Monday
	// midnights spanning DST can be 167 or 169 hours apart despite being exactly
	// one calendar week apart.
	aDay := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	bDay := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	if bDay.Before(aDay) {
		aDay, bDay = bDay, aDay
	}
	return int(bDay.Sub(aDay) / (7 * 24 * time.Hour))
}
