// failures.go — the record of an MCP server that was configured and could not
// be reached: what the host knows about why, and what it hands a status
// surface. Extracted whole from plugin.go, which is well past its ceiling.
package plugin

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Failure records one MCP server that was configured but could not connect.
type Failure struct {
	Name                   string
	Transport              string
	Error                  string
	Stage                  string
	Elapsed                time.Duration
	Stderr                 string
	HTTPStatus             int // what the endpoint answered; 0 when the failure was not one
	RequiresLaunchApproval bool
}

type launchApprovalError struct {
	server  string
	changed bool
}

func (e *launchApprovalError) Error() string {
	if e.changed {
		return fmt.Sprintf("project-provided MCP server %q changed; blocked before process or network startup and requires explicit re-authorization", e.server)
	}
	return fmt.Sprintf("project-provided MCP server %q is blocked before process or network startup until the user authorizes it", e.server)
}

func requiresLaunchApproval(err error) bool {
	var launchTarget *launchApprovalError
	return errors.As(err, &launchTarget)
}

// RecordFailure stores a failed MCP connection attempt for status UIs.
func (h *Host) RecordFailure(s Spec, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	tt := strings.ToLower(strings.TrimSpace(s.Type))
	if tt == "" {
		tt = "stdio"
	}
	stage, elapsed, stderr := startupFailureDetails(err)
	f := Failure{
		Name: s.Name, Transport: tt, Error: summarizeFailureError(err),
		Stage: stage, Elapsed: elapsed, Stderr: stderr, HTTPStatus: terminalHTTPStatus(err),
		RequiresLaunchApproval: requiresLaunchApproval(err),
	}
	for i := range h.failures {
		if h.failures[i].Name == s.Name {
			h.failures[i] = f
			return
		}
	}
	h.failures = append(h.failures, f)
}

// RecordLaunchApprovalRequired keeps an intentionally disconnected project MCP
// visible as awaiting authorization. This is used after an explicit launch
// revocation, where no failed connection attempt exists to create the status.
func (h *Host) RecordLaunchApprovalRequired(s Spec) {
	h.RecordFailure(s, &launchApprovalError{server: s.Name})
}

// ClearFailure drops a recorded startup/connection failure for status UIs.
func (h *Host) ClearFailure(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clearFailure(name)
}
