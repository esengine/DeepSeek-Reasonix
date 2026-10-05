package config

import (
	"errors"
	"strings"
	"testing"
)

func TestProgressWatchDefaultsAndRange(t *testing.T) {
	var c Config
	if c.ProgressWatchRounds() != DefaultProgressWatchRounds || c.ProgressWatchTokenMultiple() != DefaultProgressWatchTokenMultiple || c.ProgressWatch.Pause {
		t.Fatalf("zero section = %+v, want pause off at the defaults", c.ProgressWatch)
	}
	for _, bad := range []ProgressWatchConfig{{Rounds: -1}, {Rounds: 1001}, {TokenMultiple: -2}, {TokenMultiple: 5000}} {
		if err := c.SetProgressWatch(bad); !errors.Is(err, ErrProgressWatchOutOfRange) {
			t.Fatalf("SetProgressWatch(%+v) = %v, want ErrProgressWatchOutOfRange", bad, err)
		}
	}
	if err := c.SetProgressWatch(ProgressWatchConfig{Pause: true, Rounds: 7, TokenMultiple: 3}); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	renderProgressWatchSection(&b, &c)
	for _, want := range []string{"[progress_watch]", "pause = true", "rounds = 7", "token_multiple = 3"} {
		if !strings.Contains(b.String(), want) {
			t.Fatalf("rendered section lacks %q:\n%s", want, b.String())
		}
	}
	var untouched strings.Builder
	renderProgressWatchSection(&untouched, &Config{})
	if untouched.Len() != 0 {
		t.Fatalf("an untouched section rendered:\n%s", untouched.String())
	}
}
