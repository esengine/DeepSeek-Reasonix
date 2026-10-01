package config

import (
	"errors"
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
