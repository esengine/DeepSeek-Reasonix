//go:build windows

package desktopinstance

import (
	"errors"
	"time"
)

// Startup may race Chromium child teardown. Wait within the existing startup
// deadline for a fully inspectable snapshot; never report readiness from an
// incomplete snapshot or relax recovery's immediate ownership checks.
func waitForInspectable(inspect func() ([]*process, error), remaining func() time.Duration, wait func(time.Duration)) ([]*process, error) {
	for {
		list, err := inspect()
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != UnknownOwner {
			return list, err
		}
		left := remaining()
		if left <= 0 {
			return nil, err
		}
		wait(min(left, 200*time.Millisecond))
	}
}
