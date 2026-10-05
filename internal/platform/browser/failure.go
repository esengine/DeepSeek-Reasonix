package browser

import (
	"errors"
	"fmt"
)

// Code is the identity of a browser failure. The model is shown it verbatim,
// and a caller tells failures apart by it rather than by the detail beside it.
type Code string

const (
	CodeEngineMissing     Code = "browser.engine_missing"
	CodeEngineFailed      Code = "browser.engine_failed"
	CodeProfileBusy       Code = "browser.profile_busy"
	CodeURLRefused        Code = "browser.url_refused"
	CodeNavigationFailed  Code = "browser.navigation_failed"
	CodeNavigationTimeout Code = "browser.navigation_timeout"
	CodeNoTab             Code = "browser.no_tab"
	CodeTabClosed         Code = "browser.tab_closed"
	CodeUnknownRef        Code = "browser.unknown_ref"
	CodeStaleRef          Code = "browser.stale_ref"
	CodeNotVisible        Code = "browser.not_visible"
	CodeCovered           Code = "browser.covered"
	CodeNotSelectable     Code = "browser.not_selectable"
	CodeNoSuchOption      Code = "browser.no_such_option"
	CodeDialogOpen        Code = "browser.dialog_open"
	CodeNoDialog          Code = "browser.no_dialog"
	CodeWaitTimeout       Code = "browser.wait_timeout"
	CodeBadStep           Code = "browser.bad_step"
	CodeScriptFailed      Code = "browser.script_failed"
	CodeOriginChanged     Code = "browser.origin_changed"
	CodeUnconfirmedSecret Code = "browser.unconfirmed_secret"
)

// Failure is a browser operation that did not happen, with the host's account
// of why. Ref names the element it concerns when there is one.
type Failure struct {
	Code   Code
	Detail string
	Ref    string
}

func (f *Failure) Error() string {
	if f.Detail == "" {
		return string(f.Code)
	}
	return fmt.Sprintf("%s: %s", f.Code, f.Detail)
}

// Is matches another Failure by code alone, so errors.Is(err, &Failure{Code: c})
// answers whether err is that kind of failure however it was wrapped.
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
