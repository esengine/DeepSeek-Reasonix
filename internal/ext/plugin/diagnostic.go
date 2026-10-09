package plugin

import (
	"errors"
	"fmt"
	"io"

	"reasonix/internal/base/secrets"
)

var errOAuthTokenResponse = errors.New("MCP OAuth token response rejected")
var errOAuthAuthorizationResponse = errors.New("MCP OAuth authorization rejected")
var errOAuthRefreshMissing error = hostDiagnosticCause("MCP OAuth access token expired and no refresh token is available; authorize again")
var errOAuthRefreshInvalidated error = hostDiagnosticCause("MCP OAuth token refresh was invalidated while contacting the token endpoint; authorize again")

type omittedStderrWriter struct{ sink io.Writer }

type hostDiagnosticCause string

func (e hostDiagnosticCause) Error() string           { return string(e) }
func (e hostDiagnosticCause) DiagnosticFacts() string { return e.Error() }

type subprocessOutputError struct {
	cause error
	bytes int
}

func (e *subprocessOutputError) Error() string {
	return fmt.Sprintf("%s; subprocess output omitted (%d bytes)", secrets.DiagnosticError(e.cause), e.bytes)
}
func (e *subprocessOutputError) Unwrap() error           { return e.cause }
func (e *subprocessOutputError) DiagnosticFacts() string { return e.Error() }

type commandMissingError struct{ command string }

func (e *commandMissingError) Error() string {
	return fmt.Sprintf("command %q not found on PATH; use an absolute command path or set PATH in the MCP server env", secrets.RedactConfigValue("", e.command))
}
func (e *commandMissingError) DiagnosticFacts() string { return e.Error() }

type catalogPageError struct{ limit int }

type stdioReadFailure struct {
	method string
	cause  error
}

func (e *stdioReadFailure) Error() string {
	if e.method == "" {
		return fmt.Sprintf("MCP subprocess read failed: %s", secrets.DiagnosticError(e.cause))
	}
	return fmt.Sprintf("server exited while handling %s; the next call starts a fresh one: %s", secrets.RedactConfigValue("", e.method), secrets.DiagnosticError(e.cause))
}
func (e *stdioReadFailure) Unwrap() error           { return e.cause }
func (e *stdioReadFailure) DiagnosticFacts() string { return e.Error() }

func (e *catalogPageError) Error() string {
	if e.limit == 0 {
		return "MCP catalog returned a cursor twice"
	}
	return fmt.Sprintf("MCP catalog did not finish within %d pages", e.limit)
}
func (e *catalogPageError) DiagnosticFacts() string { return e.Error() }

func (w omittedStderrWriter) Write(p []byte) (int, error) {
	_, err := io.WriteString(w.sink, secrets.OmittedText(string(p))+"\n")
	if err != nil {
		return 0, err
	}
	return len(p), nil
}
