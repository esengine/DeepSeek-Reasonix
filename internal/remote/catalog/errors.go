package catalog

import (
	"errors"
	"fmt"

	"reasonix/internal/remote/protocol"
)

// Error is a transport-neutral catalog failure. The daemon maps Code to the
// frozen Remote error table; Detail is retained for Host logs and tests only
// and must not be copied into an RPC message.
type Error struct {
	Code   protocol.ReasonixErrorCode
	Detail error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Detail == nil {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %v", e.Code, e.Detail)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Detail
}

func catalogError(code protocol.ReasonixErrorCode, detail error) error {
	return &Error{Code: code, Detail: detail}
}

// ErrorCode extracts the frozen wire error code without coupling callers to
// filesystem or persistence implementation errors.
func ErrorCode(err error) (protocol.ReasonixErrorCode, bool) {
	var target *Error
	if !errors.As(err, &target) {
		return "", false
	}
	return target.Code, true
}
