package secrets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
)

type diagnosticError struct{ cause error }

func (e diagnosticError) Error() string {
	var safe interface{ DiagnosticFacts() string }
	if errors.As(e.cause, &safe) {
		return safe.DiagnosticFacts()
	}
	var endpoint *url.Error
	if errors.As(e.cause, &endpoint) {
		return fmt.Sprintf("request failed: %s: %s", RedactEndpoint(endpoint.URL), DiagnosticError(endpoint.Err))
	}
	for _, known := range []error{context.Canceled, context.DeadlineExceeded, io.EOF, io.ErrUnexpectedEOF} {
		if errors.Is(e.cause, known) {
			return known.Error()
		}
	}
	return fmt.Sprintf("dependency failure (%T)", e.cause)
}

func (e diagnosticError) Unwrap() error { return e.cause }

// DiagnosticError keeps identities while omitting untrusted error prose.
func DiagnosticError(err error) error {
	if err == nil {
		return nil
	}
	return diagnosticError{cause: err}
}

// OmittedText exposes only the byte count of unstructured dependency output.
func OmittedText(text string) string {
	if text == "" {
		return ""
	}
	return fmt.Sprintf("dependency output omitted (%d bytes)", len(text))
}
