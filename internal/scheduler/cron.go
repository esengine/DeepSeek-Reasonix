package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// cronField is one expanded field of a 5-field cron expression: the sorted
// list of values the field may take.
type cronField struct {
	values []int // allowed values, ascending
	any    bool  // "*" (or "*/1"): every value in range
	dow    bool  // this is the day-of-week field (0-7, 7 = Sunday)
	useOr  bool  // dom/dow are both constrained: match if EITHER matches
}

var (
	monthDays = [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	// maxLookahead bounds Next's search; zero time is returned beyond it.
	maxLookahead = 5 * 366 * 24 * time.Hour
)

// Valid reports whether cron is a parseable 5-field expression.
func Valid(cron string) bool {
	_, err := parseCron(cron)
	return err == nil
}

// Next returns the first fire time strictly after `after` matching the
// 5-field cron expression (minute hour day-of-month month day-of-week).
// Wildcards, steps (*/5), ranges (1-5), and lists (1,15,30) are supported;
// day-of-week 0 and 7 both mean Sunday; when both day-of-month and
// day-of-week are constrained a date matches if either field matches
// (standard vixie-cron semantics). It returns the zero time when cron is
// unparseable or no match exists within the lookahead horizon.
func Next(cron string, after time.Time) time.Time {
	f, err := parseCron(cron)
	if err != nil {
		return time.Time{}
	}
	t := after.Truncate(time.Minute).Add(time.Minute)
	horizon := after.Add(maxLookahead)
	for t.Before(horizon) {
		if f.matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

func (p *parsedCron) matches(t time.Time) bool {
	if !p.minute.matchesValue(t.Minute()) {
		return false
	}
	if !p.hour.matchesValue(t.Hour()) {
		return false
	}
	if !p.month.matchesValue(int(t.Month())) {
		return false
	}
	domOK := p.dom.matchesDay(t)
	dowOK := p.dow.matchesDay(t)
	if p.dom.useOr {
		return domOK || dowOK
	}
	return domOK && dowOK
}

// matchesValue reports whether a numeric field value (minute/hour/month) is
// allowed.
func (f *cronField) matchesValue(v int) bool {
	if f.any {
		return true
	}
	return contains(f.values, v)
}

// matchesDay checks day-of-month (1-31) or day-of-week (0-6, Sunday=0).
func (f *cronField) matchesDay(t time.Time) bool {
	if f.any {
		return true
	}
	if !f.dow {
		return contains(f.values, t.Day())
	}
	// dow field: 0 and 7 both mean Sunday; normalize 7 -> 0
	dow := int(t.Weekday())
	if dow == 7 {
		dow = 0
	}
	for _, v := range f.values {
		if v == 7 {
			v = 0
		}
		if v == dow {
			return true
		}
	}
	return false
}

// parsedCron is the expanded form of a 5-field expression.
type parsedCron struct {
	minute, hour, month *cronField
	dom, dow            *cronField
}

func parseCron(cron string) (*parsedCron, error) {
	fields := strings.Fields(strings.TrimSpace(cron))
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron: want 5 fields, got %d", len(fields))
	}
	minute, err := expandField(fields[0], 0, 59)
	if err != nil {
		return nil, err
	}
	hour, err := expandField(fields[1], 0, 23)
	if err != nil {
		return nil, err
	}
	dom, err := expandField(fields[2], 1, 31)
	if err != nil {
		return nil, err
	}
	month, err := expandField(fields[3], 1, 12)
	if err != nil {
		return nil, err
	}
	dow, err := expandField(fields[4], 0, 7)
	if err != nil {
		return nil, err
	}
	dow.dow = true
	if dom.any && dow.any {
		dom.any = false
		dow.any = false
		dom.values = allRange(1, 31)
		dow.values = allRange(0, 6)
	} else if !dom.any && !dow.any {
		// both constrained: vixie OR semantics
		dom.useOr = true
		dow.useOr = true
	}
	return &parsedCron{
		minute: minute,
		hour:   hour,
		month:  month,
		dom:    dom,
		dow:    dow,
	}, nil
}

// expandField parses one cron field into its allowed values. When the field
// is "*" the any flag is set and values is left empty.
func expandField(s string, lo, hi int) (*cronField, error) {
	s = strings.TrimSpace(s)
	if s == "*" {
		return &cronField{any: true}, nil
	}
	var out []int
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("cron: empty list element in %q", s)
		}
		step := 1
		base := part
		if i := strings.Index(part, "/"); i >= 0 {
			base, step = part[:i], 1
			if base == "" {
				base = "*"
			}
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n < 1 {
				return nil, fmt.Errorf("cron: bad step %q", part)
			}
			step = n
		}
		var from, to int
		switch {
		case base == "*":
			from, to = lo, hi
		case strings.Contains(base, "-"):
			parts := strings.SplitN(base, "-", 2)
			a, err1 := strconv.Atoi(parts[0])
			b, err2 := strconv.Atoi(parts[1])
			if err1 != nil || err2 != nil {
				return nil, fmt.Errorf("cron: bad range %q", base)
			}
			from, to = a, b
		default:
			n, err := strconv.Atoi(base)
			if err != nil {
				return nil, fmt.Errorf("cron: bad value %q", base)
			}
			from, to = n, n
		}
		if from < lo || to > hi || from > to {
			return nil, fmt.Errorf("cron: value %q out of range %d-%d", base, lo, hi)
		}
		for v := from; v <= to; v += step {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("cron: field %q expands to nothing", s)
	}
	return &cronField{values: dedupeSort(out)}, nil
}

// allRange returns every integer in [lo, hi].
func allRange(lo, hi int) []int {
	out := make([]int, 0, hi-lo+1)
	for v := lo; v <= hi; v++ {
		out = append(out, v)
	}
	return out
}

func contains(vs []int, v int) bool {
	for _, x := range vs {
		if x == v {
			return true
		}
	}
	return false
}

func dedupeSort(vs []int) []int {
	out := make([]int, 0, len(vs))
	seen := map[int]bool{}
	for _, v := range vs {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// ParseInterval maps a human interval token (30s, 5m, 2h, 1d) to a 5-field
// cron expression. Seconds round up to one minute (cron has no sub-minute
// granularity). Intervals that don't divide their unit cleanly round to the
// nearest clean step (7m -> */6, 45m -> */30, 90m -> */60 = every hour).
// Days fire at 09:00 local (1d) or approximate every-N-days via day-of-month
// stepping (nd), which drifts on short months.
func ParseInterval(s string) (cron string, ok bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) < 2 {
		return "", false
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n < 1 {
		return "", false
	}
	switch s[len(s)-1] {
	case 's':
		// cron has one-minute granularity: round seconds up to a minute
		return "*/1 * * * *", true
	case 'm':
		return minuteCron(n), true
	case 'h':
		return hourCron(n), true
	case 'd':
		if n == 1 {
			return "0 9 * * *", true // daily at 9am local
		}
		// approximate every-N-days via day-of-month stepping
		return fmt.Sprintf("0 9 */%d * *", n), true
	}
	return "", false
}

// minuteCron maps an interval in minutes to a cron expression. n divides 60
// cleanly -> */n; otherwise it rounds to the nearest divisor of 60.
func minuteCron(n int) string {
	if 60%n == 0 {
		return fmt.Sprintf("*/%d * * * *", n)
	}
	for d := 1; d <= 30; d++ {
		for _, cand := range []int{n - d, n + d} {
			if cand >= 1 && 60%cand == 0 {
				return fmt.Sprintf("*/%d * * * *", cand)
			}
		}
	}
	return "*/30 * * * *"
}

// hourCron maps an interval in hours to a cron expression. n divides 24
// cleanly -> 0 */n; otherwise it rounds to the nearest divisor of 24 (ties
// toward the smaller step, so 90 minutes of hours rounds to every hour).
func hourCron(n int) string {
	if 24%n == 0 {
		return fmt.Sprintf("0 */%d * * *", n)
	}
	for d := 1; d <= 12; d++ {
		for _, cand := range []int{n - d, n + d} {
			if cand >= 1 && 24%cand == 0 {
				return fmt.Sprintf("0 */%d * * *", cand)
			}
		}
	}
	return "0 */12 * * *"
}
