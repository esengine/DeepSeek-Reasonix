package terminal

import (
	"errors"
	"fmt"
)

// Codes name why an operation failed; the detail beside one is for a reader.
const (
	CodeNotFound     = "terminal.not_found"
	CodeShellMissing = "terminal.shell_missing"
	CodeStartFailed  = "terminal.start_failed"
	CodeUnsupported  = "terminal.unsupported"
	CodeExited       = "terminal.exited"
	CodeLimit        = "terminal.limit"
)

// Error is a failure with an identity a caller can tell apart.
type Error struct {
	Code   string
	Detail string
}

func (e *Error) Error() string { return e.Code + ": " + e.Detail }

func fail(code, format string, args ...any) *Error {
	return &Error{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// CodeOf is the Code of err when it is a terminal failure, and "" otherwise.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
