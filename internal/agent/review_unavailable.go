package agent

import (
	"errors"
	"fmt"
)

// ReviewUnavailableError is returned when an independent reviewer failed after
// its single retry. Local mutations are retained; hosts map this to
// Partial/Unverified rather than rolling back workspace changes.
type ReviewUnavailableError struct {
	Kind   string
	Nudges int
	Dump   string
}

func (e *ReviewUnavailableError) Error() string {
	if e == nil {
		return "reviewer unavailable"
	}
	msg := fmt.Sprintf("%s reviewer unavailable after %d retry; local changes kept as Partial/Unverified", e.Kind, e.Nudges)
	if e.Dump != "" {
		msg += e.Dump
	}
	return msg
}

// IsReviewUnavailable reports whether err is a reviewer-unavailable partial.
func IsReviewUnavailable(err error) bool {
	var target *ReviewUnavailableError
	return errors.As(err, &target)
}
