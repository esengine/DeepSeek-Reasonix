package teamroster

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"reasonix/internal/base/fileutil"
)

// ScheduleFile holds the team's scheduled tasks.
const ScheduleFile = "schedules.json"

// Repeat is how a scheduled task comes round.
type Repeat string

const (
	RepeatOnce   Repeat = "once"
	RepeatDaily  Repeat = "daily"
	RepeatWeekly Repeat = "weekly"
	RepeatEvery  Repeat = "every"
)

// Unit is what an every-N task counts in.
type Unit string

const (
	UnitMinutes Unit = "minutes"
	UnitHours   Unit = "hours"
	UnitDays    Unit = "days"
)

// Bounds on a scheduled task. The shortest interval is the scheduler's own
// floor, a guard against a typo spending quota every few minutes.
const (
	MaxScheduleNameRunes   = 60
	MaxSchedulePromptRunes = 2000
	MinScheduleInterval    = 10 * time.Minute
	MaxScheduleInterval    = 7 * 24 * time.Hour
)

var (
	scheduleID = regexp.MustCompile(`^s-[0-9a-f]{16}$`)
	clockForm  = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
)

// Schedule is a task the user set for an agent or a group: a prompt sent at
// the times Repeat names. Clock is a local "HH:MM"; Days are weekdays, Sunday
// 0; Once is the moment of a one-off and the anchor an every-N task counts
// from. Next is the slot it fires at next, zero when it never will again.
type Schedule struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Agent     string    `json:"agent,omitempty"`
	Group     string    `json:"group,omitempty"`
	Prompt    string    `json:"prompt"`
	Repeat    Repeat    `json:"repeat"`
	Clock     string    `json:"clock,omitempty"`
	Days      []int     `json:"days,omitempty"`
	Every     int       `json:"every,omitempty"`
	Unit      Unit      `json:"unit,omitempty"`
	Once      time.Time `json:"once,omitzero"`
	Enabled   bool      `json:"enabled"`
	Next      time.Time `json:"next,omitzero"`
	LastFired time.Time `json:"lastFired,omitzero"`
}

func (s Schedule) interval() time.Duration {
	n := time.Duration(s.Every)
	switch s.Unit {
	case UnitMinutes:
		return n * time.Minute
	case UnitHours:
		return n * time.Hour
	case UnitDays:
		return n * 24 * time.Hour
	}
	return 0
}

func (s Schedule) check() error {
	switch {
	case (s.Agent == "") == (s.Group == ""):
		return fieldErr("target", ErrInvalid)
	case s.Agent != "" && !agentID.MatchString(s.Agent), s.Group != "" && !groupID.MatchString(s.Group):
		return fieldErr("target", ErrInvalid)
	case utf8.RuneCountInString(s.Name) > MaxScheduleNameRunes:
		return fieldErr("name", ErrTooLong)
	}
	if strings.TrimSpace(s.Prompt) == "" {
		return fieldErr("prompt", ErrEmpty)
	}
	if utf8.RuneCountInString(s.Prompt) > MaxSchedulePromptRunes {
		return fieldErr("prompt", ErrTooLong)
	}
	switch s.Repeat {
	case RepeatOnce:
		if s.Once.IsZero() {
			return fieldErr("once", ErrEmpty)
		}
	case RepeatDaily, RepeatWeekly:
		if !clockForm.MatchString(s.Clock) {
			return fieldErr("clock", ErrInvalid)
		}
		if s.Repeat == RepeatWeekly && (len(s.Days) == 0 || slices.ContainsFunc(s.Days, func(d int) bool { return d < 0 || d > 6 })) {
			return fieldErr("days", ErrInvalid)
		}
	case RepeatEvery:
		if d := s.interval(); d < MinScheduleInterval || d > MaxScheduleInterval {
			return fieldErr("every", ErrInvalid)
		}
	default:
		return fieldErr("repeat", ErrInvalid)
	}
	return nil
}

// NextAfter is the first slot strictly after now, read in loc; zero when a
// one-off has passed.
func (s Schedule) NextAfter(now time.Time, loc *time.Location) time.Time {
	now = now.In(loc)
	switch s.Repeat {
	case RepeatOnce:
		if s.Once.After(now) {
			return s.Once
		}
		return time.Time{}
	case RepeatEvery:
		step := s.interval()
		switch {
		case step <= 0:
			return time.Time{}
		case s.Once.IsZero():
			return now.Add(step)
		case s.Once.After(now):
			return s.Once
		}
		return s.Once.Add(step * (now.Sub(s.Once)/step + 1))
	case RepeatDaily, RepeatWeekly:
		hh, mm, ok := parseClock(s.Clock)
		if !ok {
			return time.Time{}
		}
		for d := range 8 {
			day := now.AddDate(0, 0, d)
			slot := time.Date(day.Year(), day.Month(), day.Day(), hh, mm, 0, 0, loc)
			if !slot.After(now) {
				continue
			}
			if s.Repeat == RepeatDaily || slices.Contains(s.Days, int(slot.Weekday())) {
				return slot
			}
		}
	}
	return time.Time{}
}

func parseClock(clock string) (hh, mm int, ok bool) {
	if !clockForm.MatchString(clock) {
		return 0, 0, false
	}
	return int(clock[0]-'0')*10 + int(clock[1]-'0'), int(clock[3]-'0')*10 + int(clock[4]-'0'), true
}

// Schedules returns the team's scheduled tasks, in the order they were made.
func (s Store) Schedules() ([]Schedule, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir, ScheduleFile))
	if errors.Is(err, fs.ErrNotExist) {
		return []Schedule{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Schedule{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, ErrCorrupt
	}
	return out, nil
}

// EditSchedules applies mutate to the team's scheduled tasks under the lock and
// writes them back only if every task still checks.
func (s Store) EditSchedules(ctx context.Context, mutate func([]Schedule) ([]Schedule, error)) ([]Schedule, error) {
	var out []Schedule
	err := s.withLock(ctx, s.Dir, ScheduleFile, func() error {
		list, err := s.Schedules()
		if err != nil {
			return err
		}
		next, err := mutate(list)
		if err != nil {
			return err
		}
		for _, sc := range next {
			if err := sc.check(); err != nil {
				return err
			}
		}
		data, err := json.Marshal(next)
		if err != nil {
			return err
		}
		out = next
		return fileutil.AtomicWriteFile(filepath.Join(s.Dir, ScheduleFile), data, 0o600)
	})
	return out, err
}

// SaveSchedule adds sc under a fresh id, or replaces the task with its id.
func (s Store) SaveSchedule(ctx context.Context, sc Schedule) (Schedule, error) {
	if err := sc.check(); err != nil {
		return Schedule{}, err
	}
	_, err := s.EditSchedules(ctx, func(list []Schedule) ([]Schedule, error) {
		if sc.ID == "" {
			sc.ID = "s-" + randomHex()
			return append(list, sc), nil
		}
		i := slices.IndexFunc(list, func(x Schedule) bool { return x.ID == sc.ID })
		if i < 0 {
			return nil, ErrUnknownSchedule
		}
		list[i] = sc
		return list, nil
	})
	return sc, err
}

// DeleteSchedule removes the task with id.
func (s Store) DeleteSchedule(ctx context.Context, id string) error {
	_, err := s.EditSchedules(ctx, func(list []Schedule) ([]Schedule, error) {
		i := slices.IndexFunc(list, func(x Schedule) bool { return x.ID == id })
		if i < 0 {
			return nil, ErrUnknownSchedule
		}
		return slices.Delete(list, i, i+1), nil
	})
	return err
}
