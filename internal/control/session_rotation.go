package control

import "errors"

// IsSessionRotationBusy distinguishes transient session conflicts from
// failures so HTTP clients can receive a retryable 409.
func IsSessionRotationBusy(err error) bool {
	return errors.Is(err, errTurnRunningRotation) || errors.Is(err, errRotationInProgress)
}
