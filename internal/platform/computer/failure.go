package computer

import (
	"errors"
	"fmt"
	"strings"
)

// Code is the identity of a computer-use failure, shared with the helper that
// reports most of them.
type Code string

const (
	CodeUnavailable       Code = "computer.unavailable"
	CodeUnsupported       Code = "computer.unsupported"
	CodePermissionMissing Code = "computer.permission_missing"
	CodeNoApp             Code = "computer.no_app"
	CodeNoWindow          Code = "computer.no_window"
	CodeAmbiguousWindow   Code = "computer.ambiguous_window_target"
	CodeAppRefused        Code = "computer.app_refused"
	CodeUnknownRef        Code = "computer.unknown_ref"
	CodeStaleRef          Code = "computer.stale_ref"
	CodeNoAction          Code = "computer.no_action"
	CodeNeedsFront        Code = "computer.needs_front"
	CodeBlocked           Code = "computer.blocked"
	CodeElevated          Code = "computer.elevated"
	CodeNoElement         Code = "computer.no_element"
	CodeNeedsScreenshot   Code = "computer.needs_screenshot"
	CodeBadStep           Code = "computer.bad_step"
	CodeStopped           Code = "computer.stopped"
	CodeCaptureFailed     Code = "computer.capture_failed"
	CodeFailed            Code = "computer.failed"
)

// Failure is an operation that did not happen, and the host's account of why.
type Failure struct {
	Code   Code
	Detail string
	// BlockedBy is the modal that would have taken the input instead.
	BlockedBy *Modal
	// Candidates are the windows an ambiguous target could have meant.
	Candidates []Window
}

func (f *Failure) Error() string {
	var b strings.Builder
	b.WriteString(string(f.Code))
	if f.Detail != "" {
		b.WriteString(": " + f.Detail)
	}
	if f.BlockedBy != nil {
		b.WriteString("; held by " + f.BlockedBy.String())
	}
	for i, w := range f.Candidates {
		if i == maxCandidates {
			fmt.Fprintf(&b, "; and %d more", len(f.Candidates)-i)
			break
		}
		fmt.Fprintf(&b, "; window %d %s", w.ID, ShownName(w.Title))
		if i == 0 {
			b.WriteString(" (front)")
		}
	}
	return b.String()
}

// Is matches another Failure by code alone.
func (f *Failure) Is(target error) bool {
	other, ok := errors.AsType[*Failure](target)
	return ok && other.Code == f.Code
}

func fail(code Code, format string, args ...any) *Failure {
	return &Failure{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// CodeOf reports the failure code err carries, or "" when it carries none.
func CodeOf(err error) Code {
	if f, ok := errors.AsType[*Failure](err); ok {
		return f.Code
	}
	return ""
}
