package computer

import (
	"errors"
	"fmt"
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
	CodeAppRefused        Code = "computer.app_refused"
	CodeUnknownRef        Code = "computer.unknown_ref"
	CodeStaleRef          Code = "computer.stale_ref"
	CodeNoAction          Code = "computer.no_action"
	CodeNeedsFront        Code = "computer.needs_front"
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
}

func (f *Failure) Error() string {
	if f.Detail == "" {
		return string(f.Code)
	}
	return fmt.Sprintf("%s: %s", f.Code, f.Detail)
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
