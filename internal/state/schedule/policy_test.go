package schedule

import (
	"errors"
	"testing"
	"time"
)

func TestDefaultPolicyMatchesOwnerDecision(t *testing.T) {
	p := DefaultPolicy()
	if p.MinIntervalMinutes != 60 || p.PerRunTokens != 250_000 || p.GlobalTokensDay != 1_200_000 ||
		p.GlobalTokensWeek != 5_000_000 || p.LifetimeTokens != 3_000_000 || p.ExpiryDays != 30 {
		t.Fatalf("defaults drifted: %+v", p)
	}
}

func TestResolveUserOverridesApply(t *testing.T) {
	p, ign, err := Resolve(Overrides{PerRunTokens: new(int64(100)), MinIntervalMinutes: new(int64(120)), GlobalTokensDay: new(int64(9_000_000))}, Overrides{})
	if err != nil || len(ign) != 0 {
		t.Fatalf("err=%v ignored=%v", err, ign)
	}
	if p.PerRunTokens != 100 || p.MinIntervalMinutes != 120 || p.GlobalTokensDay != 9_000_000 {
		t.Fatalf("user layer not applied: %+v", p)
	}
}

func TestResolveProjectCannotLoosenAnyLimit(t *testing.T) {
	d := DefaultPolicy()
	loose := Overrides{
		MinIntervalMinutes: new(d.MinIntervalMinutes - 1), PerRunTokens: new(d.PerRunTokens + 1),
		PerRunWallSeconds: new(d.PerRunWallSeconds + 1), PerRunSteps: new(d.PerRunSteps + 1),
		MaxRunsPerScheduleDay: new(d.MaxRunsPerScheduleDay + 1), MaxRunsGlobalDay: new(d.MaxRunsGlobalDay + 1),
		GlobalTokensDay: new(d.GlobalTokensDay + 1), GlobalTokensWeek: new(d.GlobalTokensWeek + 1),
		LifetimeTokens: new(d.LifetimeTokens + 1), ExpiryDays: new(d.ExpiryDays + 1),
		MaxSchedules: new(d.MaxSchedules + 1), MaxConsecutiveFails: new(d.MaxConsecutiveFails + 1),
		MaxRepeatParks: new(d.MaxRepeatParks + 1),
	}
	p, ign, err := Resolve(Overrides{}, loose)
	if err != nil {
		t.Fatal(err)
	}
	if p != d {
		t.Fatalf("project layer loosened policy:\n got %+v\nwant %+v", p, d)
	}
	if len(ign) != 13 {
		t.Fatalf("every loosening must be reported, got %d: %v", len(ign), ign)
	}
}

func TestResolveProjectCanTighten(t *testing.T) {
	p, ign, err := Resolve(Overrides{PerRunTokens: new(int64(400_000))}, Overrides{PerRunTokens: new(int64(50_000)), MinIntervalMinutes: new(int64(180))})
	if err != nil || len(ign) != 0 {
		t.Fatalf("err=%v ignored=%v", err, ign)
	}
	if p.PerRunTokens != 50_000 || p.MinIntervalMinutes != 180 {
		t.Fatalf("tightening not applied: %+v", p)
	}
}

func TestResolveProjectCannotExceedUserRaisedValue(t *testing.T) {
	p, ign, _ := Resolve(Overrides{PerRunTokens: new(int64(400_000))}, Overrides{PerRunTokens: new(int64(500_000))})
	if p.PerRunTokens != 400_000 || len(ign) != 1 || ign[0].Key != "per_run_tokens" {
		t.Fatalf("project exceeded the user value: %+v %v", p, ign)
	}
}

func TestResolveRejectsBadUserValues(t *testing.T) {
	for _, o := range []Overrides{{PerRunTokens: new(int64(0))}, {ExpiryDays: new(int64(-1))}} {
		if _, _, err := Resolve(o, Overrides{}); !errors.Is(err, ErrPolicyInvalid) {
			t.Fatalf("want ErrPolicyInvalid for %+v, got %v", o, err)
		}
	}
}

func TestMinIntervalIsUserLowerableAboveHardFloor(t *testing.T) {
	p, ign, err := Resolve(Overrides{MinIntervalMinutes: new(int64(15))}, Overrides{})
	if err != nil || len(ign) != 0 || p.MinIntervalMinutes != 15 || p.MinInterval() != 15*time.Minute {
		t.Fatalf("15 minutes must be accepted: %+v %v %v", p, ign, err)
	}
	p, ign, err = Resolve(Overrides{MinIntervalMinutes: new(int64(5))}, Overrides{})
	if err != nil || p.MinIntervalMinutes != 10 || len(ign) != 1 || ign[0].Key != "min_interval_minutes" {
		t.Fatalf("5 minutes must clamp to the floor and be reported: %+v %v %v", p, ign, err)
	}
	p, ign, _ = Resolve(Overrides{MinIntervalMinutes: new(int64(15))}, Overrides{MinIntervalMinutes: new(int64(10))})
	if p.MinIntervalMinutes != 15 || len(ign) != 1 {
		t.Fatalf("project cannot lower the interval: %+v %v", p, ign)
	}
	if got := (Policy{MinIntervalMinutes: 1}).MinInterval(); got != AbsoluteMinInterval {
		t.Fatalf("a hand-built policy is floored too: %v", got)
	}
}

func TestResolveIgnoresNonPositiveProjectValue(t *testing.T) {
	p, ign, err := Resolve(Overrides{}, Overrides{PerRunTokens: new(int64(0))})
	if err != nil || p != DefaultPolicy() || len(ign) != 1 {
		t.Fatalf("p=%+v ign=%v err=%v", p, ign, err)
	}
}
