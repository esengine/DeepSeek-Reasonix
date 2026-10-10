package config

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestAutoArchiveDefaultsOffAndRange(t *testing.T) {
	var c Config
	if c.AutoArchiveAfter() != 0 || c.AutoArchiveDays() != DefaultAutoArchiveDays {
		t.Fatalf("zero value = %v / %d, want off at the default days", c.AutoArchiveAfter(), c.AutoArchiveDays())
	}
	for _, bad := range []AutoArchiveConfig{{Days: -1}, {Days: 3651}} {
		if err := c.SetAutoArchive(bad); !errors.Is(err, ErrAutoArchiveOutOfRange) {
			t.Fatalf("SetAutoArchive(%+v) = %v, want ErrAutoArchiveOutOfRange", bad, err)
		}
	}
	if err := c.SetAutoArchive(AutoArchiveConfig{Enabled: true, Days: 3}); err != nil {
		t.Fatal(err)
	}
	if c.AutoArchiveAfter() != 72*time.Hour {
		t.Fatalf("after = %v, want 72h", c.AutoArchiveAfter())
	}
	var b strings.Builder
	renderAutoArchiveSection(&b, &c)
	for _, want := range []string{"[auto_archive]", "enabled = true", "days = 3"} {
		if !strings.Contains(b.String(), want) {
			t.Fatalf("rendered section lacks %q:\n%s", want, b.String())
		}
	}
	var untouched strings.Builder
	renderAutoArchiveSection(&untouched, &Config{})
	if untouched.Len() != 0 {
		t.Fatalf("an untouched section rendered:\n%s", untouched.String())
	}
}

func TestProjectFileCannotSetAutoArchive(t *testing.T) {
	user := &Config{AutoArchive: AutoArchiveConfig{Enabled: false, Days: 9}}
	held := holdUserGlobals(user)
	user.AutoArchive = AutoArchiveConfig{Enabled: true, Days: 1}
	held.restore(user)
	if user.AutoArchive != (AutoArchiveConfig{Enabled: false, Days: 9}) {
		t.Fatalf("auto_archive = %+v, want the user's value restored", user.AutoArchive)
	}
}

func TestAutoArchiveDaysIsHeldAtTheLimit(t *testing.T) {
	day := 24 * time.Hour
	for _, tc := range []struct {
		days int
		want int
	}{
		{math.MinInt, DefaultAutoArchiveDays},
		{-1, DefaultAutoArchiveDays},
		{0, DefaultAutoArchiveDays},
		{1, 1},
		{maxAutoArchiveDays, maxAutoArchiveDays},
		{maxAutoArchiveDays + 1, maxAutoArchiveDays},
		{213504, maxAutoArchiveDays},
		{math.MaxInt, maxAutoArchiveDays},
	} {
		c := Config{AutoArchive: AutoArchiveConfig{Enabled: true, Days: tc.days}}
		if got := c.AutoArchiveDays(); got != tc.want {
			t.Errorf("days %d: AutoArchiveDays = %d, want %d", tc.days, got, tc.want)
		}
		if got := c.AutoArchiveAfter(); got != time.Duration(tc.want)*day {
			t.Errorf("days %d: AutoArchiveAfter = %v, want %v", tc.days, got, time.Duration(tc.want)*day)
		}
	}
}
