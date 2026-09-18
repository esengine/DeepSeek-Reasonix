package attachment

import (
	"errors"
	"fmt"
	"strings"
)

type Code string

const (
	CodeMissing     Code = "missing"
	CodeUnreadable  Code = "unreadable"
	CodeUnsafe      Code = "unsafe_path"
	CodeUnsupported Code = "unsupported_format"
	CodeCorrupt     Code = "corrupt"
	CodeSize        Code = "size_limit"
	CodeChanged     Code = "changed"
	CodeCanceled    Code = "canceled"
	CodeTooMany     Code = "too_many"
	CodeBatchSize   Code = "batch_size"
)

// Error is safe to return across UI/RPC: Name is a display name, not a host path.
type Error struct {
	Code     Code
	Name     string
	Index    int
	Retry    bool
	Cause    error
	Message  string
	Diagnose string
}

func (e Error) Error() string {
	name := strings.TrimSpace(e.Name)
	if name == "" {
		name = "image"
	}
	detail := strings.TrimSpace(e.Message)
	if detail == "" {
		detail = defaultDetail(e.Code)
	}
	if e.Index > 0 {
		return fmt.Sprintf("image attachment %d %q %s (%s)", e.Index, name, detail, e.Code)
	}
	return fmt.Sprintf("image attachment %q %s (%s)", name, detail, e.Code)
}

func (e Error) Unwrap() error { return e.Cause }

func (e Error) Retryable() bool { return e.Retry }

type BatchError []Error

func (e BatchError) Error() string {
	if len(e) == 0 {
		return "image attachments could not be read"
	}
	parts := make([]string, 0, len(e))
	for _, item := range e {
		parts = append(parts, item.Error())
	}
	return strings.Join(parts, "; ")
}

func Is(err error, code Code) bool {
	var item Error
	if errors.As(err, &item) {
		return item.Code == code
	}
	var batch BatchError
	if errors.As(err, &batch) {
		for _, item := range batch {
			if item.Code == code {
				return true
			}
		}
	}
	return false
}

func canceledError(err error) Error {
	return Error{Code: CodeCanceled, Message: "was canceled", Cause: err, Retry: true}
}

func defaultDetail(code Code) string {
	switch code {
	case CodeMissing:
		return "does not exist"
	case CodeUnreadable:
		return "could not be read"
	case CodeUnsafe:
		return "has an unsafe path"
	case CodeUnsupported:
		return "is not an image or uses an unsupported format"
	case CodeCorrupt:
		return "is damaged"
	case CodeSize:
		return "exceeds the allowed size"
	case CodeChanged:
		return "changed while it was being read"
	case CodeCanceled:
		return "was canceled"
	case CodeTooMany:
		return "exceeds the allowed count"
	case CodeBatchSize:
		return "exceeds the allowed batch size"
	default:
		return "could not be read"
	}
}
