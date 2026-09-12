package installlayout

import (
	"errors"
	"testing"
	"time"
)

func TestRetryTransientStopsOnPermanentErrors(t *testing.T) {
	restore := transientRetryDelay
	transientRetryDelay = func(time.Duration) { t.Fatal("permanent errors must not back off") }
	t.Cleanup(func() { transientRetryDelay = restore })

	permanent := errors.New("permanent")
	attempts := 0
	err := retryTransient(func() error { attempts++; return permanent })
	if !errors.Is(err, permanent) || attempts != 1 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
	attempts = 0
	if err := retryTransient(func() error { attempts++; return nil }); err != nil || attempts != 1 {
		t.Fatalf("attempts=%d err=%v", attempts, err)
	}
}
