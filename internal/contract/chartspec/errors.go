package chartspec

import (
	"errors"
	"strings"
)

type Code string

const (
	CodeSpecVersionUnsupported Code = "chart.spec_version_unsupported"
	CodeSchemaInvalid          Code = "chart.schema_invalid"
	CodeLimitExceeded          Code = "chart.limit_exceeded"
	CodeColumnUnknown          Code = "chart.column_unknown"
	CodeTypeMismatch           Code = "chart.type_mismatch"
)

// Cap names which limit a CodeLimitExceeded refusal hit.
type Cap string

const (
	CapBytes   Cap = "bytes"
	CapRows    Cap = "rows"
	CapColumns Cap = "columns"
	CapSeries  Cap = "series"
	CapPoints  Cap = "points"
	CapMarks   Cap = "marks"
	CapLabel   Cap = "label"
)

var (
	ErrSpecVersionUnsupported = &Error{Code: CodeSpecVersionUnsupported}
	ErrSchemaInvalid          = &Error{Code: CodeSchemaInvalid}
	ErrLimitExceeded          = &Error{Code: CodeLimitExceeded}
	ErrColumnUnknown          = &Error{Code: CodeColumnUnknown}
	ErrTypeMismatch           = &Error{Code: CodeTypeMismatch}
)

// Error is a refusal with an identity. Path is a JSON pointer into the spec and
// Cap is set only for CodeLimitExceeded; neither is meant to be parsed from Error.
type Error struct {
	Code Code
	Path string
	Cap  Cap
}

func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString(string(e.Code))
	if e.Cap != "" {
		b.WriteString(" cap=" + string(e.Cap))
	}
	if e.Path != "" {
		b.WriteString(" at " + e.Path)
	}
	return b.String()
}

// Is matches a sentinel by code alone, so errors.Is(err, ErrLimitExceeded)
// holds whatever path or cap the refusal carries.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code && t.Path == "" && t.Cap == ""
}

// CodeOf reports the code of the first *Error in err's chain.
func CodeOf(err error) (Code, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e.Code, true
	}
	return "", false
}

func fail(code Code, path string) *Error { return &Error{Code: code, Path: path} }

func exceeded(c Cap, path string) *Error {
	return &Error{Code: CodeLimitExceeded, Path: path, Cap: c}
}
