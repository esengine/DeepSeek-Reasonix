package schedule

import (
	"fmt"
	"time"
)

// AbsoluteMinInterval is a safety net against a mistyped value, not a policy:
// the default is 60 minutes and the user layer may lower it to any value at or
// above this. It is far below the default so it locks nothing in.
const AbsoluteMinInterval = 10 * time.Minute

// Policy is the resolved set of ceilings every schedule and every claim is held to.
type Policy struct {
	MinIntervalMinutes    int64
	PerRunTokens          int64
	PerRunWallSeconds     int64
	PerRunSteps           int64
	MaxRunsPerScheduleDay int64
	MaxRunsGlobalDay      int64
	GlobalTokensDay       int64
	GlobalTokensWeek      int64
	LifetimeTokens        int64
	ExpiryDays            int64
	MaxSchedules          int64
	MaxConsecutiveFails   int64
	MaxRepeatParks        int64
}

func DefaultPolicy() Policy {
	return Policy{
		MinIntervalMinutes:    60,
		PerRunTokens:          250_000,
		PerRunWallSeconds:     900,
		PerRunSteps:           40,
		MaxRunsPerScheduleDay: 4,
		MaxRunsGlobalDay:      6,
		GlobalTokensDay:       1_200_000,
		GlobalTokensWeek:      5_000_000,
		LifetimeTokens:        3_000_000,
		ExpiryDays:            30,
		MaxSchedules:          8,
		MaxConsecutiveFails:   3,
		MaxRepeatParks:        3,
	}
}

// Overrides is one configuration layer's optional values; nil means unset.
// The keys belong to a [schedule] table.
type Overrides struct {
	MinIntervalMinutes    *int64 `toml:"min_interval_minutes" json:"min_interval_minutes,omitempty"`
	PerRunTokens          *int64 `toml:"per_run_tokens" json:"per_run_tokens,omitempty"`
	PerRunWallSeconds     *int64 `toml:"per_run_wall_seconds" json:"per_run_wall_seconds,omitempty"`
	PerRunSteps           *int64 `toml:"per_run_steps" json:"per_run_steps,omitempty"`
	MaxRunsPerScheduleDay *int64 `toml:"max_runs_per_schedule_day" json:"max_runs_per_schedule_day,omitempty"`
	MaxRunsGlobalDay      *int64 `toml:"max_runs_global_day" json:"max_runs_global_day,omitempty"`
	GlobalTokensDay       *int64 `toml:"tokens_per_day" json:"tokens_per_day,omitempty"`
	GlobalTokensWeek      *int64 `toml:"tokens_per_week" json:"tokens_per_week,omitempty"`
	LifetimeTokens        *int64 `toml:"lifetime_tokens" json:"lifetime_tokens,omitempty"`
	ExpiryDays            *int64 `toml:"expiry_days" json:"expiry_days,omitempty"`
	MaxSchedules          *int64 `toml:"max_schedules" json:"max_schedules,omitempty"`
	MaxConsecutiveFails   *int64 `toml:"max_consecutive_failures" json:"max_consecutive_failures,omitempty"`
	MaxRepeatParks        *int64 `toml:"max_repeat_parks" json:"max_repeat_parks,omitempty"`
}

// Ignored names a project-layer key that was dropped and why.
type Ignored struct {
	Key    string
	Reason string
}

type field struct {
	key    string
	raise  bool
	policy *int64
	user   *int64
	proj   *int64
}

func (p *Policy) fields(user, project Overrides) []field {
	return []field{
		{"min_interval_minutes", true, &p.MinIntervalMinutes, user.MinIntervalMinutes, project.MinIntervalMinutes},
		{"per_run_tokens", false, &p.PerRunTokens, user.PerRunTokens, project.PerRunTokens},
		{"per_run_wall_seconds", false, &p.PerRunWallSeconds, user.PerRunWallSeconds, project.PerRunWallSeconds},
		{"per_run_steps", false, &p.PerRunSteps, user.PerRunSteps, project.PerRunSteps},
		{"max_runs_per_schedule_day", false, &p.MaxRunsPerScheduleDay, user.MaxRunsPerScheduleDay, project.MaxRunsPerScheduleDay},
		{"max_runs_global_day", false, &p.MaxRunsGlobalDay, user.MaxRunsGlobalDay, project.MaxRunsGlobalDay},
		{"tokens_per_day", false, &p.GlobalTokensDay, user.GlobalTokensDay, project.GlobalTokensDay},
		{"tokens_per_week", false, &p.GlobalTokensWeek, user.GlobalTokensWeek, project.GlobalTokensWeek},
		{"lifetime_tokens", false, &p.LifetimeTokens, user.LifetimeTokens, project.LifetimeTokens},
		{"expiry_days", false, &p.ExpiryDays, user.ExpiryDays, project.ExpiryDays},
		{"max_schedules", false, &p.MaxSchedules, user.MaxSchedules, project.MaxSchedules},
		{"max_consecutive_failures", false, &p.MaxConsecutiveFails, user.MaxConsecutiveFails, project.MaxConsecutiveFails},
		{"max_repeat_parks", false, &p.MaxRepeatParks, user.MaxRepeatParks, project.MaxRepeatParks},
	}
}

// Resolve applies the user layer over the defaults, then lets the project
// layer tighten the result. A project value that would raise a ceiling, lower
// the minimum interval or is not positive is dropped and reported. A user
// interval under the hard floor is clamped and reported; other bad user values
// are errors.
func Resolve(user, project Overrides) (Policy, []Ignored, error) {
	p := DefaultPolicy()
	for _, f := range p.fields(user, Overrides{}) {
		if f.user == nil {
			continue
		}
		if *f.user <= 0 {
			return Policy{}, nil, fmt.Errorf("%w: %s must be positive", ErrPolicyInvalid, f.key)
		}
		*f.policy = *f.user
	}
	var ignored []Ignored
	if floor := int64(AbsoluteMinInterval / time.Minute); p.MinIntervalMinutes < floor {
		p.MinIntervalMinutes = floor
		ignored = append(ignored, Ignored{"min_interval_minutes", fmt.Sprintf("clamped to the %d minute hard floor", floor)})
	}
	for _, f := range p.fields(Overrides{}, project) {
		if f.proj == nil {
			continue
		}
		switch v := *f.proj; {
		case v <= 0:
			ignored = append(ignored, Ignored{f.key, "not positive"})
		case f.raise && v < *f.policy, !f.raise && v > *f.policy:
			ignored = append(ignored, Ignored{f.key, "project configuration cannot loosen this limit"})
		default:
			*f.policy = v
		}
	}
	return p, ignored, nil
}

func (p Policy) MinInterval() time.Duration {
	return max(time.Duration(p.MinIntervalMinutes)*time.Minute, AbsoluteMinInterval)
}

func (p Policy) Expiry() time.Duration {
	return time.Duration(p.ExpiryDays) * 24 * time.Hour
}

// DefaultBudget is what a new schedule gets when its creator states none.
func (p Policy) DefaultBudget() Budget {
	return Budget{
		PerRunTokens:   p.PerRunTokens,
		PerRunWallSec:  p.PerRunWallSeconds,
		PerRunSteps:    p.PerRunSteps,
		MaxRunsPerDay:  p.MaxRunsPerScheduleDay,
		LifetimeTokens: p.LifetimeTokens,
	}
}

// fits reports whether a schedule budget stays inside the policy ceilings.
func (p Policy) fits(b Budget) error {
	for _, c := range []struct {
		name       string
		have, cap_ int64
	}{
		{"perRunTokens", b.PerRunTokens, p.PerRunTokens},
		{"perRunWallSec", b.PerRunWallSec, p.PerRunWallSeconds},
		{"perRunSteps", b.PerRunSteps, p.PerRunSteps},
		{"maxRunsPerDay", b.MaxRunsPerDay, p.MaxRunsPerScheduleDay},
		{"lifetimeTokens", b.LifetimeTokens, p.LifetimeTokens},
	} {
		if c.have <= 0 || c.have > c.cap_ {
			return fmt.Errorf("%w: budget %s %d is outside 1..%d", ErrInvalid, c.name, c.have, c.cap_)
		}
	}
	return nil
}
