package workspacelease

import (
	"errors"
	"sync"
)

var ErrClaimBusy = errors.New("workspace claim cannot be acquired immediately")
var ErrInvalidClaim = errors.New("workspace claim requires explicit resolvable paths")

// TryClaimPaths makes one non-blocking, independent compatibility claim.
// A successful caller owns the idempotent release, without an Owner run lifetime.
func TryClaimPaths(root, lockDir, sessionID string, paths []string) (func(), *ConflictError, error) {
	return claimAttempt(root, lockDir, sessionID, paths, true)
}

// ProbeClaimPaths checks current compatibility contention without retaining claims.
func ProbeClaimPaths(root, lockDir string, paths []string) (*ConflictError, error) {
	_, conflict, err := claimAttempt(root, lockDir, "", paths, false)
	return conflict, err
}

func claimAttempt(root, lockDir, sessionID string, paths []string, publish bool) (func(), *ConflictError, error) {
	if len(paths) == 0 {
		return nil, nil, ErrInvalidClaim
	}
	o, err := New(root, lockDir, nil)
	if err != nil {
		return nil, nil, err
	}
	o.scope.sessionID = func() string { return sessionID }
	normalized := o.normalizePaths(paths)
	if len(normalized) != len(paths) {
		return nil, nil, ErrInvalidClaim
	}
	release, conflict, err := o.tryPathSet(normalized, false, publish)
	if errors.Is(err, errHeld) {
		return nil, conflict, ErrClaimBusy
	}
	if err != nil || release == nil {
		return nil, conflict, err
	}
	var once sync.Once
	return func() { once.Do(release) }, nil, nil
}
