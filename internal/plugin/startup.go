package plugin

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/secrets"
)

// defaultStartupTimeout bounds the real initialize + tools/list handshake.
// The model-facing wait stays shorter so a slow but healthy server can finish
// in the background without holding an interactive turn open.
const defaultStartupTimeout = 30 * time.Second

// DefaultStartupTimeout is the default background handshake safety cap used by
// config and host adapters when no explicit server override is present.
func DefaultStartupTimeout() time.Duration { return defaultStartupTimeout }

// DefaultStartupWaitBudget is the maximum time one interactive tool call waits
// for a shared background handshake before asking the model to retry later.
func DefaultStartupWaitBudget() time.Duration { return defaultStartTimeout }

func (s Spec) startupTimeout() time.Duration {
	if s.StartupTimeout > 0 {
		return s.StartupTimeout
	}
	if s.DefaultStartupTimeout > 0 {
		return s.DefaultStartupTimeout
	}
	return defaultStartupTimeout
}

// ResolvedStartupTimeout returns the server override, global default, or the
// built-in background handshake cap, in that order.
func (s Spec) ResolvedStartupTimeout() time.Duration { return s.startupTimeout() }

type startupFailure struct {
	Stage   string
	Elapsed time.Duration
	Stderr  string
	Err     error
}

func (e *startupFailure) Error() string {
	if e == nil {
		return "MCP startup failed"
	}
	stage := strings.TrimSpace(e.Stage)
	if stage == "" {
		stage = "unknown"
	}
	msg := fmt.Sprintf("MCP startup %s failed after %s: %v", stage, formatElapsed(e.Elapsed), e.Err)
	if stderr := strings.TrimSpace(e.Stderr); stderr != "" {
		msg += "; stderr: " + stderr
	}
	return msg
}

func (e *startupFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newStartupFailure(stage string, started time.Time, stderr string, err error) error {
	if err == nil {
		return nil
	}
	var existing *startupFailure
	if errors.As(err, &existing) {
		return err
	}
	elapsed := max(time.Since(started), 0)
	return &startupFailure{
		Stage:   strings.TrimSpace(stage),
		Elapsed: elapsed,
		Stderr:  secrets.RedactCredentials(strings.TrimSpace(stderr)),
		Err:     err,
	}
}

func startupFailureDetails(err error) (stage string, elapsed time.Duration, stderr string) {
	var startupErr *startupFailure
	if !errors.As(err, &startupErr) || startupErr == nil {
		return "", 0, ""
	}
	return startupErr.Stage, startupErr.Elapsed, startupErr.Stderr
}

func formatElapsed(elapsed time.Duration) string {
	if elapsed < time.Millisecond {
		return elapsed.String()
	}
	return elapsed.Round(time.Millisecond).String()
}

type startupDiagnosticTransport interface {
	startupStderr() string
}

func (c *Client) startupStderr() string {
	if c == nil || c.t == nil {
		return ""
	}
	if diagnostic, ok := c.t.(startupDiagnosticTransport); ok {
		return diagnostic.startupStderr()
	}
	return ""
}
