package workspacelease

import (
	"errors"
	"fmt"
)

const CodeWriteConflict = "workspace.write_conflict"

var ErrConflict = errors.New("workspace write claims conflict")

// ConflictError identifies the holder and conflicting extent; Cause preserves
// cancellation separately from the conflict that prevented admission.
type ConflictError struct {
	Holder         string   `json:"holder"`
	SessionID      string   `json:"sessionId"`
	Paths          []string `json:"paths"`
	RequestedPaths []string `json:"requestedPaths"`
	Cause          error    `json:"-"`
}

func (e *ConflictError) RefusalCode() string  { return CodeWriteConflict }
func (e *ConflictError) Unwrap() error        { return e.Cause }
func (e *ConflictError) Is(target error) bool { return target == ErrConflict }
func (e *ConflictError) Error() string {
	return fmt.Sprintf("session %q (%q) holds write claim %q; requested claim %q was not granted. End this turn before retrying a conflicting widening (refusal: %s)", e.Holder, e.SessionID, e.Paths, e.RequestedPaths, CodeWriteConflict)
}

func (o *Owner) extent(paths []string) []string {
	if len(paths) == 0 {
		return []string{o.scope.root}
	}
	return append([]string(nil), paths...)
}

func (o *Owner) conflict(holder, sessionID string, held, requested []string) *ConflictError {
	return &ConflictError{Holder: holder, SessionID: sessionID, Paths: o.extent(held), RequestedPaths: o.extent(requested), Cause: errHeld}
}

// SetSessionID binds the stable session identity independently of its name.
func (o *Owner) SetSessionID(id func() string) {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.scope.sessionID = id
	o.mu.Unlock()
}

func (o *Owner) sessionID() string {
	o.mu.Lock()
	id := o.scope.sessionID
	o.mu.Unlock()
	if id != nil {
		if identity := id(); identity != "" {
			return identity
		}
	}
	return o.scope.identity
}
